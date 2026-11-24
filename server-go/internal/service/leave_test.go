package service

import (
	"errors"
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

func TestLeaveDayCostWeekendCountsDouble(t *testing.T) {
	// 2026-10-23 Fri, 24 Sat, 25 Sun, 26 Mon.
	for date, want := range map[string]int{"2026-10-23": 1, "2026-10-24": 2, "2026-10-25": 2, "2026-10-26": 1} {
		if got := LeaveDayCost(d(date)); got != want {
			t.Errorf("LeaveDayCost(%s) = %d, want %d", date, got, want)
		}
	}
}

func TestQuotaCostByMonthSplitsAtMonthEnd(t *testing.T) {
	// Fri 30 Oct (1) + Sat 31 Oct (2) | Sun 1 Nov (2) + Mon 2 Nov (1).
	days := WorkingDaysInRange(d("2026-10-30"), d("2026-11-02"), []int{1, 2, 3, 4, 5, 6, 7}, nil)
	got := QuotaCostByMonth(days, 0)
	if len(got) != 2 {
		t.Fatalf("expected 2 months, got %+v", got)
	}
	if !got[0].Month.Equal(d("2026-10-01")) || got[0].Cost != 3 {
		t.Errorf("October = %+v, want 2026-10-01 cost 3", got[0])
	}
	if !got[1].Month.Equal(d("2026-11-01")) || got[1].Cost != 3 {
		t.Errorf("November = %+v, want 2026-11-01 cost 3", got[1])
	}
}

func TestFourDayQuotaAllowsTwoWeekendDays(t *testing.T) {
	sat, sun := LeaveDayCost(d("2026-10-24")), LeaveDayCost(d("2026-10-25"))
	if !QuotaSufficient(DefaultMonthlyLeaveQuota, sat, sun) {
		t.Fatal("a Saturday and a Sunday off should fit in the monthly allowance")
	}
	if QuotaSufficient(DefaultMonthlyLeaveQuota, sat+sun, LeaveDayCost(d("2026-10-26"))) {
		t.Fatal("a third day after a full weekend should overdraw the allowance")
	}
}

func TestNewMonthlyLeaveBalance(t *testing.T) {
	today := d("2026-10-22")
	sep := NewMonthlyLeaveBalance(d("2026-09-01"), 4, 1, false, today, 0)
	if !sep.Closed || sep.UnusedDays != 3 || sep.Month != "2026-09" {
		t.Errorf("September = %+v, want closed with 3 unused", sep)
	}
	oct := NewMonthlyLeaveBalance(d("2026-10-01"), 4, 6, true, today, 0)
	if oct.Closed || oct.UnusedDays != 0 {
		t.Errorf("October = %+v, want open and unused clamped at 0", oct)
	}
}

func TestQuotaExceededErrorMatchesSentinel(t *testing.T) {
	var err error = &QuotaExceededError{Month: d("2026-10-01"), Cutoff: 25, Quota: 4, Used: 3, Needed: 2}
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatal("QuotaExceededError should match ErrQuotaExceeded")
	}
	if want := "jatah libur periode 26/09/2026 – 25/10/2026 tidak mencukupi: sisa 1 hari, dibutuhkan 2 hari"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

// With the default cut-off 25 the allowance period labelled M runs 26 (M-1) –
// 25 M: the 25th closes a period and the 26th opens the next one.
func TestLeavePeriodFollowsCutoff(t *testing.T) {
	for date, want := range map[string]string{
		"2026-11-25": "2026-11-01", "2026-11-26": "2026-12-01", "2026-11-01": "2026-11-01",
		"2026-10-26": "2026-11-01", "2026-12-31": "2027-01-01", "2026-02-25": "2026-02-01",
	} {
		if got := LeavePeriodOf(d(date), 25); !got.Equal(d(want)) {
			t.Errorf("LeavePeriodOf(%s, 25) = %s, want %s", date, got.Format("2006-01-02"), want)
		}
	}
	if got := LeavePeriodOf(d("2026-11-30"), 0); !got.Equal(d("2026-11-01")) {
		t.Errorf("cut-off 0 should be the calendar month, got %s", got.Format("2006-01-02"))
	}
	from, to := LeavePeriodRange(d("2026-11-01"), 25)
	if !from.Equal(d("2026-10-26")) || !to.Equal(d("2026-11-26")) {
		t.Errorf("range = %s..%s, want 2026-10-26..2026-11-26", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	from, to = LeavePeriodRange(d("2027-01-01"), 25)
	if !from.Equal(d("2026-12-26")) || !to.Equal(d("2027-01-26")) {
		t.Errorf("January range = %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
}

func TestQuotaCostByMonthSplitsAtCutoff(t *testing.T) {
	// Tue 24 Nov (1) + Wed 25 Nov (1) | Thu 26 Nov (1) + Fri 27 Nov (1).
	days := WorkingDaysInRange(d("2026-11-24"), d("2026-11-27"), []int{1, 2, 3, 4, 5, 6, 7}, nil)
	got := QuotaCostByMonth(days, 25)
	if len(got) != 2 || !got[0].Month.Equal(d("2026-11-01")) || got[0].Cost != 2 ||
		!got[1].Month.Equal(d("2026-12-01")) || got[1].Cost != 2 {
		t.Fatalf("got %+v, want Nov period 2 + Dec period 2", got)
	}
	// 30 Oct – 2 Nov sits inside one period (26 Oct – 25 Nov).
	days = WorkingDaysInRange(d("2026-10-30"), d("2026-11-02"), []int{1, 2, 3, 4, 5, 6, 7}, nil)
	if got := QuotaCostByMonth(days, 25); len(got) != 1 || got[0].Cost != 6 {
		t.Fatalf("got %+v, want one period costing 6", got)
	}
}

func TestNewMonthlyLeaveBalanceCutoff(t *testing.T) {
	// On 26 Nov the November period (26 Oct – 25 Nov) is over.
	b := NewMonthlyLeaveBalance(d("2026-11-01"), 4, 1, false, d("2026-11-26"), 25)
	if !b.Closed || b.PeriodStart != "2026-10-26" || b.PeriodEnd != "2026-11-25" {
		t.Errorf("got %+v, want closed 2026-10-26..2026-11-25", b)
	}
	if b := NewMonthlyLeaveBalance(d("2026-11-01"), 4, 1, false, d("2026-11-25"), 25); b.Closed {
		t.Errorf("November period should still be open on the 25th")
	}
}
