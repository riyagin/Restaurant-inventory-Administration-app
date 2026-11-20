package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Journal source types: what kind of row caused an entry.
const (
	SourceInvoice     = "invoice"
	SourceInvoicePay  = "invoice_payment"
	SourceDispatch    = "dispatch"
	SourceTransfer    = "stock_transfer"
	SourceOpname      = "stock_opname"
	SourceInventory   = "inventory"
	SourceSale        = "sale"
	SourcePOSImport   = "pos_import"
	SourceAdjustment  = "account_adjustment"
	SourceKasbon      = "kasbon"
	SourcePayroll     = "payroll"
	SourceTHR         = "thr"
	SourceVendorMerge = "vendor_merge"
	SourceOpening     = "opening_balance"
	SourceCapital     = "capital_injection"
	// SourceDailyPurchase is Pembelanjaan Harian, paid from petty cash.
	SourceDailyPurchase = "daily_purchase"
	// SourceCashDeposit is Setoran.
	SourceCashDeposit = "cash_deposit"
	// SourceOperationalExpense is Beban Operasional.
	SourceOperationalExpense = "operational_expense"
	SourcePettyCashVariance  = "petty_cash_variance"
)

const OwnerCapitalAccountNumber = 30100

// DeliveryFeeReceivableNumber holds the POS delivery fee until the platform settles it.
const DeliveryFeeReceivableNumber = 10400

// SuspenseAccountNumber takes legs whose account cannot be resolved, so the entry
// stays balanced and the misconfiguration is visible.
const SuspenseAccountNumber = 19999

// ManualAdjustmentEquityNumber is the counter-leg of single-account manual adjustments.
const ManualAdjustmentEquityNumber = 30800

// Line is one leg of an entry. Amount is debit-positive; build it with Dr and Cr.
type Line struct {
	AccountID uuid.UUID
	Amount    int64
	Memo      string
}

func Dr(accountID uuid.UUID, amount int64) Line {
	return Line{AccountID: accountID, Amount: amount}
}

func Cr(accountID uuid.UUID, amount int64) Line {
	return Line{AccountID: accountID, Amount: -amount}
}

func (l Line) WithMemo(memo string) Line {
	l.Memo = memo
	return l
}

type Entry struct {
	Date        time.Time
	SourceType  string
	SourceID    uuid.UUID
	Description string
	CreatedBy   uuid.UUID
	// Reversals are appended; the original is never rewritten.
	ReversesID uuid.UUID
	Lines      []Line
}

// ErrUnbalanced: the legs do not sum to zero. A deferred DB trigger enforces the same rule.
type ErrUnbalanced struct {
	Difference int64
}

func (e ErrUnbalanced) Error() string {
	return fmt.Sprintf("jurnal tidak seimbang: selisih debit-kredit = %d", e.Difference)
}

// Post writes one entry and updates accounts.balance in the same transaction; it is
// the only writer of accounts.balance. Zero legs are dropped and uuid.Nil legs go to
// suspense. Requires a transaction-scoped *db.Queries.
func Post(ctx context.Context, qtx *db.Queries, e Entry) (uuid.UUID, error) {
	lines := make([]Line, 0, len(e.Lines))
	var net int64
	for _, l := range e.Lines {
		if l.Amount == 0 {
			continue
		}
		lines = append(lines, l)
		net += l.Amount
	}

	if len(lines) == 0 {
		return uuid.Nil, nil
	}
	if net != 0 {
		return uuid.Nil, ErrUnbalanced{Difference: net}
	}
	if len(lines) < 2 {
		return uuid.Nil, fmt.Errorf("jurnal %q: entri harus memiliki minimal 2 baris", e.SourceType)
	}

	var suspense uuid.UUID
	for i := range lines {
		if lines[i].AccountID != uuid.Nil {
			continue
		}
		if suspense == uuid.Nil {
			acct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: SuspenseAccountNumber, Valid: true})
			if err != nil {
				return uuid.Nil, fmt.Errorf("akun tidak dapat ditentukan dan akun sementara (%d) tidak ada: %w",
					SuspenseAccountNumber, err)
			}
			suspense = acct.ID.Bytes
		}
		lines[i].AccountID = suspense
		if lines[i].Memo == "" {
			lines[i].Memo = "akun tidak dapat ditentukan saat posting"
		}
	}

	date := e.Date
	if date.IsZero() {
		date = time.Now()
	}

	entry, err := qtx.CreateJournalEntry(ctx, &db.CreateJournalEntryParams{
		EntryDate:   pgtype.Date{Time: date, Valid: true},
		SourceType:  e.SourceType,
		SourceID:    pgtype.UUID{Bytes: e.SourceID, Valid: e.SourceID != uuid.Nil},
		Description: e.Description,
		CreatedBy:   pgtype.UUID{Bytes: e.CreatedBy, Valid: e.CreatedBy != uuid.Nil},
		ReversesID:  pgtype.UUID{Bytes: e.ReversesID, Valid: e.ReversesID != uuid.Nil},
	})
	if err != nil {
		return uuid.Nil, err
	}

	for _, l := range lines {
		if err := qtx.InsertJournalLine(ctx, &db.InsertJournalLineParams{
			EntryID:   entry.ID,
			AccountID: pgtype.UUID{Bytes: l.AccountID, Valid: true},
			Amount:    l.Amount,
			Memo:      l.Memo,
		}); err != nil {
			return uuid.Nil, err
		}
	}

	if err := applyToBalanceCache(ctx, qtx, lines); err != nil {
		return uuid.Nil, err
	}
	return entry.ID.Bytes, nil
}

// applyToBalanceCache applies debit-positive amounts in each account type's natural sign.
func applyToBalanceCache(ctx context.Context, qtx *db.Queries, lines []Line) error {
	ids := make([]pgtype.UUID, 0, len(lines))
	seen := make(map[uuid.UUID]bool, len(lines))
	for _, l := range lines {
		if seen[l.AccountID] {
			continue
		}
		seen[l.AccountID] = true
		ids = append(ids, pgtype.UUID{Bytes: l.AccountID, Valid: true})
	}

	rows, err := qtx.ListAccountTypes(ctx, ids)
	if err != nil {
		return err
	}
	types := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		types[r.ID.Bytes] = r.AccountType
	}

	for _, l := range lines {
		t, ok := types[l.AccountID]
		if !ok {
			return fmt.Errorf("akun %s tidak ditemukan", l.AccountID)
		}
		delta := l.Amount
		if t == "liability" || t == "equity" || t == "revenue" {
			delta = -delta
		}
		if err := qtx.AddToAccountBalance(ctx, &db.AddToAccountBalanceParams{
			Column1: delta,
			ID:      pgtype.UUID{Bytes: l.AccountID, Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}
