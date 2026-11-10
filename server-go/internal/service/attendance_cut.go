package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Hourly pay cut for attendance errors. Arriving late and leaving early are the
// same failure — part of the day was not worked — so they cost the same thing:
// the day's pay for the minutes missed. The pure helpers are DB-free and back
// both payroll generation and the simulator endpoint, so the calculator cannot
// drift from what payroll actually deducts.

// AttendanceCutBlockMinutes is the rounding unit. Each end of the day is rounded
// UP to a whole block on its own, so 6 minutes late costs 15 minutes — a minute
// past the grace window is not free, and an early exit of 16 minutes costs 30.
const AttendanceCutBlockMinutes = 15

// RoundUpToBlock rounds a positive minute count up to a whole multiple of block.
// Zero and negative inputs return 0; a non-positive block means "per minute".
func RoundUpToBlock(minutes, block int) int {
	if minutes <= 0 {
		return 0
	}
	if block <= 1 {
		return minutes
	}
	return (minutes + block - 1) / block * block
}

// AttendanceCutResult is the outcome of one day's cut.
type AttendanceCutResult struct {
	LateCutMinutes  int   `json:"late_cut_minutes"`
	EarlyCutMinutes int   `json:"early_cut_minutes"`
	CutMinutes      int   `json:"cut_minutes"` // after the one-day cap
	Capped          bool  `json:"capped"`
	HourlyRate      int64 `json:"hourly_rate"` // informational: daily rate / paid hours
	Amount          int64 `json:"amount"`
}

// ComputeAttendanceCut prices one day:
//
//	cut_minutes = min(roundUp(late) + roundUp(early), paid_minutes)
//	amount      = round(daily_rate × cut_minutes / paid_minutes)
//
// lateMinutes / earlyMinutes must already be 0 when the day was inside its grace
// (the attendance flags decide that, see ComputeAnomalies). Priced per minute of
// the day's own paid length rather than through a rounded hourly rate, so a full
// cut is exactly one day's pay with no rounding residue.
func ComputeAttendanceCut(dailyRate int64, paidMinutes, lateMinutes, earlyMinutes int) AttendanceCutResult {
	if paidMinutes <= 0 {
		paidMinutes = 8 * 60
	}
	r := AttendanceCutResult{
		LateCutMinutes:  RoundUpToBlock(lateMinutes, AttendanceCutBlockMinutes),
		EarlyCutMinutes: RoundUpToBlock(earlyMinutes, AttendanceCutBlockMinutes),
		HourlyRate:      roundHalfUp(float64(dailyRate) * 60 / float64(paidMinutes)),
	}
	r.CutMinutes = r.LateCutMinutes + r.EarlyCutMinutes
	if r.CutMinutes > paidMinutes {
		r.CutMinutes = paidMinutes
		r.Capped = true
	}
	r.Amount = roundHalfUp(float64(dailyRate) * float64(r.CutMinutes) / float64(paidMinutes))
	return r
}

// AttendanceCutDay is one flagged present day in a period breakdown.
type AttendanceCutDay struct {
	Date         string     `json:"date"`
	CheckIn      *time.Time `json:"check_in"`
	CheckOut     *time.Time `json:"check_out"`
	ShiftCode    string     `json:"shift_code"`
	PaidMinutes  int        `json:"paid_minutes"`
	LateMinutes  int        `json:"late_minutes"`
	EarlyMinutes int        `json:"early_minutes"`
	// HalfDay days are listed but never cut here: their lost time is already
	// deducted through half_day_deduction.
	HalfDay bool `json:"half_day"`
	AttendanceCutResult
}

// AttendanceCutSummary is a period's cut for one employee.
type AttendanceCutSummary struct {
	Days    []AttendanceCutDay `json:"days"`
	Minutes int                `json:"minutes"`
	Amount  int64              `json:"amount"`
}

// AttendanceCutsForPeriod walks an employee's flagged present days in [from, to]
// and prices each one. A day's paid length is its rostered shift (segments minus
// break) when there is one, else the branch schedule's work_start → work_end.
func AttendanceCutsForPeriod(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, dailyRate int64, branchSched Schedule) (*AttendanceCutSummary, error) {
	rows, err := qtx.ListAttendanceCutDays(ctx, &db.ListAttendanceCutDaysParams{
		EmployeeID: employeeID,
		Date:       pgtype.Date{Time: dateOnly(from), Valid: true},
		Date_2:     pgtype.Date{Time: dateOnly(to), Valid: true},
	})
	if err != nil {
		return nil, err
	}

	branchPaid := branchSched.WorkEndMinutes - branchSched.WorkStartMinutes
	out := &AttendanceCutSummary{Days: []AttendanceCutDay{}}
	for _, r := range rows {
		day := AttendanceCutDay{
			Date:        r.Date.Time.Format("2006-01-02"),
			ShiftCode:   textVal(r.ShiftCode),
			PaidMinutes: branchPaid,
			HalfDay:     r.IsHalfDay,
		}
		if r.ShiftPaidMinutes.Valid && r.ShiftPaidMinutes.Int32 > 0 {
			day.PaidMinutes = int(r.ShiftPaidMinutes.Int32)
		}
		if r.CheckIn.Valid {
			t := r.CheckIn.Time
			day.CheckIn = &t
		}
		if r.CheckOut.Valid {
			t := r.CheckOut.Time
			day.CheckOut = &t
		}
		if !r.IsHalfDay {
			if r.IsLate {
				day.LateMinutes = int(r.LateMinutes)
			}
			if r.IsEarlyLeave {
				day.EarlyMinutes = int(r.EarlyLeaveMinutes)
			}
			day.AttendanceCutResult = ComputeAttendanceCut(dailyRate, day.PaidMinutes, day.LateMinutes, day.EarlyMinutes)
		}
		out.Minutes += day.CutMinutes
		out.Amount += day.Amount
		out.Days = append(out.Days, day)
	}
	return out, nil
}
