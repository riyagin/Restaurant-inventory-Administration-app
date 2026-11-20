package handler

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Create — POST /api/invoices
func (h *InvoicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date              string             `json:"date"`
		DueDate           string             `json:"due_date"`
		InvoiceType       string             `json:"invoice_type"`
		PaymentStatus     string             `json:"payment_status"`
		PaymentMethod     string             `json:"payment_method"`
		AccountID         string             `json:"account_id"`
		WarehouseID       string             `json:"warehouse_id"`
		BranchID          string             `json:"branch_id"`
		DivisionID        string             `json:"division_id"`
		VendorID          string             `json:"vendor_id"`
		ReferenceNumber   string             `json:"reference_number"`
		ExpenseCategoryID string             `json:"expense_category_id"`
		PaymentDate       string             `json:"payment_date"`
		IsDistributed     bool               `json:"is_distributed"`
		Items             []invoiceItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	invoiceType := body.InvoiceType
	if invoiceType == "" {
		invoiceType = "purchase"
	}
	if body.IsDistributed {
		if invoiceType != "purchase" {
			respondError(w, http.StatusBadRequest, "faktur distribusi harus berjenis pembelian")
			return
		}
		body.WarehouseID, body.BranchID, body.DivisionID, body.ExpenseCategoryID = "", "", "", ""
	} else if invoiceType == "purchase" && body.WarehouseID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id diperlukan untuk faktur pembelian")
		return
	}

	ctx := r.Context()

	warehouseID, branchID, divisionID, vendorID, err := parseInvoiceUUIDs(body.WarehouseID, body.BranchID, body.DivisionID, body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	var distLines []distributedLine
	if body.IsDistributed {
		if distLines, err = resolveDistributedLines(ctx, h.queries, h.pool, body.Items); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Only expense invoices carry a category; a stale one on a purchase is ignored.
	categoryID := uuid.Nil
	if invoiceType == "expense" {
		categoryID, err = resolveExpenseCategory(ctx, h.queries, body.ExpenseCategoryID, divisionID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	invoiceDate, dueDate, err := parseInvoiceDates(body.Date, body.DueDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	invNumRaw, err := qtx.GetNextInvoiceNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor faktur")
		return
	}
	invoiceNumber := fmt.Sprintf("%v", invNumRaw)

	invoice, err := qtx.CreateInvoice(ctx, &db.CreateInvoiceParams{
		InvoiceNumber:     invoiceNumber,
		Date:              pgtype.Date{Time: invoiceDate, Valid: true},
		DueDate:           dueDate,
		InvoiceType:       invoiceType,
		PaymentMethod:     pgtype.Text{String: body.PaymentMethod, Valid: body.PaymentMethod != ""},
		PaymentStatus:     "unpaid",
		AccountID:         pgtype.UUID{},
		WarehouseID:       pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil},
		BranchID:          pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		DivisionID:        pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		VendorID:          pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		ReferenceNumber:   pgtype.Text{String: body.ReferenceNumber, Valid: body.ReferenceNumber != ""},
		ExpenseCategoryID: pgtype.UUID{Bytes: categoryID, Valid: categoryID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat faktur")
		return
	}

	if uid := middleware.UserIDFromCtx(ctx); uid != uuid.Nil {
		if _, err := tx.Exec(ctx, `UPDATE invoices SET created_by = $2 WHERE id = $1`,
			invoice.ID, pgtype.UUID{Bytes: uid, Valid: true}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan pembuat faktur")
			return
		}
	}

	invoiceID := invoice.ID.Bytes
	var grandTotal int64
	var debitLines []service.Line

	lineItems := body.Items
	if body.IsDistributed {
		lineItems = nil
		if _, err := tx.Exec(ctx, `UPDATE invoices SET is_distributed = true WHERE id = $1`, invoice.ID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menandai faktur distribusi")
			return
		}
		grandTotal, err = writeDistributedLines(ctx, qtx, tx, invoiceID, invoiceNumber, invoiceDate, distLines, true)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		debitLines = debitLinesFor(allAllocations(distLines), false)
	}
	for _, it := range lineItems {
		var itemID uuid.UUID
		hasItemID := it.ItemID != ""
		if hasItemID {
			parsed, err := parseUUID(it.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			itemID = parsed
		}
		itemVendorID := uuid.Nil
		if it.VendorID != "" {
			id, err := parseUUID(it.VendorID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "vendor_id item tidak valid")
				return
			}
			itemVendorID = id
		}

		lineValue := int64(float64(it.Price) * it.Quantity)
		grandTotal += lineValue

		isStockLine := invoiceType == "purchase" && hasItemID
		conv := lineConversion{Factor: 1, BaseIndex: it.UnitIndex}
		if isStockLine {
			item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
				return
			}
			conv, err = resolveLineConversion(item.Units, it.UnitIndex, it.ConversionFactor)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:        invoice.ID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: hasItemID},
			VendorID:         pgtype.UUID{Bytes: itemVendorID, Valid: itemVendorID != uuid.Nil},
			Quantity:         floatToNumeric(it.Quantity),
			UnitIndex:        pgtype.Int4{Int32: it.UnitIndex, Valid: hasItemID},
			Price:            it.Price,
			Description:      pgtype.Text{String: it.Description, Valid: it.Description != ""},
			ConversionFactor: floatToNumeric(conv.Factor),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item faktur")
			return
		}

		if isStockLine {
			vendorName := ""
			if itemVendorID != uuid.Nil {
				if v, err := h.queries.GetVendorByID(ctx, pgtype.UUID{Bytes: itemVendorID, Valid: true}); err == nil {
					vendorName = v.Name
				}
			}

			baseQty := conv.BaseQty(it.Quantity)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, baseQty, conv.BaseIndex, lineValue, invoiceDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok")
				return
			}

			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID:         itemID,
				WarehouseID:    warehouseID,
				QuantityChange: baseQty,
				UnitName:       conv.BaseUnitName,
				Vendor:         vendorName,
				Type:           "invoice",
				Reference:      invoiceNumber,
				Date:           invoiceDate,
				Value:          lineValue,
				SourceID:       invoiceID,
				SourceType:     "invoice",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
		}
	}

	// An unresolved account is left as uuid.Nil and posts to suspense, never skipped.
	if !body.IsDistributed {
		var debitAcctID uuid.UUID
		if invoiceType == "purchase" {
			if invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true}); err == nil && invAcctID.Valid {
				debitAcctID = invAcctID.Bytes
			}
		} else {
			if expAcctID, err := invoiceExpenseAccountID(ctx, qtx, categoryID, divisionID, branchID); err == nil {
				debitAcctID = expAcctID
			}
		}
		debitLines = []service.Line{service.Dr(debitAcctID, grandTotal)}
	}

	apAcctID, err := service.VendorPayableAccountID(ctx, qtx, vendorID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan akun hutang vendor")
		return
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        invoiceDate,
		SourceType:  service.SourceInvoice,
		SourceID:    invoiceID,
		Description: fmt.Sprintf("Faktur %s %s", invoiceType, invoiceNumber),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines:       append(debitLines, service.Cr(apAcctID, grandTotal)),
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal faktur")
		return
	}

	if body.PaymentStatus == "paid" || body.PaymentStatus == "partial" {
		if body.AccountID == "" {
			respondError(w, http.StatusBadRequest, "account_id diperlukan untuk status pembayaran ini")
			return
		}
		cashAcctID, err := parseUUID(body.AccountID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
		payDate := invoiceDate
		if body.PaymentDate != "" {
			payDate, err = parsePaymentDate(body.PaymentDate)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		cashAcct, err := qtx.GetAccountByID(ctx, pgtype.UUID{Bytes: cashAcctID, Valid: true})
		if err != nil {
			respondError(w, http.StatusBadRequest, "akun pembayaran tidak ditemukan")
			return
		}
		if cashAcct.Balance < grandTotal {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("saldo akun \"%s\" tidak cukup", cashAcct.Name))
			return
		}
		// Known gap: a "partial" invoice settles the full total; the request has no amount field.
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        payDate,
			SourceType:  service.SourceInvoicePay,
			SourceID:    invoiceID,
			Description: fmt.Sprintf("Pembayaran faktur %s", invoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Dr(apAcctID, grandTotal),
				service.Cr(cashAcctID, grandTotal),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembayaran")
			return
		}
		if _, err := qtx.UpdateInvoicePayment(ctx, &db.UpdateInvoicePaymentParams{
			AmountPaid:    grandTotal,
			PaymentStatus: body.PaymentStatus,
			AccountID:     pgtype.UUID{Bytes: cashAcctID, Valid: true},
			PaymentDate:   pgtype.Date{Time: payDate, Valid: true},
			ID:            invoice.ID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan faktur")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Invoice",
		EntityID:    invoiceID,
		Description: fmt.Sprintf("Buat faktur %s %s", invoiceType, invoiceNumber),
	})

	respondJSON(w, http.StatusCreated, invoice)
}
