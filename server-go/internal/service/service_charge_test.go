package service

import "testing"

func sum64(xs []int64) (s int64) {
	for _, x := range xs {
		s += x
	}
	return
}

func TestSplitByWeight(t *testing.T) {
	cases := []struct {
		name    string
		total   int64
		weights []int64
		want    []int64
	}{
		{"equal", 3_000_000, []int64{100, 100, 100}, []int64{1_000_000, 1_000_000, 1_000_000}},
		// One employee ineligible (grade pays 0%): the other two split the whole pot.
		{"ineligible redistributes", 3_000_000, []int64{100, 0, 100}, []int64{1_500_000, 0, 1_500_000}},
		// Tier B (50%) gets half a share: 100+50+100 = 250 → 1.2M / 600k / 1.2M.
		{"half share", 3_000_000, []int64{100, 50, 100}, []int64{1_200_000, 600_000, 1_200_000}},
		// Remainders: 100 over three equal weights → 34/33/33, never 99 or 101.
		{"rounding", 100, []int64{1, 1, 1}, []int64{34, 33, 33}},
		{"nobody eligible", 1_000, []int64{0, 0}, []int64{0, 0}},
		{"no pool", 0, []int64{100, 100}, []int64{0, 0}},
		{"empty", 500, nil, []int64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SplitByWeight(c.total, c.weights)
			if len(got) != len(c.want) {
				t.Fatalf("len = %d, want %d", len(got), len(c.want))
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestSplitByWeightAlwaysSumsToPot(t *testing.T) {
	weights := []int64{100, 50, 50, 100, 0, 75, 100}
	for _, total := range []int64{1, 7, 99, 1_234_567, 9_999_999_999} {
		if got := sum64(SplitByWeight(total, weights)); got != total {
			t.Errorf("total %d split sums to %d", total, got)
		}
	}
}
