package service

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPayslipOffenses(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	in := func(d, hh, mm int) *time.Time {
		t := time.Date(2026, 10, d, hh, mm, 0, 0, WIB)
		return &t
	}
	violations := []OffenseViolation{
		{Date: day(6), Label: "Terlambat 11 - 20 menit", RuleType: "late", Points: 3, CheckIn: in(6, 8, 15), LateMin: 15},
		{Date: day(10), Label: "Izin Satu Hari", RuleType: "unpaid_leave", Points: 8},
		{Date: day(7), Label: "Mangkir berturut-turut", RuleType: "consecutive_absent", Points: 40},
		{Date: day(22), Label: "Tidak check-out", RuleType: "missing_checkout", Points: 10, CheckIn: in(22, 8, 0)},
	}
	cuts := []DayPayCut{
		{Date: day(6), Kind: PayCutAttendance, Amount: 3125},
		{Date: day(10), Kind: PayCutLeave, Amount: 200000, Note: "Sabtu, 2× gaji harian"},
		{Date: day(20), Kind: PayCutAlpha, Amount: 100000}, // no violation that day
		{Date: day(21), Kind: PayCutAttendance, Amount: 0}, // ignored
	}
	rows := BuildPayslipOffenses(violations, cuts)

	want := []struct {
		day    int
		label  string
		points int
		amount int64
	}{
		{6, "Terlambat 11 - 20 menit — masuk 08:15, telat 15 mnt", 3, 3125},
		{7, "Mangkir berturut-turut", 40, 0},
		{10, "Izin Satu Hari (Sabtu, 2× gaji harian)", 8, 200000},
		{20, "Alpha, jatah libur habis", 0, 100000},
		{22, "Tidak check-out — masuk 08:00", 10, 0},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Date.Day() != w.day || r.Label != w.label || r.Points != w.points || r.Amount != w.amount {
			t.Errorf("row %d = {%d %q %d %d}, want {%d %q %d %d}", i, r.Date.Day(), r.Label, r.Points, r.Amount,
				w.day, w.label, w.points, w.amount)
		}
	}
}

// A day's cut lands on one violation only, even when two of the same kind fall
// on that day (late and early: one attendance cut covers both ends).
func TestBuildPayslipOffensesCutOnce(t *testing.T) {
	d := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rows := BuildPayslipOffenses([]OffenseViolation{
		{Date: d, Label: "Terlambat", RuleType: "late", Points: 2, LateMin: 5},
		{Date: d, Label: "Pulang awal", RuleType: "early_leave", Points: 2, EarlyMin: 20},
	}, []DayPayCut{{Date: d, Kind: PayCutAttendance, Amount: 9375}})
	var total int64
	for _, r := range rows {
		total += r.Amount
	}
	if len(rows) != 2 || total != 9375 || rows[0].Amount != 9375 {
		t.Errorf("rows = %+v, want the cut once on the first", rows)
	}
	if !strings.Contains(rows[1].Label, "lebih awal") {
		t.Errorf("early leave detail missing: %q", rows[1].Label)
	}
}

func TestOffenseDateLabel(t *testing.T) {
	if got := offenseDateLabel(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)); got != "Sab 10 Okt" {
		t.Errorf("got %q", got)
	}
}
