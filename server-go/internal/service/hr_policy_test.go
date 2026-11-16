package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

func pd(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestPayrollPeriodBounds(t *testing.T) {
	cases := []struct {
		name       string
		month      string
		cutoff     int
		prevEnd    string
		start, end string
	}{
		{"calendar month", "2026-12-01", 0, "", "2026-12-01", "2026-12-31"},
		{"cut-off 25", "2026-12-01", 25, "", "2026-11-26", "2026-12-25"},
		{"cut-off across year", "2027-01-01", 25, "", "2026-12-26", "2027-01-25"},
		{"february", "2027-02-01", 25, "", "2027-01-26", "2027-02-25"},
		// Switch-over: the last calendar-month period ended 31 Oct, so the first
		// cut-off period starts 1 Nov instead of 26 Oct — no day is paid twice.
		{"switch-over", "2026-11-01", 25, "2026-10-31", "2026-11-01", "2026-11-25"},
		// The month after: previous period ended on its cut-off as normal.
		{"after switch-over", "2026-12-01", 25, "2026-11-25", "2026-11-26", "2026-12-25"},
	}
	for _, c := range cases {
		var prev *time.Time
		if c.prevEnd != "" {
			p := pd(c.prevEnd)
			prev = &p
		}
		s, e := PayrollPeriodBounds(pd(c.month), c.cutoff, prev)
		if s != pd(c.start) || e != pd(c.end) {
			t.Errorf("%s: got %s → %s, want %s → %s", c.name, s.Format("2006-01-02"), e.Format("2006-01-02"), c.start, c.end)
		}
	}
}

func TestCheckKasbonEligibility(t *testing.T) {
	s := DefaultPolicySettings()
	ok := KasbonEligibilityInput{
		RequestDate:    pd("2026-11-15"),
		Amount:         500_000,
		EmploymentType: "permanent",
		Score:          95,
		Installments: []InstallmentInput{
			{DueMonth: pd("2026-11-01"), Amount: 250_000},
			{DueMonth: pd("2026-12-01"), Amount: 250_000},
		},
	}
	if track, err := CheckKasbonEligibility(s, ok); err != nil || track != KasbonTrackPriority {
		t.Fatalf("eligible request: track=%q err=%v", track, err)
	}

	mod := func(f func(*KasbonEligibilityInput)) KasbonEligibilityInput {
		in := ok
		in.Installments = append([]InstallmentInput(nil), ok.Installments...)
		f(&in)
		return in
	}
	refusals := []struct {
		name string
		in   KasbonEligibilityInput
		want string
	}{
		{"before window", mod(func(i *KasbonEligibilityInput) { i.RequestDate = pd("2026-11-09") }), "tanggal 10–20"},
		{"after window", mod(func(i *KasbonEligibilityInput) { i.RequestDate = pd("2026-11-21") }), "tanggal 10–20"},
		{"contract", mod(func(i *KasbonEligibilityInput) { i.EmploymentType = "contract" }), "PKWTT"},
		{"running kasbon", mod(func(i *KasbonEligibilityInput) { i.OpenKasbons = 1 }), "belum lunas"},
		{"over cap", mod(func(i *KasbonEligibilityInput) { i.Amount = 500_001 }), "500.000"},
		{"three installments", mod(func(i *KasbonEligibilityInput) {
			i.Installments = append(i.Installments, InstallmentInput{DueMonth: pd("2027-01-01"), Amount: 1})
		}), "maksimal 2"},
		{"starts next month", mod(func(i *KasbonEligibilityInput) {
			i.Installments = []InstallmentInput{{DueMonth: pd("2026-12-01"), Amount: 500_000}}
		}), "bulan pengajuan"},
		{"tier C", mod(func(i *KasbonEligibilityInput) { i.Score = 69 }), "tidak disetujui"},
	}
	for _, c := range refusals {
		_, err := CheckKasbonEligibility(s, c.in)
		if !errors.Is(err, ErrKasbonPolicy) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want ErrKasbonPolicy containing %q", c.name, err, c.want)
		}
	}

	if track, err := CheckKasbonEligibility(s, mod(func(i *KasbonEligibilityInput) { i.Score = 70 })); err != nil || track != KasbonTrackEvaluation {
		t.Errorf("score 70: track=%q err=%v, want evaluation", track, err)
	}
	if track, err := CheckKasbonEligibility(s, mod(func(i *KasbonEligibilityInput) { i.Score = 90 })); err != nil || track != KasbonTrackPriority {
		t.Errorf("score 90: track=%q err=%v, want priority", track, err)
	}
	// A cap of 0 means no cap.
	noCap := *s
	noCap.KasbonMaxAmount = 0
	if _, err := CheckKasbonEligibility(&noCap, mod(func(i *KasbonEligibilityInput) { i.Amount = 5_000_000 })); err != nil {
		t.Errorf("no cap: %v", err)
	}
}

func TestLeaveViolationRule(t *testing.T) {
	cases := []struct {
		paid, quota, needsNote, hasNote bool
		want                            string
	}{
		{true, true, false, false, ""},               // day off from the monthly allowance
		{false, false, false, false, "unpaid_leave"}, // Izin Tanpa Gaji → Izin Satu Hari
		{true, false, true, false, "sick_no_note"},   // Sakit without a note
		{true, false, true, true, ""},                // Sakit with a note
		{true, false, false, false, ""},              // other paid / normative leave
	}
	for _, c := range cases {
		if got := LeaveViolationRule(c.paid, c.quota, c.needsNote, c.hasNote); got != c.want {
			t.Errorf("%+v → %q, want %q", c, got, c.want)
		}
	}
}

func TestLeavePayCutDayWeight(t *testing.T) {
	if w := LeavePayCutDayWeight(pd("2026-11-14"), true); w != 2 { // Saturday
		t.Errorf("unpaid Saturday = %d, want 2", w)
	}
	if w := LeavePayCutDayWeight(pd("2026-11-15"), true); w != 2 { // Sunday
		t.Errorf("unpaid Sunday = %d, want 2", w)
	}
	if w := LeavePayCutDayWeight(pd("2026-11-16"), true); w != 1 { // Monday
		t.Errorf("unpaid Monday = %d, want 1", w)
	}
	if w := LeavePayCutDayWeight(pd("2026-11-14"), false); w != 1 { // sick, no note, Saturday
		t.Errorf("sick without note on Saturday = %d, want 1", w)
	}
}

func TestMealAllowanceDailyRate(t *testing.T) {
	comps := []*db.ListEmployeeWageComponentsRow{
		{ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodPerPresentDay, Amount: 20_000},
		{ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodPerPresentDay, Amount: 5_000},
		{ComponentType: ComponentTypeDailyAllowance, ComponentCalcMethod: CalcMethodFixed, Amount: 99_000},
		{ComponentType: ComponentTypeAllowance, ComponentCalcMethod: CalcMethodPerPresentDay, Amount: 99_000},
	}
	if got := MealAllowanceDailyRate(comps); got != 25_000 {
		t.Errorf("daily uang makan = %d, want 25000", got)
	}
}

func TestGradeComponentOverrides(t *testing.T) {
	sc := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}  // Service Charge
	kj := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}  // Uang Kerajinan
	oth := pgtype.UUID{Bytes: [16]byte{3}, Valid: true} // anything else
	scKey, kjKey := "01000000-0000-0000-0000-000000000000", "02000000-0000-0000-0000-000000000000"

	// The policy's scale: A ≥ 90 pays everything; B ≥ 70 pays Service Charge
	// at 50% and Uang Kerajinan at 0%; C pays nothing.
	s, err := ValidateGradeScale([]GradeTier{
		{Label: "A", MinScore: 90, PayPercent: 100},
		{Label: "B", MinScore: 70, PayPercent: 0, ComponentPercents: map[string]int32{scKey: 50}},
		{Label: "C", MinScore: 0, PayPercent: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := s.Resolve(pgtype.Int4{Int32: 80, Valid: true})
	if got := GradedComponentAmount(1_000_000, true, b, sc); got != 500_000 {
		t.Errorf("Tier B service charge = %d, want 500000", got)
	}
	if got := GradedComponentAmount(350_000, true, b, kj); got != 0 {
		t.Errorf("Tier B kerajinan = %d, want 0", got)
	}
	if got := GradedComponentAmount(100_000, false, b, oth); got != 100_000 {
		t.Errorf("not grade-linked = %d, want unchanged", got)
	}
	a := s.Resolve(pgtype.Int4{Int32: 95, Valid: true})
	if got := GradedComponentAmount(350_000, true, a, kj); got != 350_000 {
		t.Errorf("Tier A kerajinan = %d, want 350000", got)
	}

	// An override that pays a lower tier more than a higher one is refused.
	_, err = ValidateGradeScale([]GradeTier{
		{Label: "A", MinScore: 90, PayPercent: 100, ComponentPercents: map[string]int32{kjKey: 40}},
		{Label: "B", MinScore: 0, PayPercent: 50},
	})
	if !errors.Is(err, ErrInvalidGradeScale) {
		t.Errorf("non-monotonic override: err = %v", err)
	}
}
