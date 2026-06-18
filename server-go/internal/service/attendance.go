package service

import (
	"time"
)

// Attendance core logic. These functions are deliberately DB-free and operate on
// plain Go values so they can be unit-tested without a database. The handlers and
// the nightly reconciliation goroutine both call into this package.

// Schedule is the subset of a branch work_schedule the anomaly/reconcile logic
// needs. Times are wall-clock minutes-since-midnight to keep the math pure and
// independent of pgtype.Time internals.
type Schedule struct {
	WorkStartMinutes  int   // minutes since midnight, e.g. 08:00 -> 480
	WorkEndMinutes    int   // minutes since midnight, e.g. 17:00 -> 1020
	GraceMinutes      int   // late beyond work_start + grace = anomaly
	EarlyLeaveMinutes int   // leaving before work_end - early_leave = anomaly
	WorkDays          []int // ISO weekday numbers (1=Mon .. 7=Sun)
}

// AttendanceState is the mutable per-record state the anomaly/merge functions
// read and write. It mirrors the persisted columns but uses plain Go types.
type AttendanceState struct {
	CheckIn        *time.Time
	CheckOut       *time.Time
	CheckInSource  string // "", "face", "fingerprint", "manual"
	CheckOutSource string
	Status         string // present|absent|leave|holiday

	IsLate            bool
	LateMinutes       int
	IsEarlyLeave      bool
	EarlyLeaveMinutes int
	IsMissingCheckout bool
}

// minutesSinceMidnight returns the wall-clock minute offset of t within its day.
func minutesSinceMidnight(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// ComputeAnomalies (re)derives the anomaly flags for a record given its branch
// schedule. `dayIsOver` indicates the work day has ended (used to detect a
// missing check-out). It mutates and returns the same state for convenience.
//
//   - is_late          = check_in > work_start + grace (late_minutes counted from work_start)
//   - is_early_leave   = check_out < work_end - early_leave
//   - is_missing_checkout = has check_in, no check_out, and the day is over
//
// Only meaningful for a 'present' record. Absent/leave/holiday records carry no
// anomaly flags.
func ComputeAnomalies(s *AttendanceState, sched Schedule, dayIsOver bool) *AttendanceState {
	// Reset before recomputing so corrections clear stale flags.
	s.IsLate = false
	s.LateMinutes = 0
	s.IsEarlyLeave = false
	s.EarlyLeaveMinutes = 0
	s.IsMissingCheckout = false

	if s.Status != "present" {
		return s
	}

	if s.CheckIn != nil {
		inMin := minutesSinceMidnight(*s.CheckIn)
		if inMin > sched.WorkStartMinutes+sched.GraceMinutes {
			s.IsLate = true
			s.LateMinutes = inMin - sched.WorkStartMinutes
		}
	}

	if s.CheckOut != nil {
		outMin := minutesSinceMidnight(*s.CheckOut)
		threshold := sched.WorkEndMinutes - sched.EarlyLeaveMinutes
		if outMin < threshold {
			s.IsEarlyLeave = true
			s.EarlyLeaveMinutes = sched.WorkEndMinutes - outMin
		}
	}

	if s.CheckIn != nil && s.CheckOut == nil && dayIsOver {
		s.IsMissingCheckout = true
	}

	return s
}

// ComputeOvertimeMinutes returns the minutes a present-day check-out falls past
// the branch's scheduled work_end (0 when there's no check-out or it's at/before
// work_end). Mirrors the is_early_leave check in ComputeAnomalies but in the
// opposite direction; used to seed payroll's auto-computed overtime hours from
// attendance instead of requiring a fully manual entry.
func ComputeOvertimeMinutes(checkOut *time.Time, sched Schedule) int {
	if checkOut == nil {
		return 0
	}
	outMin := minutesSinceMidnight(*checkOut)
	if outMin <= sched.WorkEndMinutes {
		return 0
	}
	return outMin - sched.WorkEndMinutes
}

const dedupWindow = 5 * time.Minute

// AttendanceEvent is a single incoming punch to be merged into a day record.
type AttendanceEvent struct {
	Timestamp time.Time
	Source    string // "face" | "fingerprint" | "manual"
	Direction string // "check_in" | "check_out" (already resolved; "auto" handled by caller)
}

// sourceWins reports whether an incoming source may overwrite an existing value.
// Face is primary and always wins. Fingerprint may only fill a field that is
// empty or currently fingerprint-sourced; it never overwrites a face value.
// Manual is set explicitly by admins and is treated like face (wins).
func sourceWins(existingSource, incomingSource string) bool {
	if incomingSource == "face" || incomingSource == "manual" {
		return true
	}
	// incoming == fingerprint
	if existingSource == "" || existingSource == "fingerprint" {
		return true
	}
	return false
}

// MergeAttendanceEvent folds an incoming event into the existing day state.
// Rules:
//   - First event of the day becomes check_in, the last becomes check_out.
//   - An event within 5 minutes of an existing same-direction event with a
//     source of equal-or-higher precedence is deduplicated (ignored).
//   - Face/manual always win; fingerprint only fills empty or fingerprint fields.
//
// It returns the updated state (same pointer). Anomalies are NOT recomputed here;
// the caller runs ComputeAnomalies afterwards.
func MergeAttendanceEvent(s *AttendanceState, ev AttendanceEvent) *AttendanceState {
	if s.Status == "" {
		s.Status = "present"
	} else if s.Status != "present" {
		// A real punch overrides absent/holiday; leave stays leave only if the
		// caller decides — here a physical event means the person was present.
		s.Status = "present"
	}

	switch ev.Direction {
	case "check_in":
		applyDirection(&s.CheckIn, &s.CheckInSource, ev)
	case "check_out":
		applyDirection(&s.CheckOut, &s.CheckOutSource, ev)
	}
	return s
}

// applyDirection applies an event to one direction slot (check-in or check-out),
// enforcing the dedup window + source precedence rules.
func applyDirection(slot **time.Time, slotSource *string, ev AttendanceEvent) {
	if *slot == nil {
		t := ev.Timestamp
		*slot = &t
		*slotSource = ev.Source
		return
	}

	// Dedup: same-direction event within the window. A higher-precedence source
	// (face/manual over fingerprint) is allowed to replace the value even inside
	// the window; an equal/lower source within the window is dropped.
	within := absDuration(ev.Timestamp.Sub(**slot)) <= dedupWindow
	if within && !sourceUpgrades(*slotSource, ev.Source) {
		return
	}

	if sourceWins(*slotSource, ev.Source) {
		t := ev.Timestamp
		*slot = &t
		*slotSource = ev.Source
	}
}

// sourceUpgrades reports whether incoming is strictly higher precedence than
// existing (used to allow replacement inside the dedup window).
func sourceUpgrades(existing, incoming string) bool {
	rank := func(s string) int {
		switch s {
		case "manual":
			return 3
		case "face":
			return 2
		case "fingerprint":
			return 1
		default:
			return 0
		}
	}
	return rank(incoming) > rank(existing)
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// shouldMarkAbsent is the pure decision predicate used by ReconcileAbsent. An
// employee is marked absent for `date` when ALL of:
//   - the date's ISO weekday is in the branch work_days, AND
//   - the date is not a public holiday, AND
//   - there is no existing record for that employee+date (hasRecord=false), AND
//   - the existing record (if any) is not a 'leave' record.
//
// hasRecord and existingStatus describe the current attendance_records row (if
// any). When hasRecord is false the status is ignored.
func shouldMarkAbsent(isoWeekday int, workDays []int, isHoliday, hasRecord bool, existingStatus string) bool {
	if isHoliday {
		return false
	}
	if !isWorkDay(isoWeekday, workDays) {
		return false
	}
	if hasRecord {
		// Already recorded (present/leave/absent/holiday) — never overwrite.
		return false
	}
	_ = existingStatus
	return true
}

// isWorkDay reports whether the ISO weekday (1=Mon..7=Sun) is a scheduled work day.
func isWorkDay(isoWeekday int, workDays []int) bool {
	for _, d := range workDays {
		if d == isoWeekday {
			return true
		}
	}
	return false
}

// isoWeekday converts Go's time.Weekday (Sunday=0) to ISO (Monday=1..Sunday=7).
func isoWeekday(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}
