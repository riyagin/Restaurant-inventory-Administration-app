package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Alpha — a working day missed without permission (an attendance record left
// 'absent') — is paid for out of the same monthly allowance as a day off:
//
//   - It costs what a day off costs (LeaveDayCost): 1 on a weekday, 2 on a
//     Saturday or Sunday.
//   - It draws on what that month's allowance has left after approved leave,
//     in date order.
//   - A day the remaining allowance cannot cover in full is not paid at all:
//     cut at cost × daily rate (so 2× on a weekend, like unpaid weekend leave),
//     and the partial allowance it could not use is kept for a later day.
//   - The performance violation for Alpha (absent_no_leave) is unchanged.
//
// Nothing is stored: every figure here is derived from attendance_records and
// leave_quota_usage when asked. A corrected record, a backdated leave approval
// or a cancelled leave therefore changes the outcome by itself, the same way
// the day's performance violations are re-derived. Payroll freezes the result
// on the line at generation, like every other deduction.
//
// Leave approval counts the allowance Alpha has already used, so once an Alpha
// day has been paid from the allowance, leave cannot later be approved over it
// and push that day into unpaid.

// AlphaDay is one Alpha day and how it was paid for.
type AlphaDay struct {
	Date    time.Time
	Cost    int  // allowance days it costs (LeaveDayCost)
	Covered bool // paid from the allowance; false = unpaid
}

// AllocateAlpha pays for dates (ascending) from remaining allowance days, whole
// days only. Pure; the rules live here.
func AllocateAlpha(remaining int, dates []time.Time) []AlphaDay {
	if remaining < 0 {
		remaining = 0
	}
	out := make([]AlphaDay, 0, len(dates))
	for _, d := range dates {
		day := AlphaDay{Date: dateOnlyUTC(d), Cost: LeaveDayCost(d)}
		if remaining >= day.Cost {
			day.Covered = true
			remaining -= day.Cost
		}
		out = append(out, day)
	}
	return out
}

// AlphaQuotaUsed is the allowance the covered days used.
func AlphaQuotaUsed(days []AlphaDay) int {
	n := 0
	for _, d := range days {
		if d.Covered {
			n += d.Cost
		}
	}
	return n
}

// AlphaUnpaidDays counts the days the allowance could not cover.
func AlphaUnpaidDays(days []AlphaDay) int {
	n := 0
	for _, d := range days {
		if !d.Covered {
			n++
		}
	}
	return n
}

// AlphaMonth is one employee's allowance for one month with Alpha applied.
type AlphaMonth struct {
	Month     time.Time
	Quota     int
	LeaveUsed int // allowance used by approved leave
	Days      []AlphaDay
}

// AlphaUsed is the allowance Alpha used this month.
func (m *AlphaMonth) AlphaUsed() int { return AlphaQuotaUsed(m.Days) }

// AlphaForMonth derives the month's Alpha days and how each was paid for.
// Dates in exclude are left out: a leave request being approved over an Alpha
// day turns it into leave, so it must not be counted as both.
func AlphaForMonth(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, month time.Time, exclude ...time.Time) (*AlphaMonth, error) {
	skip := make(map[time.Time]bool, len(exclude))
	for _, d := range exclude {
		skip[dateOnlyUTC(d)] = true
	}
	m := MonthStart(month)
	quota, err := MonthlyLeaveQuota(ctx, qtx, employeeID, m)
	if err != nil {
		return nil, err
	}
	leaveUsed, err := monthlyLeaveUsed(ctx, qtx, employeeID, m)
	if err != nil {
		return nil, err
	}
	rows, err := qtx.ListAbsentDatesForEmployee(ctx, &db.ListAbsentDatesForEmployeeParams{
		EmployeeID: employeeID,
		DateFrom:   pgtype.Date{Time: m, Valid: true},
		DateTo:     pgtype.Date{Time: m.AddDate(0, 1, 0), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	dates := make([]time.Time, 0, len(rows))
	for _, r := range rows {
		if !skip[dateOnlyUTC(r.Time)] {
			dates = append(dates, r.Time)
		}
	}
	return &AlphaMonth{Month: m, Quota: quota, LeaveUsed: leaveUsed, Days: AllocateAlpha(quota-leaveUsed, dates)}, nil
}

// AlphaPayCut is the pay lost to Alpha days in [from, to] that the allowance
// could not cover: days counts them, deduction = Σ cost × dailyRate.
func AlphaPayCut(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, dailyRate int64) (int, int64, error) {
	from, to = dateOnlyUTC(from), dateOnlyUTC(to)
	days, weight := 0, 0
	for m := MonthStart(from); !m.After(to); m = m.AddDate(0, 1, 0) {
		am, err := AlphaForMonth(ctx, qtx, employeeID, m)
		if err != nil {
			return 0, 0, err
		}
		for _, d := range am.Days {
			if d.Covered || d.Date.Before(from) || d.Date.After(to) {
				continue
			}
			days++
			weight += d.Cost
		}
	}
	return days, int64(weight) * dailyRate, nil
}
