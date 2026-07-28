package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Journal source types. One per kind of financial event, so every entry can be
// traced back to the row that caused it.
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
)

// OwnerCapitalAccountNumber is the equity account owner deposits are credited
// to — including cash carried over when a previous system is replaced.
const OwnerCapitalAccountNumber = 30100

// DeliveryFeeReceivableNumber is where the POS "Biaya Tambahan" delivery fee is
// debited. The fee is earned at the sale but excluded from the POS payment
// breakdown, so it is receivable from the delivery platform until settled.
const DeliveryFeeReceivableNumber = 10400

// SuspenseAccountNumber is where a leg lands when its account cannot be
// resolved — an unconfigured warehouse inventory account, a branch with no
// expense account. The old code skipped such legs, which silently unbalanced
// the books. Parking them in a real, visibly-named account keeps the entry
// balanced and turns a misconfiguration into something you can see in the CoA.
const SuspenseAccountNumber = 19999

// ManualAdjustmentEquityNumber is the counter-leg for single-account manual
// adjustments. That screen moves one account's balance without naming a
// counterparty, so the balancing half lands in equity, where an otherwise
// unexplained movement belongs.
const ManualAdjustmentEquityNumber = 30800

// Line is one leg of a journal entry. Amount is debit-positive: a debit is
// positive, a credit is negative. Build lines with Dr and Cr rather than setting
// Amount directly, so the sign convention lives in one place.
type Line struct {
	AccountID uuid.UUID
	Amount    int64
	Memo      string
}

// Dr returns a debit leg. A negative amount is a credit, which is what makes
// reversals natural: Dr(acct, -x) undoes Dr(acct, x).
func Dr(accountID uuid.UUID, amount int64) Line {
	return Line{AccountID: accountID, Amount: amount}
}

// Cr returns a credit leg.
func Cr(accountID uuid.UUID, amount int64) Line {
	return Line{AccountID: accountID, Amount: -amount}
}

// WithMemo annotates a leg. Optional; the entry description usually says enough.
func (l Line) WithMemo(memo string) Line {
	l.Memo = memo
	return l
}

// Entry is one financial event and all of its legs.
type Entry struct {
	Date        time.Time
	SourceType  string
	SourceID    uuid.UUID
	Description string
	CreatedBy   uuid.UUID
	// ReversesID links this entry to the one it reverses. Reversals are appended,
	// never applied by rewriting the original.
	ReversesID uuid.UUID
	Lines      []Line
}

// ErrUnbalanced is returned when an entry's legs do not sum to zero. The DB
// carries the same rule as a deferred constraint trigger, so an unbalanced entry
// cannot reach disk even if a caller bypasses Post.
type ErrUnbalanced struct {
	Difference int64
}

func (e ErrUnbalanced) Error() string {
	return fmt.Sprintf("jurnal tidak seimbang: selisih debit-kredit = %d", e.Difference)
}

// Post writes one journal entry and updates the cached account balances in the
// same transaction. It is the only way anything should ever write
// accounts.balance.
//
// Legs with a zero amount are dropped (a zero leg carries no information and
// would otherwise pad a one-sided entry into looking two-sided). Legs whose
// account is uuid.Nil are redirected to the suspense account rather than
// skipped — skipping is exactly how the books drifted before.
//
// Must be called with a transaction-scoped *db.Queries.
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
		// Nothing of value happened (an invoice with a zero total, a dispatch that
		// consumed no value). Not an error — just no entry.
		return uuid.Nil, nil
	}
	if net != 0 {
		return uuid.Nil, ErrUnbalanced{Difference: net}
	}
	if len(lines) < 2 {
		return uuid.Nil, fmt.Errorf("jurnal %q: entri harus memiliki minimal 2 baris", e.SourceType)
	}

	// Resolve unresolvable legs to the suspense account so the entry still
	// balances and the misconfiguration surfaces in the CoA.
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

// applyToBalanceCache moves accounts.balance in each account's natural direction.
// journal_lines is debit-positive; accounts.balance is natural-sign per type, so
// debit-normal accounts take the amount as-is and credit-normal accounts take it
// negated. A zero-sum entry therefore always leaves A = L + E + R - Exp intact.
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
