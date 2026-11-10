package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Punch log → work day → check-in / check-out.
//
// Every punch is kept in attendance_punches. A day's check-in and check-out are
// DERIVED from its punches, using the employee's shift — not a fixed 12:00 split
// and not the calendar day:
//
//   - A punch belongs to the work day whose shift window holds it: from
//     PunchWindowBefore minutes before the shift starts to PunchWindowAfter
//     minutes after it ends. A 00:20 check-out after a 22:00 shift belongs to
//     the previous day.
//   - The first punch of the day is the check-in and the last the check-out. An
//     employee arriving at 12:40 for an 08:30 shift is checked in at 12:40 —
//     250 minutes late — not "checked out".
//   - A lone punch (or one burst of taps) is a check-in or a check-out depending
//     on which end of the shift it is closer to, unless the device said which.
//   - On a split shift the punch back from the break (near the second segment's
//     start) is not a check-out: if it is the last punch, the check-out is
//     missing.
//
// Manual corrections are overrides: a slot set by a manager is never replaced
// by a rebuild. Approved permits (attendance_permissions) are applied last.

const (
	PunchWindowBeforeMinutes = 180
	PunchWindowAfterMinutes  = 240
	// Taps this close together are one punch (double tap, retry).
	samePunchMinutes = 5
	// A punch this close to a split shift's second-segment start is the return
	// from the break.
	splitResumeToleranceMinutes = 30
)

// Punch is one logged punch.
type Punch struct {
	At     time.Time
	Source string // fingerprint | face | manual
	Hint   string // "", check_in, check_out (what the device said, if anything)
}

func shiftEnd(s Schedule) int {
	if s.WorkEndMinutes <= s.WorkStartMinutes {
		return s.WorkEndMinutes + 24*60 // overnight shift
	}
	return s.WorkEndMinutes
}

// punchWindow is the span (minutes from the work day's WIB midnight) whose
// punches belong to that day.
func punchWindow(s Schedule) (lo, hi int) {
	return s.WorkStartMinutes - PunchWindowBeforeMinutes, shiftEnd(s) + PunchWindowAfterMinutes
}

func distanceToShift(rel int, s Schedule) int {
	switch {
	case rel < s.WorkStartMinutes:
		return s.WorkStartMinutes - rel
	case rel > shiftEnd(s):
		return rel - shiftEnd(s)
	}
	return 0
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// PickWorkDate returns the work day a punch belongs to — its own WIB calendar
// day or the day before — and that day's schedule. schedFor returns the
// employee's schedule (rostered shift, else branch hours) for a date.
func PickWorkDate(t time.Time, schedFor func(date time.Time) Schedule) (time.Time, Schedule) {
	today := WIBDate(t)
	prev := today.AddDate(0, 0, -1)
	sT, sP := schedFor(today), schedFor(prev)
	relT, relP := MinutesFromWorkDay(today, t), MinutesFromWorkDay(prev, t)
	loT, hiT := punchWindow(sT)
	loP, hiP := punchWindow(sP)
	inT := relT >= loT && relT <= hiT
	inP := relP >= loP && relP <= hiP
	if inP && (!inT || distanceToShift(relP, sP) < distanceToShift(relT, sT)) {
		return prev, sP
	}
	// Outside every window (e.g. a 03:00 punch on a day off) the punch is still
	// recorded, on its calendar day.
	return today, sT
}

// ResolveDay derives a day's check-in and check-out from its punches (sorted by
// time). Either may be nil.
func ResolveDay(punches []Punch, date time.Time, s Schedule) (in, out *Punch) {
	if len(punches) == 0 {
		return nil, nil
	}
	first, last := punches[0], punches[len(punches)-1]
	if last.At.Sub(first.At) >= samePunchMinutes*time.Minute {
		in, out = &first, &last
		if s.ResumeMinutes > 0 && absInt(MinutesFromWorkDay(date, last.At)-s.ResumeMinutes) <= splitResumeToleranceMinutes {
			out = nil // back from the break, then never punched out
		}
		return in, out
	}
	// One punch (or a burst of taps): the device's own direction if it gave one,
	// else whichever end of the shift is nearer.
	p := first
	switch p.Hint {
	case "check_in":
		return &p, nil
	case "check_out":
		return nil, &p
	}
	rel := MinutesFromWorkDay(date, p.At)
	if absInt(rel-s.WorkStartMinutes) <= absInt(rel-shiftEnd(s)) {
		return &p, nil
	}
	return nil, &p
}

// ApplyPermissions applies a day's approved permits (types) to its freshly
// computed anomalies. It first undoes the effect of a permit applied earlier
// (ExcusedBy), so a cancelled permit leaves no trace; a half day a manager set
// by hand (ExcusedBy empty) is left alone.
//
//   - off_site: no late / early / missing flags; a day without punches is present.
//   - late_arrival: a late arrival becomes a half day (type late) — the
//     half_day_late rule applies instead of the late tiers, and the lost minutes
//     are deducted through the half-day deduction.
//   - early_departure: same for an early departure (type early).
func ApplyPermissions(s *AttendanceState, perms []string) {
	if (s.ExcusedBy == "late_arrival" || s.ExcusedBy == "early_departure") && s.IsHalfDay {
		s.IsHalfDay, s.HalfDayLostMinutes, s.HalfDayType = false, 0, ""
	}
	s.ExcusedBy = ""
	if s.IsHalfDay {
		return // set by a manager
	}
	has := map[string]bool{}
	for _, p := range perms {
		has[p] = true
	}
	if has["off_site"] {
		switch s.Status {
		case "absent", "":
			s.Status = "present"
		case "present":
		default:
			return // leave / holiday stay as they are
		}
		s.IsLate, s.LateMinutes = false, 0
		s.IsEarlyLeave, s.EarlyLeaveMinutes = false, 0
		s.IsMissingCheckout, s.IsMissingCheckin, s.IsNoPunch = false, false, false
		s.ExcusedBy = "off_site"
		return
	}
	if s.Status != "present" {
		return
	}
	lost, typ, by := 0, "", ""
	if has["late_arrival"] && s.IsLate {
		lost, typ, by = s.LateMinutes, "late", "late_arrival"
	}
	if has["early_departure"] && s.IsEarlyLeave {
		lost += s.EarlyLeaveMinutes
		if typ == "" {
			typ, by = "early", "early_departure"
		}
	}
	if typ != "" {
		s.IsHalfDay, s.HalfDayType, s.HalfDayLostMinutes, s.ExcusedBy = true, typ, lost, by
	}
}

// ComputeDay recomputes a day's anomalies and applies its approved permits.
// Every path that recomputes a record (corrections, rebuilds) goes through here
// so a permit is never lost by an unrelated edit.
func ComputeDay(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, s *AttendanceState, sched Schedule) error {
	ComputeAnomalies(s, sched, DayIsOver(s.Date, sched, time.Now()))
	perms, err := q.ListApprovedPermissionTypes(ctx, &db.ListApprovedPermissionTypesParams{
		EmployeeID: employeeID,
		Date:       pgtype.Date{Time: dateOnly(s.Date), Valid: true},
	})
	if err != nil {
		return err
	}
	ApplyPermissions(s, perms)
	return nil
}

// ScheduleForEmployee is the employee's home-branch schedule (or the default)
// with that day's rostered shift applied.
func ScheduleForEmployee(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, date time.Time) Schedule {
	sched := DefaultSchedule()
	if emp, err := q.GetEmployeeByID(ctx, employeeID); err == nil && emp.BranchID.Valid {
		if ws, err := q.GetWorkScheduleByBranch(ctx, emp.BranchID); err == nil && ws != nil {
			sched = ScheduleFromRow(ws)
		}
	}
	return ScheduleWithShiftQ(ctx, q, sched, employeeID, date)
}

// LogPunch stores a punch and returns the work day it belongs to with that day's
// schedule. base is the branch schedule that governs the punch (the employee's
// branch, or the device's branch for a visitor check-in).
func LogPunch(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, p Punch, base Schedule, deviceID pgtype.UUID) (time.Time, Schedule, error) {
	day, sched := PickWorkDate(p.At, func(d time.Time) Schedule {
		return ScheduleWithShiftQ(ctx, q, base, employeeID, d)
	})
	hint := pgtype.Text{String: p.Hint, Valid: p.Hint != ""}
	err := q.InsertAttendancePunch(ctx, &db.InsertAttendancePunchParams{
		EmployeeID:    employeeID,
		PunchedAt:     pgtype.Timestamptz{Time: p.At, Valid: true},
		WorkDate:      pgtype.Date{Time: day, Valid: true},
		Source:        p.Source,
		DirectionHint: hint,
		DeviceID:      deviceID,
	})
	return day, sched, err
}

// RebuildOptions carries the record metadata a caller wants written.
type RebuildOptions struct {
	DeviceID pgtype.UUID // set when non-zero
	Note     string      // set when non-empty
}

// RebuildDay re-derives an employee's record for a work day from the punch log,
// keeping manual overrides, recomputes its anomalies and permits, and saves it.
// It returns (nil, nil, nil) when the day has neither punches, a record nor an
// off-site permit — there is nothing to write.
func RebuildDay(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, workDate time.Time, sched Schedule, opt RebuildOptions) (*AttendanceState, *db.AttendanceRecord, error) {
	pgDate := pgtype.Date{Time: dateOnly(workDate), Valid: true}
	rows, err := q.ListPunchesForWorkDay(ctx, &db.ListPunchesForWorkDayParams{EmployeeID: employeeID, WorkDate: pgDate})
	if err != nil {
		return nil, nil, err
	}
	punches := make([]Punch, 0, len(rows))
	for _, r := range rows {
		punches = append(punches, Punch{At: r.PunchedAt.Time, Source: r.Source, Hint: textVal(r.DirectionHint)})
	}

	existing, err := q.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{EmployeeID: employeeID, Date: pgDate})
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if !hasExisting && len(punches) == 0 {
		perms, err := q.ListApprovedPermissionTypes(ctx, &db.ListApprovedPermissionTypesParams{EmployeeID: employeeID, Date: pgDate})
		if err != nil {
			return nil, nil, err
		}
		offSite := false
		for _, p := range perms {
			offSite = offSite || p == "off_site"
		}
		if !offSite {
			return nil, nil, nil
		}
	}

	var s *AttendanceState
	if hasExisting {
		s = StateFromRecord(existing)
	} else {
		s = &AttendanceState{Status: "absent"}
	}
	s.Date = workDate

	in, out := ResolveDay(punches, workDate, sched)
	if s.CheckInSource != "manual" {
		s.CheckIn, s.CheckInSource = nil, ""
		if in != nil {
			t := in.At
			s.CheckIn, s.CheckInSource = &t, in.Source
		}
	}
	if s.CheckOutSource != "manual" {
		s.CheckOut, s.CheckOutSource = nil, ""
		if out != nil {
			t := out.At
			s.CheckOut, s.CheckOutSource = &t, out.Source
		}
	}
	// A punch makes an absent / holiday day present; leave stays leave (the
	// punch is flagged instead) and present stays present.
	if s.CheckIn != nil || s.CheckOut != nil {
		if s.Status != "leave" {
			s.Status = "present"
		}
	}
	if err := ComputeDay(ctx, q, employeeID, s, sched); err != nil {
		return nil, nil, err
	}

	if hasExisting {
		params := db.UpdateAttendanceRecordParams{
			CheckInPhotoPath: existing.CheckInPhotoPath,
			DeviceID:         existing.DeviceID,
			Note:             existing.Note,
			ID:               existing.ID,
		}
		if opt.DeviceID.Valid {
			params.DeviceID = opt.DeviceID
		}
		if opt.Note != "" {
			params.Note = pgtype.Text{String: opt.Note, Valid: true}
		}
		FillUpdateParams(&params, s)
		rec, err := q.UpdateAttendanceRecord(ctx, &params)
		return s, rec, err
	}
	params := db.InsertAttendanceRecordParams{EmployeeID: employeeID, Date: pgDate, DeviceID: opt.DeviceID}
	if opt.Note != "" {
		params.Note = pgtype.Text{String: opt.Note, Valid: true}
	}
	FillInsertParams(&params, s)
	rec, err := q.InsertAttendanceRecord(ctx, &params)
	return s, rec, err
}

// RecomputeDay rebuilds a past or current day after something outside the punch
// log changed it (a permit approved or cancelled) and, for a finished day,
// re-evaluates its violations and the monthly score.
func RecomputeDay(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, date time.Time) error {
	sched := ScheduleForEmployee(ctx, q, employeeID, date)
	_, rec, err := RebuildDay(ctx, q, employeeID, date, sched, RebuildOptions{})
	if err != nil || rec == nil {
		return err
	}
	if dateOnly(date).Before(TodayWIB()) {
		return DeleteAutoViolationsForRecord(ctx, q, rec)
	}
	return nil
}
