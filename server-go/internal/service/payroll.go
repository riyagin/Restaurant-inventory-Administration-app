package service

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Payroll business logic (prompt 08). The calculation core (CalcLine) is pure and
// DB-free so it can be unit-tested without a database. GenerateLines, ReviewLine and
// ClosePeriod orchestrate the wage/attendance/performance/leave/kasbon/accounts
// modules inside a transaction.
//
// MONEY: whole rupiah int64 everywhere (no ×100), consistent with prompts 02/03/07.
// NUMERIC↔Go: overtime_days / public_holiday_days and the multipliers are stored as
// NUMERIC in Postgres (pgtype.Numeric at the db boundary). To keep the multiplier
// math simple we represent day counts and multipliers as float64 inside Go and
// convert to/from pgtype.Numeric only at the db boundary (numericToF / fToNumeric).
// Rounding to whole rupiah is half-up.

// Wage-component calculation methods (mirrors the wage_components.calc_method
// CHECK constraint). A 'fixed' component contributes its stored amount as-is; a
// 'per_present_day' component treats the stored amount as a per-day rate and
// multiplies it by the number of 'present' attendance days in the period.
const (
	CalcMethodFixed         = "fixed"
	CalcMethodPerPresentDay = "per_present_day"
)

// EffectiveComponentAmount resolves a wage-structure component's rupiah
// contribution for a period given how many days the employee was present.
func EffectiveComponentAmount(calcMethod string, amount int64, presentDays int32) int64 {
	if calcMethod == CalcMethodPerPresentDay {
		return amount * int64(presentDays)
	}
	return amount
}

// ScoreGatePasses reports whether a score-gated component should pay for the
// period. A component with no min_score (minScore invalid) is never gated. When a
// threshold is set the employee's performance score must meet it; a missing score
// (evaluation not run yet) passes so pay is not silently withheld — the reviewer
// can still zero the component at payroll review if the score later disqualifies it.
func ScoreGatePasses(minScore, score pgtype.Int4) bool {
	if !minScore.Valid {
		return true
	}
	if !score.Valid {
		return true
	}
	return score.Int32 >= minScore.Int32
}

// anyPerPresentDay reports whether any component uses the per_present_day method,
// so callers can skip the attendance count for fixed-only wage structures.
func anyPerPresentDay(components []*db.ListEmployeeWageComponentsRow) bool {
	for _, c := range components {
		if c.ComponentCalcMethod == CalcMethodPerPresentDay {
			return true
		}
	}
	return false
}

// ErrLinesNotReviewed is returned by ClosePeriod when at least one line is still
// unreviewed. The handler maps it to HTTP 409 Conflict.
var ErrLinesNotReviewed = errors.New("semua baris penggajian harus direview sebelum periode ditutup")

// ErrPeriodLocked is returned when a mutation targets a closed/paid period or line.
// The handler maps it to HTTP 409 Conflict.
var ErrPeriodLocked = errors.New("periode penggajian sudah ditutup dan tidak dapat diubah")

// roundHalfUp rounds a float to the nearest whole rupiah, half away from zero.
func roundHalfUp(v float64) int64 {
	return int64(math.Floor(v + 0.5))
}

// PeriodBounds returns the first and last calendar day of the month that the
// given date falls in (both date-only, UTC). Used to derive a payroll period's
// start_date / end_date from its period_month.
func PeriodBounds(month time.Time) (time.Time, time.Time) {
	start := FirstOfMonth(month)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// ── NUMERIC <-> float64 boundary helpers ─────────────────────────────────────

// numericToF converts a pgtype.Numeric to float64 (0 when invalid/NaN). Decodes the
// mantissa/exponent directly (same approach as handler.numericToFloat64) to avoid
// depending on optional pgtype helpers.
func numericToF(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN || n.Int == nil {
		return 0
	}
	f, _ := new(big.Float).SetInt(n.Int).Float64()
	if n.Exp > 0 {
		for i := int32(0); i < n.Exp; i++ {
			f *= 10
		}
	} else if n.Exp < 0 {
		for i := n.Exp; i < 0; i++ {
			f /= 10
		}
	}
	return f
}

// NumericFromFloat converts a float64 to a pgtype.Numeric with 2 decimal places,
// matching the NUMERIC(5,2) / NUMERIC(4,2) columns. Exported so handlers can build
// CreatePayrollLine params at the db boundary.
func NumericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	// Format with 2 decimals so Scan produces an exact-scale numeric.
	_ = n.Scan(strconv.FormatFloat(v, 'f', 2, 64))
	return n
}

// ── Pure calculation core (DB-free, unit-tested) ─────────────────────────────

// CalcLineInput carries every value needed to compute a payroll line's derived
// amounts. Day/hour counts and multipliers are float64; money is int64 whole rupiah.
type CalcLineInput struct {
	BaseSalary              int64
	DailyRate               int64
	OvertimeDays            float64
	OvertimeHours           float64
	OvertimeHourlyRate      int64
	PublicHolidayDays       float64
	OvertimeMultiplier      float64
	HolidayMultiplier       float64
	AllowanceTotal          int64
	BonusTotal              int64
	ComponentDeductionTotal int64
	KasbonDeduction         int64
	UnpaidLeaveDeduction    int64
}

// CalcLineResult holds the derived amounts for a payroll line.
type CalcLineResult struct {
	OvertimeAmount       int64
	OvertimeHourlyAmount int64
	PublicHolidayAmount  int64
	GrossPay             int64
	NetPay               int64
}

// CalcLine applies the spec formulas with half-up rounding on the multiplier math:
//
//	overtime_amount        = round(overtime_days × daily_rate × overtime_multiplier)
//	overtime_hourly_amount = round(overtime_hours × overtime_hourly_rate × overtime_multiplier)
//	public_holiday_amount  = round(public_holiday_days × daily_rate × holiday_multiplier)
//	gross_pay = base_salary + allowance_total + bonus_total + overtime_amount
//	            + overtime_hourly_amount + public_holiday_amount
//	net_pay   = gross_pay − component_deduction_total − kasbon_deduction − unpaid_leave_deduction
//
// Hourly overtime is tracked alongside the day-based overtime_days field (not a
// replacement for it) so both can be logged when applicable, e.g. a handful of
// overtime hours on top of full overtime days in the same period.
func CalcLine(in CalcLineInput) CalcLineResult {
	overtime := roundHalfUp(in.OvertimeDays * float64(in.DailyRate) * in.OvertimeMultiplier)
	overtimeHourly := roundHalfUp(in.OvertimeHours * float64(in.OvertimeHourlyRate) * in.OvertimeMultiplier)
	holiday := roundHalfUp(in.PublicHolidayDays * float64(in.DailyRate) * in.HolidayMultiplier)

	gross := in.BaseSalary + in.AllowanceTotal + in.BonusTotal + overtime + overtimeHourly + holiday
	net := gross - in.ComponentDeductionTotal - in.KasbonDeduction - in.UnpaidLeaveDeduction

	return CalcLineResult{
		OvertimeAmount:       overtime,
		OvertimeHourlyAmount: overtimeHourly,
		PublicHolidayAmount:  holiday,
		GrossPay:             gross,
		NetPay:               net,
	}
}

// StandardDailyHours returns the standard work hours per day implied by a branch's
// work schedule (work_end − work_start), falling back to 8 when non-positive.
func StandardDailyHours(sched Schedule) float64 {
	minutes := sched.WorkEndMinutes - sched.WorkStartMinutes
	if minutes <= 0 {
		return 8
	}
	return float64(minutes) / 60
}

// HourlyRateFromDaily derives a whole-rupiah hourly rate from a daily rate and the
// branch's standard work hours, used to snapshot overtime_hourly_rate at generation
// time (kept stable even if the schedule changes afterwards).
func HourlyRateFromDaily(dailyRate int64, sched Schedule) int64 {
	return roundHalfUp(float64(dailyRate) / StandardDailyHours(sched))
}

// GetOvertimeHours sums minutes worked past the branch's scheduled work_end across
// every 'present' attendance record for an employee in [from, to], converting to
// hours. Seeds payroll's overtime_hours automatically from attendance instead of
// requiring a fully manual entry; the reviewer can still adjust it afterwards.
func GetOvertimeHours(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, sched Schedule) (float64, error) {
	rows, err := qtx.ListPresentAttendanceForEmployeeRange(ctx, &db.ListPresentAttendanceForEmployeeRangeParams{
		EmployeeID: employeeID,
		Date:       pgtype.Date{Time: dateOnly(from), Valid: true},
		Date_2:     pgtype.Date{Time: dateOnly(to), Valid: true},
	})
	if err != nil {
		return 0, err
	}

	totalMinutes := 0
	for _, r := range rows {
		if !r.CheckOut.Valid {
			continue
		}
		checkOut := r.CheckOut.Time
		totalMinutes += ComputeOvertimeMinutes(&checkOut, sched)
	}
	return float64(totalMinutes) / 60, nil
}

// AllLinesReviewed reports whether every line in a period has been reviewed. Used by
// the close-blocked-until-reviewed predicate (and unit-tested directly).
func AllLinesReviewed(reviewedCount, total int) bool {
	return total > 0 && reviewedCount >= total
}

// ── Period / line orchestration (DB-touching) ────────────────────────────────

// PayrollMultipliers carries the two configurable multipliers as float64.
type PayrollMultipliers struct {
	Overtime float64
	Holiday  float64
}

// LoadMultipliers reads the singleton payroll_settings row and returns its
// multipliers as float64. Falls back to 1.5 / 2.0 when the row is missing.
func LoadMultipliers(ctx context.Context, qtx *db.Queries) (PayrollMultipliers, error) {
	s, err := qtx.GetPayrollSettings(ctx)
	if err != nil {
		if isNoRows(err) {
			return PayrollMultipliers{Overtime: 1.5, Holiday: 2.0}, nil
		}
		return PayrollMultipliers{}, err
	}
	return PayrollMultipliers{
		Overtime: numericToF(s.OvertimeMultiplier),
		Holiday:  numericToF(s.HolidayMultiplier),
	}, nil
}

// GenerateLinesResult reports employees that were skipped (no open wage structure).
type GenerateLinesResult struct {
	Created      int      `json:"created"`
	SkippedNames []string `json:"skipped"`
}

// GenerateLines snapshots a payroll line for every active employee. Employees with no
// applicable wage structure on the period end date are skipped (collected as
// warnings). Must run inside a transaction (qtx).
func GenerateLines(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod) (*GenerateLinesResult, error) {
	mult, err := LoadMultipliers(ctx, qtx)
	if err != nil {
		return nil, err
	}

	employees, err := qtx.ListActiveEmployeesForPayroll(ctx)
	if err != nil {
		return nil, err
	}

	periodMonth := period.PeriodMonth.Time
	start := period.StartDate.Time
	end := period.EndDate.Time

	res := &GenerateLinesResult{SkippedNames: []string{}}

	// Cache per-branch schedules to avoid repeated lookups (mirrors ReconcileAbsent).
	schedCache := map[string]Schedule{}
	getSched := func(branchID pgtype.UUID) Schedule {
		key := string(branchID.Bytes[:])
		if s, ok := schedCache[key]; ok {
			return s
		}
		ws, err := qtx.GetWorkScheduleByBranch(ctx, branchID)
		var s Schedule
		if err != nil || ws == nil {
			s = DefaultSchedule()
		} else {
			s = ScheduleFromRow(ws)
		}
		schedCache[key] = s
		return s
	}

	for _, emp := range employees {
		ws, err := GetCurrentWage(ctx, qtx, emp.ID, end)
		if err != nil {
			return nil, err
		}
		if ws == nil {
			res.SkippedNames = append(res.SkippedNames, emp.FullName)
			continue
		}
		sched := getSched(emp.BranchID)
		hourlyRate := HourlyRateFromDaily(ws.DailyRate, sched)

		// Snapshot allowance/bonus/deduction totals from every wage structure
		// component — fixed AND variable (variable components carry their current
		// catalog amount as a starting point; the reviewer can still adjust bonus/
		// allowance amounts later, which recomputes these totals).
		components, err := qtx.ListEmployeeWageComponents(ctx, ws.ID)
		if err != nil {
			return nil, err
		}

		// Present-day count drives per_present_day components. Only queried when at
		// least one such component exists, so fixed-only structures skip the lookup.
		var presentDays int32
		if anyPerPresentDay(components) {
			presentDays, err = qtx.CountPresentDays(ctx, &db.CountPresentDaysParams{
				EmployeeID: emp.ID,
				Date:       pgtype.Date{Time: dateOnly(start), Valid: true},
				Date_2:     pgtype.Date{Time: dateOnly(end), Valid: true},
			})
			if err != nil {
				return nil, err
			}
		}

		// Performance score snapshot (nullable) — also gates score-conditional
		// components below, so it is resolved before component amounts.
		var perfScore pgtype.Int4
		score, serr := qtx.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
			EmployeeID:  emp.ID,
			PeriodMonth: pgtype.Date{Time: dateOnly(periodMonth), Valid: true},
		})
		if serr == nil && score != nil {
			perfScore = pgtype.Int4{Int32: score.Score, Valid: true}
		}

		// Effective amount per component: per_present_day → rate × present days;
		// score-gated components pay 0 when the score misses their min_score.
		compAmounts := make(map[pgtype.UUID]int64, len(components))
		var allowanceTotal, bonusTotal, deductionTotal int64
		for _, c := range components {
			amt := EffectiveComponentAmount(c.ComponentCalcMethod, c.Amount, presentDays)
			if !ScoreGatePasses(c.ComponentMinScore, perfScore) {
				amt = 0
			}
			compAmounts[c.WageComponentID] = amt
			switch c.ComponentType {
			case "allowance":
				allowanceTotal += amt
			case "bonus":
				bonusTotal += amt
			case "deduction":
				deductionTotal += amt
			}
		}

		// Prefill public_holiday_days from attendance present-on-holiday count.
		holidayCount, err := qtx.CountPresentOnHolidays(ctx, &db.CountPresentOnHolidaysParams{
			EmployeeID: emp.ID,
			Date:       pgtype.Date{Time: dateOnly(start), Valid: true},
			Date_2:     pgtype.Date{Time: dateOnly(end), Valid: true},
		})
		if err != nil {
			return nil, err
		}

		// Seed overtime_hours from formal overtime requests logged for this period.
		overtimeHours, err := qtx.SumOvertimeHoursForEmployee(ctx, &db.SumOvertimeHoursForEmployeeParams{
			EmployeeID: emp.ID,
			Date:       pgtype.Date{Time: dateOnly(start), Valid: true},
			Date_2:     pgtype.Date{Time: dateOnly(end), Valid: true},
		})
		if err != nil {
			return nil, err
		}

		// Kasbon: sum pending installments due in the period month.
		installments, err := GetPendingInstallments(ctx, qtx, emp.ID, periodMonth)
		if err != nil {
			return nil, err
		}
		var kasbonDeduction int64
		for _, ins := range installments {
			kasbonDeduction += ins.Amount
		}

		// Unpaid leave: working days × daily_rate.
		unpaidDays, err := GetUnpaidLeaveDays(ctx, qtx, emp.ID, start, end)
		if err != nil {
			return nil, err
		}
		unpaidDeduction := int64(unpaidDays) * ws.DailyRate

		calc := CalcLine(CalcLineInput{
			BaseSalary:              ws.BaseSalary,
			DailyRate:               ws.DailyRate,
			OvertimeDays:            0, // entered manually during review (no day-based attendance signal)
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
			EmployeeID:              emp.ID,
			WageStructureID:         ws.ID,
			BaseSalary:              ws.BaseSalary,
			DailyRate:               ws.DailyRate,
			OvertimeDays:            NumericFromFloat(0),
			PublicHolidayDays:       NumericFromFloat(float64(holidayCount)),
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
			OvertimeHours:           NumericFromFloat(overtimeHours),
			OvertimeHourlyRate:      hourlyRate,
			OvertimeHourlyAmount:    calc.OvertimeHourlyAmount,
		})
		if err != nil {
			return nil, err
		}

		// Denormalized component snapshots (drive the payslip breakdown). The snapshot
		// stores the computed amount so per_present_day components show the resolved
		// figure (rate × present days), which the reviewer can still adjust.
		for _, c := range components {
			if _, err := qtx.CreatePayrollLineComponent(ctx, &db.CreatePayrollLineComponentParams{
				PayrollLineID:   line.ID,
				WageComponentID: c.WageComponentID,
				Name:            c.ComponentName,
				Type:            c.ComponentType,
				Amount:          compAmounts[c.WageComponentID],
			}); err != nil {
				return nil, err
			}
		}

		res.Created++
	}

	return res, nil
}

// ReviewLineInput carries the reviewer's adjustments for a single line.
type ReviewLineInput struct {
	OvertimeDays      float64
	OvertimeHours     float64
	PublicHolidayDays float64
	// AdjustedBonusTotal/AdjustedAllowanceTotal are the recomputed totals after the
	// reviewer edits variable bonus/allowance component amounts. The handler computes
	// them from the adjusted component amounts it persists.
	AdjustedBonusTotal     int64
	AdjustedAllowanceTotal int64
	ReviewNote             pgtype.Text
	ReviewedBy             pgtype.UUID
}

// ReviewLine recomputes a line's amounts from the reviewer's adjusted overtime
// days/hours, holiday days, bonus total and allowance total, persists them, and marks
// the line reviewed. Must run inside a transaction. The caller is responsible for
// persisting any per-component amount edits (and computing AdjustedBonusTotal /
// AdjustedAllowanceTotal from them) before/after calling this. The hourly overtime
// rate stays at its generation-time snapshot (line.OvertimeHourlyRate) — only the
// hours figure is reviewer-editable.
func ReviewLine(ctx context.Context, qtx *db.Queries, line *db.PayrollLine, in ReviewLineInput) (*db.PayrollLine, error) {
	mult, err := LoadMultipliers(ctx, qtx)
	if err != nil {
		return nil, err
	}

	calc := CalcLine(CalcLineInput{
		BaseSalary:              line.BaseSalary,
		DailyRate:               line.DailyRate,
		OvertimeDays:            in.OvertimeDays,
		OvertimeHours:           in.OvertimeHours,
		OvertimeHourlyRate:      line.OvertimeHourlyRate,
		PublicHolidayDays:       in.PublicHolidayDays,
		OvertimeMultiplier:      mult.Overtime,
		HolidayMultiplier:       mult.Holiday,
		AllowanceTotal:          in.AdjustedAllowanceTotal,
		BonusTotal:              in.AdjustedBonusTotal,
		ComponentDeductionTotal: line.ComponentDeductionTotal,
		KasbonDeduction:         line.KasbonDeduction,
		UnpaidLeaveDeduction:    line.UnpaidLeaveDeduction,
	})

	return qtx.UpdatePayrollLineReview(ctx, &db.UpdatePayrollLineReviewParams{
		OvertimeDays:         NumericFromFloat(in.OvertimeDays),
		PublicHolidayDays:    NumericFromFloat(in.PublicHolidayDays),
		OvertimeAmount:       calc.OvertimeAmount,
		PublicHolidayAmount:  calc.PublicHolidayAmount,
		BonusTotal:           in.AdjustedBonusTotal,
		AllowanceTotal:       in.AdjustedAllowanceTotal,
		GrossPay:             calc.GrossPay,
		NetPay:               calc.NetPay,
		OvertimeHours:        NumericFromFloat(in.OvertimeHours),
		OvertimeHourlyAmount: calc.OvertimeHourlyAmount,
		ReviewedBy:           in.ReviewedBy,
		ReviewNote:           in.ReviewNote,
		ID:                   line.ID,
	})
}

// ClosePeriod locks the period after verifying every line is reviewed, marks each
// line's due kasbon installments as deducted (resolving fully-paid kasbons), and posts
// the total payroll expense per branch to the branch expense account. Returns
// ErrLinesNotReviewed (→ 409) when any line is unreviewed. Must run inside a
// transaction.
func ClosePeriod(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod) (*db.PayrollPeriod, error) {
	unreviewed, err := qtx.CountUnreviewedLines(ctx, period.ID)
	if err != nil {
		return nil, err
	}
	if unreviewed > 0 {
		return nil, ErrLinesNotReviewed
	}

	// Mark kasbon installments deducted + resolve fully-paid kasbons. We re-pull the
	// pending installments due this month per employee and link them to the line.
	lines, err := qtx.ListPayrollLinesForPeriod(ctx, &db.ListPayrollLinesForPeriodParams{
		PayrollPeriodID: period.ID,
	})
	if err != nil {
		return nil, err
	}
	periodMonth := period.PeriodMonth.Time
	for _, l := range lines {
		installments, err := GetPendingInstallments(ctx, qtx, l.EmployeeID, periodMonth)
		if err != nil {
			return nil, err
		}
		for _, ins := range installments {
			if err := MarkInstallmentDeducted(ctx, qtx, ins.ID, l.ID); err != nil {
				return nil, err
			}
			if err := ResolveKasbonIfComplete(ctx, qtx, ins.KasbonID); err != nil {
				return nil, err
			}
		}
	}

	// Post total payroll expense per branch to the branch's expense account. We use
	// gross_pay as the "total payroll expense" (full cost of labour, before employee
	// deductions which net out elsewhere). Expense accounts increase with a positive
	// delta, following the dispatches/sales CoA posting direction.
	branchTotals, err := qtx.ListPayrollLineBranchTotals(ctx, period.ID)
	if err != nil {
		return nil, err
	}
	for _, bt := range branchTotals {
		if !bt.BranchID.Valid || bt.TotalGross == 0 {
			continue
		}
		expenseAcct, err := qtx.GetBranchExpenseAccountID(ctx, bt.BranchID)
		if err != nil {
			return nil, err
		}
		if !expenseAcct.Valid {
			continue
		}
		if err := UpdateBalance(ctx, qtx, expenseAcct.Bytes, bt.TotalGross); err != nil {
			return nil, err
		}
	}

	return qtx.ClosePayrollPeriod(ctx, period.ID)
}
