package service

import "testing"

func TestRoundUpToBlock(t *testing.T) {
	cases := []struct{ in, block, want int }{
		{0, 15, 0},
		{-5, 15, 0},
		{1, 15, 15},
		{15, 15, 15},
		{16, 15, 30},
		{120, 15, 120},
		{7, 1, 7},
		{7, 0, 7},
	}
	for _, c := range cases {
		if got := RoundUpToBlock(c.in, c.block); got != c.want {
			t.Errorf("RoundUpToBlock(%d, %d) = %d, want %d", c.in, c.block, got, c.want)
		}
	}
}

func TestComputeAttendanceCut(t *testing.T) {
	// 60 000 over an 8-hour day = 125 per minute, 7 500 per hour.
	const daily, eightHours = 60_000, 480
	cases := []struct {
		name              string
		paid, late, early int
		wantMinutes       int
		wantAmount        int64
		wantCapped        bool
	}{
		{"on time", eightHours, 0, 0, 0, 0, false},
		{"6 min late rounds to one block", eightHours, 6, 0, 15, 1_875, false},
		{"2h late", eightHours, 120, 0, 120, 15_000, false},
		{"2h early costs the same as 2h late", eightHours, 0, 120, 120, 15_000, false},
		{"each end rounds on its own", eightHours, 10, 25, 15 + 30, 5_625, false},
		{"scan in and leave costs the whole day", eightHours, 0, 479, 480, 60_000, false},
		{"late and early together never exceed the day", eightHours, 200, 400, 480, 60_000, true},
		{"11h shift prices per its own length", 660, 60, 0, 60, 5_455, false},
	}
	for _, c := range cases {
		r := ComputeAttendanceCut(daily, c.paid, c.late, c.early)
		if r.CutMinutes != c.wantMinutes || r.Amount != c.wantAmount || r.Capped != c.wantCapped {
			t.Errorf("%s: got %d min / %d (capped %v), want %d min / %d (capped %v)",
				c.name, r.CutMinutes, r.Amount, r.Capped, c.wantMinutes, c.wantAmount, c.wantCapped)
		}
	}
}

func TestCalcLineSubtractsAttendanceCut(t *testing.T) {
	r := CalcLine(CalcLineInput{BaseSalary: 1_560_000, AttendanceCutDeduction: 15_000})
	if r.GrossPay != 1_560_000 || r.NetPay != 1_545_000 {
		t.Errorf("gross/net = %d/%d, want 1560000/1545000", r.GrossPay, r.NetPay)
	}
}
