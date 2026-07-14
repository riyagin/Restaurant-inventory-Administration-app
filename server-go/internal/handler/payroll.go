package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// PayrollHandler serves the HR payroll endpoints. All routes are wired behind
// RequireAdminOrManager. Closed/paid periods (and their lines) are immutable: every
// mutation guards with a 409 when the period is locked.
type PayrollHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPayrollHandler(pool *pgxpool.Pool, queries *db.Queries) *PayrollHandler {
	return &PayrollHandler{pool: pool, queries: queries}
}

// ── Periods ──────────────────────────────────────────────────────────────────

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

type createPeriodBody struct {
	PeriodMonth string `json:"period_month"` // "YYYY-MM" or "YYYY-MM-DD"
}

// CreatePeriod — POST /api/hr/payroll/periods
// Creates the period for a month (rejecting duplicates) and generates lines for all
// active employees in one transaction. Returns the period plus skipped-employee
// warnings.
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

	// Reject duplicate period_month.
	if _, err := h.queries.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: month, Valid: true}); err == nil {
		respondError(w, http.StatusConflict, "periode penggajian untuk bulan ini sudah ada")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa periode penggajian")
		return
	}

	start, end := service.PeriodBounds(month)

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

	respondJSON(w, http.StatusOK, map[string]any{
		"period":  period,
		"summary": summary,
	})
}

// ListLines — GET /api/hr/payroll/periods/:id/lines?q=&position_id=&branch_id=&sort=&order=
func (h *PayrollHandler) ListLines(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	q := r.URL.Query()

	params := &db.ListPayrollLinesForPeriodParams{
		PayrollPeriodID: pgtype.UUID{Bytes: id, Valid: true},
		Q:               strings.TrimSpace(q.Get("q")),
		Sort:            "name",
		Order:           "asc",
	}
	if s := strings.TrimSpace(q.Get("sort")); s == "net_pay" || s == "name" {
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

	rows, err := h.queries.ListPayrollLinesForPeriod(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	if rows == nil {
		rows = []*db.ListPayrollLinesForPeriodRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// ── Line review drawer data ──────────────────────────────────────────────────

// GetLineReview — GET /api/hr/payroll/lines/:id/review
// Returns the line, its component snapshots, attendance summary, performance score +
// violations, and the kasbon installments due in the period month.
func (h *PayrollHandler) GetLineReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	line, err := h.queries.GetPayrollLineByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	period, err := h.queries.GetPayrollPeriodByID(ctx, line.PayrollPeriodID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}

	components, _ := h.queries.ListPayrollLineComponents(ctx, pgID)
	if components == nil {
		components = []*db.PayrollLineComponent{}
	}

	monthDate := pgtype.Date{Time: period.PeriodMonth.Time, Valid: true}
	attendance, _ := h.queries.GetAttendanceSummaryForMonth(ctx, &db.GetAttendanceSummaryForMonthParams{
		EmployeeID: line.EmployeeID,
		Date:       period.StartDate,
		Date_2:     period.EndDate,
	})
	violations, _ := h.queries.ListViolationsForEmployeeMonth(ctx, &db.ListViolationsForEmployeeMonthParams{
		EmployeeID: line.EmployeeID,
		Date:       monthDate,
	})
	if violations == nil {
		violations = []*db.ListViolationsForEmployeeMonthRow{}
	}
	installments, _ := service.GetPendingInstallments(ctx, h.queries, line.EmployeeID, period.PeriodMonth.Time)
	if installments == nil {
		installments = []*db.KasbonInstallment{}
	}

	holidaysWorked, _ := h.queries.ListHolidaysWorked(ctx, &db.ListHolidaysWorkedParams{
		EmployeeID: line.EmployeeID,
		Date:       period.StartDate,
		Date_2:     period.EndDate,
	})
	if holidaysWorked == nil {
		holidaysWorked = []*db.PublicHoliday{}
	}

	overtimeRequests, _ := h.queries.ListOvertimeRequestsForEmployee(ctx, &db.ListOvertimeRequestsForEmployeeParams{
		EmployeeID: line.EmployeeID,
		Date:       period.StartDate,
		Date_2:     period.EndDate,
	})
	if overtimeRequests == nil {
		overtimeRequests = []*db.OvertimeRequest{}
	}

	mult, err := service.LoadMultipliers(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan lembur")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"line":              line,
		"components":        components,
		"attendance":        attendance,
		"violations":        violations,
		"installments":      installments,
		"holidays_worked":   holidaysWorked,
		"overtime_requests": overtimeRequests,
		"multipliers":       map[string]float64{"overtime": mult.Overtime, "holiday": mult.Holiday},
	})
}

// ── Review / unreview ────────────────────────────────────────────────────────

type reviewComponentInput struct {
	ID     string `json:"id"`     // payroll_line_components.id
	Amount int64  `json:"amount"` // adjusted amount
}

type reviewLineBody struct {
	OvertimeDays      float64                `json:"overtime_days"`
	OvertimeHours     float64                `json:"overtime_hours"`
	PublicHolidayDays float64                `json:"public_holiday_days"`
	Components        []reviewComponentInput `json:"components"` // adjusted bonus/allowance variable amounts
	ReviewNote        string                 `json:"review_note"`
}

// ReviewLine — POST /api/hr/payroll/lines/:id/review
func (h *PayrollHandler) ReviewLine(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body reviewLineBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.OvertimeDays < 0 || body.OvertimeHours < 0 || body.PublicHolidayDays < 0 {
		respondError(w, http.StatusBadRequest, "jumlah hari/jam tidak boleh negatif")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	line, err := h.queries.GetPayrollLineByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	if locked, msg := h.periodLocked(ctx, line.PayrollPeriodID); locked {
		respondError(w, http.StatusConflict, msg)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Persist adjusted component amounts and recompute the bonus/allowance totals from
	// the updated component snapshots (deduction-type components stay fixed-only, as
	// before — only bonus and allowance are reviewer-editable here).
	adjusted := map[string]int64{}
	for _, c := range body.Components {
		adjusted[c.ID] = c.Amount
	}
	components, err := qtx.ListPayrollLineComponents(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil komponen baris")
		return
	}
	var bonusTotal, allowanceTotal int64
	for _, c := range components {
		amt := c.Amount
		if v, ok := adjusted[c.ID.String()]; ok {
			amt = v
			if err := qtx.UpdatePayrollLineComponentAmount(ctx, &db.UpdatePayrollLineComponentAmountParams{
				Amount: v,
				ID:     c.ID,
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui komponen baris")
				return
			}
		}
		switch c.Type {
		case "bonus":
			bonusTotal += amt
		case "allowance":
			allowanceTotal += amt
		}
	}

	reviewedBy := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.ReviewNote); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := service.ReviewLine(ctx, qtx, line, service.ReviewLineInput{
		OvertimeDays:           body.OvertimeDays,
		OvertimeHours:          body.OvertimeHours,
		PublicHolidayDays:      body.PublicHolidayDays,
		AdjustedBonusTotal:     bonusTotal,
		AdjustedAllowanceTotal: allowanceTotal,
		ReviewNote:             note,
		ReviewedBy:             pgtype.UUID{Bytes: reviewedBy, Valid: reviewedBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mereview baris penggajian")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      reviewedBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_line",
		EntityID:    id,
		Description: "Mereview baris penggajian karyawan",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// UnreviewLine — POST /api/hr/payroll/lines/:id/unreview
func (h *PayrollHandler) UnreviewLine(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	line, err := h.queries.GetPayrollLineByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	if locked, msg := h.periodLocked(ctx, line.PayrollPeriodID); locked {
		respondError(w, http.StatusConflict, msg)
		return
	}

	updated, err := h.queries.UnreviewPayrollLine(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuka kembali review")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_line",
		EntityID:    id,
		Description: "Membuka kembali review baris penggajian",
	})

	respondJSON(w, http.StatusOK, updated)
}

// RegenerateLine — POST /api/hr/payroll/periods/:id/regenerate-line/:employeeId
// Re-snapshots a single employee's line (loses any prior review). Period must be open.
func (h *PayrollHandler) RegenerateLine(w http.ResponseWriter, r *http.Request) {
	periodID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID periode tidak valid")
		return
	}
	empID, err := parseUUID(chi.URLParam(r, "employeeId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
		return
	}
	ctx := r.Context()
	pgPeriodID := pgtype.UUID{Bytes: periodID, Valid: true}
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgPeriodID)
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

	existing, err := h.queries.GetPayrollLineByPeriodEmployee(ctx, &db.GetPayrollLineByPeriodEmployeeParams{
		PayrollPeriodID: pgPeriodID,
		EmployeeID:      pgEmpID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Delete the existing line (cascade removes its component snapshots) and rebuild
	// via a single-employee generation pass.
	if err := qtx.DeletePayrollLine(ctx, existing.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus baris lama")
		return
	}
	if err := h.regenerateSingle(ctx, qtx, period, pgEmpID); err != nil {
		if errors.Is(err, errNoWageStructure) {
			respondError(w, http.StatusBadRequest, "karyawan tidak memiliki struktur gaji aktif")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat ulang baris penggajian")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_line",
		EntityID:    empID,
		Description: "Membuat ulang baris penggajian karyawan",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	line, _ := h.queries.GetPayrollLineByPeriodEmployee(ctx, &db.GetPayrollLineByPeriodEmployeeParams{
		PayrollPeriodID: pgPeriodID,
		EmployeeID:      pgEmpID,
	})
	respondJSON(w, http.StatusOK, line)
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

	respondJSON(w, http.StatusOK, updated)
}

// DeletePeriod — DELETE /api/hr/payroll/periods/:id
// Only open (unfinished) periods may be deleted. Cascades to lines and components.
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

// ReviewAll — POST /api/hr/payroll/periods/{id}/review-all
// Marks every still-unreviewed line in an OPEN period as reviewed, keeping the
// generated amounts as-is (no recompute). This lets a trusted operator finish the
// review in one click without opening each line's breakdown. Individual lines can
// still be reopened/edited afterwards (period stays open until explicitly closed).
func (h *PayrollHandler) ReviewAll(w http.ResponseWriter, r *http.Request) {
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

	reviewedBy := middleware.UserIDFromCtx(ctx)
	reviewer := pgtype.UUID{Bytes: reviewedBy, Valid: reviewedBy != [16]byte{}}

	tag, err := h.pool.Exec(ctx,
		`UPDATE payroll_lines
		    SET reviewed = true, reviewed_by = $2, reviewed_at = now()
		  WHERE payroll_period_id = $1 AND reviewed = false`,
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
		EntityType:  "payroll_period",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai %d baris penggajian direview sekaligus (periode %s)", count, period.PeriodMonth.Time.Format("2006-01")),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":  "semua baris ditandai direview",
		"reviewed": count,
	})
}

// ── Bonus Distribution ───────────────────────────────────────────────────────

// BonusEligible — GET /api/hr/payroll/periods/:id/bonus-eligible?wage_component_id=...
// Returns all payroll lines in this period whose wage structure contains the given
// bonus component (identified by its wage_component catalog ID).
func (h *PayrollHandler) BonusEligible(w http.ResponseWriter, r *http.Request) {
	periodID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID periode tidak valid")
		return
	}
	wcID, err := parseUUID(r.URL.Query().Get("wage_component_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "wage_component_id tidak valid")
		return
	}
	ctx := r.Context()

	rows, err := h.queries.GetBonusEligibleLines(ctx, &db.GetBonusEligibleLinesParams{
		PayrollPeriodID: pgtype.UUID{Bytes: periodID, Valid: true},
		WageComponentID: pgtype.UUID{Bytes: wcID, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar karyawan eligible")
		return
	}
	if rows == nil {
		rows = []*db.GetBonusEligibleLinesRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

type applyBonusBody struct {
	WageComponentID   string   `json:"wage_component_id"`
	AmountPerEmployee int64    `json:"amount_per_employee"` // cents
	LineComponentIDs  []string `json:"line_component_ids"`  // payroll_line_components.id
}

// ApplyBonus — POST /api/hr/payroll/periods/:id/apply-bonus
// Adds amount_per_employee to each listed line component's amount, then recomputes
// bonus_total / gross_pay / net_pay for every affected line. Period must be open.
func (h *PayrollHandler) ApplyBonus(w http.ResponseWriter, r *http.Request) {
	periodID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID periode tidak valid")
		return
	}
	var body applyBonusBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.AmountPerEmployee <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah per karyawan harus lebih dari 0")
		return
	}
	if len(body.LineComponentIDs) == 0 {
		respondError(w, http.StatusBadRequest, "pilih minimal satu karyawan")
		return
	}

	ctx := r.Context()
	pgPeriodID := pgtype.UUID{Bytes: periodID, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgPeriodID)
	if err != nil {
		respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
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

	// Fetch all eligible rows for this period+component to build a lookup map
	// from line_component_id → (line_id, current_amount).
	wcID, err := parseUUID(body.WageComponentID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "wage_component_id tidak valid")
		return
	}
	eligible, err := qtx.GetBonusEligibleLines(ctx, &db.GetBonusEligibleLinesParams{
		PayrollPeriodID: pgPeriodID,
		WageComponentID: pgtype.UUID{Bytes: wcID, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar eligible")
		return
	}

	// Build lookup: line_component_id → line_id.
	type eligibleEntry struct {
		lineID        pgtype.UUID
		currentAmount int64
	}
	lcToLine := map[string]eligibleEntry{}
	for _, row := range eligible {
		lcToLine[row.LineComponentID.String()] = eligibleEntry{
			lineID:        row.LineID,
			currentAmount: row.CurrentAmount,
		}
	}

	selectedLineIDs := map[string]pgtype.UUID{}

	for _, lcIDStr := range body.LineComponentIDs {
		entry, ok := lcToLine[lcIDStr]
		if !ok {
			respondError(w, http.StatusBadRequest, "komponen tidak ditemukan dalam periode ini: "+lcIDStr)
			return
		}
		lcID, _ := parseUUID(lcIDStr)
		pgLCID := pgtype.UUID{Bytes: lcID, Valid: true}

		newAmount := entry.currentAmount + body.AmountPerEmployee
		if err := qtx.UpdatePayrollLineComponentAmount(ctx, &db.UpdatePayrollLineComponentAmountParams{
			Amount: newAmount,
			ID:     pgLCID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui komponen bonus")
			return
		}
		selectedLineIDs[entry.lineID.String()] = entry.lineID
	}

	// Recompute bonus_total / gross_pay / net_pay for each affected line.
	for _, lineID := range selectedLineIDs {
		line, err := qtx.GetPayrollLineByID(ctx, lineID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
			return
		}
		comps, err := qtx.ListPayrollLineComponents(ctx, lineID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil komponen baris")
			return
		}
		var bonusTotal int64
		for _, c := range comps {
			if c.Type == "bonus" {
				bonusTotal += c.Amount
			}
		}
		grossPay := line.BaseSalary + line.AllowanceTotal + bonusTotal +
			line.OvertimeAmount + line.OvertimeHourlyAmount + line.PublicHolidayAmount
		netPay := grossPay - line.ComponentDeductionTotal - line.KasbonDeduction - line.UnpaidLeaveDeduction
		if err := qtx.UpdatePayrollLineTotals(ctx, &db.UpdatePayrollLineTotalsParams{
			BonusTotal: bonusTotal,
			GrossPay:   grossPay,
			NetPay:     netPay,
			ID:         lineID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui total baris")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "payroll_period",
		EntityID:    periodID,
		Description: fmt.Sprintf("Distribusi bonus ke %d karyawan pada periode %s", len(body.LineComponentIDs), period.PeriodMonth.Time.Format("2006-01")),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"updated": len(body.LineComponentIDs),
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// periodLocked reports whether the period of the given id is closed/paid (immutable).
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

// errNoWageStructure signals a single-line regeneration found no applicable wage.
var errNoWageStructure = errors.New("no wage structure")

// regenerateSingle rebuilds one employee's payroll line (mirrors GenerateLines for a
// single employee). Returns errNoWageStructure when the employee has no applicable
// wage structure on the period end date.
func (h *PayrollHandler) regenerateSingle(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod, empID pgtype.UUID) error {
	mult, err := service.LoadMultipliers(ctx, qtx)
	if err != nil {
		return err
	}
	start := period.StartDate.Time
	end := period.EndDate.Time
	periodMonth := period.PeriodMonth.Time

	ws, err := service.GetCurrentWage(ctx, qtx, empID, end)
	if err != nil {
		return err
	}
	if ws == nil {
		return errNoWageStructure
	}

	emp, err := qtx.GetEmployeeByID(ctx, empID)
	if err != nil {
		return err
	}
	var sched service.Schedule
	if branchWS, err := qtx.GetWorkScheduleByBranch(ctx, emp.BranchID); err == nil && branchWS != nil {
		sched = service.ScheduleFromRow(branchWS)
	} else {
		sched = service.DefaultSchedule()
	}
	hourlyRate := service.HourlyRateFromDaily(ws.DailyRate, sched)

	components, err := qtx.ListEmployeeWageComponents(ctx, ws.ID)
	if err != nil {
		return err
	}
	var allowanceTotal, bonusTotal, deductionTotal int64
	for _, c := range components {
		switch c.ComponentType {
		case "allowance":
			allowanceTotal += c.Amount
		case "bonus":
			bonusTotal += c.Amount
		case "deduction":
			deductionTotal += c.Amount
		}
	}

	holidayCount, err := qtx.CountPresentOnHolidays(ctx, &db.CountPresentOnHolidaysParams{
		EmployeeID: empID,
		Date:       pgtype.Date{Time: start, Valid: true},
		Date_2:     pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return err
	}

	// Seed overtime_hours from formal overtime requests logged for this period.
	overtimeHours, err := qtx.SumOvertimeHoursForEmployee(ctx, &db.SumOvertimeHoursForEmployeeParams{
		EmployeeID: empID,
		Date:       pgtype.Date{Time: start, Valid: true},
		Date_2:     pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return err
	}

	installments, err := service.GetPendingInstallments(ctx, qtx, empID, periodMonth)
	if err != nil {
		return err
	}
	var kasbonDeduction int64
	for _, ins := range installments {
		kasbonDeduction += ins.Amount
	}

	unpaidDays, err := service.GetUnpaidLeaveDays(ctx, qtx, empID, start, end)
	if err != nil {
		return err
	}
	unpaidDeduction := int64(unpaidDays) * ws.DailyRate

	var perfScore pgtype.Int4
	if score, serr := qtx.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
		EmployeeID:  empID,
		PeriodMonth: pgtype.Date{Time: periodMonth, Valid: true},
	}); serr == nil && score != nil {
		perfScore = pgtype.Int4{Int32: score.Score, Valid: true}
	}

	calc := service.CalcLine(service.CalcLineInput{
		BaseSalary:              ws.BaseSalary,
		DailyRate:               ws.DailyRate,
		OvertimeDays:            0,
		OvertimeHours:           overtimeHours,
		OvertimeHourlyRate:      hourlyRate,
		PublicHolidayDays:       float64(holidayCount),
		OvertimeMultiplier:      mult.Overtime,
		HolidayMultiplier:       mult.Holiday,
		AllowanceTotal:          allowanceTotal,
		BonusTotal:              bonusTotal,
		ComponentDeductionTotal: deductionTotal,
		KasbonDeduction:         kasbonDeduction,
		UnpaidLeaveDeduction:    unpaidDeduction,
	})

	line, err := qtx.CreatePayrollLine(ctx, &db.CreatePayrollLineParams{
		PayrollPeriodID:         period.ID,
		EmployeeID:              empID,
		WageStructureID:         ws.ID,
		BaseSalary:              ws.BaseSalary,
		DailyRate:               ws.DailyRate,
		OvertimeDays:            service.NumericFromFloat(0),
		PublicHolidayDays:       service.NumericFromFloat(float64(holidayCount)),
		OvertimeAmount:          calc.OvertimeAmount,
		PublicHolidayAmount:     calc.PublicHolidayAmount,
		AllowanceTotal:          allowanceTotal,
		BonusTotal:              bonusTotal,
		ComponentDeductionTotal: deductionTotal,
		KasbonDeduction:         kasbonDeduction,
		UnpaidLeaveDays:         int32(unpaidDays),
		UnpaidLeaveDeduction:    unpaidDeduction,
		GrossPay:                calc.GrossPay,
		NetPay:                  calc.NetPay,
		PerformanceScore:        perfScore,
		OvertimeHours:           service.NumericFromFloat(overtimeHours),
		OvertimeHourlyRate:      hourlyRate,
		OvertimeHourlyAmount:    calc.OvertimeHourlyAmount,
	})
	if err != nil {
		return err
	}
	for _, c := range components {
		if _, err := qtx.CreatePayrollLineComponent(ctx, &db.CreatePayrollLineComponentParams{
			PayrollLineID:   line.ID,
			WageComponentID: c.WageComponentID,
			Name:            c.ComponentName,
			Type:            c.ComponentType,
			Amount:          c.Amount,
		}); err != nil {
			return err
		}
	}
	return nil
}
