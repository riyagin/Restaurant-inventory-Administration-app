package service

import "testing"

func TestTerbilang(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "nol"},
		{1, "satu"},
		{7, "tujuh"},
		{10, "sepuluh"},
		{11, "sebelas"},
		{12, "dua belas"},
		{19, "sembilan belas"},
		{20, "dua puluh"},
		{21, "dua puluh satu"},
		{99, "sembilan puluh sembilan"},
		// seratus / 1xx rules
		{100, "seratus"},
		{101, "seratus satu"},
		{110, "seratus sepuluh"},
		{115, "seratus lima belas"},
		{200, "dua ratus"},
		{250, "dua ratus lima puluh"},
		{999, "sembilan ratus sembilan puluh sembilan"},
		// seribu vs satu ribu rule
		{1000, "seribu"},
		{1001, "seribu satu"},
		{1500, "seribu lima ratus"},
		{2000, "dua ribu"},
		{11000, "sebelas ribu"},
		{21000, "dua puluh satu ribu"},
		// exact thousands
		{15000, "lima belas ribu"},
		{100000, "seratus ribu"},
		// the canonical 1.500.000 example
		{1500000, "satu juta lima ratus ribu"},
		// juta / miliar / triliun boundaries
		{1000000, "satu juta"},
		{2500000, "dua juta lima ratus ribu"},
		{1000000000, "satu miliar"},
		{1234567, "satu juta dua ratus tiga puluh empat ribu lima ratus enam puluh tujuh"},
		{1000000000000, "satu triliun"},
		// negative
		{-1500, "minus seribu lima ratus"},
	}
	for _, c := range cases {
		if got := Terbilang(c.n); got != c.want {
			t.Errorf("Terbilang(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestTerbilangPayslipExample mirrors the spec's example, including the " rupiah"
// suffix the payslip caller appends.
func TestTerbilangPayslipExample(t *testing.T) {
	got := Terbilang(1500000) + " rupiah"
	want := "satu juta lima ratus ribu rupiah"
	if got != want {
		t.Errorf("payslip terbilang = %q, want %q", got, want)
	}
}
