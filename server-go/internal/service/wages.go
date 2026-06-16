package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// ErrEffectiveDateNotAfter is returned when a new wage version's effective date
// is not strictly after the current open version's effective date.
var ErrEffectiveDateNotAfter = errors.New("effective_date harus setelah versi struktur gaji yang berlaku saat ini")

// ComputeDailyRate returns base_salary / working_days_per_month rounded to the
// nearest whole unit (round half up). workingDays is guaranteed 1..31 by the DB
// CHECK constraint, but we guard against zero defensively.
func ComputeDailyRate(baseSalary int64, workingDays int32) int64 {
	if workingDays <= 0 {
		return 0
	}
	wd := int64(workingDays)
	return (baseSalary + wd/2) / wd
}

// IsWageVersionActiveOn reports whether a wage version with the given
// effective/end dates is the applicable version on date d:
// effective_date <= d AND (end_date IS NULL OR end_date >= d).
// Comparisons are date-only (time component ignored).
func IsWageVersionActiveOn(effective time.Time, endDate pgtype.Date, d time.Time) bool {
	eff := dateOnly(effective)
	day := dateOnly(d)
	if eff.After(day) {
		return false
	}
	if !endDate.Valid {
		return true
	}
	end := dateOnly(endDate.Time)
	return !end.Before(day) // end >= d
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// WageComponentInput is one component to attach to a new wage version.
type WageComponentInput struct {
	ComponentID pgtype.UUID
	Amount      int64
}

// CreateWageVersionParams carries the inputs for CreateWageVersion.
type CreateWageVersionParams struct {
	EmployeeID    pgtype.UUID
	BaseSalary    int64
	WorkingDays   int32
	EffectiveDate time.Time
	CreatedBy     pgtype.UUID
	Components    []WageComponentInput
}

// CreateWageVersion closes the current open wage version (if any) and inserts a
// new open version plus its components, in one transaction. The caller must
// pass a transactional *db.Queries (qtx). Rejects effective dates that are not
// strictly after the current open version's effective date.
func CreateWageVersion(ctx context.Context, qtx *db.Queries, p CreateWageVersionParams) (*db.WageStructure, error) {
	effDate := dateOnly(p.EffectiveDate)

	current, err := qtx.GetCurrentOpenWageStructure(ctx, p.EmployeeID)
	hasCurrent := err == nil
	if err != nil && !isNoRows(err) {
		return nil, err
	}

	if hasCurrent {
		curEff := dateOnly(current.EffectiveDate.Time)
		if !effDate.After(curEff) {
			return nil, ErrEffectiveDateNotAfter
		}
		// Close previous open version: end_date = effective_date - 1 day.
		closeDate := effDate.AddDate(0, 0, -1)
		if err := qtx.CloseOpenWageStructure(ctx, &db.CloseOpenWageStructureParams{
			EndDate:    pgtype.Date{Time: closeDate, Valid: true},
			EmployeeID: p.EmployeeID,
		}); err != nil {
			return nil, err
		}
	}

	dailyRate := ComputeDailyRate(p.BaseSalary, p.WorkingDays)

	ws, err := qtx.CreateWageStructure(ctx, &db.CreateWageStructureParams{
		EmployeeID:          p.EmployeeID,
		BaseSalary:          p.BaseSalary,
		WorkingDaysPerMonth: p.WorkingDays,
		DailyRate:           dailyRate,
		EffectiveDate:       pgtype.Date{Time: effDate, Valid: true},
		CreatedBy:           p.CreatedBy,
	})
	if err != nil {
		return nil, err
	}

	for _, c := range p.Components {
		if _, err := qtx.CreateEmployeeWageComponent(ctx, &db.CreateEmployeeWageComponentParams{
			WageStructureID: ws.ID,
			WageComponentID: c.ComponentID,
			Amount:          c.Amount,
		}); err != nil {
			return nil, err
		}
	}

	return ws, nil
}

// GetCurrentWage returns the wage version applicable on asOfDate, i.e.
// effective_date <= d AND (end_date IS NULL OR end_date >= d). Returns
// (nil, nil) when the employee has no applicable version.
func GetCurrentWage(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, asOfDate time.Time) (*db.WageStructure, error) {
	ws, err := qtx.GetWageStructureAsOf(ctx, &db.GetWageStructureAsOfParams{
		EmployeeID:    employeeID,
		EffectiveDate: pgtype.Date{Time: dateOnly(asOfDate), Valid: true},
	})
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return ws, nil
}

// isNoRows reports whether err is pgx.ErrNoRows.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
