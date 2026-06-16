package service

import "testing"

func TestCalcLineMultipliersAndRounding(t *testing.T) {
	// daily_rate 100_000, 1.5 overtime multiplier, 2 overtime days:
	//   2 × 100000 × 1.5 = 300000 (exact)
	// holiday 1 day × 100000 × 2.0 = 200000.
	r := CalcLine(CalcLineInput{
		BaseSalary:         5_000_000,
		DailyRate:          100_000,
		OvertimeDays:       2,
		PublicHolidayDays:  1,
		OvertimeMultiplier: 1.5,
		HolidayMultiplier:  2.0,
	})
	if r.OvertimeAmount != 300_000 {
		t.Errorf("overtime_amount = %d, want 300000", r.OvertimeAmount)
	}
	if r.PublicHolidayAmount != 200_000 {
		t.Errorf("public_holiday_amount = %d, want 200000", r.PublicHolidayAmount)
	}
	// gross = 5_000_000 + 0 + 0 + 300_000 + 200_000 = 5_500_000
	if r.GrossPay != 5_500_000 {
		t.Errorf("gross_pay = %d, want 5500000", r.GrossPay)
	}
	if r.NetPay != 5_500_000 {
		t.Errorf("net_pay = %d, want 5500000", r.NetPay)
	}
}

func TestCalcLineHalfUpRounding(t *testing.T) {
	// daily_rate 33_333, 0.5 overtime days, 1.5 multiplier:
	//   0.5 × 33333 × 1.5 = 24999.75 → round half-up → 25000
	r := CalcLine(CalcLineInput{
		DailyRate:          33_333,
		OvertimeDays:       0.5,
		OvertimeMultiplier: 1.5,
	})
	if r.OvertimeAmount != 25_000 {
		t.Errorf("half-up overtime = %d, want 25000", r.OvertimeAmount)
	}

	// daily_rate 100_000, 0.255 holiday days, 2.0 multiplier:
	//   0.255 × 100000 × 2 = 51000 (exact). Use a value that lands on .5 exactly:
	//   1.5 days × 1 × 1.0 multiplier → daily 1, 1.5 → 1.5 → round to 2.
	r2 := CalcLine(CalcLineInput{
		DailyRate:          1,
		PublicHolidayDays:  1.5,
		HolidayMultiplier:  1.0,
		OvertimeMultiplier: 1.0,
	})
	if r2.PublicHolidayAmount != 2 {
		t.Errorf("half-up holiday = %d, want 2", r2.PublicHolidayAmount)
	}
}

func TestCalcLineNetWithAllDeductions(t *testing.T) {
	r := CalcLine(CalcLineInput{
		BaseSalary:              4_000_000,
		DailyRate:               160_000,
		OvertimeDays:            1,
		PublicHolidayDays:       1,
		OvertimeMultiplier:      1.5,
		HolidayMultiplier:       2.0,
		AllowanceTotal:          500_000,
		BonusTotal:              300_000,
		ComponentDeductionTotal: 150_000,
		KasbonDeduction:         400_000,
		UnpaidLeaveDeduction:    160_000, // 1 day × 160000
	})
	// overtime = 1 × 160000 × 1.5 = 240000
	// holiday  = 1 × 160000 × 2.0 = 320000
	if r.OvertimeAmount != 240_000 {
		t.Errorf("overtime = %d, want 240000", r.OvertimeAmount)
	}
	if r.PublicHolidayAmount != 320_000 {
		t.Errorf("holiday = %d, want 320000", r.PublicHolidayAmount)
	}
	// gross = 4_000_000 + 500_000 + 300_000 + 240_000 + 320_000 = 5_360_000
	if r.GrossPay != 5_360_000 {
		t.Errorf("gross = %d, want 5360000", r.GrossPay)
	}
	// net = 5_360_000 − 150_000 − 400_000 − 160_000 = 4_650_000
	if r.NetPay != 4_650_000 {
		t.Errorf("net = %d, want 4650000", r.NetPay)
	}
}

func TestAllLinesReviewed(t *testing.T) {
	if AllLinesReviewed(0, 0) {
		t.Error("empty period should not be considered fully reviewed")
	}
	if AllLinesReviewed(11, 15) {
		t.Error("11/15 should block close")
	}
	if !AllLinesReviewed(15, 15) {
		t.Error("15/15 should allow close")
	}
	if !AllLinesReviewed(16, 15) {
		t.Error("over-count should allow close (defensive)")
	}
}

func TestRoundHalfUp(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{0.4, 0},
		{0.5, 1},
		{0.49999, 0},
		{24999.75, 25000},
		{1.5, 2},
		{2.5, 3},
	}
	for _, c := range cases {
		if got := roundHalfUp(c.in); got != c.want {
			t.Errorf("roundHalfUp(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
