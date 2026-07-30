package handler

import (
	"strings"
	"testing"
)

func TestParseStatusList(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"single", "dispatched", []string{"dispatched"}},
		{"the common case: hide both machine-written statuses",
			"dispatched,cancelled", []string{"dispatched", "cancelled"}},
		{"spaces are trimmed", " paid , unpaid ", []string{"paid", "unpaid"}},
		{"duplicates collapse", "paid,paid", []string{"paid"}},
		{"unknown values are dropped, not passed to the query",
			"dispatched,'; DROP TABLE invoices--,bogus", []string{"dispatched"}},
		{"all unknown", "bogus", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStatusList(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("parseStatusList(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parseStatusList(%q) = %v, want %v", tc.raw, got, tc.want)
				}
			}
		})
	}
}

// Excluding every known status must still produce a valid, empty result rather
// than silently dropping the filter and showing everything.
func TestParseStatusListAcceptsEveryKnownStatus(t *testing.T) {
	var all []string
	for s := range invoicePaymentStatuses {
		all = append(all, s)
	}
	got := parseStatusList(strings.Join(all, ","))
	if len(got) != len(invoicePaymentStatuses) {
		t.Fatalf("got %d statuses, want %d: %v", len(got), len(invoicePaymentStatuses), got)
	}
}
