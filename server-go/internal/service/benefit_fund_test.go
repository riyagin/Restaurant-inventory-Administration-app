package service

import "testing"

func TestSplitClaim(t *testing.T) {
	cases := []struct {
		name                       string
		total, monthly, covered    int64
		wantAvail, wantCov, wantEx int64
	}{
		{"fits in allowance", 80_000, 200_000, 0, 200_000, 80_000, 0},
		{"exactly the allowance", 200_000, 200_000, 0, 200_000, 200_000, 0},
		{"over → kasbon", 350_000, 200_000, 0, 200_000, 200_000, 150_000},
		{"second claim uses the rest", 150_000, 200_000, 80_000, 120_000, 120_000, 30_000},
		{"allowance used up → all kasbon", 50_000, 200_000, 200_000, 0, 0, 50_000},
		{"over-covered month never goes negative", 50_000, 100_000, 150_000, 0, 0, 50_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SplitClaim(c.total, c.monthly, c.covered)
			if got.Available != c.wantAvail || got.Covered != c.wantCov || got.Excess != c.wantEx {
				t.Fatalf("SplitClaim(%d,%d,%d) = %+v, want avail %d covered %d excess %d",
					c.total, c.monthly, c.covered, got, c.wantAvail, c.wantCov, c.wantEx)
			}
			if got.Covered+got.Excess != c.total {
				t.Fatalf("covered + excess must equal the claim")
			}
		})
	}
}

func TestMonthEndSweep(t *testing.T) {
	if got := MonthEndSweep(200_000, 0); got != 200_000 {
		t.Fatalf("healthy month sweeps everything, got %d", got)
	}
	if got := MonthEndSweep(200_000, 80_000); got != 120_000 {
		t.Fatalf("partial use sweeps the rest, got %d", got)
	}
	if got := MonthEndSweep(200_000, 200_000); got != 0 {
		t.Fatalf("fully used sweeps nothing, got %d", got)
	}
	if got := MonthEndSweep(100_000, 150_000); got != 0 {
		t.Fatalf("over-used never pulls from pension, got %d", got)
	}
}

func TestGenerateClaimNumber(t *testing.T) {
	if got := GenerateClaimNumber(2026, 41); got != "KLM-2026-0042" {
		t.Fatalf("got %s", got)
	}
}
