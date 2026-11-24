package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ThrHandler mirrors payroll: lines are reviewed before close; closed/paid runs are immutable.
type ThrHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewThrHandler(pool *pgxpool.Pool, queries *db.Queries) *ThrHandler {
	return &ThrHandler{pool: pool, queries: queries}
}

// SetUploadsDir injects the uploads directory (for the payslip logo).
func (h *ThrHandler) SetUploadsDir(dir string) { h.uploadsDir = dir }

func (h *ThrHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// ListRuns — GET /api/hr/thr/runs
func (h *ThrHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListThrRuns(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar THR")
		return
	}
	if rows == nil {
		rows = []*db.ListThrRunsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

type createThrRunBody struct {
	Name        string `json:"name"`
	PaymentDate string `json:"payment_date"` // "YYYY-MM-DD"
}

// CreateRun — POST /api/hr/thr/runs (also generates every active employee's line)
func (h *ThrHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	var body createThrRunBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama run THR wajib diisi")
		return
	}
	payDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.PaymentDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal pembayaran tidak valid")
		return
	}

	ctx := r.Context()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	createdBy := middleware.UserIDFromCtx(ctx)
	run, err := qtx.CreateThrRun(ctx, &db.CreateThrRunParams{
		Name:        body.Name,
		PaymentDate: pgtype.Date{Time: payDate, Valid: true},
		CreatedBy:   pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat run THR")
		return
	}

	genRes, err := service.GenerateThrLines(ctx, qtx, run)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat baris THR")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "thr_run",
		EntityID:    run.ID.Bytes,
		Description: fmt.Sprintf("Membuat run THR %s (%d baris)", run.Name, genRes.Created),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"run":      run,
		"created":  genRes.Created,
		"skipped":  genRes.SkippedNames,
		"contract": genRes.ContractNames,
	})
}

// GetRun — GET /api/hr/thr/runs/:id (run + summary totals)
func (h *ThrHandler) GetRun(w http.ResponseWriter, r *http.Request) {
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
	summary, err := h.queries.GetThrRunSummary(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan THR")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"run":     run,
		"summary": summary,
	})
}

// ListLines — GET /api/hr/thr/runs/:id/lines?q=&position_id=&branch_id=&sort=&order=
func (h *ThrHandler) ListLines(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	q := r.URL.Query()

	params := &db.ListThrLinesForRunParams{
		ThrRunID: pgtype.UUID{Bytes: id, Valid: true},
		Q:        strings.TrimSpace(q.Get("q")),
		Sort:     "name",
		Order:    "asc",
	}
	if s := strings.TrimSpace(q.Get("sort")); s == "thr_amount" || s == "name" {
		params.Sort = s
	}
	if o := strings.TrimSpace(q.Get("order")); o == "asc" || o == "desc" {
		params.Order = o
	}
	if v := strings.TrimSpace(q.Get("position_id")); v != "" {
		pid, perr := parseUUID(v)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "position_id tidak valid")
			return
		}
		params.PositionID = pgtype.UUID{Bytes: pid, Valid: true}
	}
	if v := strings.TrimSpace(q.Get("branch_id")); v != "" {
		bid, berr := parseUUID(v)
		if berr != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params.BranchID = pgtype.UUID{Bytes: bid, Valid: true}
	}

	rows, err := h.queries.ListThrLinesForRun(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	if rows == nil {
		rows = []*db.ListThrLinesForRunRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// DeleteRun — DELETE /api/hr/thr/runs/:id (only open runs, cascades to lines)
func (h *ThrHandler) DeleteRun(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusConflict, "hanya run yang belum ditutup yang dapat dihapus")
		return
	}

	if err := h.queries.DeleteThrRun(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus run THR")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "thr_run",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus run THR %s", run.Name),
	})

	w.WriteHeader(http.StatusNoContent)
}

// CloseRun — POST /api/hr/thr/runs/:id/close
func (h *ThrHandler) CloseRun(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	updated, err := service.CloseThrRun(ctx, qtx, run)
	if err != nil {
		if errors.Is(err, service.ErrThrLinesNotReviewed) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menutup run THR")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_run",
		EntityID:    id,
		Description: fmt.Sprintf("Menutup run THR %s", run.Name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// MarkPaid — POST /api/hr/thr/runs/:id/mark-paid
func (h *ThrHandler) MarkPaid(w http.ResponseWriter, r *http.Request) {
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
	if run.Status != "closed" {
		respondError(w, http.StatusConflict, "hanya run yang sudah ditutup yang dapat ditandai dibayar")
		return
	}

	updated, err := h.queries.MarkThrRunPaid(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menandai run dibayar")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "thr_run",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai run THR %s dibayar", run.Name),
	})

	respondJSON(w, http.StatusOK, updated)
}

// runLocked reports whether the run of the given id is closed/paid (immutable).
func (h *ThrHandler) runLocked(ctx context.Context, runID pgtype.UUID) (bool, string) {
	run, err := h.queries.GetThrRunByID(ctx, runID)
	if err != nil {
		return false, ""
	}
	if run.Status != "open" {
		return true, service.ErrThrRunLocked.Error()
	}
	return false, ""
}
