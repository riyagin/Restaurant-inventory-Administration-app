package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ListPeriods — GET /api/hr/payroll/periods
func (h *PayrollHandler) ListPeriods(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListPayrollPeriods(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if rows == nil {
		rows = []*db.ListPayrollPeriodsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// CreatePeriod — POST /api/hr/payroll/periods (also generates every active employee's line)
func (h *PayrollHandler) CreatePeriod(w http.ResponseWriter, r *http.Request) {
	var body createPeriodBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	month, err := parseMonth(body.PeriodMonth)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan periode tidak valid")
		return
	}

	ctx := r.Context()

	if _, err := h.queries.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: month, Valid: true}); err == nil {
		respondError(w, http.StatusConflict, "periode penggajian untuk bulan ini sudah ada")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa periode penggajian")
		return
	}

	// Cut-off window (e.g. 26 -> 25), never overlapping the previous period.
	start, end, err := service.ResolvePayrollPeriodBounds(ctx, h.queries, month)
	if err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	createdBy := middleware.UserIDFromCtx(ctx)
	period, err := qtx.CreatePayrollPeriod(ctx, &db.CreatePayrollPeriodParams{
		PeriodMonth: pgtype.Date{Time: month, Valid: true},
		StartDate:   pgtype.Date{Time: start, Valid: true},
		EndDate:     pgtype.Date{Time: end, Valid: true},
		CreatedBy:   pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat periode penggajian")
		return
	}

	genRes, err := service.GenerateLines(ctx, qtx, period)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat baris penggajian")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "payroll_period",
		EntityID:    period.ID.Bytes,
		Description: fmt.Sprintf("Membuat periode penggajian %s (%d baris)", month.Format("2006-01"), genRes.Created),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"period":  period,
		"created": genRes.Created,
		"skipped": genRes.SkippedNames,
	})
}

// GetPeriod — GET /api/hr/payroll/periods/:id (summary totals)
func (h *PayrollHandler) GetPeriod(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	summary, err := h.queries.GetPayrollPeriodSummary(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan penggajian")
		return
	}

	// No posting row: still open, or closed before automatic posting existed.
	var posting any
	if p, perr := h.queries.GetPayrollPosting(ctx, pgID); perr == nil {
		posting = p
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"period":  period,
		"summary": summary,
		"posting": posting,
	})
}

// ClosePeriod — POST /api/hr/payroll/periods/:id/close
func (h *PayrollHandler) ClosePeriod(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if period.Status != "open" {
		respondError(w, http.StatusConflict, service.ErrPeriodLocked.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	updated, err := service.ClosePeriod(ctx, qtx, period)
	if err != nil {
		if errors.Is(err, service.ErrLinesNotReviewed) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menutup periode penggajian")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_period",
		EntityID:    id,
		Description: fmt.Sprintf("Menutup periode penggajian %s", period.PeriodMonth.Time.Format("2006-01")),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	// The queue row is committed; post now, and the sweep retries if this attempt dies.
	h.poster.Enqueue(pgID, middleware.UserIDFromCtx(ctx))

	respondJSON(w, http.StatusOK, updated)
}

// RetryPosting — POST /api/hr/payroll/periods/:id/post-accounting
func (h *PayrollHandler) RetryPosting(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if period.Status == "open" {
		respondError(w, http.StatusConflict, "periode belum ditutup, belum ada yang perlu dicatat")
		return
	}

	// No queue row: posted by hand before automatic posting; queueing it would post twice.
	if _, err := h.queries.GetPayrollPosting(ctx, pgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusConflict,
				"periode ini ditutup sebelum pencatatan otomatis ada dan sudah dicatat manual")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil status pencatatan")
		return
	}

	if err := service.QueuePayrollPosting(ctx, h.queries, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menjadwalkan pencatatan akuntansi")
		return
	}

	if err := h.poster.PostOne(ctx, pgID, middleware.UserIDFromCtx(ctx)); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("gagal mencatat ke akuntansi: %v", err))
		return
	}

	posting, err := h.queries.GetPayrollPosting(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil status pencatatan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_period",
		EntityID:    id,
		Description: fmt.Sprintf("Mencatat penggajian %s ke buku besar", period.PeriodMonth.Time.Format("2006-01")),
	})

	respondJSON(w, http.StatusOK, posting)
}

// DeletePeriod — DELETE /api/hr/payroll/periods/:id (open periods only)
func (h *PayrollHandler) DeletePeriod(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if period.Status != "open" {
		respondError(w, http.StatusConflict, "hanya periode yang belum ditutup yang dapat dihapus")
		return
	}

	if err := h.queries.DeletePayrollPeriod(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus periode penggajian")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "payroll_period",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus periode penggajian %s", period.PeriodMonth.Time.Format("2006-01")),
	})

	w.WriteHeader(http.StatusNoContent)
}

// MarkPaid — POST /api/hr/payroll/periods/:id/mark-paid
func (h *PayrollHandler) MarkPaid(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if period.Status != "closed" {
		respondError(w, http.StatusConflict, "hanya periode yang sudah ditutup yang dapat ditandai dibayar")
		return
	}

	updated, err := h.queries.MarkPayrollPeriodPaid(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menandai periode dibayar")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_period",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai periode penggajian %s dibayar", period.PeriodMonth.Time.Format("2006-01")),
	})

	respondJSON(w, http.StatusOK, updated)
}

func (h *PayrollHandler) periodLocked(ctx context.Context, periodID pgtype.UUID) (bool, string) {
	period, err := h.queries.GetPayrollPeriodByID(ctx, periodID)
	if err != nil {
		return false, ""
	}
	if period.Status != "open" {
		return true, service.ErrPeriodLocked.Error()
	}
	return false, ""
}
