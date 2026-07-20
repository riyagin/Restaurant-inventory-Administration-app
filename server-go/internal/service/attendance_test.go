package service

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		t.Fatalf("bad test time %q: %v", s, err)
	}
	return parsed
}

func ptr(tm time.Time) *time.Time { return &tm }

func stdSchedule() Schedule {
	return Schedule{
		WorkStartMinutes:  8 * 60,  // 08:00
		WorkEndMinutes:    17 * 60, // 17:00
		GraceMinutes:      15,
		EarlyLeaveMinutes: 30,
		WorkDays:          []int{1, 2, 3, 4, 5, 6},
	}
}

// ── Late math across grace boundary ──────────────────────────────────────────

func TestComputeAnomalies_LateBoundary(t *testing.T) {
	sched := stdSchedule()

	cases := []struct {
		name        string
		checkIn     string
		wantLate    bool
		wantMinutes int
	}{
		{"on time", "2026-06-09 08:00", false, 0},
		{"within grace exactly at limit", "2026-06-09 08:15", false, 0},
		{"one minute past grace", "2026-06-09 08:16", true, 16},
		{"well late", "2026-06-09 08:23", true, 23},
		{"early arrival", "2026-06-09 07:50", false, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &AttendanceState{Status: "present", CheckIn: ptr(mustTime(t, c.checkIn))}
			ComputeAnomalies(s, sched, false)
			if s.IsLate != c.wantLate {
				t.Errorf("IsLate = %v, want %v", s.IsLate, c.wantLate)
			}
			if s.LateMinutes != c.wantMinutes {
				t.Errorf("LateMinutes = %d, want %d", s.LateMinutes, c.wantMinutes)
			}
		})
	}
}

// ── Half-day lost-minutes math ───────────────────────────────────────────────

func TestComputeLostMinutes(t *testing.T) {
	sched := stdSchedule() // work start 08:00

	cases := []struct {
		name    string
		checkIn *time.Time
		want    int
	}{
		{"nil check-in", nil, 0},
		{"before start", ptr(mustTime(t, "2026-06-09 07:30")), 0},
		{"exactly at start", ptr(mustTime(t, "2026-06-09 08:00")), 0},
		{"arrives 12:00 (half day)", ptr(mustTime(t, "2026-06-09 12:00")), 240},
		{"arrives 13:30", ptr(mustTime(t, "2026-06-09 13:30")), 330},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ComputeLostMinutes(c.checkIn, sched); got != c.want {
				t.Errorf("ComputeLostMinutes = %d, want %d", got, c.want)
			}
		})
	}
}

// ── Early-leave math across boundary ─────────────────────────────────────────

func TestComputeAnomalies_EarlyLeaveBoundary(t *testing.T) {
	sched := stdSchedule() // work_end 17:00, early_leave 30 => threshold 16:30

	cases := []struct {
		name        string
		checkOut    string
		wantEarly   bool
		wantMinutes int
	}{
		{"leaves on time", "2026-06-09 17:00", false, 0},
		{"leaves at threshold (16:30)", "2026-06-09 16:30", false, 0},
		{"one minute before threshold", "2026-06-09 16:29", true, 31},
		{"leaves much earlier", "2026-06-09 15:00", true, 120},
		{"stays late", "2026-06-09 18:00", false, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &AttendanceState{
				Status:   "present",
				CheckIn:  ptr(mustTime(t, "2026-06-09 08:00")),
				CheckOut: ptr(mustTime(t, c.checkOut)),
			}
			ComputeAnomalies(s, sched, true)
			if s.IsEarlyLeave != c.wantEarly {
				t.Errorf("IsEarlyLeave = %v, want %v", s.IsEarlyLeave, c.wantEarly)
			}
			if s.EarlyLeaveMinutes != c.wantMinutes {
				t.Errorf("EarlyLeaveMinutes = %d, want %d", s.EarlyLeaveMinutes, c.wantMinutes)
			}
		})
	}
}

func TestComputeAnomalies_MissingCheckout(t *testing.T) {
	sched := stdSchedule()

	// has check-in, no check-out, day over => missing checkout
	s := &AttendanceState{Status: "present", CheckIn: ptr(mustTime(t, "2026-06-09 08:00"))}
	ComputeAnomalies(s, sched, true)
	if !s.IsMissingCheckout {
		t.Errorf("expected IsMissingCheckout=true when day over and no checkout")
	}

	// day not over yet => not flagged
	s2 := &AttendanceState{Status: "present", CheckIn: ptr(mustTime(t, "2026-06-09 08:00"))}
	ComputeAnomalies(s2, sched, false)
	if s2.IsMissingCheckout {
		t.Errorf("expected IsMissingCheckout=false while day not over")
	}
}

func TestComputeAnomalies_NonPresentNoFlags(t *testing.T) {
	sched := stdSchedule()
	s := &AttendanceState{Status: "absent", CheckIn: ptr(mustTime(t, "2026-06-09 09:30"))}
	ComputeAnomalies(s, sched, true)
	if s.IsLate || s.IsEarlyLeave || s.IsMissingCheckout {
		t.Errorf("absent record must carry no anomaly flags, got %+v", s)
	}
}

// ── Merge precedence: face beats fingerprint ─────────────────────────────────

func TestMergeAttendanceEvent_FaceBeatsFingerprint(t *testing.T) {
	// Existing fingerprint check-in, then a face event much later in the day —
	// face wins and overwrites the fingerprint value.
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:05"),
		Source:    "fingerprint",
		Direction: "check_in",
	})
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 09:00"),
		Source:    "face",
		Direction: "check_in",
	})
	if s.CheckInSource != "face" {
		t.Errorf("check_in_source = %q, want face", s.CheckInSource)
	}
	if got := s.CheckIn.Format("15:04"); got != "09:00" {
		t.Errorf("check_in = %q, want 09:00 (face wins)", got)
	}
}

func TestMergeAttendanceEvent_FingerprintNeverOverwritesFace(t *testing.T) {
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:00"),
		Source:    "face",
		Direction: "check_in",
	})
	// A later fingerprint punch must NOT overwrite the face value.
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:40"),
		Source:    "fingerprint",
		Direction: "check_in",
	})
	if s.CheckInSource != "face" {
		t.Errorf("check_in_source = %q, want face (fingerprint must not overwrite)", s.CheckInSource)
	}
	if got := s.CheckIn.Format("15:04"); got != "08:00" {
		t.Errorf("check_in = %q, want 08:00", got)
	}
}

func TestMergeAttendanceEvent_FingerprintFillsEmpty(t *testing.T) {
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:02"),
		Source:    "fingerprint",
		Direction: "check_in",
	})
	if s.CheckInSource != "fingerprint" || s.CheckIn == nil {
		t.Errorf("fingerprint should fill an empty check_in, got %+v", s)
	}
}

// ── 5-minute dedup window ────────────────────────────────────────────────────

func TestMergeAttendanceEvent_DedupWithinWindow(t *testing.T) {
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:00"),
		Source:    "face",
		Direction: "check_in",
	})
	// Same source, within 5 minutes => deduplicated (ignored), original kept.
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:04"),
		Source:    "face",
		Direction: "check_in",
	})
	if got := s.CheckIn.Format("15:04"); got != "08:00" {
		t.Errorf("check_in = %q, want 08:00 (dup within window ignored)", got)
	}
}

func TestMergeAttendanceEvent_OutsideWindowReplacesSameSource(t *testing.T) {
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:00"),
		Source:    "face",
		Direction: "check_in",
	})
	// Same source but 6 minutes later (outside window) => allowed to replace.
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:06"),
		Source:    "face",
		Direction: "check_in",
	})
	if got := s.CheckIn.Format("15:04"); got != "08:06" {
		t.Errorf("check_in = %q, want 08:06 (outside dedup window)", got)
	}
}

func TestMergeAttendanceEvent_HigherSourceReplacesInsideWindow(t *testing.T) {
	s := &AttendanceState{}
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:00"),
		Source:    "fingerprint",
		Direction: "check_in",
	})
	// Face within the window should still upgrade the value.
	MergeAttendanceEvent(s, AttendanceEvent{
		Timestamp: mustTime(t, "2026-06-09 08:03"),
		Source:    "face",
		Direction: "check_in",
	})
	if s.CheckInSource != "face" {
		t.Errorf("check_in_source = %q, want face (upgrade inside window)", s.CheckInSource)
	}
}

// ── Reconcile predicate: skips holidays + non-work-days ──────────────────────

func TestShouldMarkAbsent(t *testing.T) {
	workDays := []int{1, 2, 3, 4, 5, 6} // Mon-Sat

	cases := []struct {
		name           string
		isoWeekday     int
		isHoliday      bool
		hasRecord      bool
		existingStatus string
		want           bool
	}{
		{"normal work day, no record => absent", 3, false, false, "", true},
		{"holiday => skip", 3, true, false, "", false},
		{"sunday non-work day => skip", 7, false, false, "", false},
		{"already has present record => skip", 2, false, true, "present", false},
		{"already on leave => skip", 2, false, true, "leave", false},
		{"saturday is a work day => absent", 6, false, false, "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldMarkAbsent(c.isoWeekday, workDays, c.isHoliday, c.hasRecord, c.existingStatus)
			if got != c.want {
				t.Errorf("shouldMarkAbsent(%d, holiday=%v, hasRecord=%v, %q) = %v, want %v",
					c.isoWeekday, c.isHoliday, c.hasRecord, c.existingStatus, got, c.want)
			}
		})
	}
}

func TestIsoWeekday(t *testing.T) {
	// 2026-06-07 is a Sunday => ISO 7; 2026-06-08 Monday => ISO 1.
	sun := mustTime(t, "2026-06-07 00:00")
	mon := mustTime(t, "2026-06-08 00:00")
	if got := isoWeekday(sun); got != 7 {
		t.Errorf("Sunday ISO = %d, want 7", got)
	}
	if got := isoWeekday(mon); got != 1 {
		t.Errorf("Monday ISO = %d, want 1", got)
	}
}
