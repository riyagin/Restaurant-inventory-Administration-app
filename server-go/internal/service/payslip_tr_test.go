package service

import "testing"

// tr must emit single cp1252 bytes, not UTF-8: fpdf's core fonts read every
// byte as one glyph, so a UTF-8 "·" (C2 B7) printed as "Â·".
func TestTrEncodesCP1252(t *testing.T) {
	cases := map[string]string{
		"Poin 10 · Grade C": "Poin 10 \xb7 Grade C",
		"5× Alpha":          "5\xd7 Alpha",
		"CONTOH — data":     "CONTOH \x97 data",
		"Café":              "Caf\xe9",
		"plain":             "plain",
	}
	for in, want := range cases {
		if got := tr(in); got != want {
			t.Errorf("tr(%q) = %q, want %q", in, got, want)
		}
	}
}

// A character cp1252 cannot hold must still come out as one byte.
func TestTrUnmappable(t *testing.T) {
	if got := tr("a✓b"); len(got) != 3 {
		t.Errorf("tr(\"a✓b\") = %q (%d bytes), want 3 bytes", got, len(got))
	}
}
