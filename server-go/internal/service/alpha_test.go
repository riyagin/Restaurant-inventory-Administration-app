package service

import (
	"testing"
	"time"
)

func TestAllocateAlpha(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	// 2026-11-02 Mon, 11-03 Tue, 11-07 Sat, 11-08 Sun.
	cases := []struct {
		name      string
		remaining int
		dates     []string
		covered   []bool
		used      int
	}{
		{"weekday from allowance", 4, []string{"2026-11-02"}, []bool{true}, 1},
		{"weekend costs two", 4, []string{"2026-11-07"}, []bool{true}, 2},
		{"allowance gone: unpaid", 0, []string{"2026-11-02"}, []bool{false}, 0},
		// One day left, weekend needs two: the whole day is unpaid and the
		// leftover day is KEPT — the following weekday Alpha still uses it.
		{"partial weekend unpaid, leftover kept", 1, []string{"2026-11-07", "2026-11-09"}, []bool{false, true}, 1},
		{"date order decides", 2, []string{"2026-11-02", "2026-11-03", "2026-11-07"}, []bool{true, true, false}, 2},
		{"negative remaining (leave over an overridden quota)", -3, []string{"2026-11-02"}, []bool{false}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var dates []time.Time
			for _, s := range c.dates {
				dates = append(dates, d(s))
			}
			got := AllocateAlpha(c.remaining, dates)
			for i, day := range got {
				if day.Covered != c.covered[i] {
					t.Fatalf("day %s covered=%v, want %v", c.dates[i], day.Covered, c.covered[i])
				}
			}
			if u := AlphaQuotaUsed(got); u != c.used {
				t.Fatalf("allowance used = %d, want %d", u, c.used)
			}
		})
	}
}

func TestMonthlyLeaveBalanceWithAlpha(t *testing.T) {
	m := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	// Quota 4, approved leave used 3; Alpha on Sat 7th (cost 2, can't fit) and
	// Mon 9th (cost 1, fits).
	b := NewMonthlyLeaveBalance(m, 4, 3, false, today).WithAlpha([]time.Time{
		time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
	})
	if b.LeaveUsedDays != 3 || b.AlphaUsedDays != 1 || b.UsedDays != 4 || b.UnusedDays != 0 ||
		b.AlphaDays != 2 || b.AlphaUnpaidDays != 1 {
		t.Fatalf("got %+v", b)
	}
	// No Alpha: unchanged from the leave-only balance.
	plain := NewMonthlyLeaveBalance(m, 4, 1, false, today)
	if got := plain.WithAlpha(nil); got != plain {
		t.Fatalf("WithAlpha(nil) changed the balance: %+v vs %+v", got, plain)
	}
}
