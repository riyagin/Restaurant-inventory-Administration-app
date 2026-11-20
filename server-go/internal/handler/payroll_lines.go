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

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

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
	grades, err := h.queries.ListPayrollLineGradesForPeriod(ctx, params.PayrollPeriodID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil grade penggajian")
		return
	}
	byLine := make(map[[16]byte]*db.ListPayrollLineGradesForPeriodRow, len(grades))
	for _, g := range grades {
		byLine[g.ID.Bytes] = g
	}
	alphas, err := h.queries.ListPayrollLineAlphaForPeriod(ctx, params.PayrollPeriodID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil potongan alpha")
		return
	}
	alphaByLine := make(map[[16]byte]*db.ListPayrollLineAlphaForPeriodRow, len(alphas))
	for _, a := range alphas {
		alphaByLine[a.ID.Bytes] = a
	}
	out := make([]payrollLineWithGrade, 0, len(rows))
	for _, row := range rows {
		v := payrollLineWithGrade{ListPayrollLinesForPeriodRow: row}
		if g := byLine[row.ID.Bytes]; g != nil {
			v.PerformanceGrade, v.GradePayPercent = g.PerformanceGrade, g.GradePayPercent
		}
		if a := alphaByLine[row.ID.Bytes]; a != nil {
			v.AlphaUnpaidDays, v.AlphaDeduction = a.AlphaUnpaidDays, a.AlphaDeduction
		}
		out = append(out, v)
	}
	respondJSON(w, http.StatusOK, out)
}

// GetLineReview — GET /api/hr/payroll/lines/:id/review
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
		overtimeRequests = []*db.ListOvertimeRequestsForEmployeeRow{}
	}

	mult, err := service.LoadMultipliers(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan lembur")
		return
	}

	var grade *db.GetPayrollLineGradeRow
	if g, gerr := h.queries.GetPayrollLineGrade(ctx, pgID); gerr == nil {
		grade = g
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"line":              line,
		"grade":             grade,
		"components":        components,
		"attendance":        attendance,
		"violations":        violations,
		"installments":      installments,
		"holidays_worked":   holidaysWorked,
		"overtime_requests": overtimeRequests,
		"multipliers":       map[string]float64{"overtime": mult.Overtime, "holiday": mult.Holiday},
	})
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

	// Only bonus and allowance components are reviewer-editable.
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
		// daily_allowance is paid in cash during the month and stays out of gross/net.
		switch c.Type {
		case service.ComponentTypeBonus:
			bonusTotal += amt
		case service.ComponentTypeAllowance:
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

// RegenerateLine — POST /api/hr/payroll/periods/:id/regenerate-line/:employeeId (drops any review)
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

// ReviewAll — POST /api/hr/payroll/periods/{id}/review-all (marks reviewed, no recompute)
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

// regenerateSingle is service.GenerateLines for one employee; keep the two in step.
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

	var presentDays int32
	for _, c := range components {
		if c.ComponentCalcMethod == service.CalcMethodPerPresentDay {
			presentDays, err = qtx.CountPresentDays(ctx, &db.CountPresentDaysParams{
				EmployeeID: empID,
				Date:       pgtype.Date{Time: start, Valid: true},
				Date_2:     pgtype.Date{Time: end, Valid: true},
			})
			if err != nil {
				return err
			}
			break
		}
	}

	// The score picks the grade, so it is resolved before graded amounts.
	var perfScore pgtype.Int4
	if score, serr := qtx.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
		EmployeeID:  empID,
		PeriodMonth: pgtype.Date{Time: periodMonth, Valid: true},
	}); serr == nil && score != nil {
		perfScore = pgtype.Int4{Int32: score.Score, Valid: true}
	}

	grades, err := service.LoadGradeScale(ctx, qtx)
	if err != nil {
		return err
	}
	tier := grades.Resolve(perfScore)

	scPools, err := service.NewServiceChargeResolver(ctx, qtx, period)
	if err != nil {
		return err
	}

	compAmounts := make(map[pgtype.UUID]int64, len(components))
	var allowanceTotal, bonusTotal, deductionTotal int64
	for _, c := range components {
		amt := service.GradedComponentAmount(service.EffectiveComponentAmount(c.ComponentCalcMethod, c.Amount, presentDays),
			c.ComponentGradeLinked, tier, c.WageComponentID)
		if share, ok, err := scPools.Amount(ctx, emp.BranchID, empID, c.WageComponentID); err != nil {
			return err
		} else if ok {
			amt = share
		}
		compAmounts[c.WageComponentID] = amt
		switch c.ComponentType {
		case service.ComponentTypeAllowance:
			allowanceTotal += amt
		case service.ComponentTypeBonus:
			bonusTotal += amt
		case service.ComponentTypeDeduction:
			deductionTotal += amt
		case service.ComponentTypeDailyAllowance:
			// Paid in cash outside payroll: payslip information only.
		}
	}

	holidayCount, holidayRate, err := service.HolidayIncentive(ctx, qtx, empID, start, end, components)
	if err != nil {
		return err
	}

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

	unpaidDays, unpaidDeduction, err := service.LeavePayCut(ctx, qtx, empID, start, end, ws.DailyRate)
	if err != nil {
		return err
	}
	// Uncovered Alpha joins the unpaid-leave figures, as in service.GenerateLines.
	alphaDays, alphaDeduction, err := service.AlphaPayCut(ctx, qtx, empID, start, end, ws.DailyRate)
	if err != nil {
		return err
	}
	unpaidDays += alphaDays
	unpaidDeduction += alphaDeduction

	halfDayHours, halfDayDeduction, err := service.HalfDayDeductionForPeriod(ctx, qtx, empID, start, end, hourlyRate)
	if err != nil {
		return err
	}

	cut, err := service.AttendanceCutsForPeriod(ctx, qtx, empID, start, end, ws.DailyRate, sched)
	if err != nil {
		return err
	}

	calc := service.CalcLine(service.CalcLineInput{
		BaseSalary:              ws.BaseSalary,
		DailyRate:               ws.DailyRate,
		OvertimeDays:            0,
		OvertimeHours:           overtimeHours,
		OvertimeHourlyRate:      hourlyRate,
		PublicHolidayDays:       float64(holidayCount),
		PublicHolidayRate:       holidayRate,
		OvertimeMultiplier:      mult.Overtime,
		HolidayMultiplier:       mult.Holiday,
		AllowanceTotal:          allowanceTotal,
		BonusTotal:              bonusTotal,
		ComponentDeductionTotal: deductionTotal,
		KasbonDeduction:         kasbonDeduction,
		UnpaidLeaveDeduction:    unpaidDeduction,
		HalfDayDeduction:        halfDayDeduction,
		AttendanceCutDeduction:  cut.Amount,
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
		HalfDayHours:            service.NumericFromFloat(halfDayHours),
		HalfDayDeduction:        halfDayDeduction,
		AttendanceCutMinutes:    int32(cut.Minutes),
		AttendanceCutDeduction:  cut.Amount,
	})
	if err != nil {
		return err
	}
	if err := service.FreezePayrollLineGrade(ctx, qtx, line.ID, tier); err != nil {
		return err
	}
	if err := qtx.SetPayrollLineAlpha(ctx, &db.SetPayrollLineAlphaParams{
		ID: line.ID, AlphaUnpaidDays: int32(alphaDays), AlphaDeduction: alphaDeduction,
	}); err != nil {
		return err
	}
	if err := qtx.SetPayrollLineHolidayRate(ctx, &db.SetPayrollLineHolidayRateParams{
		ID: line.ID, PublicHolidayRate: holidayRate,
	}); err != nil {
		return err
	}
	for _, c := range components {
		if _, err := qtx.CreatePayrollLineComponent(ctx, &db.CreatePayrollLineComponentParams{
			PayrollLineID:   line.ID,
			WageComponentID: c.WageComponentID,
			Name:            c.ComponentName,
			Type:            c.ComponentType,
			Amount:          compAmounts[c.WageComponentID],
		}); err != nil {
			return err
		}
	}
	return nil
}
