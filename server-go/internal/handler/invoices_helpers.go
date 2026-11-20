package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// invoiceExpenseAccountID resolves category, then division, then branch account.
// Reversals must pass the category stored on the invoice, not the form's.
func invoiceExpenseAccountID(ctx context.Context, qtx *db.Queries, categoryID, divisionID, branchID uuid.UUID) (uuid.UUID, error) {
	if categoryID != uuid.Nil {
		cat, err := qtx.GetExpenseCategoryByID(ctx, pgtype.UUID{Bytes: categoryID, Valid: true})
		if err == nil && cat.AccountID.Valid {
			return cat.AccountID.Bytes, nil
		}
	}
	if divisionID != uuid.Nil {
		aid, err := qtx.GetDivisionExpenseAccountID(ctx, pgtype.UUID{Bytes: divisionID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	if branchID != uuid.Nil {
		aid, err := qtx.GetBranchExpenseAccountID(ctx, pgtype.UUID{Bytes: branchID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	return uuid.Nil, nil
}

// resolveExpenseCategory rejects a category from another division, whose account
// would put the debit on that division's books.
func resolveExpenseCategory(ctx context.Context, q *db.Queries, raw string, divisionID uuid.UUID) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	categoryID, err := parseUUID(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("expense_category_id tidak valid")
	}
	cat, err := q.GetExpenseCategoryByID(ctx, pgtype.UUID{Bytes: categoryID, Valid: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("kategori beban tidak ditemukan")
	}
	if divisionID == uuid.Nil || cat.DivisionID.Bytes != divisionID {
		return uuid.Nil, fmt.Errorf("kategori beban %q bukan milik divisi yang dipilih", cat.Name)
	}
	return categoryID, nil
}

func parseInvoiceUUIDs(warehouseStr, branchStr, divisionStr, vendorStr string) (warehouseID, branchID, divisionID, vendorID uuid.UUID, err error) {
	if warehouseStr != "" {
		warehouseID, err = parseUUID(warehouseStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("warehouse_id tidak valid")
		}
	}
	if branchStr != "" {
		branchID, err = parseUUID(branchStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("branch_id tidak valid")
		}
	}
	if divisionStr != "" {
		divisionID, err = parseUUID(divisionStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("division_id tidak valid")
		}
	}
	if vendorStr != "" {
		vendorID, err = parseUUID(vendorStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("vendor_id tidak valid")
		}
	}
	return warehouseID, branchID, divisionID, vendorID, nil
}

func parseInvoiceDates(dateStr, dueDateStr string) (invoiceDate time.Time, dueDate pgtype.Date, err error) {
	invoiceDate = time.Now()
	if dateStr != "" {
		invoiceDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return invoiceDate, dueDate, fmt.Errorf("format tanggal tidak valid")
		}
	}
	if dueDateStr != "" {
		t, e := time.Parse("2006-01-02", dueDateStr)
		if e != nil {
			return invoiceDate, dueDate, fmt.Errorf("format due_date tidak valid")
		}
		dueDate = pgtype.Date{Time: t, Valid: true}
	}
	return invoiceDate, dueDate, nil
}
