package service

import (
	"testing"
	"time"
)

func dThr(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMonthsWorkedCeil(t *testing.T) {
	cases := []struct {
		name       string
		join, ref  string
		wantMonths int32
	}{
		{"exactly one year", "2025-04-01", "2026-04-01", 12},
		{"over one year", "2024-01-01", "2026-04-01", 27},
		{"partial rounds up", "2026-01-15", "2026-04-01", 3},   // 2m16d -> 3
		{"exact whole months", "2026-01-15", "2026-04-15", 3},  // exactly 3
		{"just under a month rounds to 1", "2026-03-20", "2026-04-01", 1},
		{"one day counts as a month", "2026-03-31", "2026-04-01", 1},
		{"joined after payment date", "2026-05-01", "2026-04-01", 0},
		{"same day", "2026-04-01", "2026-04-01", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MonthsWorkedCeil(dThr(c.join), dThr(c.ref))
			if got != c.wantMonths {
				t.Fatalf("MonthsWorkedCeil(%s,%s) = %d, want %d", c.join, c.ref, got, c.wantMonths)
			}
		})
	}
}

func TestComputeThrEntitlement(t *testing.T) {
	base := int64(3_000_000)
	cases := []struct {
		name       string
		join, ref  string
		wantAmount int64
	}{
		{"full year -> 1 month", "2024-01-01", "2026-04-01", 3_000_000},
		{"exactly 12 months -> 1 month", "2025-04-01", "2026-04-01", 3_000_000},
		{"3/12 proportional", "2026-01-15", "2026-04-01", 750_000},     // 3/12 * 3,000,000
		{"1/12 proportional", "2026-03-20", "2026-04-01", 250_000},     // 1/12 * 3,000,000
		{"not yet entitled", "2026-05-01", "2026-04-01", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ent := ComputeThrEntitlement(base, dThr(c.join), dThr(c.ref))
			if ent.Amount != c.wantAmount {
				t.Fatalf("ComputeThrEntitlement amount = %d, want %d (months=%d ratio=%.4f)",
					ent.Amount, c.wantAmount, ent.MonthsWorked, ent.Ratio)
			}
		})
	}
}
