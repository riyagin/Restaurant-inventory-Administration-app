package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// THR (Tunjangan Hari Raya) business logic. A THR run mirrors a payroll period but
// pays a single religious-holiday allowance per employee derived purely from base
// salary and tenure (masa kerja) at the run's payment_date:
//
//	masa kerja >= 12 bulan  -> THR = 1 × base_salary
//	masa kerja  < 12 bulan  -> THR = ceil(months) / 12 × base_salary
//
// "months" is rounded up: any partial month counts as a whole month. MONEY: whole
// rupiah int64 (no ×100), consistent with wages/payroll; rounding half-up via
// roundHalfUp (shared with payroll.go).

// ErrThrLinesNotReviewed is returned by CloseThrRun when at least one line is still
// unreviewed. The handler maps it to HTTP 409 Conflict.
var ErrThrLinesNotReviewed = errors.New("semua baris THR harus direview sebelum run ditutup")

// ErrThrRunLocked is returned when a mutation targets a closed/paid run or its lines.
// The handler maps it to HTTP 409 Conflict.
var ErrThrRunLocked = errors.New("run THR sudah ditutup dan tidak dapat diubah")

// MonthsWorkedCeil returns the tenure in whole months between join and ref, rounding
// any partial month up. Returns 0 when ref is not after join (e.g. an employee who
// joins on/after the payment date is not yet entitled to THR). The minimum for an
// employee who has worked any positive span is 1 month.
func MonthsWorkedCeil(join, ref time.Time) int32 {
	j := dateOnly(join)
	r := dateOnly(ref)
	if !r.After(j) {
		return 0
	}
	months := (r.Year()-j.Year())*12 + int(r.Month()) - int(j.Month())
	if r.Day() < j.Day() {
		months-- // last month not yet completed
	}
	if r.Day() != j.Day() {
		months++ // round the partial month up
	}
	if months < 1 {
		months = 1
	}
	return int32(months)
}

// ThrTenureStart returns the THR tenure "day 0" for an employee: their permanent-status
// date when set (a contract worker who transitioned to permanent starts accruing THR
// from that date), otherwise their original join date (employees permanent from the
// start). Both are date-only.
func ThrTenureStart(joinDate pgtype.Date, permanentSince pgtype.Date) time.Time {
	if permanentSince.Valid {
		return permanentSince.Time
	}
	return joinDate.Time
}

// ThrRatio returns the THR proportion of one month's salary for the given tenure:
// months/12 capped at 1.0 (>= 12 months is a full month's salary).
func ThrRatio(months int32) float64 {
	if months >= 12 {
		return 1.0
	}
	if months < 0 {
		return 0
	}
	return float64(months) / 12
}

// ThrEntitlement is the resolved THR figures for one employee.
type ThrEntitlement struct {
	MonthsWorked int32   `json:"months_worked"`
	Ratio        float64 `json:"ratio"`
	Amount       int64   `json:"amount"`
}

// ComputeThrEntitlement resolves the tenure, ratio and rupiah THR amount for an
// employee given their base salary, the tenure start date (day 0 — see ThrTenureStart)
// and the reference (payment) date.
func ComputeThrEntitlement(baseSalary int64, startDate, refDate time.Time) ThrEntitlement {
	months := MonthsWorkedCeil(startDate, refDate)
	ratio := ThrRatio(months)
	return ThrEntitlement{
		MonthsWorked: months,
		Ratio:        ratio,
		Amount:       roundHalfUp(float64(baseSalary) * ratio),
	}
}

// EmploymentTypeContract is the employees.employment_type value for contract staff
// (PKWT). Contract workers are not entitled to THR and are excluded from THR runs.
const EmploymentTypeContract = "contract"

// GenerateThrResult reports employees that were skipped. SkippedNames lists employees
// with no applicable wage structure; ContractNames lists contract (PKWT) employees who
// are excluded because they are not THR-eligible.
type GenerateThrResult struct {
	Created       int      `json:"created"`
	SkippedNames  []string `json:"skipped"`
	ContractNames []string `json:"contract"`
}

// GenerateThrLines snapshots a THR line for every active, THR-eligible employee.
// Contract workers (employment_type = 'contract') are excluded entirely — they are not
// entitled to THR. Employees with no wage structure applicable on the run's payment
// date are skipped (collected as warnings). Must run inside a transaction (qtx).
func GenerateThrLines(ctx context.Context, qtx *db.Queries, run *db.ThrRun) (*GenerateThrResult, error) {
	employees, err := qtx.ListActiveEmployeesForPayroll(ctx)
	if err != nil {
		return nil, err
	}

	refDate := run.PaymentDate.Time
	res := &GenerateThrResult{SkippedNames: []string{}, ContractNames: []string{}}

	for _, emp := range employees {
		full, err := qtx.GetEmployeeByID(ctx, emp.ID)
		if err != nil {
			return nil, err
		}
		// Contract (PKWT) workers are not eligible for THR.
		if full.EmploymentType == EmploymentTypeContract {
			res.ContractNames = append(res.ContractNames, emp.FullName)
			continue
		}

		ws, err := GetCurrentWage(ctx, qtx, emp.ID, refDate)
		if err != nil {
			return nil, err
		}
		if ws == nil {
			res.SkippedNames = append(res.SkippedNames, emp.FullName)
			continue
		}

		start := ThrTenureStart(full.JoinDate, full.PermanentSince)
		ent := ComputeThrEntitlement(ws.BaseSalary, start, refDate)

		if _, err := qtx.CreateThrLine(ctx, &db.CreateThrLineParams{
			ThrRunID:        run.ID,
			EmployeeID:      emp.ID,
			WageStructureID: ws.ID,
			BaseSalary:      ws.BaseSalary,
			JoinDate:        full.JoinDate,
			MonthsWorked:    ent.MonthsWorked,
			ThrRatio:        NumericFromFloat(ent.Ratio),
			ComputedAmount:  ent.Amount,
			ThrAmount:       ent.Amount,
		}); err != nil {
			return nil, err
		}
		res.Created++
	}

	return res, nil
}

// CloseThrRun locks the run after verifying every line is reviewed, then posts the
// total THR expense per branch to the branch's expense account (same direction as
// payroll's ClosePeriod). Returns ErrThrLinesNotReviewed (→ 409) when any line is
// unreviewed. Must run inside a transaction.
func CloseThrRun(ctx context.Context, qtx *db.Queries, run *db.ThrRun) (*db.ThrRun, error) {
	unreviewed, err := qtx.CountUnreviewedThrLines(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	if unreviewed > 0 {
		return nil, ErrThrLinesNotReviewed
	}

	branchTotals, err := qtx.ListThrLineBranchTotals(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	for _, bt := range branchTotals {
		if !bt.BranchID.Valid || bt.TotalThr == 0 {
			continue
		}
		expenseAcct, err := qtx.GetBranchExpenseAccountID(ctx, bt.BranchID)
		if err != nil {
			return nil, err
		}
		if !expenseAcct.Valid {
			continue
		}
		if err := UpdateBalance(ctx, qtx, expenseAcct.Bytes, bt.TotalThr); err != nil {
			return nil, err
		}
	}

	return qtx.CloseThrRun(ctx, run.ID)
}
