package handler

import "testing"

func TestNormalizeRuleType(t *testing.T) {
	cases := map[string]string{
		"late":                         "late",
		"LATE":                         "late",
		"half_day_early":               "half_day_early",
		"Manual":                       "manual",
		"Setengah Hari (Datang Siang)": "half_day_late",
		"Tidak Absen Masuk & Pulang":   "no_punch",
		"Terlambat":                    "late",
		"bukan aturan":                 "bukan aturan", // unknown passes through for validRuleType to reject
	}
	for in, want := range cases {
		if got := normalizeRuleType(in); got != want {
			t.Errorf("normalizeRuleType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseYaTidak(t *testing.T) {
	truthy := []string{"", "ya", "YA", "yes", "true", "1", "aktif", "y"}
	for _, s := range truthy {
		if !parseYaTidak(s) {
			t.Errorf("parseYaTidak(%q) = false, want true", s)
		}
	}
	falsy := []string{"tidak", "no", "false", "0", "nonaktif"}
	for _, s := range falsy {
		if parseYaTidak(s) {
			t.Errorf("parseYaTidak(%q) = true, want false", s)
		}
	}
}
