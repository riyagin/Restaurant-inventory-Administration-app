package service

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func score(v int32) pgtype.Int4 { return pgtype.Int4{Int32: v, Valid: true} }

func testScale(t *testing.T) GradeScale {
	t.Helper()
	s, err := ValidateGradeScale([]GradeTier{
		{Label: "D", MinScore: 0, PayPercent: 0},
		{Label: "B", MinScore: 80, PayPercent: 75},
		{Label: "A", MinScore: 90, PayPercent: 100},
		{Label: "C", MinScore: 70, PayPercent: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGradeScaleResolve(t *testing.T) {
	s := testScale(t)
	cases := []struct {
		score int32
		label string
		pct   int32
	}{
		{100, "A", 100}, {90, "A", 100}, {89, "B", 75}, {80, "B", 75},
		{79, "C", 50}, {70, "C", 50}, {69, "D", 0}, {0, "D", 0},
	}
	for _, c := range cases {
		got := s.Resolve(score(c.score))
		if got == nil || got.Label != c.label || got.PayPercent != c.pct {
			t.Errorf("score %d → %+v, want %s/%d%%", c.score, got, c.label, c.pct)
		}
	}
	if s.Resolve(pgtype.Int4{}) != nil {
		t.Error("missing score must resolve to no tier (paid in full)")
	}
	if (GradeScale{}).Resolve(score(50)) != nil {
		t.Error("empty scale must resolve to no tier (paid in full)")
	}
	// A scale saved without a 0 tier (only possible outside ValidateGradeScale)
	// must not pay a score below every tier in full.
	partial := GradeScale{{Label: "A", MinScore: 90, PayPercent: 100}}
	if got := partial.Resolve(score(10)); got == nil || got.PayPercent != 0 {
		t.Errorf("below every tier → %+v, want 0%%", got)
	}
}

func TestGradedAmount(t *testing.T) {
	tier := &GradeTier{Label: "B", MinScore: 80, PayPercent: 75}
	if got := GradedAmount(300_000, true, tier); got != 225_000 {
		t.Errorf("75%% of 300000 = %d", got)
	}
	if got := GradedAmount(300_000, false, tier); got != 300_000 {
		t.Errorf("unlinked component = %d, want full", got)
	}
	if got := GradedAmount(300_000, true, nil); got != 300_000 {
		t.Errorf("no tier = %d, want full", got)
	}
	// Half-up rounding to the rupiah: 33% of 1001 = 330.33 → 330; 50% of 1001 = 500.5 → 501.
	if got := ApplyGradePercent(1001, 33); got != 330 {
		t.Errorf("33%% of 1001 = %d, want 330", got)
	}
	if got := ApplyGradePercent(1001, 50); got != 501 {
		t.Errorf("50%% of 1001 = %d, want 501", got)
	}
}

func TestValidateGradeScale(t *testing.T) {
	bad := map[string][]GradeTier{
		"empty":           {},
		"no zero tier":    {{Label: "A", MinScore: 90, PayPercent: 100}},
		"duplicate score": {{Label: "A", MinScore: 0, PayPercent: 100}, {Label: "B", MinScore: 0, PayPercent: 50}},
		"duplicate label": {{Label: "A", MinScore: 90, PayPercent: 100}, {Label: "a", MinScore: 0, PayPercent: 0}},
		"blank label":     {{Label: " ", MinScore: 0, PayPercent: 0}},
		"percent > 100":   {{Label: "A", MinScore: 0, PayPercent: 120}},
		"score > 100":     {{Label: "A", MinScore: 101, PayPercent: 100}, {Label: "B", MinScore: 0, PayPercent: 0}},
		"pay rises as score falls": {
			{Label: "A", MinScore: 90, PayPercent: 50}, {Label: "B", MinScore: 0, PayPercent: 100},
		},
	}
	for name, in := range bad {
		if _, err := ValidateGradeScale(in); !errors.Is(err, ErrInvalidGradeScale) {
			t.Errorf("%s: want ErrInvalidGradeScale, got %v", name, err)
		}
	}
	s := testScale(t)
	if s[0].Label != "A" || s[3].Label != "D" {
		t.Errorf("scale must be ordered highest first, got %+v", s)
	}
}
