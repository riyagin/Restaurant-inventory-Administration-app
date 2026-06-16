package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func uuidFrom(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[15] = b
	u.Valid = true
	return u
}

func TestSelectLatePolicyHighestThresholdOnly(t *testing.T) {
	p2 := ThresholdPolicy{ID: uuidFrom(1), Threshold: 15, Points: 2}
	p5 := ThresholdPolicy{ID: uuidFrom(2), Threshold: 60, Points: 5}
	policies := []ThresholdPolicy{p2, p5}

	// 70 minutes late: only the highest matching threshold (>=60, 5pt) applies.
	got, ok := SelectLatePolicy(70, policies)
	if !ok {
		t.Fatalf("expected a matching policy for 70-min late")
	}
	if got.Points != 5 || got.Threshold != 60 {
		t.Fatalf("expected the 5pt/>=60 policy, got %d pts / threshold %d", got.Points, got.Threshold)
	}

	// 30 minutes late: only the >=15 (2pt) policy matches.
	got, ok = SelectLatePolicy(30, policies)
	if !ok || got.Points != 2 || got.Threshold != 15 {
		t.Fatalf("expected the 2pt/>=15 policy for 30-min late, got ok=%v pts=%d", ok, got.Points)
	}

	// 10 minutes late: nothing matches.
	if _, ok := SelectLatePolicy(10, policies); ok {
		t.Fatalf("expected no policy for 10-min late")
	}
}

func TestSelectEarlyLeavePolicyHighestThresholdOnly(t *testing.T) {
	policies := []ThresholdPolicy{
		{ID: uuidFrom(1), Threshold: 10, Points: 1},
		{ID: uuidFrom(2), Threshold: 45, Points: 4},
	}
	got, ok := SelectEarlyLeavePolicy(50, policies)
	if !ok || got.Points != 4 {
		t.Fatalf("expected 4pt policy for 50-min early leave, got ok=%v pts=%d", ok, got.Points)
	}
}

func TestSelectHighestThresholdEqualBoundary(t *testing.T) {
	policies := []ThresholdPolicy{
		{ID: uuidFrom(1), Threshold: 15, Points: 2},
		{ID: uuidFrom(2), Threshold: 60, Points: 5},
	}
	// Exactly at the boundary (60) selects the 5pt policy (threshold <= minutes).
	got, ok := selectHighestThreshold(60, policies)
	if !ok || got.Points != 5 {
		t.Fatalf("expected 5pt at exactly 60, got ok=%v pts=%d", ok, got.Points)
	}
	// One below the higher boundary (59) selects the 2pt policy.
	got, ok = selectHighestThreshold(59, policies)
	if !ok || got.Points != 2 {
		t.Fatalf("expected 2pt at 59, got ok=%v pts=%d", ok, got.Points)
	}
}

func TestComputeScoreFloorAtZero(t *testing.T) {
	cases := []struct {
		points int
		want   int
	}{
		{0, 100},
		{30, 70},
		{100, 0},
		{120, 0}, // floor at 0, never negative
	}
	for _, c := range cases {
		if got := ComputeScore(c.points); got != c.want {
			t.Errorf("ComputeScore(%d) = %d, want %d", c.points, got, c.want)
		}
	}
}

func TestMonthlyCapReached(t *testing.T) {
	cap2 := 2

	// Unlimited (nil) cap is never reached.
	if MonthlyCapReached(nil, 99) {
		t.Errorf("nil cap should be unlimited")
	}
	// Below the cap: not reached.
	if MonthlyCapReached(&cap2, 1) {
		t.Errorf("1 existing < cap 2 should not be reached")
	}
	// At the cap: reached (no further insert).
	if !MonthlyCapReached(&cap2, 2) {
		t.Errorf("2 existing == cap 2 should be reached")
	}
	// Above the cap: reached.
	if !MonthlyCapReached(&cap2, 3) {
		t.Errorf("3 existing > cap 2 should be reached")
	}
}
