package service

import (
	"testing"
	"time"
)

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestGenerateKasbonNumber(t *testing.T) {
	cases := []struct {
		year   int
		maxSeq int32
		want   string
	}{
		{2026, 0, "KSB-2026-0001"},
		{2026, 4, "KSB-2026-0005"},
		{2026, 41, "KSB-2026-0042"},
		{2025, 999, "KSB-2025-1000"},
		{2026, 9999, "KSB-2026-10000"},
	}
	for _, c := range cases {
		if got := GenerateKasbonNumber(c.year, c.maxSeq); got != c.want {
			t.Errorf("GenerateKasbonNumber(%d, %d) = %q, want %q", c.year, c.maxSeq, got, c.want)
		}
	}
}

func TestValidateResolutionWindow(t *testing.T) {
	req := mustDate("2026-06-10")
	cases := []struct {
		name    string
		resMon  time.Time
		wantErr bool
	}{
		{"same month (in window)", mustDate("2026-06-01"), false},
		{"one month after", mustDate("2026-07-15"), false},
		{"six months after (in window)", mustDate("2026-12-25"), false},
		{"twelve months after (boundary)", mustDate("2027-06-30"), false},
		{"thirteen months after (out of window)", mustDate("2027-07-01"), true},
		{"before request month", mustDate("2026-05-20"), true},
	}
	for _, c := range cases {
		err := ValidateResolutionWindow(req, c.resMon)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: ValidateResolutionWindow err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}

	// Year-crossing boundary: Nov 2026 request, Nov 2027 = 12 months → in window.
	if err := ValidateResolutionWindow(mustDate("2026-11-05"), mustDate("2027-11-10")); err != nil {
		t.Errorf("year-crossing 12-month window should be valid, got %v", err)
	}
	// Nov 2026 → Dec 2027 = 13 months → out of window.
	if err := ValidateResolutionWindow(mustDate("2026-11-05"), mustDate("2027-12-10")); err == nil {
		t.Error("year-crossing 13-month window should be invalid")
	}
}

func TestValidateInstallmentSplit(t *testing.T) {
	req := mustDate("2026-06-10")

	// Single default installment in resolution month.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-07-01"), Amount: 1_000_000},
	}); err != nil {
		t.Errorf("valid single installment rejected: %v", err)
	}

	// Valid 2-way split summing to total, both in window.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-06-01"), Amount: 400_000},
		{DueMonth: mustDate("2026-07-01"), Amount: 600_000},
	}); err != nil {
		t.Errorf("valid 2-way split rejected: %v", err)
	}

	// Sum mismatch.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-06-01"), Amount: 400_000},
		{DueMonth: mustDate("2026-07-01"), Amount: 500_000},
	}); err == nil {
		t.Error("sum mismatch should be rejected")
	}

	// A valid 3-way split summing to total, all within the window.
	if err := ValidateInstallmentSplit(900_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-06-01"), Amount: 300_000},
		{DueMonth: mustDate("2026-07-01"), Amount: 300_000},
		{DueMonth: mustDate("2026-08-01"), Amount: 300_000},
	}); err != nil {
		t.Errorf("valid 3-way split rejected: %v", err)
	}

	// More than MaxRepaymentMonths installments should be rejected.
	tooMany := make([]InstallmentInput, 0, MaxRepaymentMonths+1)
	for i := 0; i <= MaxRepaymentMonths; i++ {
		tooMany = append(tooMany, InstallmentInput{DueMonth: req.AddDate(0, i, 0), Amount: 1_000})
	}
	if err := ValidateInstallmentSplit(int64(len(tooMany))*1_000, req, tooMany); err == nil {
		t.Errorf(">%d installments should be rejected", MaxRepaymentMonths)
	}

	// Zero / negative amount.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-07-01"), Amount: 0},
	}); err == nil {
		t.Error("zero amount should be rejected")
	}

	// Duplicate due months.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-07-01"), Amount: 500_000},
		{DueMonth: mustDate("2026-07-15"), Amount: 500_000},
	}); err == nil {
		t.Error("duplicate due months should be rejected")
	}

	// Installment out of the 2-month window.
	if err := ValidateInstallmentSplit(1_000_000, req, []InstallmentInput{
		{DueMonth: mustDate("2026-06-01"), Amount: 500_000},
		{DueMonth: mustDate("2026-10-01"), Amount: 500_000},
	}); err == nil {
		t.Error("out-of-window installment should be rejected")
	}
}

func TestTransitionGuards(t *testing.T) {
	if !CanEdit("pending") || CanEdit("approved") || CanEdit("processed") {
		t.Error("CanEdit should be pending-only")
	}
	if !CanCancel("pending") || !CanCancel("approved") {
		t.Error("CanCancel should allow pending and approved")
	}
	if CanCancel("processed") || CanCancel("resolved") || CanCancel("rejected") || CanCancel("cancelled") {
		t.Error("CanCancel should reject post-approval states")
	}
	if !CanApprove("pending") || CanApprove("approved") {
		t.Error("CanApprove should be pending-only")
	}
	if !CanProcess("approved") || CanProcess("pending") || CanProcess("processed") {
		t.Error("CanProcess should be approved-only")
	}
}
