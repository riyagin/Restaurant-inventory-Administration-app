package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Shift system. A branch owns its own shift definitions; manpower planning
// rosters one per employee per day (shift_assignments). Only the *approved*
// roster lives in shift_assignments — a change to today's roster made by
// someone who cannot approve waits in shift_change_requests — so anything that
// reads the roster here reads what a manager signed off on.

// ShiftWorkMinutes is the paid length of a shift: the span of each segment
// minus the break allowance. Times are minutes since midnight; segment two is
// optional (a "jumping" shift works a morning block and an evening block).
func ShiftWorkMinutes(start1, end1 int, start2, end2 *int, breakMinutes int) int {
	total := end1 - start1
	if start2 != nil && end2 != nil {
		total += *end2 - *start2
	}
	total -= breakMinutes
	if total < 0 {
		return 0
	}
	return total
}

// ScheduleWithShift returns the branch schedule with its start and end replaced
// by the employee's rostered shift for the date, so late arrival and early
// leave are measured against the shift they were actually on. A split shift is
// judged from the start of its first segment to the end of its last: attendance
// holds one check-in and one check-out per day, so the mid-day gap is not a
// departure. With no assignment (or on any read error) the branch schedule is
// returned unchanged — rostering is additive, never a reason to lose the
// default.
func ScheduleWithShift(ctx context.Context, q db.DBTX, sched Schedule, employeeID pgtype.UUID, date time.Time) Schedule {
	return ScheduleWithShiftQ(ctx, db.New(q), sched, employeeID, date)
}

// ScheduleWithShiftQ is ScheduleWithShift for callers holding a *db.Queries.
// For a split shift it also records when the second segment starts
// (ResumeMinutes): a punch near that time is the return from the break, not a
// check-out (see ResolveDay). Rostered and branch hours are kept apart through
// Rostered, so a day with no roster can be flagged.
func ScheduleWithShiftQ(ctx context.Context, q *db.Queries, sched Schedule, employeeID pgtype.UUID, date time.Time) Schedule {
	sched.Rostered = false
	sched.ResumeMinutes = 0
	if !employeeID.Valid {
		return sched
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	row, err := q.GetShiftHoursForEmployeeDate(ctx, &db.GetShiftHoursForEmployeeDateParams{
		EmployeeID: employeeID,
		Date:       pgtype.Date{Time: day, Valid: true},
	})
	if err != nil || !row.Start1.Valid || !row.End1.Valid {
		return sched
	}
	sched.WorkStartMinutes = pgTimeToMinutes(row.Start1)
	sched.WorkEndMinutes = pgTimeToMinutes(row.End1)
	if row.Start2.Valid && row.End2.Valid {
		sched.ResumeMinutes = pgTimeToMinutes(row.Start2)
		sched.WorkEndMinutes = pgTimeToMinutes(row.End2)
	}
	sched.Rostered = true
	return sched
}
