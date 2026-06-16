package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// ReconcileResult summarizes a reconciliation run.
type ReconcileResult struct {
	Date          string `json:"date"`
	AbsentCreated int    `json:"absent_created"`
	Skipped       int    `json:"skipped"`
}

// ScheduleFromRow converts a db.WorkSchedule into the pure Schedule used by the
// anomaly/reconcile logic. pgtype.Time stores microseconds since midnight.
func ScheduleFromRow(ws *db.WorkSchedule) Schedule {
	days := make([]int, 0, len(ws.WorkDays))
	for _, d := range ws.WorkDays {
		days = append(days, int(d))
	}
	return Schedule{
		WorkStartMinutes:  pgTimeToMinutes(ws.WorkStart),
		WorkEndMinutes:    pgTimeToMinutes(ws.WorkEnd),
		GraceMinutes:      int(ws.GraceMinutes),
		EarlyLeaveMinutes: int(ws.EarlyLeaveMinutes),
		WorkDays:          days,
	}
}

// DefaultSchedule is used when a branch has no configured work_schedule row.
func DefaultSchedule() Schedule {
	return Schedule{
		WorkStartMinutes:  8 * 60,
		WorkEndMinutes:    17 * 60,
		GraceMinutes:      15,
		EarlyLeaveMinutes: 30,
		WorkDays:          []int{1, 2, 3, 4, 5, 6},
	}
}

// pgTimeToMinutes converts a pgtype.Time (microseconds since midnight) to whole
// minutes since midnight.
func pgTimeToMinutes(t pgtype.Time) int {
	if !t.Valid {
		return 0
	}
	return int(t.Microseconds / 1_000_000 / 60)
}

// ReconcileAbsent inserts 'absent' records for every active employee scheduled to
// work on `date` who has no existing record and whose branch counts that ISO
// weekday as a work day (and the date is not a public holiday). It is shared by
// the nightly goroutine and the manual POST /reconcile endpoint.
//
// The per-employee decision is delegated to the pure shouldMarkAbsent predicate
// so it stays unit-testable; this function only does the DB walking.
func ReconcileAbsent(ctx context.Context, q *db.Queries, date time.Time) (*ReconcileResult, error) {
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	pgDate := pgtype.Date{Time: dateOnly, Valid: true}
	res := &ReconcileResult{Date: dateOnly.Format("2006-01-02")}

	holiday, err := q.IsPublicHoliday(ctx, pgDate)
	if err != nil {
		return nil, err
	}

	employees, err := q.ListActiveEmployeesForReconcile(ctx)
	if err != nil {
		return nil, err
	}

	// Cache per-branch schedules to avoid repeated lookups.
	schedCache := map[string]Schedule{}
	getSched := func(branchID pgtype.UUID) Schedule {
		key := branchID.Bytes
		ks := string(key[:])
		if s, ok := schedCache[ks]; ok {
			return s
		}
		ws, err := q.GetWorkScheduleByBranch(ctx, branchID)
		var s Schedule
		if err != nil || ws == nil {
			s = DefaultSchedule()
		} else {
			s = ScheduleFromRow(ws)
		}
		schedCache[ks] = s
		return s
	}

	weekday := isoWeekday(dateOnly)

	for _, e := range employees {
		sched := getSched(e.BranchID)

		// Does a record already exist for this employee+date?
		hasRecord := true
		existingStatus := ""
		rec, err := q.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{
			EmployeeID: e.ID,
			Date:       pgDate,
		})
		if err != nil {
			// pgx.ErrNoRows (or any read miss) => treat as no record.
			hasRecord = false
		} else {
			existingStatus = rec.Status
		}

		if !shouldMarkAbsent(weekday, sched.WorkDays, holiday, hasRecord, existingStatus) {
			res.Skipped++
			continue
		}

		if err := q.InsertAbsentRecord(ctx, &db.InsertAbsentRecordParams{
			EmployeeID: e.ID,
			Date:       pgDate,
		}); err != nil {
			return nil, err
		}
		res.AbsentCreated++
	}

	return res, nil
}
