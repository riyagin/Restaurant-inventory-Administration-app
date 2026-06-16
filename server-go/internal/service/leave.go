package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Leave management business logic. The day-count and overlap helpers are DB-free
// and unit-tested; ApproveLeave / GetUnpaidLeaveDays walk the DB and are shared by
// the handler (approve endpoint) and prompt 08 payroll.

// ErrQuotaExceeded is returned by ApproveLeave when an annual (quota) leave type
// would push used_days past quota_days. The handler maps it to a 400 with a clear
// Indonesian message.
var ErrQuotaExceeded = errors.New("quota cuti tidak mencukupi")

// ── Pure helpers (DB-free, testable) ─────────────────────────────────────────

// CountWorkingDays counts the days in [start, end] (inclusive) whose ISO weekday
// (1=Mon..7=Sun) is in workDays AND that are not public holidays. holidays keys
// are "YYYY-MM-DD" dates. Pure and deterministic for unit testing.
func CountWorkingDays(start, end time.Time, workDays []int, holidays map[string]bool) int {
	start = dateOnlyUTC(start)
	end = dateOnlyUTC(end)
	if end.Before(start) {
		return 0
	}
	count := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if !isWorkDay(isoWeekday(d), workDays) {
			continue
		}
		if holidays != nil && holidays[d.Format("2006-01-02")] {
			continue
		}
		count++
	}
	return count
}

// WorkingDaysInRange returns the actual list of working-day dates in [start, end]
// (inclusive) — used by the approve flow to decide which attendance rows to upsert.
func WorkingDaysInRange(start, end time.Time, workDays []int, holidays map[string]bool) []time.Time {
	start = dateOnlyUTC(start)
	end = dateOnlyUTC(end)
	var out []time.Time
	if end.Before(start) {
		return out
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if !isWorkDay(isoWeekday(d), workDays) {
			continue
		}
		if holidays != nil && holidays[d.Format("2006-01-02")] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// RangesOverlap reports whether [aStart, aEnd] intersects [bStart, bEnd]
// (inclusive). Pure predicate used by the create-time overlap check.
func RangesOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	aStart = dateOnlyUTC(aStart)
	aEnd = dateOnlyUTC(aEnd)
	bStart = dateOnlyUTC(bStart)
	bEnd = dateOnlyUTC(bEnd)
	return !aStart.After(bEnd) && !bStart.After(aEnd)
}

// QuotaSufficient reports whether quota - used >= needed.
func QuotaSufficient(quota, used, needed int) bool {
	return quota-used >= needed
}

// dateOnlyUTC zeroes the time-of-day in UTC.
func dateOnlyUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ── DB-touching helpers ──────────────────────────────────────────────────────

// LeaveScheduleForBranch loads the branch work schedule (falling back to the
// default schedule when none exists) and returns its ISO work-day list.
func LeaveScheduleForBranch(ctx context.Context, qtx *db.Queries, branchID pgtype.UUID) []int {
	ws, err := qtx.GetWorkScheduleByBranch(ctx, branchID)
	if err != nil || ws == nil {
		return DefaultSchedule().WorkDays
	}
	return ScheduleFromRow(ws).WorkDays
}

// HolidaySetInRange loads public holidays in [start, end] into a lookup set keyed
// by "YYYY-MM-DD".
func HolidaySetInRange(ctx context.Context, qtx *db.Queries, start, end time.Time) (map[string]bool, error) {
	rows, err := qtx.ListHolidaysInRange(ctx, &db.ListHolidaysInRangeParams{
		Date:   pgtype.Date{Time: dateOnlyUTC(start), Valid: true},
		Date_2: pgtype.Date{Time: dateOnlyUTC(end), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, d := range rows {
		if d.Valid {
			set[d.Time.Format("2006-01-02")] = true
		}
	}
	return set, nil
}

// ComputeLeaveDayCount computes the working-day count for a leave request given
// the employee's branch and date range (skips non-work days and public holidays).
func ComputeLeaveDayCount(ctx context.Context, qtx *db.Queries, branchID pgtype.UUID, start, end time.Time) (int, error) {
	workDays := LeaveScheduleForBranch(ctx, qtx, branchID)
	holidays, err := HolidaySetInRange(ctx, qtx, start, end)
	if err != nil {
		return 0, err
	}
	return CountWorkingDays(start, end, workDays, holidays), nil
}

// ApproveResult summarizes an approval, listing the work days whose attendance
// rows were skipped because the employee already had a check-in.
type ApproveResult struct {
	Request     *db.LeaveRequest `json:"request"`
	CoveredDays int              `json:"covered_days"`
	SkippedDays []string         `json:"skipped_days"`
}

// ApproveLeave validates quota (for quota leave types), sets the request to
// approved, increments quota used_days, and upserts 'leave' attendance rows for
// each covered work day — skipping days that already carry a check-in (their dates
// are returned). Must run inside a transaction (qtx).
func ApproveLeave(ctx context.Context, qtx *db.Queries, req *db.LeaveRequest, leaveType *db.LeaveType, deciderID pgtype.UUID, note pgtype.Text) (*ApproveResult, error) {
	start := req.StartDate.Time
	end := req.EndDate.Time
	year := int32(start.Year())

	// Quota check + increment for quota leave types (e.g. annual leave).
	if leaveType.UsesQuota {
		bal, err := ensureLeaveBalance(ctx, qtx, req.EmployeeID, year)
		if err != nil {
			return nil, err
		}
		if !QuotaSufficient(int(bal.QuotaDays), int(bal.UsedDays), int(req.DayCount)) {
			return nil, ErrQuotaExceeded
		}
		if _, err := qtx.IncrementLeaveBalanceUsed(ctx, &db.IncrementLeaveBalanceUsedParams{
			UsedDays:   req.DayCount,
			EmployeeID: req.EmployeeID,
			Year:       year,
		}); err != nil {
			return nil, err
		}
	}

	updated, err := qtx.SetLeaveRequestStatus(ctx, &db.SetLeaveRequestStatusParams{
		Status:       "approved",
		DecidedBy:    deciderID,
		DecisionNote: note,
		ID:           req.ID,
	})
	if err != nil {
		return nil, err
	}

	// Upsert leave attendance rows for each covered work day, skipping days that
	// already have a check-in.
	workDays := LeaveScheduleForBranch(ctx, qtx, branchForEmployee(ctx, qtx, req.EmployeeID))
	holidays, err := HolidaySetInRange(ctx, qtx, start, end)
	if err != nil {
		return nil, err
	}
	days := WorkingDaysInRange(start, end, workDays, holidays)

	res := &ApproveResult{Request: updated, SkippedDays: []string{}}
	for _, d := range days {
		pgDate := pgtype.Date{Time: d, Valid: true}
		hasCheckIn, err := qtx.HasCheckInOnDate(ctx, &db.HasCheckInOnDateParams{
			EmployeeID: req.EmployeeID,
			Date:       pgDate,
		})
		if err != nil {
			return nil, err
		}
		if hasCheckIn {
			res.SkippedDays = append(res.SkippedDays, d.Format("2006-01-02"))
			continue
		}
		if err := qtx.UpsertLeaveAttendance(ctx, &db.UpsertLeaveAttendanceParams{
			EmployeeID: req.EmployeeID,
			Date:       pgDate,
		}); err != nil {
			return nil, err
		}
		res.CoveredDays++
	}

	return res, nil
}

// CancelApprovedLeave reverses an approved leave: for a quota leave type whose
// range is entirely in the future, it decrements used_days; it also removes the
// 'leave' attendance rows it created for covered work days that carry no check-in.
// today is the reference "now" date (UTC). Must run inside a transaction.
func CancelApprovedLeave(ctx context.Context, qtx *db.Queries, req *db.LeaveRequest, leaveType *db.LeaveType, today time.Time) error {
	start := req.StartDate.Time
	end := req.EndDate.Time
	today = dateOnlyUTC(today)

	// Decrement quota only when the request is for a quota type AND has not started
	// yet (entirely future) — past/ongoing approved leave keeps its consumed quota.
	if leaveType.UsesQuota && dateOnlyUTC(start).After(today) {
		if _, err := qtx.IncrementLeaveBalanceUsed(ctx, &db.IncrementLeaveBalanceUsedParams{
			UsedDays:   -req.DayCount,
			EmployeeID: req.EmployeeID,
			Year:       int32(start.Year()),
		}); err != nil {
			return err
		}
	}

	// Remove leave attendance rows for future covered days that have no check-in.
	workDays := LeaveScheduleForBranch(ctx, qtx, branchForEmployee(ctx, qtx, req.EmployeeID))
	holidays, err := HolidaySetInRange(ctx, qtx, start, end)
	if err != nil {
		return err
	}
	for _, d := range WorkingDaysInRange(start, end, workDays, holidays) {
		if !d.After(today) {
			continue // don't touch past/ongoing attendance history
		}
		if err := qtx.DeleteLeaveAttendanceWithoutCheckIn(ctx, &db.DeleteLeaveAttendanceWithoutCheckInParams{
			EmployeeID: req.EmployeeID,
			Date:       pgtype.Date{Time: d, Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

// GetUnpaidLeaveDays counts approved unpaid-leave (leave_type.is_paid=false)
// working days that overlap [from, to] for the employee. Prompt 08 payroll calls
// this to deduct daily_rate × days. from/to are inclusive period bounds.
func GetUnpaidLeaveDays(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time) (int, error) {
	from = dateOnlyUTC(from)
	to = dateOnlyUTC(to)

	rows, err := qtx.ListApprovedUnpaidLeaveOverlapping(ctx, &db.ListApprovedUnpaidLeaveOverlappingParams{
		EmployeeID: employeeID,
		StartDate:  pgtype.Date{Time: from, Valid: true},
		EndDate:    pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	workDays := LeaveScheduleForBranch(ctx, qtx, branchForEmployee(ctx, qtx, employeeID))
	holidays, err := HolidaySetInRange(ctx, qtx, from, to)
	if err != nil {
		return 0, err
	}

	total := 0
	for _, r := range rows {
		// Clamp each request's range to the requested [from, to] window.
		s := dateOnlyUTC(r.StartDate.Time)
		e := dateOnlyUTC(r.EndDate.Time)
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		total += CountWorkingDays(s, e, workDays, holidays)
	}
	return total, nil
}

// ensureLeaveBalance returns the employee's balance for the year, creating it with
// the default quota (12) when missing.
func ensureLeaveBalance(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, year int32) (*db.LeaveBalance, error) {
	bal, err := qtx.GetLeaveBalance(ctx, &db.GetLeaveBalanceParams{EmployeeID: employeeID, Year: year})
	if err == nil {
		return bal, nil
	}
	return qtx.CreateLeaveBalance(ctx, &db.CreateLeaveBalanceParams{
		EmployeeID: employeeID,
		Year:       year,
		QuotaDays:  12,
	})
}

// branchForEmployee looks up an employee's branch_id (zero UUID on miss → default
// schedule downstream).
func branchForEmployee(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID) pgtype.UUID {
	emp, err := qtx.GetEmployeeByID(ctx, employeeID)
	if err != nil || emp == nil {
		return pgtype.UUID{}
	}
	return emp.BranchID
}
