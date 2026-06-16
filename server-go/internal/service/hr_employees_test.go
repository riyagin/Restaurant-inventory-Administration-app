package service

import "testing"

// TestNextEmployeeCode verifies zero-padded sequential code generation.
func TestNextEmployeeCode(t *testing.T) {
	cases := []struct {
		maxSeq int32
		want   string
	}{
		{0, "EMP-0001"},
		{1, "EMP-0002"},
		{41, "EMP-0042"},
		{999, "EMP-1000"},
		{9999, "EMP-10000"},
	}
	for _, c := range cases {
		if got := NextEmployeeCode(c.maxSeq); got != c.want {
			t.Errorf("NextEmployeeCode(%d) = %q, want %q", c.maxSeq, got, c.want)
		}
	}
}
