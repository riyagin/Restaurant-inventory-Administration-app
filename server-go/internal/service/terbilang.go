package service

import "strings"

// Terbilang converts a whole-rupiah amount into Indonesian words (the "terbilang"
// used on payslips and financial documents). It does NOT append the " rupiah"
// suffix — the payslip caller adds it (see service/payslip.go) so the same helper
// can be reused for non-currency wording if ever needed.
//
// Rules implemented:
//   - 0 => "nol"
//   - satuan / belasan / puluhan
//   - "seratus", "seribu" special cases (1 hundred/thousand => se-, not "satu")
//   - ratus, ribu, juta, miliar, triliun scales
//   - negative numbers are prefixed with "minus "
func Terbilang(n int64) string {
	if n == 0 {
		return "nol"
	}
	if n < 0 {
		// Guard against overflow on math.MinInt64 by negating as uint64 below.
		return "minus " + terbilangPositive(uint64(-n))
	}
	return terbilangPositive(uint64(n))
}

var satuan = []string{
	"", "satu", "dua", "tiga", "empat", "lima",
	"enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas",
}

// terbilangThreeDigits handles 0..999. Returns words without leading/trailing space.
func terbilangThreeDigits(n uint64) string {
	switch {
	case n == 0:
		return ""
	case n < 12:
		return satuan[n]
	case n < 20:
		return satuan[n-10] + " belas"
	case n < 100:
		return strings.TrimSpace(satuan[n/10] + " puluh " + satuan[n%10])
	case n < 200:
		return strings.TrimSpace("seratus " + terbilangThreeDigits(n%100))
	default: // 200..999
		return strings.TrimSpace(satuan[n/100] + " ratus " + terbilangThreeDigits(n%100))
	}
}

// terbilangPositive handles any value > 0 by chunking into groups of three digits
// and attaching the scale words. The thousands group keeps the "seribu" special
// case (exactly 1000 => "seribu", not "satu ribu").
func terbilangPositive(n uint64) string {
	if n < 1000 {
		return terbilangThreeDigits(n)
	}

	// Scales in descending order with their divisors.
	scales := []struct {
		name    string
		divisor uint64
	}{
		{"triliun", 1_000_000_000_000},
		{"miliar", 1_000_000_000},
		{"juta", 1_000_000},
		{"ribu", 1_000},
	}

	var parts []string
	rem := n
	for _, s := range scales {
		if rem >= s.divisor {
			chunk := rem / s.divisor
			rem = rem % s.divisor
			if s.name == "ribu" && chunk == 1 {
				parts = append(parts, "seribu")
			} else {
				parts = append(parts, terbilangThreeDigits(chunk)+" "+s.name)
			}
		}
	}
	if rem > 0 {
		parts = append(parts, terbilangThreeDigits(rem))
	}
	return strings.Join(parts, " ")
}
