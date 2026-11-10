package service

import "testing"

// A punch on an approved leave day keeps the leave and flags the day; it is not
// scored as a late/early working day.
func TestMergeAttendanceEvent_PunchOnLeaveKeepsLeave(t *testing.T) {
	s := &AttendanceState{Status: "leave"}
	MergeAttendanceEvent(s, AttendanceEvent{Timestamp: mustTime(t, "2026-10-18 10:02"), Source: "fingerprint", Direction: "check_in"})
	MergeAttendanceEvent(s, AttendanceEvent{Timestamp: mustTime(t, "2026-10-18 14:05"), Source: "fingerprint", Direction: "check_out"})
	ComputeAnomalies(s, stdSchedule(), true)

	if s.Status != "leave" {
		t.Fatalf("status = %q, want leave", s.Status)
	}
	if !s.IsLeavePunch {
		t.Error("is_leave_punch not set")
	}
	if s.CheckIn == nil || s.CheckOut == nil {
		t.Error("punches were not stored")
	}
	if s.IsLate || s.IsEarlyLeave || s.IsMissingCheckout || s.IsMissingCheckin {
		t.Errorf("leave day scored as a working day: %+v", s)
	}
}

// Absent / holiday records still become present on a punch.
func TestMergeAttendanceEvent_PunchOnAbsentBecomesPresent(t *testing.T) {
	for _, st := range []string{"absent", "holiday", ""} {
		s := &AttendanceState{Status: st}
		MergeAttendanceEvent(s, AttendanceEvent{Timestamp: mustTime(t, "2026-10-18 08:00"), Source: "face", Direction: "check_in"})
		ComputeAnomalies(s, stdSchedule(), false)
		if s.Status != "present" || s.IsLeavePunch {
			t.Errorf("from %q: status=%q leavePunch=%v, want present/false", st, s.Status, s.IsLeavePunch)
		}
	}
}

// A manager turning the day into a working day clears the flag.
func TestComputeAnomalies_LeaveCorrectedToPresentClearsFlag(t *testing.T) {
	s := &AttendanceState{Status: "leave", CheckIn: ptr(mustTime(t, "2026-10-18 08:00"))}
	ComputeAnomalies(s, stdSchedule(), false)
	if !s.IsLeavePunch {
		t.Fatal("expected flag on leave with punch")
	}
	s.Status = "present"
	ComputeAnomalies(s, stdSchedule(), false)
	if s.IsLeavePunch {
		t.Error("flag survived the correction to present")
	}
}
