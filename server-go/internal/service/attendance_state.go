package service

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// StateFromRecord converts a persisted attendance record into the pure
// AttendanceState used by the merge/anomaly logic.
func StateFromRecord(rec *db.AttendanceRecord) *AttendanceState {
	s := &AttendanceState{
		Status:            rec.Status,
		CheckInSource:     textVal(rec.CheckInSource),
		CheckOutSource:    textVal(rec.CheckOutSource),
		IsLate:            rec.IsLate,
		LateMinutes:       int(rec.LateMinutes),
		IsEarlyLeave:       rec.IsEarlyLeave,
		EarlyLeaveMinutes:  int(rec.EarlyLeaveMinutes),
		IsMissingCheckout:  rec.IsMissingCheckout,
		IsMissingCheckin:   rec.IsMissingCheckin,
		IsNoPunch:          rec.IsNoPunch,
		IsHalfDay:          rec.IsHalfDay,
		HalfDayLostMinutes: int(rec.HalfDayLostMinutes),
		HalfDayType:        textVal(rec.HalfDayType),
		IsLeavePunch:       rec.IsLeavePunch,
		ExcusedBy:          textVal(rec.ExcusedBy),
		ReviewResolved:     rec.ReviewResolved,
	}
	if rec.Date.Valid {
		s.Date = rec.Date.Time
	}
	if rec.CheckIn.Valid {
		t := rec.CheckIn.Time
		s.CheckIn = &t
	}
	if rec.CheckOut.Valid {
		t := rec.CheckOut.Time
		s.CheckOut = &t
	}
	return s
}

// EmptyState returns a fresh present-state for a new day record.
func EmptyState() *AttendanceState {
	return &AttendanceState{Status: "present"}
}

func textVal(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func tsOrNull(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func srcOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// FillInsertParams populates the timestamp/source/anomaly fields of an insert
// params struct from a state (caller sets EmployeeID/Date/Device/Photo).
func FillInsertParams(p *db.InsertAttendanceRecordParams, s *AttendanceState) {
	p.CheckIn = tsOrNull(s.CheckIn)
	p.CheckOut = tsOrNull(s.CheckOut)
	p.CheckInSource = srcOrNull(s.CheckInSource)
	p.CheckOutSource = srcOrNull(s.CheckOutSource)
	p.Status = s.Status
	p.IsLate = s.IsLate
	p.LateMinutes = int32(s.LateMinutes)
	p.IsEarlyLeave = s.IsEarlyLeave
	p.EarlyLeaveMinutes = int32(s.EarlyLeaveMinutes)
	p.IsMissingCheckout = s.IsMissingCheckout
	p.IsMissingCheckin = s.IsMissingCheckin
	p.IsNoPunch = s.IsNoPunch
	p.IsLeavePunch = s.IsLeavePunch
	p.IsHalfDay = s.IsHalfDay
	p.HalfDayLostMinutes = int32(s.HalfDayLostMinutes)
	p.HalfDayType = srcOrNull(s.HalfDayType)
	p.ExcusedBy = srcOrNull(s.ExcusedBy)
}

// FillUpdateParams populates the timestamp/source/anomaly/status fields of an
// update params struct from a state (caller sets ID and any photo/device/note).
func FillUpdateParams(p *db.UpdateAttendanceRecordParams, s *AttendanceState) {
	p.CheckIn = tsOrNull(s.CheckIn)
	p.CheckOut = tsOrNull(s.CheckOut)
	p.CheckInSource = srcOrNull(s.CheckInSource)
	p.CheckOutSource = srcOrNull(s.CheckOutSource)
	p.Status = s.Status
	p.IsLate = s.IsLate
	p.LateMinutes = int32(s.LateMinutes)
	p.IsEarlyLeave = s.IsEarlyLeave
	p.EarlyLeaveMinutes = int32(s.EarlyLeaveMinutes)
	p.IsMissingCheckout = s.IsMissingCheckout
	p.IsMissingCheckin = s.IsMissingCheckin
	p.IsNoPunch = s.IsNoPunch
	p.IsHalfDay = s.IsHalfDay
	p.HalfDayLostMinutes = int32(s.HalfDayLostMinutes)
	p.HalfDayType = srcOrNull(s.HalfDayType)
	p.IsLeavePunch = s.IsLeavePunch
	p.ExcusedBy = srcOrNull(s.ExcusedBy)
}

// DayIsOver reports whether the work day for `date` (a WIB calendar date) has
// ended relative to `now`.
// Used to decide is_missing_checkout. A day is "over" once we're past its end.
func DayIsOver(date time.Time, sched Schedule, now time.Time) bool {
	// Not before the check-out window closes: a check-out up to PunchWindowAfter
	// past the shift end (e.g. 00:20 after a 22:00 shift) still belongs to the day.
	return now.After(WIBWallClock(date, sched.WorkEndMinutes+PunchWindowAfterMinutes))
}
