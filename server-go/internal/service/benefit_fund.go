package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Dana Kesejahteraan Karyawan — employer-funded health benefit + pension fund.
// See migration 085 for the money flow. In short, per employee and month M:
//
//	sick claim in M      covered = min(claim, M's allowance − already covered in M)
//	                     Dr Dana Kesehatan / Cr <kas> (covered)
//	                     claim − covered → a PENDING kasbon, repaid through payroll
//	payroll close of M   Dr Beban Gaji - <cabang> / Cr Dana Kesehatan (contribution)
//	                     Dr Dana Kesehatan / Cr Dana Pensiun (contribution − covered)
//	resignation          Dr Dana Pensiun / Cr <kas> (whole pension balance)
//
// The contribution comes from the payroll line's 'benefit_fund' component
// snapshot, so it follows whatever the reviewer saw on the line. Per-employee
// balances are SUM(amount) over benefit_fund_movements, never a counter.

// ComponentTypeBenefitFund is the wage_components.type of the monthly allowance.
// Like daily_allowance it never enters gross/net pay: every payroll switch only
// sums the types it names, so this one falls through untouched.
const ComponentTypeBenefitFund = "benefit_fund"

// System liability accounts seeded by migration 085.
const (
	HealthFundAccountNumber  = 21100 // Dana Kesehatan Karyawan
	PensionFundAccountNumber = 21200 // Dana Pensiun Karyawan
)

// Journal source types.
const (
	SourceBenefitClaim  = "benefit_claim"  // source_id = benefit_claims.id
	SourceBenefitFund   = "benefit_fund"   // source_id = payroll_periods.id
	SourceBenefitPayout = "benefit_payout" // source_id = employees.id
)

// Funds and movement types (mirror the CHECK constraints).
const (
	FundHealth  = "health"
	FundPension = "pension"

	MovementContribution  = "contribution"
	MovementClaim         = "claim"
	MovementClaimReversal = "claim_reversal"
	MovementSweepOut      = "sweep_out"
	MovementSweepIn       = "sweep_in"
	MovementPayout        = "payout"
	MovementOpening       = "opening"
)

var (
	ErrBenefitNoAllowance   = errors.New("karyawan tidak memiliki komponen dana kesejahteraan pada tanggal klaim")
	ErrBenefitMonthClosed   = errors.New("penggajian bulan klaim sudah ditutup; dana bulan itu sudah dialihkan ke dana pensiun")
	ErrBenefitFutureDate    = errors.New("tanggal tidak boleh di masa depan")
	ErrBenefitKasbonLocked  = errors.New("kasbon kelebihan klaim sudah dicairkan; klaim tidak dapat dibatalkan")
	ErrBenefitStillEmployed = errors.New("dana pensiun hanya dapat dicairkan untuk karyawan yang sudah resign / nonaktif")
	ErrBenefitNothingToPay  = errors.New("saldo dana pensiun kosong")
	ErrBenefitUnsettled     = errors.New("masih ada periode penggajian karyawan ini yang belum ditutup / belum terposting; selesaikan dulu agar iuran terakhirnya masuk ke dana pensiun")
	ErrBenefitAccountsGone  = errors.New("akun 'Dana Kesehatan Karyawan' (21100) / 'Dana Pensiun Karyawan' (21200) tidak ditemukan — jalankan migrasi 085")
)

// ── Pure helpers (DB-free, unit-tested) ──────────────────────────────────────

// ClaimSplit is how one sick claim is paid.
type ClaimSplit struct {
	Available int64 `json:"available"` // what was left of the month's allowance
	Covered   int64 `json:"covered"`   // paid from the health fund
	Excess    int64 `json:"excess"`    // becomes a kasbon
}

// SplitClaim divides a claim between the month's remaining allowance and a kasbon.
func SplitClaim(total, monthly, alreadyCovered int64) ClaimSplit {
	avail := monthly - alreadyCovered
	if avail < 0 {
		avail = 0
	}
	covered := total
	if covered > avail {
		covered = avail
	}
	if covered < 0 {
		covered = 0
	}
	return ClaimSplit{Available: avail, Covered: covered, Excess: total - covered}
}

// MonthEndSweep is how much of a month's contribution moves to the pension fund:
// whatever the month's claims did not use. Never negative — if claims exceeded
// the contribution (a wage lowered mid-month, a component zeroed at review), the
// shortfall stays visible as a negative health balance instead of being taken
// out of the pension.
func MonthEndSweep(contribution, covered int64) int64 {
	if s := contribution - covered; s > 0 {
		return s
	}
	return 0
}

// GenerateClaimNumber returns KLM-YYYY-NNNN.
func GenerateClaimNumber(year int, maxSeq int32) string {
	return fmt.Sprintf("KLM-%04d-%04d", year, maxSeq+1)
}

// ── DB helpers ───────────────────────────────────────────────────────────────

func benefitAccounts(ctx context.Context, qtx *db.Queries) (health, pension uuid.UUID, err error) {
	h, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: HealthFundAccountNumber, Valid: true})
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrBenefitAccountsGone
	}
	p, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PensionFundAccountNumber, Valid: true})
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrBenefitAccountsGone
	}
	return h.ID.Bytes, p.ID.Bytes, nil
}

func pgDate(t time.Time) pgtype.Date { return pgtype.Date{Time: dateOnly(t), Valid: true} }

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

// monthIsOpen reports whether month M's benefit allowance can still be claimed:
// true unless M's payroll period exists and is closed (its sweep is then done or
// queued, and a later claim would draw on money already moved to pension).
func monthIsOpen(ctx context.Context, qtx *db.Queries, month time.Time) (bool, error) {
	p, err := qtx.GetPayrollPeriodByMonth(ctx, pgDate(FirstOfMonth(month)))
	if err != nil {
		if isNoRows(err) {
			return true, nil
		}
		return false, err
	}
	return p.Status == "open", nil
}

// MonthAllowance is an employee's allowance position for one month.
type MonthAllowance struct {
	Month     string `json:"month"`
	Monthly   int64  `json:"monthly_amount"`
	Covered   int64  `json:"covered"`
	Available int64  `json:"available"`
	Open      bool   `json:"open"`
}

// GetMonthAllowance returns the month's allowance, what claims used, and what is
// left. asOf picks the wage structure (the claim date, or the month's end).
func GetMonthAllowance(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, asOf time.Time) (*MonthAllowance, error) {
	month := FirstOfMonth(asOf)
	monthly, err := qtx.GetBenefitMonthlyAmount(ctx, &db.GetBenefitMonthlyAmountParams{EmployeeID: employeeID, AsOf: pgDate(asOf)})
	if err != nil {
		return nil, err
	}
	covered, err := qtx.SumCoveredClaimsForMonth(ctx, &db.SumCoveredClaimsForMonthParams{EmployeeID: employeeID, PeriodMonth: pgDate(month)})
	if err != nil {
		return nil, err
	}
	open, err := monthIsOpen(ctx, qtx, month)
	if err != nil {
		return nil, err
	}
	s := SplitClaim(0, monthly, covered)
	return &MonthAllowance{Month: month.Format("2006-01"), Monthly: monthly, Covered: covered, Available: s.Available, Open: open}, nil
}

// ── Claims ───────────────────────────────────────────────────────────────────

// ClaimInput is a sick claim to record. KasbonInstallments is only used when the
// claim exceeds the month's remaining allowance; the handler validates it with the
// same rules as a hand-entered kasbon.
type ClaimInput struct {
	EmployeeID          pgtype.UUID
	EmployeeName        string
	ClaimDate           time.Time
	TotalAmount         int64
	FundSourceAccountID uuid.UUID
	Description         string
	LeaveRequestID      pgtype.UUID
	Actor               uuid.UUID

	KasbonSendingMethod   string
	KasbonResolutionMonth time.Time
	KasbonInstallments    []InstallmentInput
}

// ClaimResult is what CreateClaim did.
type ClaimResult struct {
	Claim  *db.BenefitClaim `json:"claim"`
	Split  ClaimSplit       `json:"split"`
	Kasbon *db.Kasbon       `json:"kasbon,omitempty"`
}

// PreviewClaim computes the split without writing anything.
func PreviewClaim(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, claimDate time.Time, total int64) (ClaimSplit, *MonthAllowance, error) {
	a, err := GetMonthAllowance(ctx, qtx, employeeID, claimDate)
	if err != nil {
		return ClaimSplit{}, nil, err
	}
	return SplitClaim(total, a.Monthly, a.Covered), a, nil
}

// CreateClaim records a sick claim: pays the covered part from the health fund and
// files the excess as a pending kasbon. Must run inside a transaction.
func CreateClaim(ctx context.Context, qtx *db.Queries, in ClaimInput) (*ClaimResult, error) {
	if dateOnly(in.ClaimDate).After(TodayWIB()) {
		return nil, ErrBenefitFutureDate
	}
	split, allowance, err := PreviewClaim(ctx, qtx, in.EmployeeID, in.ClaimDate, in.TotalAmount)
	if err != nil {
		return nil, err
	}
	if !allowance.Open {
		return nil, ErrBenefitMonthClosed
	}
	if allowance.Monthly <= 0 {
		return nil, ErrBenefitNoAllowance
	}
	healthAcct, _, err := benefitAccounts(ctx, qtx)
	if err != nil {
		return nil, err
	}

	year := in.ClaimDate.Year()
	maxSeq, err := qtx.GetMaxBenefitClaimSeqForYear(ctx, fmt.Sprintf("%04d", year))
	if err != nil {
		return nil, err
	}
	number := GenerateClaimNumber(year, maxSeq)

	res := &ClaimResult{Split: split}

	// The excess is an ordinary kasbon in 'pending': a manager still approves it and
	// it is disbursed through the normal kasbon "process" step, so the rule that
	// only managers approve advances holds for claims too.
	var kasbonID pgtype.UUID
	if split.Excess > 0 {
		reqDate := TodayWIB()
		kYear := reqDate.Year()
		kSeq, err := qtx.GetMaxKasbonSeqForYear(ctx, fmt.Sprintf("%04d", kYear))
		if err != nil {
			return nil, err
		}
		k, err := qtx.CreateKasbon(ctx, &db.CreateKasbonParams{
			KasbonNumber:        GenerateKasbonNumber(kYear, kSeq),
			EmployeeID:          in.EmployeeID,
			Amount:              split.Excess,
			Details:             fmt.Sprintf("Kelebihan klaim kesehatan %s: %s", number, in.Description),
			SendingMethod:       in.KasbonSendingMethod,
			FundSourceAccountID: pgUUID(in.FundSourceAccountID),
			RequestDate:         pgDate(reqDate),
			ResolutionMonth:     pgDate(FirstOfMonth(in.KasbonResolutionMonth)),
			CreatedBy:           pgUUID(in.Actor),
		})
		if err != nil {
			return nil, err
		}
		for _, ins := range in.KasbonInstallments {
			if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
				KasbonID: k.ID,
				DueMonth: pgDate(FirstOfMonth(ins.DueMonth)),
				Amount:   ins.Amount,
			}); err != nil {
				return nil, err
			}
		}
		kasbonID = k.ID
		res.Kasbon = k
	}

	claim, err := qtx.CreateBenefitClaim(ctx, &db.CreateBenefitClaimParams{
		ClaimNumber:         number,
		EmployeeID:          in.EmployeeID,
		ClaimDate:           pgDate(in.ClaimDate),
		PeriodMonth:         pgDate(FirstOfMonth(in.ClaimDate)),
		TotalAmount:         in.TotalAmount,
		CoveredAmount:       split.Covered,
		ExcessAmount:        split.Excess,
		MonthlyAmount:       allowance.Monthly,
		AvailableBefore:     split.Available,
		FundSourceAccountID: pgUUID(in.FundSourceAccountID),
		KasbonID:            kasbonID,
		LeaveRequestID:      in.LeaveRequestID,
		Description:         in.Description,
		CreatedBy:           pgUUID(in.Actor),
	})
	if err != nil {
		return nil, err
	}

	if split.Covered > 0 {
		entryID, err := Post(ctx, qtx, Entry{
			Date:        in.ClaimDate,
			SourceType:  SourceBenefitClaim,
			SourceID:    claim.ID.Bytes,
			Description: fmt.Sprintf("Klaim kesehatan %s — %s", number, in.EmployeeName),
			CreatedBy:   in.Actor,
			Lines: []Line{
				Dr(healthAcct, split.Covered),
				Cr(in.FundSourceAccountID, split.Covered),
			},
		})
		if err != nil {
			return nil, err
		}
		if err := qtx.SetBenefitClaimJournal(ctx, &db.SetBenefitClaimJournalParams{ID: claim.ID, JournalEntryID: pgUUID(entryID)}); err != nil {
			return nil, err
		}
		claim.JournalEntryID = pgUUID(entryID)
		if _, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
			EmployeeID:     in.EmployeeID,
			Fund:           FundHealth,
			MovementType:   MovementClaim,
			Amount:         -split.Covered,
			PeriodMonth:    claim.PeriodMonth,
			MovementDate:   claim.ClaimDate,
			ClaimID:        claim.ID,
			JournalEntryID: pgUUID(entryID),
			Note:           in.Description,
			CreatedBy:      pgUUID(in.Actor),
		}); err != nil {
			return nil, err
		}
	}

	res.Claim = claim
	return res, nil
}

// CancelClaim reverses a claim while its month is still open. Its kasbon, if any,
// is cancelled with it — refused once that kasbon has been disbursed, because the
// money has left and the advance must then run its course. Must run inside a tx.
func CancelClaim(ctx context.Context, qtx *db.Queries, claim *db.BenefitClaim, actor uuid.UUID, note string) (*db.BenefitClaim, error) {
	if claim.Status != "paid" {
		return nil, errors.New("klaim sudah dibatalkan")
	}
	open, err := monthIsOpen(ctx, qtx, claim.PeriodMonth.Time)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrBenefitMonthClosed
	}
	if claim.KasbonID.Valid {
		k, err := qtx.GetKasbonByID(ctx, claim.KasbonID)
		if err != nil {
			return nil, err
		}
		switch k.Status {
		case "pending", "approved":
			if _, err := qtx.SetKasbonCancelled(ctx, k.ID); err != nil {
				return nil, err
			}
		case "rejected", "cancelled":
			// nothing to undo
		default:
			return nil, ErrBenefitKasbonLocked
		}
	}

	if claim.CoveredAmount > 0 {
		healthAcct, _, err := benefitAccounts(ctx, qtx)
		if err != nil {
			return nil, err
		}
		entryID, err := Post(ctx, qtx, Entry{
			Date:        TodayWIB(),
			SourceType:  SourceBenefitClaim,
			SourceID:    claim.ID.Bytes,
			Description: fmt.Sprintf("Pembatalan klaim kesehatan %s", claim.ClaimNumber),
			CreatedBy:   actor,
			ReversesID:  claim.JournalEntryID.Bytes,
			Lines: []Line{
				Dr(claim.FundSourceAccountID.Bytes, claim.CoveredAmount),
				Cr(healthAcct, claim.CoveredAmount),
			},
		})
		if err != nil {
			return nil, err
		}
		if _, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
			EmployeeID:     claim.EmployeeID,
			Fund:           FundHealth,
			MovementType:   MovementClaimReversal,
			Amount:         claim.CoveredAmount,
			PeriodMonth:    claim.PeriodMonth,
			MovementDate:   pgDate(TodayWIB()),
			ClaimID:        claim.ID,
			JournalEntryID: pgUUID(entryID),
			Note:           note,
			CreatedBy:      pgUUID(actor),
		}); err != nil {
			return nil, err
		}
	}

	return qtx.SetBenefitClaimCancelled(ctx, &db.SetBenefitClaimCancelledParams{
		ID:          claim.ID,
		CancelledBy: pgUUID(actor),
		CancelNote:  pgtype.Text{String: note, Valid: note != ""},
	})
}

// ── Payroll close ────────────────────────────────────────────────────────────

// PostBenefitFundPeriod books a closed payroll period's contributions and the
// month-end sweep to pension, as one balanced journal entry plus per-employee
// movements. Idempotent; called by the payroll poster in the same transaction as
// the payroll entry. Returns uuid.Nil when the period has nothing to book.
func PostBenefitFundPeriod(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod, actor uuid.UUID) (uuid.UUID, error) {
	done, err := qtx.CountBenefitMovementsForPeriod(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if done > 0 {
		return uuid.Nil, nil
	}
	existing, err := qtx.ListJournalEntriesBySource(ctx, &db.ListJournalEntriesBySourceParams{
		SourceType: SourceBenefitFund, SourceID: period.ID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if len(existing) > 0 {
		return uuid.Nil, nil
	}

	contribs, err := qtx.ListBenefitContributionsForPeriod(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	coveredRows, err := qtx.ListCoveredClaimsByEmployeeForMonth(ctx, period.PeriodMonth)
	if err != nil {
		return uuid.Nil, err
	}
	covered := make(map[pgtype.UUID]int64, len(coveredRows))
	for _, c := range coveredRows {
		covered[c.EmployeeID] = c.Covered
	}
	// Employees whose month was already settled at a pension payout.
	settledRows, err := qtx.ListEmployeesContributedForMonth(ctx, period.PeriodMonth)
	if err != nil {
		return uuid.Nil, err
	}
	settled := make(map[pgtype.UUID]bool, len(settledRows))
	for _, id := range settledRows {
		settled[id] = true
	}

	type perEmp struct {
		id           pgtype.UUID
		contribution int64
		sweep        int64
	}
	var (
		emps         []perEmp
		byBranch     = map[pgtype.UUID]int64{}
		branchName   = map[pgtype.UUID]string{}
		branchOrder  []pgtype.UUID
		totalContrib int64
		totalSweep   int64
	)
	for _, c := range contribs {
		if c.Contribution <= 0 || settled[c.EmployeeID] {
			continue
		}
		sweep := MonthEndSweep(c.Contribution, covered[c.EmployeeID])
		emps = append(emps, perEmp{id: c.EmployeeID, contribution: c.Contribution, sweep: sweep})
		if _, ok := byBranch[c.BranchID]; !ok {
			branchOrder = append(branchOrder, c.BranchID)
			branchName[c.BranchID] = c.BranchName.String
		}
		byBranch[c.BranchID] += c.Contribution
		totalContrib += c.Contribution
		totalSweep += sweep
	}
	if totalContrib == 0 {
		return uuid.Nil, nil
	}

	healthAcct, pensionAcct, err := benefitAccounts(ctx, qtx)
	if err != nil {
		return uuid.Nil, err
	}

	var lines []Line
	for _, b := range branchOrder {
		name := branchName[b]
		memo := name
		if memo == "" {
			memo = "Tanpa Cabang"
		}
		lines = append(lines, Dr(resolveWageAccount(ctx, qtx, b, name), byBranch[b]).WithMemo("iuran dana kesejahteraan "+memo))
	}
	lines = append(lines, Cr(healthAcct, totalContrib).WithMemo("iuran bulan ini"))
	if totalSweep > 0 {
		lines = append(lines,
			Dr(healthAcct, totalSweep).WithMemo("sisa tidak terpakai"),
			Cr(pensionAcct, totalSweep).WithMemo("dialihkan ke dana pensiun"))
	}

	month := period.PeriodMonth.Time
	entryID, err := Post(ctx, qtx, Entry{
		Date:        period.EndDate.Time,
		SourceType:  SourceBenefitFund,
		SourceID:    period.ID.Bytes,
		Description: fmt.Sprintf("Dana kesejahteraan karyawan %s", month.Format("2006-01")),
		CreatedBy:   actor,
		Lines:       lines,
	})
	if err != nil {
		return uuid.Nil, err
	}

	mk := func(emp pgtype.UUID, fund, typ string, amount int64, note string) error {
		if amount == 0 {
			return nil
		}
		_, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
			EmployeeID:      emp,
			Fund:            fund,
			MovementType:    typ,
			Amount:          amount,
			PeriodMonth:     period.PeriodMonth,
			MovementDate:    period.EndDate,
			PayrollPeriodID: period.ID,
			JournalEntryID:  pgUUID(entryID),
			Note:            note,
			CreatedBy:       pgUUID(actor),
		})
		return err
	}
	for _, e := range emps {
		if err := mk(e.id, FundHealth, MovementContribution, e.contribution, "iuran gaji "+month.Format("2006-01")); err != nil {
			return uuid.Nil, err
		}
		if err := mk(e.id, FundHealth, MovementSweepOut, -e.sweep, "sisa tidak terpakai → pensiun"); err != nil {
			return uuid.Nil, err
		}
		if err := mk(e.id, FundPension, MovementSweepIn, e.sweep, "sisa dana kesehatan "+month.Format("2006-01")); err != nil {
			return uuid.Nil, err
		}
	}
	return entryID, nil
}

// ── Payout ───────────────────────────────────────────────────────────────────

// PayoutInput pays an employee's whole pension balance on resignation/retirement.
type PayoutInput struct {
	EmployeeID          pgtype.UUID
	EmployeeName        string
	EmployeeStatus      string
	BranchID            pgtype.UUID
	BranchName          string
	ResignDate          pgtype.Date
	PayoutDate          time.Time
	FundSourceAccountID uuid.UUID
	Note                string
	Actor               uuid.UUID
}

// FinalMonth is the last month's settlement done at payout.
type FinalMonth struct {
	Month        string `json:"month"`
	Contribution int64  `json:"contribution"`
	Covered      int64  `json:"covered"`
	Sweep        int64  `json:"sweep"`
}

// PayoutResult is what PayoutPension paid.
type PayoutResult struct {
	Amount         int64       `json:"amount"`
	JournalEntryID uuid.UUID   `json:"journal_entry_id"`
	FinalMonth     *FinalMonth `json:"final_month,omitempty"`
}

// settleFinalMonth books the leaving month the way payday would have: the month's
// contribution into Dana Kesehatan and its unused part into Dana Pensiun. Without
// it, a claim paid in the month someone resigns would leave Dana Kesehatan in
// debit forever, because payroll only generates lines for active employees. The
// month is the resign month (else the payout month). Skipped when that month's
// contribution is already booked; PostBenefitFundPeriod in turn skips employees
// settled here, so a final payroll run afterwards cannot book it twice.
func settleFinalMonth(ctx context.Context, qtx *db.Queries, in PayoutInput, healthAcct, pensionAcct uuid.UUID) (*FinalMonth, error) {
	ref := in.PayoutDate
	if in.ResignDate.Valid {
		ref = in.ResignDate.Time
	}
	month := FirstOfMonth(ref)
	n, err := qtx.CountContributionForEmployeeMonth(ctx, &db.CountContributionForEmployeeMonthParams{
		EmployeeID: in.EmployeeID, PeriodMonth: pgDate(month),
	})
	if err != nil || n > 0 {
		return nil, err
	}
	// A closed period for that month that did not include this employee still
	// leaves the month unsettled, so only the movements decide — not the period.
	monthly, err := qtx.GetBenefitMonthlyAmount(ctx, &db.GetBenefitMonthlyAmountParams{EmployeeID: in.EmployeeID, AsOf: pgDate(ref)})
	if err != nil {
		return nil, err
	}
	covered, err := qtx.SumCoveredClaimsForMonth(ctx, &db.SumCoveredClaimsForMonthParams{EmployeeID: in.EmployeeID, PeriodMonth: pgDate(month)})
	if err != nil {
		return nil, err
	}
	if monthly <= 0 {
		return nil, nil
	}
	sweep := MonthEndSweep(monthly, covered)
	_, monthEnd := PeriodBounds(month)
	date := in.PayoutDate
	if monthEnd.Before(date) {
		date = monthEnd
	}
	lines := []Line{
		Dr(resolveWageAccount(ctx, qtx, in.BranchID, in.BranchName), monthly).WithMemo("iuran bulan terakhir"),
		Cr(healthAcct, monthly).WithMemo("iuran bulan terakhir"),
	}
	if sweep > 0 {
		lines = append(lines, Dr(healthAcct, sweep), Cr(pensionAcct, sweep).WithMemo("sisa bulan terakhir"))
	}
	entryID, err := Post(ctx, qtx, Entry{
		Date:        date,
		SourceType:  SourceBenefitFund,
		SourceID:    in.EmployeeID.Bytes,
		Description: fmt.Sprintf("Dana kesejahteraan bulan terakhir %s — %s", month.Format("2006-01"), in.EmployeeName),
		CreatedBy:   in.Actor,
		Lines:       lines,
	})
	if err != nil {
		return nil, err
	}
	mk := func(fund, typ string, amount int64, note string) error {
		if amount == 0 {
			return nil
		}
		_, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
			EmployeeID: in.EmployeeID, Fund: fund, MovementType: typ, Amount: amount,
			PeriodMonth: pgDate(month), MovementDate: pgDate(date),
			JournalEntryID: pgUUID(entryID), Note: note, CreatedBy: pgUUID(in.Actor),
		})
		return err
	}
	if err := mk(FundHealth, MovementContribution, monthly, "iuran bulan terakhir (pencairan)"); err != nil {
		return nil, err
	}
	if err := mk(FundHealth, MovementSweepOut, -sweep, "sisa tidak terpakai → pensiun"); err != nil {
		return nil, err
	}
	if err := mk(FundPension, MovementSweepIn, sweep, "sisa dana kesehatan bulan terakhir"); err != nil {
		return nil, err
	}
	return &FinalMonth{Month: month.Format("2006-01"), Contribution: monthly, Covered: covered, Sweep: sweep}, nil
}

// PayoutPension pays out the full pension balance. Refused while the employee is
// still active, or while one of their payroll periods has not finished posting —
// that period's sweep would otherwise land after the payout and leave a balance
// nobody is owed a reminder about. Must run inside a transaction.
func PayoutPension(ctx context.Context, qtx *db.Queries, in PayoutInput) (*PayoutResult, error) {
	if in.EmployeeStatus == "active" {
		return nil, ErrBenefitStillEmployed
	}
	if dateOnly(in.PayoutDate).After(TodayWIB()) {
		return nil, ErrBenefitFutureDate
	}
	unsettled, err := qtx.CountUnsettledPayrollForEmployee(ctx, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if unsettled > 0 {
		return nil, ErrBenefitUnsettled
	}
	healthAcct, pensionAcct, err := benefitAccounts(ctx, qtx)
	if err != nil {
		return nil, err
	}
	final, err := settleFinalMonth(ctx, qtx, in, healthAcct, pensionAcct)
	if err != nil {
		return nil, err
	}
	bal, err := qtx.GetBenefitBalances(ctx, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if bal.PensionBalance <= 0 {
		return nil, ErrBenefitNothingToPay
	}
	entryID, err := Post(ctx, qtx, Entry{
		Date:        in.PayoutDate,
		SourceType:  SourceBenefitPayout,
		SourceID:    in.EmployeeID.Bytes,
		Description: fmt.Sprintf("Pencairan dana pensiun — %s", in.EmployeeName),
		CreatedBy:   in.Actor,
		Lines: []Line{
			Dr(pensionAcct, bal.PensionBalance),
			Cr(in.FundSourceAccountID, bal.PensionBalance),
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
		EmployeeID:     in.EmployeeID,
		Fund:           FundPension,
		MovementType:   MovementPayout,
		Amount:         -bal.PensionBalance,
		MovementDate:   pgDate(in.PayoutDate),
		JournalEntryID: pgUUID(entryID),
		Note:           in.Note,
		CreatedBy:      pgUUID(in.Actor),
	}); err != nil {
		return nil, err
	}
	return &PayoutResult{Amount: bal.PensionBalance, JournalEntryID: entryID, FinalMonth: final}, nil
}
