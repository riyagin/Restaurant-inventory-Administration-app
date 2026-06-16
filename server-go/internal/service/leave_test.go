package service

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCountWorkingDaysSkipsWeekendsAndHolidays(t *testing.T) {
	// Mon–Fri work week.
	workDays := []int{1, 2, 3, 4, 5}

	// 2026-06-08 (Mon) .. 2026-06-14 (Sun): 5 work days, minus weekend = Mon-Fri.
	got := CountWorkingDays(d("2026-06-08"), d("2026-06-14"), workDays, nil)
	if got != 5 {
		t.Fatalf("expected 5 working days Mon-Sun, got %d", got)
	}

	// Add a public holiday on Wed 2026-06-10 → 4 working days.
	holidays := map[string]bool{"2026-06-10": true}
	got = CountWorkingDays(d("2026-06-08"), d("2026-06-14"), workDays, holidays)
	if got != 4 {
		t.Fatalf("expected 4 working days with one holiday, got %d", got)
	}

	// A pure weekend range counts zero.
	if got := CountWorkingDays(d("2026-06-13"), d("2026-06-14"), workDays, nil); got != 0 {
		t.Fatalf("expected 0 working days over a weekend, got %d", got)
	}

	// Single work day.
	if got := CountWorkingDays(d("2026-06-08"), d("2026-06-08"), workDays, nil); got != 1 {
		t.Fatalf("expected 1 working day for a single Monday, got %d", got)
	}

	// Inverted range yields 0.
	if got := CountWorkingDays(d("2026-06-14"), d("2026-06-08"), workDays, nil); got != 0 {
		t.Fatalf("expected 0 for inverted range, got %d", got)
	}
}

func TestCountWorkingDaysSixDayWeek(t *testing.T) {
	// Mon–Sat work week (Sunday off) — matches the default schedule.
	workDays := []int{1, 2, 3, 4, 5, 6}
	// Mon..Sun = 6 work days (only Sunday excluded).
	if got := CountWorkingDays(d("2026-06-08"), d("2026-06-14"), workDays, nil); got != 6 {
		t.Fatalf("expected 6 working days on a six-day week, got %d", got)
	}
}

func TestWorkingDaysInRangeReturnsActualDates(t *testing.T) {
	workDays := []int{1, 2, 3, 4, 5}
	holidays := map[string]bool{"2026-06-10": true}
	days := WorkingDaysInRange(d("2026-06-08"), d("2026-06-12"), workDays, holidays)
	want := []string{"2026-06-08", "2026-06-09", "2026-06-11", "2026-06-12"}
	if len(days) != len(want) {
		t.Fatalf("expected %d days, got %d", len(want), len(days))
	}
	for i, ds := range want {
		if days[i].Format("2006-01-02") != ds {
			t.Fatalf("day %d: expected %s, got %s", i, ds, days[i].Format("2006-01-02"))
		}
	}
}

func TestRangesOverlap(t *testing.T) {
	cases := []struct {
		aS, aE, bS, bE string
		want           bool
	}{
		{"2026-06-08", "2026-06-10", "2026-06-10", "2026-06-12", true},  // touch at boundary
		{"2026-06-08", "2026-06-10", "2026-06-11", "2026-06-12", false}, // adjacent, no overlap
		{"2026-06-08", "2026-06-15", "2026-06-10", "2026-06-12", true},  // contained
		{"2026-06-10", "2026-06-12", "2026-06-08", "2026-06-15", true},  // contains
		{"2026-06-08", "2026-06-09", "2026-06-20", "2026-06-25", false}, // disjoint
	}
	for i, c := range cases {
		got := RangesOverlap(d(c.aS), d(c.aE), d(c.bS), d(c.bE))
		if got != c.want {
			t.Fatalf("case %d: RangesOverlap=%v want %v", i, got, c.want)
		}
	}
}

func TestQuotaSufficient(t *testing.T) {
	// 12 quota, 8 used → 4 remaining.
	if !QuotaSufficient(12, 8, 4) {
		t.Fatalf("expected 4 remaining to cover a 4-day request")
	}
	if QuotaSufficient(12, 8, 5) {
		t.Fatalf("expected 4 remaining NOT to cover a 5-day request")
	}
	if !QuotaSufficient(12, 0, 12) {
		t.Fatalf("expected full quota to cover an exactly-12-day request")
	}
}
