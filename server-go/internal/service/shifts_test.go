package service

import "testing"

func TestShiftWorkMinutes(t *testing.T) {
	hm := func(h, m int) int { return h*60 + m }
	p := func(v int) *int { return &v }
	cases := []struct {
		name          string
		s1, e1        int
		s2, e2        *int
		brk, wantMins int
	}{
		// The shift versions as specified by the business.
		{"jumping 08.30-14.00 + 16.30-22.00 = 11 jam", hm(8, 30), hm(14, 0), p(hm(16, 30)), p(hm(22, 0)), 0, 660},
		{"opening 08.30-19.30 = 11 jam", hm(8, 30), hm(19, 30), nil, nil, 0, 660},
		{"middle 09.30-22.00 less 1h break = 11.5 jam", hm(9, 30), hm(22, 0), nil, nil, 60, 690},
		{"longshift 06.00-18.00 = 12 jam", hm(6, 0), hm(18, 0), nil, nil, 0, 720},
		{"break longer than span floors at zero", hm(8, 0), hm(9, 0), nil, nil, 120, 0},
	}
	for _, c := range cases {
		if got := ShiftWorkMinutes(c.s1, c.e1, c.s2, c.e2, c.brk); got != c.wantMins {
			t.Errorf("%s: got %d minutes, want %d", c.name, got, c.wantMins)
		}
	}
}
