package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Leave management business logic. The day-count and overlap helpers are DB-free
// and unit-tested; ApproveLeave / GetUnpaidLeaveDays walk the DB and are shared by
// the handler (approve endpoint) and prompt 08 payroll.

// ErrQuotaExceeded is matched (errors.Is) by every *QuotaExceededError, which
// ApproveLeave returns when a quota leave type would overdraw a month's allowance.
var ErrQuotaExceeded = errors.New("quota cuti tidak mencukupi")

// Employees work seven days a week, and their days off are paid leave drawn from
// a monthly allowance. A Saturday or Sunday off costs two days of it, because
// those are the days the outlets most need people. Unused allowance does not
// carry over; it is reported per month (MonthlyLeaveBalances) so evaluation can
// convert it to cash.
const (
	DefaultMonthlyLeaveQuota = 4
	WeekendLeaveCost         = 2
)

// QuotaExceededError names the month that would be overdrawn and by how much.
type QuotaExceededError struct {
	Month  time.Time
	Quota  int
	Used   int
	Needed int
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("jatah cuti %s tidak mencukupi: sisa %d hari, dibutuhkan %d hari",
		e.Month.Format("01/2006"), max(e.Quota-e.Used, 0), e.Needed)
}

func (e *QuotaExceededError) Is(target error) bool { return target == ErrQuotaExceeded }

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

// LeaveDayCost is what one day off costs against the monthly allowance.
func LeaveDayCost(d time.Time) int {
	if wd := isoWeekday(d); wd == 6 || wd == 7 {
		return WeekendLeaveCost
	}
	return 1
}

// MonthStart returns the first day of t's month (UTC, date only).
func MonthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// MonthCost is the allowance a set of leave days draws from one month.
type MonthCost struct {
	Month time.Time
	Cost  int
}

// QuotaCostByMonth splits the cost of days (in ascending order) across the
// months they fall in — a leave spanning month-end draws on both allowances.
func QuotaCostByMonth(days []time.Time) []MonthCost {
	var out []MonthCost
	for _, d := range days {
		m := MonthStart(d)
		if n := len(out); n > 0 && out[n-1].Month.Equal(m) {
			out[n-1].Cost += LeaveDayCost(d)
			continue
		}
		out = append(out, MonthCost{Month: m, Cost: LeaveDayCost(d)})
	}
	return out
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

// MonthlyLeaveQuota is the employee's allowance for month: the per-month
// override when one is set, otherwise DefaultMonthlyLeaveQuota.
func MonthlyLeaveQuota(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, month time.Time) (int, error) {
	q, err := qtx.GetLeaveMonthlyQuota(ctx, &db.GetLeaveMonthlyQuotaParams{
		EmployeeID: employeeID,
		Month:      pgtype.Date{Time: MonthStart(month), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultMonthlyLeaveQuota, nil
	}
	if err != nil {
		return 0, err
	}
	return int(q), nil
}

// monthlyLeaveUsed sums the allowance already consumed in month.
func monthlyLeaveUsed(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, month time.Time) (int, error) {
	m := MonthStart(month)
	used, err := qtx.SumLeaveQuotaUsage(ctx, &db.SumLeaveQuotaUsageParams{
		EmployeeID: employeeID,
		From:       pgtype.Date{Time: m, Valid: true},
		To:         pgtype.Date{Time: m.AddDate(0, 1, 0), Valid: true},
	})
	return int(used), err
}

// MonthlyLeaveBalance is one month of an employee's paid-leave allowance.
// UnusedDays is final once Closed (the month has ended) and provisional before.
type MonthlyLeaveBalance struct {
	Month      string `json:"month"` // YYYY-MM
	QuotaDays  int    `json:"quota_days"`
	UsedDays   int    `json:"used_days"`
	UnusedDays int    `json:"unused_days"`
	Closed     bool   `json:"closed"`
	Overridden bool   `json:"overridden"`
}

// NewMonthlyLeaveBalance derives the unused/closed fields for one month.
func NewMonthlyLeaveBalance(month time.Time, quota, used int, overridden bool, today time.Time) MonthlyLeaveBalance {
	m := MonthStart(month)
	return MonthlyLeaveBalance{
		Month:      m.Format("2006-01"),
		QuotaDays:  quota,
		UsedDays:   used,
		UnusedDays: max(quota-used, 0),
		Closed:     m.Before(MonthStart(dateOnlyUTC(today))),
		Overridden: overridden,
	}
}

// MonthlyLeaveBalances returns all twelve months of year for the employee.
// today decides which months are closed.
func MonthlyLeaveBalances(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, year int, today time.Time) ([]MonthlyLeaveBalance, error) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	rows, err := qtx.ListLeaveQuotaMonths(ctx, &db.ListLeaveQuotaMonthsParams{
		EmployeeID: employeeID,
		From:       pgtype.Date{Time: from, Valid: true},
		To:         pgtype.Date{Time: from.AddDate(1, 0, 0), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	byMonth := map[time.Month]*db.ListLeaveQuotaMonthsRow{}
	for _, r := range rows {
		byMonth[r.Month.Time.Month()] = r
	}
	out := make([]MonthlyLeaveBalance, 12)
	for i := range out {
		m := from.AddDate(0, i, 0)
		quota, used, overridden := DefaultMonthlyLeaveQuota, 0, false
		if r := byMonth[m.Month()]; r != nil {
			used = int(r.UsedDays)
			if r.QuotaDays.Valid {
				quota, overridden = int(r.QuotaDays.Int32), true
			}
		}
		out[i] = NewMonthlyLeaveBalance(m, quota, used, overridden, today)
	}
	return out, nil
}

// ApproveLeave sets the request to approved and upserts 'leave' attendance rows
// for each covered work day — skipping days that already carry a check-in (their
// dates are returned). For a quota leave type it first checks every month the
// covered days fall in against that month's allowance, then records the per-day
// cost in leave_quota_usage. A day the employee turned up for is not charged.
// Must run inside a transaction (qtx).
func ApproveLeave(ctx context.Context, qtx *db.Queries, req *db.LeaveRequest, leaveType *db.LeaveType, deciderID pgtype.UUID, note pgtype.Text) (*ApproveResult, error) {
	start := req.StartDate.Time
	end := req.EndDate.Time

	workDays := LeaveScheduleForBranch(ctx, qtx, branchForEmployee(ctx, qtx, req.EmployeeID))
	holidays, err := HolidaySetInRange(ctx, qtx, start, end)
	if err != nil {
		return nil, err
	}

	res := &ApproveResult{SkippedDays: []string{}}
	var covered []time.Time
	for _, d := range WorkingDaysInRange(start, end, workDays, holidays) {
		hasCheckIn, err := qtx.HasCheckInOnDate(ctx, &db.HasCheckInOnDateParams{
			EmployeeID: req.EmployeeID,
			Date:       pgtype.Date{Time: d, Valid: true},
		})
		if err != nil {
			return nil, err
		}
		if hasCheckIn {
			res.SkippedDays = append(res.SkippedDays, d.Format("2006-01-02"))
			continue
		}
		covered = append(covered, d)
	}

	if leaveType.UsesQuota {
		for _, mc := range QuotaCostByMonth(covered) {
			quota, err := MonthlyLeaveQuota(ctx, qtx, req.EmployeeID, mc.Month)
			if err != nil {
				return nil, err
			}
			used, err := monthlyLeaveUsed(ctx, qtx, req.EmployeeID, mc.Month)
			if err != nil {
				return nil, err
			}
			if !QuotaSufficient(quota, used, mc.Cost) {
				return nil, &QuotaExceededError{Month: mc.Month, Quota: quota, Used: used, Needed: mc.Cost}
			}
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
	res.Request = updated

	for _, d := range covered {
		pgDate := pgtype.Date{Time: d, Valid: true}
		if err := qtx.UpsertLeaveAttendance(ctx, &db.UpsertLeaveAttendanceParams{
			EmployeeID: req.EmployeeID,
			Date:       pgDate,
		}); err != nil {
			return nil, err
		}
		if leaveType.UsesQuota {
			if err := qtx.InsertLeaveQuotaUsage(ctx, &db.InsertLeaveQuotaUsageParams{
				LeaveRequestID: req.ID,
				EmployeeID:     req.EmployeeID,
				Date:           pgDate,
				Cost:           int32(LeaveDayCost(d)),
			}); err != nil {
				return nil, err
			}
		}
		res.CoveredDays++
	}

	return res, nil
}

// CancelApprovedLeave reverses an approved leave for the days still ahead: it
// refunds their allowance and removes the 'leave' attendance rows it created for
// them (those without a check-in). Days up to and including today stay consumed
// and keep their attendance history — otherwise cancelling old leave would
// inflate the unused balance that evaluation pays out. today is the reference
// "now" date (UTC). Must run inside a transaction.
func CancelApprovedLeave(ctx context.Context, qtx *db.Queries, req *db.LeaveRequest, leaveType *db.LeaveType, today time.Time) error {
	start := req.StartDate.Time
	end := req.EndDate.Time
	today = dateOnlyUTC(today)

	if leaveType.UsesQuota {
		if err := qtx.DeleteLeaveQuotaUsageAfter(ctx, &db.DeleteLeaveQuotaUsageAfterParams{
			LeaveRequestID: req.ID,
			Date:           pgtype.Date{Time: today, Valid: true},
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

// branchForEmployee looks up an employee's branch_id (zero UUID on miss → default
// schedule downstream).
func branchForEmployee(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID) pgtype.UUID {
	emp, err := qtx.GetEmployeeByID(ctx, employeeID)
	if err != nil || emp == nil {
		return pgtype.UUID{}
	}
	return emp.BranchID
}
