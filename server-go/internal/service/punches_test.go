package service

import (
	"testing"
	"time"
)

func wib(d, hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", d+" "+hhmm, WIB)
	if err != nil {
		panic(err)
	}
	return t
}

func day(d string) time.Time {
	t, _ := time.Parse("2006-01-02", d)
	return t
}

// Branch-like schedule 08:30–22:00 (Longshift), grace 5, tolerance 10.
func longShift() Schedule {
	return Schedule{WorkStartMinutes: 510, WorkEndMinutes: 1320, GraceMinutes: 5, EarlyLeaveMinutes: 10, Rostered: true}
}

func splitShift() Schedule { // 08:30–14:00 + 16:30–22:00
	return Schedule{WorkStartMinutes: 510, WorkEndMinutes: 1320, ResumeMinutes: 990, GraceMinutes: 5, EarlyLeaveMinutes: 10, Rostered: true}
}

func fp(t time.Time) Punch { return Punch{At: t, Source: "fingerprint"} }

func TestPickWorkDate_AfterMidnightBelongsToPreviousDay(t *testing.T) {
	sched := func(time.Time) Schedule { return longShift() }
	d, _ := PickWorkDate(wib("2026-10-11", "00:20"), sched)
	if got := d.Format("2006-01-02"); got != "2026-10-10" {
		t.Fatalf("00:20 assigned to %s, want 2026-10-10", got)
	}
	d, _ = PickWorkDate(wib("2026-10-11", "08:27"), sched)
	if got := d.Format("2006-01-02"); got != "2026-10-11" {
		t.Fatalf("08:27 assigned to %s, want 2026-10-11", got)
	}
}

func TestResolveDay_ArrivalAfterNoonIsCheckIn(t *testing.T) {
	d := day("2026-10-15")
	in, out := ResolveDay([]Punch{fp(wib("2026-10-15", "12:40")), fp(wib("2026-10-15", "22:05"))}, d, longShift())
	if in == nil || out == nil || in.At.Hour() != 12 || out.At.Hour() != 22 {
		t.Fatalf("in=%v out=%v, want 12:40 / 22:05", in, out)
	}
	// And the record measures 250 minutes late.
	s := &AttendanceState{Status: "present", Date: d, CheckIn: &in.At, CheckOut: &out.At}
	ComputeAnomalies(s, longShift(), true)
	if !s.IsLate || s.LateMinutes != 250 {
		t.Fatalf("late=%v %d, want 250", s.IsLate, s.LateMinutes)
	}
}

func TestResolveDay_SinglePunchNearestEnd(t *testing.T) {
	d := day("2026-10-15")
	if in, out := ResolveDay([]Punch{fp(wib("2026-10-15", "12:40"))}, d, longShift()); in == nil || out != nil {
		t.Errorf("lone 12:40 should be a check-in (closer to 08:30 than 22:00)")
	}
	if in, out := ResolveDay([]Punch{fp(wib("2026-10-15", "19:00"))}, d, longShift()); in != nil || out == nil {
		t.Errorf("lone 19:00 should be a check-out")
	}
	// A double tap is still one punch.
	if in, out := ResolveDay([]Punch{fp(wib("2026-10-15", "08:20")), fp(wib("2026-10-15", "08:21"))}, d, longShift()); in == nil || out != nil {
		t.Errorf("double tap at 08:20 should be a check-in only")
	}
	// The device's own direction wins for a lone punch.
	p := Punch{At: wib("2026-10-15", "09:00"), Source: "face", Hint: "check_out"}
	if in, out := ResolveDay([]Punch{p}, d, longShift()); in != nil || out == nil {
		t.Errorf("hinted check_out ignored")
	}
}

func TestResolveDay_LeftBeforeNoonIsEarlyLeave(t *testing.T) {
	d := day("2026-10-25")
	in, out := ResolveDay([]Punch{fp(wib("2026-10-25", "08:22")), fp(wib("2026-10-25", "11:50"))}, d, longShift())
	if in == nil || out == nil {
		t.Fatal("08:22 + 11:50 should be check-in and check-out")
	}
}

func TestResolveDay_CheckoutAfterMidnight(t *testing.T) {
	d := day("2026-10-10")
	in, out := ResolveDay([]Punch{fp(wib("2026-10-10", "08:25")), fp(wib("2026-10-11", "00:20"))}, d, longShift())
	s := &AttendanceState{Status: "present", Date: d, CheckIn: &in.At, CheckOut: &out.At}
	ComputeAnomalies(s, longShift(), true)
	if s.IsEarlyLeave || s.IsMissingCheckout {
		t.Fatalf("00:20 check-out flagged: early=%v missing=%v", s.IsEarlyLeave, s.IsMissingCheckout)
	}
}

func TestResolveDay_SplitShiftReturnIsNotCheckout(t *testing.T) {
	d := day("2026-10-04")
	ps := []Punch{fp(wib("2026-10-04", "08:24")), fp(wib("2026-10-04", "14:02")), fp(wib("2026-10-04", "16:25"))}
	in, out := ResolveDay(ps, d, splitShift())
	if in == nil || out != nil {
		t.Fatalf("forgot final punch: in=%v out=%v, want check-in only (missing check-out)", in, out)
	}
	ps = append(ps, fp(wib("2026-10-04", "22:04")))
	if _, out := ResolveDay(ps, d, splitShift()); out == nil || out.At.Hour() != 22 {
		t.Fatalf("full split day: out=%v, want 22:04", out)
	}
}

func TestApplyPermissions(t *testing.T) {
	late := func() *AttendanceState {
		return &AttendanceState{Status: "present", IsLate: true, LateMinutes: 250}
	}
	s := late()
	ApplyPermissions(s, []string{"late_arrival"})
	if !s.IsHalfDay || s.HalfDayType != "late" || s.HalfDayLostMinutes != 250 || s.ExcusedBy != "late_arrival" {
		t.Fatalf("late_arrival: %+v", s)
	}
	// Permit cancelled: the permit-made half day is undone.
	ApplyPermissions(s, nil)
	if s.IsHalfDay || s.ExcusedBy != "" {
		t.Fatalf("after cancel: %+v", s)
	}
	// A manager's own half day is never touched.
	m := late()
	m.IsHalfDay, m.HalfDayType, m.HalfDayLostMinutes = true, "late", 200
	ApplyPermissions(m, nil)
	if !m.IsHalfDay || m.HalfDayLostMinutes != 200 {
		t.Fatalf("manual half day changed: %+v", m)
	}
	// Off site: flags cleared; an absent day becomes present.
	o := late()
	o.IsMissingCheckout = true
	ApplyPermissions(o, []string{"off_site"})
	if o.IsLate || o.IsMissingCheckout || o.ExcusedBy != "off_site" {
		t.Fatalf("off_site present: %+v", o)
	}
	a := &AttendanceState{Status: "absent"}
	ApplyPermissions(a, []string{"off_site"})
	if a.Status != "present" || a.IsNoPunch {
		t.Fatalf("off_site absent: %+v", a)
	}
	// Leave stays leave.
	l := &AttendanceState{Status: "leave"}
	ApplyPermissions(l, []string{"off_site"})
	if l.Status != "leave" {
		t.Fatalf("leave changed: %+v", l)
	}
}
