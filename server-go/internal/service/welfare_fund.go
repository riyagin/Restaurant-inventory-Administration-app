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

// Dana Kesejahteraan: employer retirement fund (migration 090).
// Close: Dr Beban Gaji / Cr Dana Kesejahteraan; payout: Dr Dana Kesejahteraan / Cr kas.
// Balances are SUM(amount) over welfare_fund_movements.

// ComponentTypeWelfareFund is the wage_components.type of the per-period
// contribution. It is an employer contribution and never enters gross/net pay.
const ComponentTypeWelfareFund = "welfare_fund"

// WelfareFundAccountNumber is the system liability account seeded by migration 090.
const WelfareFundAccountNumber = 21300 // Dana Kesejahteraan Karyawan

// Journal source types.
const (
	SourceWelfareFund   = "welfare_fund"   // source_id = payroll_periods.id
	SourceWelfarePayout = "welfare_payout" // source_id = employees.id
)

// Movement types (mirror the CHECK constraint).
const (
	WelfareContribution = "contribution"
	WelfarePayout       = "payout"
	WelfareOpening      = "opening"
)

var (
	ErrWelfareStillEmployed = errors.New("dana kesejahteraan hanya dapat dicairkan untuk karyawan yang sudah pensiun / resign / nonaktif")
	ErrWelfareNothingToPay  = errors.New("saldo dana kesejahteraan kosong")
	ErrWelfareUnsettled     = errors.New("masih ada periode penggajian karyawan ini yang belum ditutup / belum terposting; selesaikan dulu agar iuran terakhirnya masuk ke dana kesejahteraan")
	ErrWelfareFutureDate    = errors.New("tanggal pencairan tidak boleh di masa depan")
	ErrWelfareAccountGone   = errors.New("akun 'Dana Kesejahteraan Karyawan' (21300) tidak ditemukan — jalankan migrasi 090")
)

func welfareAccount(ctx context.Context, qtx *db.Queries) (uuid.UUID, error) {
	a, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: WelfareFundAccountNumber, Valid: true})
	if err != nil {
		return uuid.Nil, ErrWelfareAccountGone
	}
	return a.ID.Bytes, nil
}

// PostWelfareFundPeriod is idempotent and runs in the payroll poster's transaction.
func PostWelfareFundPeriod(ctx context.Context, qtx *db.Queries, period *db.PayrollPeriod, actor uuid.UUID) (uuid.UUID, error) {
	done, err := qtx.CountWelfareContributionsForPeriod(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if done > 0 {
		return uuid.Nil, nil
	}
	existing, err := qtx.ListJournalEntriesBySource(ctx, &db.ListJournalEntriesBySourceParams{
		SourceType: SourceWelfareFund, SourceID: period.ID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if len(existing) > 0 {
		return uuid.Nil, nil
	}

	contribs, err := qtx.ListWelfareContributionsForPeriod(ctx, period.ID)
	if err != nil {
		return uuid.Nil, err
	}
	var (
		byBranch    = map[pgtype.UUID]int64{}
		branchName  = map[pgtype.UUID]string{}
		branchOrder []pgtype.UUID
		total       int64
	)
	var paying []*db.ListWelfareContributionsForPeriodRow
	for _, c := range contribs {
		if c.Contribution <= 0 {
			continue
		}
		paying = append(paying, c)
		if _, ok := byBranch[c.BranchID]; !ok {
			branchOrder = append(branchOrder, c.BranchID)
			branchName[c.BranchID] = c.BranchName.String
		}
		byBranch[c.BranchID] += c.Contribution
		total += c.Contribution
	}
	if total == 0 {
		return uuid.Nil, nil
	}

	acct, err := welfareAccount(ctx, qtx)
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
	lines = append(lines, Cr(acct, total).WithMemo("iuran periode ini"))

	month := period.PeriodMonth.Time.Format("2006-01")
	entryID, err := Post(ctx, qtx, Entry{
		Date:        period.EndDate.Time,
		SourceType:  SourceWelfareFund,
		SourceID:    period.ID.Bytes,
		Description: fmt.Sprintf("Dana kesejahteraan karyawan %s", month),
		CreatedBy:   actor,
		Lines:       lines,
	})
	if err != nil {
		return uuid.Nil, err
	}
	for _, c := range paying {
		if _, err := qtx.CreateWelfareMovement(ctx, &db.CreateWelfareMovementParams{
			EmployeeID:      c.EmployeeID,
			MovementType:    WelfareContribution,
			Amount:          c.Contribution,
			PeriodMonth:     period.PeriodMonth,
			MovementDate:    period.EndDate,
			PayrollPeriodID: period.ID,
			JournalEntryID:  pgUUID(entryID),
			Note:            "iuran gaji " + month,
			CreatedBy:       pgUUID(actor),
		}); err != nil {
			return uuid.Nil, err
		}
	}
	return entryID, nil
}

// WelfarePayoutInput pays an employee's whole welfare balance on retirement.
type WelfarePayoutInput struct {
	EmployeeID          pgtype.UUID
	EmployeeName        string
	EmployeeStatus      string
	PayoutDate          time.Time
	FundSourceAccountID uuid.UUID
	Note                string
	Actor               uuid.UUID
}

// WelfarePayoutResult is what PayoutWelfare paid.
type WelfarePayoutResult struct {
	Amount         int64     `json:"amount"`
	JournalEntryID uuid.UUID `json:"journal_entry_id"`
}

// PayoutWelfare: inactive employees only, once all their periods are posted. Requires a transaction.
func PayoutWelfare(ctx context.Context, qtx *db.Queries, in WelfarePayoutInput) (*WelfarePayoutResult, error) {
	if in.EmployeeStatus == "active" {
		return nil, ErrWelfareStillEmployed
	}
	if dateOnly(in.PayoutDate).After(TodayWIB()) {
		return nil, ErrWelfareFutureDate
	}
	unsettled, err := qtx.CountUnsettledPayrollForEmployee(ctx, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if unsettled > 0 {
		return nil, ErrWelfareUnsettled
	}
	acct, err := welfareAccount(ctx, qtx)
	if err != nil {
		return nil, err
	}
	bal, err := qtx.GetWelfareBalance(ctx, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if bal <= 0 {
		return nil, ErrWelfareNothingToPay
	}
	entryID, err := Post(ctx, qtx, Entry{
		Date:        in.PayoutDate,
		SourceType:  SourceWelfarePayout,
		SourceID:    in.EmployeeID.Bytes,
		Description: fmt.Sprintf("Pencairan dana kesejahteraan — %s", in.EmployeeName),
		CreatedBy:   in.Actor,
		Lines: []Line{
			Dr(acct, bal),
			Cr(in.FundSourceAccountID, bal),
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := qtx.CreateWelfareMovement(ctx, &db.CreateWelfareMovementParams{
		EmployeeID:     in.EmployeeID,
		MovementType:   WelfarePayout,
		Amount:         -bal,
		MovementDate:   pgDate(in.PayoutDate),
		JournalEntryID: pgUUID(entryID),
		Note:           in.Note,
		CreatedBy:      pgUUID(in.Actor),
	}); err != nil {
		return nil, err
	}
	return &WelfarePayoutResult{Amount: bal, JournalEntryID: entryID}, nil
}

func pgDate(t time.Time) pgtype.Date { return pgtype.Date{Time: dateOnly(t), Valid: true} }

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
