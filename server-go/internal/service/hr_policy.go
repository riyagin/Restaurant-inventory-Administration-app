package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Kebijakan HRD AA (HRD-AA-001 v1.1) rules that are not already expressed by
// the configurable performance policies or the grade scale (migration 088):
//
//   - payroll cut-off: a period for month M runs 26 (M-1) → 25 M;
//   - Kebijakan Kasbon: request window, permanent staff only, no running
//     kasbon, a cap per request, at most two installments starting in the
//     request month, and a score gate (≥ 90 priority, 70–89 evaluation,
//     < 70 refused);
//   - leave that costs pay: unpaid leave (a Saturday/Sunday costs two days'
//     pay) and sick leave without a doctor's note (one day's pay);
//   - Insentif Libur Nasional: 2× the daily uang makan, Monday–Friday only.

// DefaultPolicySettings mirrors the migration defaults; used when the row is
// missing so the rules degrade to the policy rather than to "no rules".
func DefaultPolicySettings() *db.HrPolicySetting {
	return &db.HrPolicySetting{
		ID:                    1,
		PayrollCutoffDay:      25,
		KasbonWindowStartDay:  10,
		KasbonWindowEndDay:    20,
		KasbonMaxAmount:       500_000,
		KasbonMaxInstallments: 2,
		KasbonPermanentOnly:   true,
		KasbonMinScore:        70,
		KasbonPriorityScore:   90,
	}
}

// LoadPolicySettings reads the singleton, falling back to the defaults.
func LoadPolicySettings(ctx context.Context, q *db.Queries) (*db.HrPolicySetting, error) {
	s, err := q.GetHRPolicySettings(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultPolicySettings(), nil
		}
		return nil, err
	}
	return s, nil
}

// ── Payroll cut-off ──────────────────────────────────────────────────────────

// PayrollPeriodBounds returns the attendance window a payroll period for
// `month` covers. cutoffDay 0 is the calendar month. Otherwise the window ends
// on the cutoffDay of the month and starts the day after the previous month's
// cut-off — unless the previous period ended later than that (prevEnd, e.g. the
// last calendar-month period before the switch), in which case it starts the
// day after prevEnd so no day is paid twice.
func PayrollPeriodBounds(month time.Time, cutoffDay int, prevEnd *time.Time) (time.Time, time.Time) {
	first := FirstOfMonth(month)
	if cutoffDay <= 0 {
		return PeriodBounds(first)
	}
	end := first.AddDate(0, 0, cutoffDay-1)
	start := first.AddDate(0, -1, cutoffDay)
	if prevEnd != nil {
		pe := dateOnly(*prevEnd)
		if !pe.Before(start) {
			start = pe.AddDate(0, 0, 1)
		}
	}
	return start, end
}

// ResolvePayrollPeriodBounds applies the configured cut-off and the previous
// period's end date.
func ResolvePayrollPeriodBounds(ctx context.Context, q *db.Queries, month time.Time) (time.Time, time.Time, error) {
	settings, err := LoadPolicySettings(ctx, q)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var prevEnd *time.Time
	prev, err := q.GetLatestPayrollPeriodBefore(ctx, pgtype.Date{Time: FirstOfMonth(month), Valid: true})
	if err == nil && prev != nil && prev.EndDate.Valid {
		t := prev.EndDate.Time
		prevEnd = &t
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, time.Time{}, err
	}
	start, end := PayrollPeriodBounds(month, int(settings.PayrollCutoffDay), prevEnd)
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("periode sebelumnya sudah mencakup sampai %s", prevEnd.Format("2006-01-02"))
	}
	return start, end, nil
}

// ── Kebijakan Kasbon ─────────────────────────────────────────────────────────

// Kasbon approval tracks, stored on kasbons.approval_track.
const (
	KasbonTrackPriority   = "priority"   // score ≥ priority threshold: approval eased
	KasbonTrackEvaluation = "evaluation" // between the thresholds: pending evaluation
)

// ErrKasbonPolicy wraps every refusal by the kasbon policy (→ HTTP 422).
var ErrKasbonPolicy = errors.New("pengajuan kasbon tidak memenuhi kebijakan")

// KasbonEligibilityInput is everything the pure check needs.
type KasbonEligibilityInput struct {
	RequestDate    time.Time
	Amount         int64
	EmploymentType string
	OpenKasbons    int
	Score          int // current month's performance score (100 when not yet evaluated)
	Installments   []InstallmentInput
}

// CheckKasbonEligibility applies Kebijakan Kasbon and returns the approval
// track. Every failure wraps ErrKasbonPolicy with the reason in Indonesian.
func CheckKasbonEligibility(s *db.HrPolicySetting, in KasbonEligibilityInput) (string, error) {
	fail := func(format string, a ...any) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrKasbonPolicy, fmt.Sprintf(format, a...))
	}
	day := in.RequestDate.Day()
	if day < int(s.KasbonWindowStartDay) || day > int(s.KasbonWindowEndDay) {
		return fail("pengajuan hanya dibuka tanggal %d–%d setiap bulan", s.KasbonWindowStartDay, s.KasbonWindowEndDay)
	}
	if s.KasbonPermanentOnly && in.EmploymentType != "permanent" {
		return fail("kasbon hanya untuk karyawan tetap (PKWTT)")
	}
	if in.OpenKasbons > 0 {
		return fail("karyawan masih memiliki kasbon yang belum lunas atau belum diputuskan")
	}
	if s.KasbonMaxAmount > 0 && in.Amount > s.KasbonMaxAmount {
		return fail("nominal maksimal Rp%s per pengajuan", formatRupiahPlain(s.KasbonMaxAmount))
	}
	if err := CheckKasbonInstallmentPolicy(s, in.RequestDate, in.Installments); err != nil {
		return "", err
	}
	switch {
	case in.Score < int(s.KasbonMinScore):
		return fail("nilai poin bulan ini %d (di bawah %d) — tidak disetujui", in.Score, s.KasbonMinScore)
	case in.Score >= int(s.KasbonPriorityScore):
		return KasbonTrackPriority, nil
	default:
		return KasbonTrackEvaluation, nil
	}
}

// CheckKasbonInstallmentPolicy: at most KasbonMaxInstallments salary
// deductions, the first in the request month (Ketentuan Pembayaran Kasbon 1–2).
// Also applied when an approver adjusts the split.
func CheckKasbonInstallmentPolicy(s *db.HrPolicySetting, requestDate time.Time, installments []InstallmentInput) error {
	if n := len(installments); n > int(s.KasbonMaxInstallments) {
		return fmt.Errorf("%w: cicilan maksimal %d kali pemotongan gaji", ErrKasbonPolicy, s.KasbonMaxInstallments)
	}
	reqMonth := FirstOfMonth(requestDate)
	startsInRequestMonth := false
	for _, ins := range installments {
		diff := monthsBetween(reqMonth, FirstOfMonth(ins.DueMonth))
		if diff < 0 || diff >= int(s.KasbonMaxInstallments) {
			return fmt.Errorf("%w: pemotongan cicilan dimulai pada bulan pengajuan dan selesai dalam %d bulan",
				ErrKasbonPolicy, s.KasbonMaxInstallments)
		}
		if diff == 0 {
			startsInRequestMonth = true
		}
	}
	if len(installments) > 0 && !startsInRequestMonth {
		return fmt.Errorf("%w: pemotongan cicilan pertama harus pada bulan pengajuan", ErrKasbonPolicy)
	}
	return nil
}

// formatRupiahPlain renders 500000 as "500.000".
func formatRupiahPlain(v int64) string {
	s := fmt.Sprintf("%d", v)
	out := []byte{}
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return string(out)
}

// CurrentMonthScore is the employee's running score for the month containing
// date: 100 when nothing has been evaluated yet (everyone starts the month at
// 100 points).
func CurrentMonthScore(ctx context.Context, q *db.Queries, employeeID pgtype.UUID, date time.Time) (int, error) {
	s, err := q.GetPerformanceScore(ctx, &db.GetPerformanceScoreParams{
		EmployeeID:  employeeID,
		PeriodMonth: pgtype.Date{Time: FirstOfMonth(date), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 100, nil
		}
		return 0, err
	}
	return int(s.Score), nil
}

// ── Leave that costs pay ─────────────────────────────────────────────────────

// LeavePayCutDayWeight is how many days' pay one leave day costs: unpaid leave
// on a Saturday or Sunday costs two (Ketentuan Khusus Izin Sabtu dan Minggu),
// any other unpaid day one, and a sick day without a doctor's note one.
func LeavePayCutDayWeight(d time.Time, unpaid bool) int {
	if unpaid {
		if wd := isoWeekday(d); wd == 6 || wd == 7 {
			return 2
		}
	}
	return 1
}

// LeavePayCutDay is one leave day that costs pay.
type LeavePayCutDay struct {
	Date   time.Time
	Unpaid bool // unpaid leave; false = sick leave without a doctor's note
	Weight int  // days' pay it costs (2 for unpaid leave on a weekend)
	Amount int64
}

// LeavePayCut counts the approved leave days in [from, to] that cost pay and
// prices them at dailyRate. days is the number of leave days (what the payslip
// shows); deduction already includes the weekend weight.
func LeavePayCut(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, dailyRate int64) (int, int64, error) {
	list, err := LeavePayCutDays(ctx, qtx, employeeID, from, to, dailyRate)
	var total int64
	for _, d := range list {
		total += d.Amount
	}
	return len(list), total, err
}

// LeavePayCutDays is LeavePayCut day by day, in date order (the payslip lists
// them). LeavePayCut sums it, so the two cannot disagree.
func LeavePayCutDays(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, dailyRate int64) ([]LeavePayCutDay, error) {
	from, to = dateOnlyUTC(from), dateOnlyUTC(to)
	rows, err := qtx.ListApprovedPayCutLeaveOverlapping(ctx, &db.ListApprovedPayCutLeaveOverlappingParams{
		EmployeeID: employeeID,
		WindowFrom: pgtype.Date{Time: from, Valid: true},
		WindowTo:   pgtype.Date{Time: to, Valid: true},
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	workDays := LeaveScheduleForBranch(ctx, qtx, branchForEmployee(ctx, qtx, employeeID))
	holidays, err := HolidaySetInRange(ctx, qtx, from, to)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []LeavePayCutDay
	for _, r := range rows {
		s, e := dateOnlyUTC(r.StartDate.Time), dateOnlyUTC(r.EndDate.Time)
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		for _, d := range WorkingDaysInRange(s, e, workDays, holidays) {
			key := d.Format("2006-01-02")
			if seen[key] {
				continue
			}
			seen[key] = true
			w := LeavePayCutDayWeight(d, !r.IsPaid)
			out = append(out, LeavePayCutDay{Date: d, Unpaid: !r.IsPaid, Weight: w, Amount: int64(w) * dailyRate})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// ── Insentif Libur Nasional ──────────────────────────────────────────────────

// MealAllowanceDailyRate is the employee's daily uang makan: the sum of the
// per-present-day daily_allowance components of the wage structure, before any
// grade (the policy pays uang makan on attendance alone).
func MealAllowanceDailyRate(components []*db.ListEmployeeWageComponentsRow) int64 {
	var rate int64
	for _, c := range components {
		if c.ComponentType == ComponentTypeDailyAllowance && c.ComponentCalcMethod == CalcMethodPerPresentDay {
			rate += c.Amount
		}
	}
	return rate
}

// HolidayIncentive returns the national-holiday days the employee worked in
// [from, to] that fall Monday–Friday, and the per-day rate the payroll
// multiplier (2×) is applied to: the daily uang makan.
func HolidayIncentive(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, from, to time.Time, components []*db.ListEmployeeWageComponentsRow) (int, int64, error) {
	n, err := qtx.CountPresentOnWeekdayHolidays(ctx, &db.CountPresentOnWeekdayHolidaysParams{
		EmployeeID: employeeID,
		DateFrom:   pgtype.Date{Time: dateOnly(from), Valid: true},
		DateTo:     pgtype.Date{Time: dateOnly(to), Valid: true},
	})
	if err != nil {
		return 0, 0, err
	}
	return int(n), MealAllowanceDailyRate(components), nil
}
