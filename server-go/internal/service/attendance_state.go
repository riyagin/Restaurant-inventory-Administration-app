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
		IsHalfDay:          rec.IsHalfDay,
		HalfDayLostMinutes: int(rec.HalfDayLostMinutes),
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
	p.IsHalfDay = s.IsHalfDay
	p.HalfDayLostMinutes = int32(s.HalfDayLostMinutes)
}

// DayIsOver reports whether the work day for `date` has ended relative to `now`.
// Used to decide is_missing_checkout. A day is "over" once we're past its end.
func DayIsOver(date time.Time, sched Schedule, now time.Time) bool {
	end := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, now.Location()).
		Add(time.Duration(sched.WorkEndMinutes) * time.Minute)
	return now.After(end)
}
