package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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

// ThrHandler serves the HR THR (Tunjangan Hari Raya) endpoints. It mirrors the
// payroll handler: a run holds one snapshot line per active employee, lines must be
// reviewed before the run can be closed, closing posts the total THR expense per
// branch, and closed/paid runs are immutable. Payslip rendering reuses the shared
// BuildPayslipPDF with a "SLIP THR" title.
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

// ── Runs ─────────────────────────────────────────────────────────────────────

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

// CreateRun — POST /api/hr/thr/runs
// Creates the run and generates a THR line for every active employee in one
// transaction. Returns the run plus skipped-employee warnings.
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

// ── Payslip rendering ──────────────────────────────────────────────────────────

// buildThrPayslipData assembles the DB-free PayslipData for a single THR line.
func (h *ThrHandler) buildThrPayslipData(ctx context.Context, lineID pgtype.UUID) (service.PayslipData, *db.GetThrLineForPayslipRow, error) {
	var empty service.PayslipData

	line, err := h.queries.GetThrLineForPayslip(ctx, lineID)
	if err != nil {
		return empty, nil, err
	}
	settings, err := h.queries.GetHRSettings(ctx)
	if err != nil {
		return empty, line, err
	}

	logoPath := ""
	if settings.LogoPath.Valid && strings.TrimSpace(settings.LogoPath.String) != "" {
		p := filepath.Join(h.resolveUploadsDir(), settings.LogoPath.String)
		if _, statErr := os.Stat(p); statErr == nil {
			logoPath = p
		}
	}

	note := fmt.Sprintf("Masa kerja: %d bulan (%s × gaji pokok %s).",
		line.MonthsWorked, ratioLabel(line.MonthsWorked), formatRupiahShort(line.BaseSalary))
	if line.ReviewNote.Valid && strings.TrimSpace(line.ReviewNote.String) != "" {
		note += " " + line.ReviewNote.String
	}

	data := service.PayslipData{
		CompanyName:    settings.CompanyName,
		Address:        settings.Address,
		LogoPath:       logoPath,
		PayslipFooter:  settings.PayslipFooter,
		Title:          "SLIP THR",
		EmployeeName:   line.EmployeeName,
		EmployeeCode:   line.EmployeeCode,
		Position:       textValue(line.PositionName),
		Branch:         textValue(line.BranchName),
		JoinDate:       dateLabelID(line.JoinDate.Time),
		PeriodLabel:    line.RunName,
		Earnings:       []service.PayslipLineItem{{Label: "THR (Tunjangan Hari Raya)", Amount: line.ThrAmount}},
		Deductions:     nil,
		TotalEarnings:  line.ThrAmount,
		TotalDeduction: 0,
		NetPay:         line.ThrAmount,
		Note:           note,
	}
	return data, line, nil
}

// ratioLabel renders the THR proportion as "n/12" (or "1 bulan penuh" at ≥12 months).
func ratioLabel(months int32) string {
	if months >= 12 {
		return "1 bulan penuh"
	}
	return fmt.Sprintf("%d/12", months)
}

// formatRupiahShort renders a whole-rupiah amount id-ID style (e.g. "Rp 1.500.000").
func formatRupiahShort(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp " + b.String()
	if neg {
		out = "-" + out
	}
	return out
}

// DownloadPayslip — GET /api/hr/thr/lines/:id/payslip
func (h *ThrHandler) DownloadPayslip(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	data, line, err := h.buildThrPayslipData(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan data slip THR")
		return
	}
	if line.RunStatus == "open" {
		respondError(w, http.StatusConflict, "slip THR hanya tersedia untuk run yang sudah ditutup")
		return
	}

	pdfBytes, err := service.BuildPayslipPDF(data)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat PDF slip THR")
		return
	}

	filename := fmt.Sprintf("slip-thr-%s-%s.pdf", line.EmployeeCode, line.PaymentDate.Time.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write(pdfBytes)
}

// DownloadRunPayslips — GET /api/hr/thr/runs/:id/payslips (ZIP of all line PDFs)
func (h *ThrHandler) DownloadRunPayslips(w http.ResponseWriter, r *http.Request) {
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
	if run.Status == "open" {
		respondError(w, http.StatusConflict, "slip THR hanya tersedia untuk run yang sudah ditutup")
		return
	}

	lines, err := h.queries.ListThrLinesForRun(ctx, &db.ListThrLinesForRunParams{
		ThrRunID: pgID,
		Q:        "",
		Sort:     "name",
		Order:    "asc",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	if len(lines) == 0 {
		respondError(w, http.StatusNotFound, "tidak ada baris THR pada run ini")
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dateStr := run.PaymentDate.Time.Format("2006-01-02")
	for _, l := range lines {
		data, lineRow, derr := h.buildThrPayslipData(ctx, l.ID)
		if derr != nil {
			continue
		}
		pdfBytes, perr := service.BuildPayslipPDF(data)
		if perr != nil {
			continue
		}
		entryName := fmt.Sprintf("slip-thr-%s-%s.pdf", lineRow.EmployeeCode, dateStr)
		fwz, werr := zw.Create(entryName)
		if werr != nil {
			continue
		}
		_, _ = io.Copy(fwz, bytes.NewReader(pdfBytes))
	}
	if err := zw.Close(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat arsip ZIP")
		return
	}

	zipName := fmt.Sprintf("slip-thr-%s.zip", dateStr)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	_, _ = w.Write(buf.Bytes())
}

// ── helpers ──────────────────────────────────────────────────────────────────

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
