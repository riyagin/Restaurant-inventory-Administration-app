package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Uang makan and other daily_allowance components.
//
// They are handed out in cash every day, outside payroll, so they never enter
// gross / net pay. Two things are built on them here:
//
//   - DailyAllowanceForMonth: what an employee earned from them in a month, for
//     the evaluation screen, so a review shows the money next to the score. It
//     uses exactly the rule payroll snapshots (EffectiveComponentAmount over
//     CountPresentDays, scaled by the grade for grade-linked components), so the
//     two cannot disagree.
//   - PostDailyAllowancePeriod: the month's expense. When a payroll period is
//     posted, the daily_allowance amounts snapshotted on its lines are booked as
//     one entry per period: Dr Beban Gaji - <cabang> (memo "uang makan") / Cr Kas.
//     The cash left the till day by day; this is where the ledger catches up.

// SourceDailyAllowance marks the monthly uang makan expense entry; source_id is
// the payroll period.
const SourceDailyAllowance = "daily_allowance"

// DailyAllowanceItem is one daily_allowance component for one employee-month.
type DailyAllowanceItem struct {
	Name       string `json:"name"`
	Rate       int64  `json:"rate"`        // per day for per_present_day, else the monthly amount
	CalcMethod string `json:"calc_method"` // fixed | per_present_day
	Days       int32  `json:"days"`        // present days counted (per_present_day only)
	FullAmount int64  `json:"full_amount"` // before the grade
	Amount     int64  `json:"amount"`      // after the grade
	// GradeLinked components pay only their grade's percentage; Grade and
	// PayPercent are set when a grade applied (a score exists).
	GradeLinked bool   `json:"grade_linked"`
	Grade       string `json:"grade,omitempty"`
	PayPercent  *int32 `json:"pay_percent,omitempty"`
}

// DailyAllowanceSummary is what the evaluation screen shows for an employee-month.
type DailyAllowanceSummary struct {
	HasWage     bool                 `json:"has_wage"`
	PresentDays int32                `json:"present_days"`
	BaseSalary  int64                `json:"base_salary"`
	Items       []DailyAllowanceItem `json:"items"`
	Total       int64                `json:"total"`
}

// dailyAllowanceItems applies payroll's rule to the daily_allowance components.
func dailyAllowanceItems(components []*db.ListEmployeeWageComponentsRow, presentDays int32, tier *GradeTier) ([]DailyAllowanceItem, int64) {
	items := []DailyAllowanceItem{}
	var total int64
	for _, c := range components {
		if c.ComponentType != ComponentTypeDailyAllowance {
			continue
		}
		it := DailyAllowanceItem{Name: c.ComponentName, Rate: c.Amount, CalcMethod: c.ComponentCalcMethod}
		if c.ComponentCalcMethod == CalcMethodPerPresentDay {
			it.Days = presentDays
		}
		it.FullAmount = EffectiveComponentAmount(c.ComponentCalcMethod, c.Amount, presentDays)
		it.GradeLinked = c.ComponentGradeLinked
		it.Amount = GradedAmount(it.FullAmount, c.ComponentGradeLinked, tier)
		if c.ComponentGradeLinked && tier != nil {
			pct := tier.PayPercent
			it.Grade, it.PayPercent = tier.Label, &pct
		}
		total += it.Amount
		items = append(items, it)
	}
	return items, total
}

// DailyAllowanceForMonth computes an employee's daily_allowance earnings for the
// calendar month containing `month`, from the wage structure in force on the
// month's last day (as payroll does).
func DailyAllowanceForMonth(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, month time.Time) (*DailyAllowanceSummary, error) {
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	out := &DailyAllowanceSummary{Items: []DailyAllowanceItem{}}

	ws, err := GetCurrentWage(ctx, q, employeeID, end)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return out, nil
	}
	out.HasWage, out.BaseSalary = true, ws.BaseSalary

	components, err := q.ListEmployeeWageComponents(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	out.PresentDays, err = q.CountPresentDays(ctx, &db.CountPresentDaysParams{
		EmployeeID: employeeID,
		Date:       pgtype.Date{Time: start, Valid: true},
		Date_2:     pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	var score pgtype.Int4
	s, err := q.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
		EmployeeID: employeeID, PeriodMonth: pgtype.Date{Time: start, Valid: true},
	})
	if err == nil && s != nil {
		score = pgtype.Int4{Int32: s.Score, Valid: true}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	grades, err := LoadGradeScale(ctx, q)
	if err != nil {
		return nil, err
	}
	out.Items, out.Total = dailyAllowanceItems(components, out.PresentDays, grades.Resolve(score))
	return out, nil
}

// DailyAllowanceEntryDescription is the journal description of a month's entry.
func DailyAllowanceEntryDescription(periodMonth time.Time) string {
	return fmt.Sprintf("Uang Makan %s", periodMonth.Format("2006-01"))
}

// PostDailyAllowancePeriod books the period's uang makan as expense:
// Dr Beban Gaji - <cabang> per branch / Cr Kas. Idempotent (one entry per
// period, keyed by source). Must run inside the payroll posting transaction.
func PostDailyAllowancePeriod(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod, actor uuid.UUID) (uuid.UUID, error) {
	existing, err := qtx.ListJournalEntriesBySource(ctx, &db.ListJournalEntriesBySourceParams{
		SourceType: SourceDailyAllowance, SourceID: period.ID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if len(existing) > 0 {
		return uuid.Nil, nil
	}
	totals, err := qtx.ListDailyAllowanceByBranchForPeriod(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	var lines []Line
	var total int64
	for _, t := range totals {
		if t.Total <= 0 {
			continue
		}
		name := t.BranchName.String
		memo := "uang makan"
		if name != "" {
			memo += " - " + name
		}
		lines = append(lines, Dr(resolveWageAccount(ctx, qtx, t.BranchID, name), t.Total).WithMemo(memo))
		total += t.Total
	}
	if total == 0 {
		return uuid.Nil, nil
	}
	cash, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: CashAccountNumber, Valid: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("akun sistem 'Kas' (%d) tidak ditemukan: %w", CashAccountNumber, err)
	}
	lines = append(lines, Cr(cash.ID.Bytes, total).WithMemo("uang makan dibayar tunai harian"))
	return Post(ctx, qtx, Entry{
		Date:        period.EndDate.Time,
		SourceType:  SourceDailyAllowance,
		SourceID:    period.ID.Bytes,
		Description: DailyAllowanceEntryDescription(period.PeriodMonth.Time),
		CreatedBy:   actor,
		Lines:       lines,
	})
}
