package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Chart-of-accounts numbers for the payables group.
const (
	PayableParentNumber = 20100 // "Utang Usaha" — parent, rolls up its children
	PayableOtherNumber  = 20999 // "Utang Usaha - Lainnya" — invoices with no vendor
)

// VendorPayablePrefix is the name prefix of every per-vendor payable sub-account.
const VendorPayablePrefix = "Utang Usaha - "

// UpdateBalance is deliberately gone.
//
// It moved a single account's balance with no counterpart and no record of what
// the entry was supposed to be, which is how the books drifted ~982M out of
// balance: several callers moved one side and forgot the other, and nothing
// recorded the intent, so the mistakes were neither detectable nor reversible.
//
// Every financial mutation now goes through service.Post (journal.go), which
// writes both legs and the balance cache together and refuses an entry whose
// legs do not sum to zero. If you need to move a balance, write the journal
// entry that explains it.

// PayableOtherAccountID returns the shared "Utang Usaha - Lainnya" bucket, used
// by invoices that carry no vendor. Falls back to the 20100 parent if the
// bucket is missing (DB not migrated yet). Returns uuid.Nil if neither exists.
func PayableOtherAccountID(ctx context.Context, qtx *db.Queries) uuid.UUID {
	if acct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PayableOtherNumber, Valid: true}); err == nil {
		return acct.ID.Bytes
	}
	if acct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PayableParentNumber, Valid: true}); err == nil {
		return acct.ID.Bytes
	}
	return uuid.Nil
}

// VendorPayableAccountID resolves the payable account an invoice for this vendor
// posts to, creating the vendor's sub-account on first use. Vendors added before
// migration 038 (or through paths that skip the vendors handler) have no account
// yet, so this is the single point that guarantees one exists.
//
// A zero vendorID means "no vendor" and maps to the shared bucket.
// Must be called with a transaction-scoped *db.Queries.
func VendorPayableAccountID(ctx context.Context, qtx *db.Queries, vendorID uuid.UUID) (uuid.UUID, error) {
	if vendorID == uuid.Nil {
		return PayableOtherAccountID(ctx, qtx), nil
	}

	vendor, err := qtx.GetVendorByID(ctx, pgtype.UUID{Bytes: vendorID, Valid: true})
	if err != nil {
		return uuid.Nil, err
	}
	if vendor.AccountID.Valid {
		return vendor.AccountID.Bytes, nil
	}
	return CreateVendorPayableAccount(ctx, qtx, vendorID, vendor.Name)
}

// CreateVendorPayableAccount creates "Utang Usaha - <vendor>" under the 20100
// parent and links it to the vendor. Must be called with a transaction-scoped
// *db.Queries.
func CreateVendorPayableAccount(ctx context.Context, qtx *db.Queries, vendorID uuid.UUID, vendorName string) (uuid.UUID, error) {
	parent, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: PayableParentNumber, Valid: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("akun induk utang usaha tidak ditemukan: %w", err)
	}

	number, err := qtx.NextVendorPayableNumber(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	// accounts.name is UNIQUE; disambiguate with the account number on a clash.
	name := VendorPayablePrefix + vendorName
	exists, err := qtx.AccountNameExists(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	if exists {
		name = fmt.Sprintf("%s (%d)", name, number)
	}

	acct, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{
		Name:          name,
		AccountNumber: pgtype.Int4{Int32: number, Valid: true},
		AccountType:   "liability",
		ParentID:      parent.ID,
	})
	if err != nil {
		return uuid.Nil, err
	}

	if err := qtx.SetVendorAccountID(ctx, &db.SetVendorAccountIDParams{
		AccountID: acct.ID,
		ID:        pgtype.UUID{Bytes: vendorID, Valid: true},
	}); err != nil {
		return uuid.Nil, err
	}
	return acct.ID.Bytes, nil
}

// RenameVendorPayableAccount keeps a vendor's payable account name in sync after
// the vendor is renamed. A name clash leaves the old account name in place —
// the link, not the label, is what the ledger depends on.
func RenameVendorPayableAccount(ctx context.Context, qtx *db.Queries, accountID uuid.UUID, vendorName string) error {
	name := VendorPayablePrefix + vendorName
	exists, err := qtx.AccountNameExists(ctx, name)
	if err != nil || exists {
		return err
	}
	return qtx.RenameAccount(ctx, &db.RenameAccountParams{
		Name: name,
		ID:   pgtype.UUID{Bytes: accountID, Valid: true},
	})
}
