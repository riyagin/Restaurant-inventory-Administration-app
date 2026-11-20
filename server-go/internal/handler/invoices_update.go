package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Update - PUT /api/invoices/:id
// Paid invoices are editable: the purchase posting is restated, settlements stay, and the status is recomputed.
func (h *InvoicesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body invoiceUpdateRequest
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	old, err := h.queries.GetInvoiceWithTotal(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	switch old.PaymentStatus {
	case "dispatched":
		respondError(w, http.StatusConflict, "faktur ini dibuat otomatis dari pengiriman — ubah lewat halaman pengiriman")
		return
	case "cancelled":
		respondError(w, http.StatusConflict, "faktur yang sudah dibatalkan tidak dapat diubah")
		return
	}
	oldHeader, err := h.queries.GetInvoiceByID(ctx, invoiceUUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}

	distributed, err := isDistributedInvoice(ctx, h.pool, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	if distributed {
		h.updateDistributed(w, r, id, old, oldHeader, body)
		return
	}

	oldItems, err := h.queries.GetInvoiceItems(ctx, invoiceUUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item lama")
		return
	}

	warehouseID, branchID, divisionID, vendorID, err := parseInvoiceUUIDs(body.WarehouseID, body.BranchID, body.DivisionID, body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Settlements already debited the old vendor's payable.
	if old.AmountPaid > 0 && vendorID != uuidFromPg(old.VendorID) {
		respondError(w, http.StatusConflict, "vendor tidak dapat diganti pada faktur yang sudah ada pembayarannya")
		return
	}

	newCategoryID := uuid.Nil
	if old.InvoiceType == "expense" {
		newCategoryID, err = resolveExpenseCategory(ctx, h.queries, body.ExpenseCategoryID, divisionID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	accountID := uuid.Nil
	if body.AccountID != "" {
		aid, err := parseUUID(body.AccountID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
		accountID = aid
	}

	invoiceDate, dueDate, err := parseInvoiceDates(body.Date, body.DueDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	lines := make([]resolvedInvoiceLine, 0, len(body.Items))
	for _, it := range body.Items {
		l := resolvedInvoiceLine{in: it, conv: lineConversion{Factor: 1, BaseIndex: it.UnitIndex}}
		if it.ItemID != "" {
			parsed, err := parseUUID(it.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			l.itemID, l.hasItemID = parsed, true
		}
		if it.VendorID != "" {
			ivid, err := parseUUID(it.VendorID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "vendor_id item tidak valid")
				return
			}
			l.vendorID = ivid
		}
		l.value = int64(float64(it.Price) * it.Quantity)
		if old.InvoiceType == "purchase" && l.hasItemID {
			item, err := h.queries.GetItemByID(ctx, pgtype.UUID{Bytes: l.itemID, Valid: true})
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
				return
			}
			l.conv, err = resolveLineConversion(item.Units, it.UnitIndex, it.ConversionFactor)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		lines = append(lines, l)
	}

	// Same goods, quantities, warehouse and date: a price correction. Revalue the
	// stock instead of reversing it, since it has usually been used by now.
	repriceOnly := old.InvoiceType == "purchase" &&
		old.WarehouseID.Valid && warehouseID == old.WarehouseID.Bytes &&
		oldHeader.Date.Valid && oldHeader.Date.Time.Format("2006-01-02") == invoiceDate.Format("2006-01-02") &&
		purchaseQuantitiesUnchanged(oldItems, lines)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	oldWarehouseID := old.WarehouseID.Bytes
	oldBranchID := old.BranchID.Bytes
	oldDivisionID := old.DivisionID.Bytes
	// Stored category: the reversal must credit the account originally debited.
	oldCategoryID := uuid.Nil
	if old.ExpenseCategoryID.Valid {
		oldCategoryID = old.ExpenseCategoryID.Bytes
	}

	var oldDebitAcctID uuid.UUID
	if old.InvoiceType == "purchase" && old.WarehouseID.Valid {
		for _, it := range oldItems {
			if repriceOnly || !it.ItemID.Valid {
				continue
			}
			// At the stored factor; the catalogue units may have changed since.
			qty := numericToFloat64(it.Quantity) * storedConversionFactor(it.ConversionFactor)
			if _, err := service.FIFODeduct(ctx, qtx, it.ItemID.Bytes, oldWarehouseID, qty); err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity,
						fmt.Sprintf("stok tidak mencukupi untuk membalik item: %s", it.ItemName.String))
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal membalik stok lama")
				return
			}
		}
		if err := qtx.DeleteStockHistoryBySource(ctx, &db.DeleteStockHistoryBySourceParams{
			SourceID:   pgtype.UUID{Bytes: id, Valid: true},
			SourceType: pgtype.Text{String: "invoice", Valid: true},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghapus riwayat stok lama")
			return
		}
		if oldInvAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: oldWarehouseID, Valid: old.WarehouseID.Valid}); err == nil && oldInvAcctID.Valid {
			oldDebitAcctID = oldInvAcctID.Bytes
		}
	} else if old.InvoiceType == "expense" {
		if expAcctID, err := invoiceExpenseAccountID(ctx, qtx, oldCategoryID, oldDivisionID, oldBranchID); err == nil {
			oldDebitAcctID = expAcctID
		}
	}

	oldAPAcctID, err := service.VendorPayableAccountID(ctx, qtx, old.VendorID.Bytes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun hutang vendor lama")
		return
	}

	// Dated on the old date so it cancels the original in its own period.
	reversalDate := invoiceDate
	if oldHeader.Date.Valid {
		reversalDate = oldHeader.Date.Time
	}
	if !repriceOnly {
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        reversalDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Pembalikan faktur %s (revisi)", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Cr(oldDebitAcctID, old.TotalAmount),
				service.Dr(oldAPAcctID, old.TotalAmount),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembalikan faktur")
			return
		}
	}

	if err := qtx.DeleteInvoiceItems(ctx, invoiceUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item lama")
		return
	}

	if _, err := qtx.UpdateInvoice(ctx, &db.UpdateInvoiceParams{
		Date:              pgtype.Date{Time: invoiceDate, Valid: true},
		DueDate:           dueDate,
		PaymentMethod:     pgtype.Text{String: body.PaymentMethod, Valid: body.PaymentMethod != ""},
		AccountID:         pgtype.UUID{Bytes: accountID, Valid: accountID != uuid.Nil},
		VendorID:          pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		ReferenceNumber:   pgtype.Text{String: body.ReferenceNumber, Valid: body.ReferenceNumber != ""},
		WarehouseID:       pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil},
		BranchID:          pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		DivisionID:        pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		ExpenseCategoryID: pgtype.UUID{Bytes: newCategoryID, Valid: newCategoryID != uuid.Nil},
		ID:                invoiceUUID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui faktur")
		return
	}

	var grandTotal int64
	for _, l := range lines {
		it, itemID, hasItemID, itemVendorID, conv, lineValue := l.in, l.itemID, l.hasItemID, l.vendorID, l.conv, l.value
		grandTotal += lineValue
		isStockLine := old.InvoiceType == "purchase" && hasItemID

		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:        invoiceUUID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: hasItemID},
			VendorID:         pgtype.UUID{Bytes: itemVendorID, Valid: itemVendorID != uuid.Nil},
			Quantity:         floatToNumeric(it.Quantity),
			UnitIndex:        pgtype.Int4{Int32: it.UnitIndex, Valid: hasItemID},
			Price:            it.Price,
			Description:      pgtype.Text{String: it.Description, Valid: it.Description != ""},
			ConversionFactor: floatToNumeric(conv.Factor),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item baru")
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
			if !repriceOnly {
				if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, baseQty, conv.BaseIndex, lineValue, invoiceDate); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menambah stok baru")
					return
				}
			}

			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID:         itemID,
				WarehouseID:    warehouseID,
				QuantityChange: baseQty,
				UnitName:       conv.BaseUnitName,
				Vendor:         vendorName,
				Type:           "invoice",
				Reference:      old.InvoiceNumber,
				Date:           invoiceDate,
				Value:          lineValue,
				SourceID:       id,
				SourceType:     "invoice",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok baru")
				return
			}
		}
	}

	var newDebitAcctID uuid.UUID
	if old.InvoiceType == "purchase" {
		if newInvAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil}); err == nil && newInvAcctID.Valid {
			newDebitAcctID = newInvAcctID.Bytes
		}
	} else {
		if expAcctID, err := invoiceExpenseAccountID(ctx, qtx, newCategoryID, divisionID, branchID); err == nil {
			newDebitAcctID = expAcctID
		}
	}
	newAPAcctID, err := service.VendorPayableAccountID(ctx, qtx, vendorID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan akun hutang vendor")
		return
	}

	if !repriceOnly {
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        invoiceDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Faktur %s (revisi)", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Dr(newDebitAcctID, grandTotal),
				service.Cr(newAPAcctID, grandTotal),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal faktur revisi")
			return
		}
	} else {
		// Price correction: revalue what is still on hand; the consumed share's
		// difference goes to Selisih Persediaan.
		oldValue := map[uuid.UUID]int64{}
		oldQty := map[uuid.UUID]float64{}
		var oldTotal int64
		for _, it := range oldItems {
			v := int64(float64(it.Price) * numericToFloat64(it.Quantity))
			oldTotal += v
			oldValue[it.ItemID.Bytes] += v
			oldQty[it.ItemID.Bytes] += numericToFloat64(it.Quantity) * storedConversionFactor(it.ConversionFactor)
		}
		newValue := map[uuid.UUID]int64{}
		for _, l := range lines {
			newValue[l.itemID] += l.value
		}

		var invDelta, varianceDelta int64
		for itemID, nv := range newValue {
			delta := nv - oldValue[itemID]
			if delta == 0 {
				continue
			}
			applied, err := repriceReceivedStock(ctx, qtx, itemID, warehouseID, oldQty[itemID], delta)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyesuaikan nilai stok")
				return
			}
			invDelta += applied
			varianceDelta += delta - applied
		}

		var varianceAcctID uuid.UUID
		if varianceDelta != 0 {
			varianceAcctID = inventoryAdjustmentAccountID(ctx, qtx)
		}
		journal := []service.Line{
			service.Dr(newDebitAcctID, invDelta),
			service.Dr(varianceAcctID, varianceDelta).WithMemo("selisih harga atas stok yang sudah terpakai"),
		}
		if oldAPAcctID == newAPAcctID {
			journal = append(journal, service.Cr(newAPAcctID, grandTotal-oldTotal))
		} else {
			journal = append(journal, service.Dr(oldAPAcctID, oldTotal), service.Cr(newAPAcctID, grandTotal))
		}
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        invoiceDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Koreksi harga faktur %s", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines:       journal,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal koreksi harga")
			return
		}
	}

	newStatus := invoicePaymentStatus(old.AmountPaid, grandTotal)
	if newStatus != old.PaymentStatus {
		if _, err := tx.Exec(ctx, `UPDATE invoices SET payment_status = $1 WHERE id = $2`, newStatus, id); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	desc := fmt.Sprintf("Update faktur %s", old.InvoiceNumber)
	if grandTotal != old.TotalAmount {
		desc += fmt.Sprintf(" (total %d → %d)", old.TotalAmount, grandTotal)
	}
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: desc,
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"id":             id,
		"payment_status": newStatus,
		"total_amount":   grandTotal,
		"amount_paid":    old.AmountPaid,
	})
}

// purchaseQuantitiesUnchanged compares base-unit quantities per item, old lines at their stored factor.
func purchaseQuantitiesUnchanged(old []*db.GetInvoiceItemsRow, lines []resolvedInvoiceLine) bool {
	qty := map[uuid.UUID]float64{}
	for _, it := range old {
		if !it.ItemID.Valid {
			return false
		}
		qty[it.ItemID.Bytes] += numericToFloat64(it.Quantity) * storedConversionFactor(it.ConversionFactor)
	}
	for _, l := range lines {
		if !l.hasItemID {
			return false
		}
		qty[l.itemID] -= l.conv.BaseQty(l.in.Quantity)
	}
	for _, q := range qty {
		if math.Abs(q) > 0.0001 {
			return false
		}
	}
	return true
}

// repriceReceivedStock spreads a price difference over the newest lots (FIFO: those
// still on hand), never below zero value, and returns the amount absorbed.
func repriceReceivedStock(ctx context.Context, qtx *db.Queries, itemID, warehouseID uuid.UUID, receivedQty float64, delta int64) (int64, error) {
	if receivedQty <= 0 || delta == 0 {
		return 0, nil
	}
	lots, err := qtx.GetInventoryLotsForFIFO(ctx, &db.GetInventoryLotsForFIFOParams{
		ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	var onHand float64
	for _, lot := range lots {
		onHand += numericToFloat64(lot.Quantity)
	}
	cover := math.Min(onHand, receivedQty)
	if cover <= 0 {
		return 0, nil
	}
	target := int64(float64(delta) * cover / receivedQty)

	var allotted, applied int64
	remaining := cover
	for i := len(lots) - 1; i >= 0 && remaining > 1e-9; i-- {
		lot := lots[i]
		take := math.Min(numericToFloat64(lot.Quantity), remaining)
		remaining -= take
		share := int64(float64(target) * take / cover)
		if remaining <= 1e-9 {
			share = target - allotted // last lot takes the rounding remainder
		}
		allotted += share
		if lot.Value+share < 0 {
			share = -lot.Value
		}
		if share == 0 {
			continue
		}
		if err := qtx.UpdateInventoryLotQuantity(ctx, &db.UpdateInventoryLotQuantityParams{
			ID:       lot.ID,
			Quantity: lot.Quantity,
			Value:    lot.Value + share,
		}); err != nil {
			return 0, err
		}
		applied += share
	}
	return applied, nil
}
