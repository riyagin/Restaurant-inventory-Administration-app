package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestComputeDailyRate(t *testing.T) {
	cases := []struct {
		name       string
		baseSalary int64
		workDays   int32
		want       int64
	}{
		{"exact divisible", 3000000, 30, 100000},
		{"exact divisible 26", 2600000, 26, 100000},
		{"round down", 1000000, 30, 33333},       // 33333.33 -> 33333
		{"round up half", 100, 8, 13},            // 12.5 -> 13 (round half up)
		{"round up", 1000000, 26, 38462},         // 38461.53 -> 38462
		{"round down just under half", 1000, 8, 125}, // 125.0 exact
		{"single working day", 5000000, 1, 5000000},
		{"max working days", 31000000, 31, 1000000},
		{"zero salary", 0, 30, 0},
		{"guard zero days", 1000000, 0, 0},
		{"round half up small", 5, 2, 3}, // 2.5 -> 3
		{"round down small", 7, 3, 2},    // 2.33 -> 2
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ComputeDailyRate(c.baseSalary, c.workDays); got != c.want {
				t.Errorf("ComputeDailyRate(%d, %d) = %d, want %d", c.baseSalary, c.workDays, got, c.want)
			}
		})
	}
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestIsWageVersionActiveOn(t *testing.T) {
	openEnd := pgtype.Date{} // invalid = NULL = open
	closedEnd := pgtype.Date{Time: date(2026, 5, 31), Valid: true}

	cases := []struct {
		name      string
		effective time.Time
		end       pgtype.Date
		d         time.Time
		want      bool
	}{
		// Open version, effective_date == d boundary
		{"open: d equals effective", date(2026, 6, 1), openEnd, date(2026, 6, 1), true},
		{"open: d before effective", date(2026, 6, 1), openEnd, date(2026, 5, 31), false},
		{"open: d after effective", date(2026, 6, 1), openEnd, date(2026, 12, 1), true},
		// Closed version, end_date == d boundary (inclusive)
		{"closed: d equals end_date", date(2026, 5, 1), closedEnd, date(2026, 5, 31), true},
		{"closed: d after end_date", date(2026, 5, 1), closedEnd, date(2026, 6, 1), false},
		{"closed: d equals effective", date(2026, 5, 1), closedEnd, date(2026, 5, 1), true},
		{"closed: d before effective", date(2026, 5, 1), closedEnd, date(2026, 4, 30), false},
		{"closed: d mid-range", date(2026, 5, 1), closedEnd, date(2026, 5, 15), true},
		// Time component on the date should be ignored
		{"effective with time ignored", time.Date(2026, 6, 1, 23, 59, 0, 0, time.UTC), openEnd, date(2026, 6, 1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsWageVersionActiveOn(c.effective, c.end, c.d); got != c.want {
				t.Errorf("IsWageVersionActiveOn(%v, %v, %v) = %v, want %v",
					c.effective, c.end, c.d, got, c.want)
			}
		})
	}
}
