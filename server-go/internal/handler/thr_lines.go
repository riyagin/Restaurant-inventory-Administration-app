package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ReviewAll — POST /api/hr/thr/runs/:id/review-all
// Marks every still-unreviewed line in an OPEN run as reviewed, keeping the computed
// amounts as-is. Lines can still be reopened while the run is open.
func (h *ThrHandler) ReviewAll(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	run, err := h.queries.GetThrRunByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "run THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil run THR")
		return
	}
	if run.Status != "open" {
		respondError(w, http.StatusConflict, service.ErrThrRunLocked.Error())
		return
	}

	reviewedBy := middleware.UserIDFromCtx(ctx)
	reviewer := pgtype.UUID{Bytes: reviewedBy, Valid: reviewedBy != [16]byte{}}

	tag, err := h.pool.Exec(ctx,
		`UPDATE thr_lines
		    SET reviewed = true, reviewed_by = $2, reviewed_at = now()
		  WHERE thr_run_id = $1 AND reviewed = false`,
		pgID, reviewer,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menandai semua baris direview")
		return
	}
	count := tag.RowsAffected()

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      reviewedBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_run",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai %d baris THR direview sekaligus (%s)", count, run.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":  "semua baris ditandai direview",
		"reviewed": count,
	})
}

// GetLineReview — GET /api/hr/thr/lines/:id/review (single line, fresh)
func (h *ThrHandler) GetLineReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	line, err := h.queries.GetThrLineByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"line": line})
}

type reviewThrLineBody struct {
	ThrAmount  int64  `json:"thr_amount"`
	ReviewNote string `json:"review_note"`
}

// ReviewLine — POST /api/hr/thr/lines/:id/review
func (h *ThrHandler) ReviewLine(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body reviewThrLineBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.ThrAmount < 0 {
		respondError(w, http.StatusBadRequest, "nominal THR tidak boleh negatif")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	line, err := h.queries.GetThrLineByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	if locked, msg := h.runLocked(ctx, line.ThrRunID); locked {
		respondError(w, http.StatusConflict, msg)
		return
	}

	reviewedBy := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.ReviewNote); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := h.queries.UpdateThrLineReview(ctx, &db.UpdateThrLineReviewParams{
		ThrAmount:  body.ThrAmount,
		ReviewedBy: pgtype.UUID{Bytes: reviewedBy, Valid: reviewedBy != [16]byte{}},
		ReviewNote: note,
		ID:         pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mereview baris THR")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      reviewedBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_line",
		EntityID:    id,
		Description: "Mereview baris THR karyawan",
	})

	respondJSON(w, http.StatusOK, updated)
}

// UnreviewLine — POST /api/hr/thr/lines/:id/unreview
func (h *ThrHandler) UnreviewLine(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	line, err := h.queries.GetThrLineByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	if locked, msg := h.runLocked(ctx, line.ThrRunID); locked {
		respondError(w, http.StatusConflict, msg)
		return
	}

	updated, err := h.queries.UnreviewThrLine(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuka kembali review")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_line",
		EntityID:    id,
		Description: "Membuka kembali review baris THR",
	})

	respondJSON(w, http.StatusOK, updated)
}

// RegenerateLine — POST /api/hr/thr/runs/:id/regenerate-line/:employeeId
// Re-snapshots a single employee's THR line from their current wage + tenure (loses
// any prior review/adjustment). Run must be open.
func (h *ThrHandler) RegenerateLine(w http.ResponseWriter, r *http.Request) {
	runID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID run tidak valid")
		return
	}
	empID, err := parseUUID(chi.URLParam(r, "employeeId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
		return
	}
	ctx := r.Context()
	pgRunID := pgtype.UUID{Bytes: runID, Valid: true}
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}

	run, err := h.queries.GetThrRunByID(ctx, pgRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "run THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil run THR")
		return
	}
	if run.Status != "open" {
		respondError(w, http.StatusConflict, service.ErrThrRunLocked.Error())
		return
	}

	existing, err := h.queries.GetThrLineByRunEmployee(ctx, &db.GetThrLineByRunEmployeeParams{
		ThrRunID:   pgRunID,
		EmployeeID: pgEmpID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}

	ws, err := service.GetCurrentWage(ctx, h.queries, pgEmpID, run.PaymentDate.Time)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil struktur gaji")
		return
	}
	if ws == nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak memiliki struktur gaji aktif")
		return
	}
	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.EmploymentType == service.EmploymentTypeContract {
		respondError(w, http.StatusBadRequest, "karyawan kontrak tidak berhak menerima THR")
		return
	}
	start := service.ThrTenureStart(emp.JoinDate, emp.PermanentSince)
	ent := service.ComputeThrEntitlement(ws.BaseSalary, start, run.PaymentDate.Time)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.DeleteThrLine(ctx, existing.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus baris lama")
		return
	}
	line, err := qtx.CreateThrLine(ctx, &db.CreateThrLineParams{
		ThrRunID:        pgRunID,
		EmployeeID:      pgEmpID,
		WageStructureID: ws.ID,
		BaseSalary:      ws.BaseSalary,
		JoinDate:        emp.JoinDate,
		MonthsWorked:    ent.MonthsWorked,
		ThrRatio:        service.NumericFromFloat(ent.Ratio),
		ComputedAmount:  ent.Amount,
		ThrAmount:       ent.Amount,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat ulang baris THR")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_line",
		EntityID:    empID,
		Description: "Membuat ulang baris THR karyawan",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, line)
}
