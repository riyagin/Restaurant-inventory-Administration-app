package service

import (
	"strings"
	"testing"
	"time"
)

// The same instant must give the same attendance verdict whatever zone the
// time.Time happens to carry (pgx decodes into time.Local, devices send +07:00,
// a UTC server prints UTC).
func TestComputeAnomalies_ZoneIndependent(t *testing.T) {
	sched := stdSchedule() // 08:00–17:00, grace 15
	inWIB := time.Date(2026, 10, 5, 8, 20, 0, 0, WIB)
	for _, in := range []time.Time{inWIB, inWIB.UTC(), inWIB.In(time.FixedZone("X", -5*3600))} {
		s := &AttendanceState{Status: "present", CheckIn: ptr(in)}
		ComputeAnomalies(s, sched, false)
		if !s.IsLate || s.LateMinutes != 20 {
			t.Errorf("check-in %v: late=%v minutes=%d, want late 20", in, s.IsLate, s.LateMinutes)
		}
	}
}

// A ZKTeco .dat line carries the device wall clock (WIB) with no offset; it must
// be stored as that WIB instant, not as the same digits in UTC (7 h later).
func TestDATParser_WallClockIsWIB(t *testing.T) {
	ps, errs, err := NewDATFingerprintParser().Parse(strings.NewReader("      101\t2026-10-05 08:11:06\t5\t0\t1\t0\r\n"))
	if err != nil || len(errs) > 0 || len(ps) != 1 {
		t.Fatalf("parse: %v %v %d", err, errs, len(ps))
	}
	want := time.Date(2026, 10, 5, 1, 11, 6, 0, time.UTC)
	if !ps[0].Timestamp.Equal(want) {
		t.Fatalf("got instant %v, want %v (08:11:06 WIB)", ps[0].Timestamp.UTC(), want)
	}
}

func TestWIBDate(t *testing.T) {
	// 23:30 UTC on 4 Oct is 06:30 WIB on 5 Oct.
	got := WIBDate(time.Date(2026, 10, 4, 23, 30, 0, 0, time.UTC))
	if got.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("WIBDate = %s, want 2026-10-05", got.Format("2006-01-02"))
	}
}

// The day is over once its check-out window (shift end + PunchWindowAfter)
// closes, measured on the WIB clock.
func TestDayIsOver_WIB(t *testing.T) {
	sched := stdSchedule() // ends 17:00 WIB; window closes 21:00 WIB = 14:00 UTC
	date := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if DayIsOver(date, sched, time.Date(2026, 10, 5, 13, 59, 0, 0, time.UTC)) {
		t.Error("20:59 WIB reported as over")
	}
	if !DayIsOver(date, sched, time.Date(2026, 10, 5, 14, 1, 0, 0, time.UTC)) {
		t.Error("21:01 WIB reported as not over")
	}
}
