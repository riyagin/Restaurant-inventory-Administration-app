package service

import (
	"testing"
	"time"
)

func TestProbationEndDate(t *testing.T) {
	cases := map[string]string{
		"2026-01-15": "2026-02-15",
		"2026-01-31": "2026-02-28",
		"2028-01-31": "2028-02-29",
		"2026-03-31": "2026-04-30",
		"2026-12-10": "2027-01-10",
	}
	for in, want := range cases {
		j, _ := time.Parse("2006-01-02", in)
		if got := ProbationEndDate(j).Format("2006-01-02"); got != want {
			t.Errorf("ProbationEndDate(%s) = %s, want %s", in, got, want)
		}
	}
}
