package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Create — POST /api/dispatches
func (h *DispatchesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID     string              `json:"branch_id"`
		DivisionID   string              `json:"division_id"`
		WarehouseID  string              `json:"warehouse_id"`
		Notes        string              `json:"notes"`
		DispatchDate string              `json:"dispatch_date"`
		Items        []dispatchItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.BranchID == "" {
		respondError(w, http.StatusBadRequest, "branch_id diperlukan")
		return
	}
	if body.WarehouseID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id diperlukan")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	// The entry date decides the period stock_history and the mirror invoice land in; empty means now.
	var entryDate time.Time
	hasEntryDate := false
	if body.DispatchDate != "" {
		t, err := time.Parse("2006-01-02", body.DispatchDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal pengiriman tidak valid")
			return
		}
		entryDate, hasEntryDate = t, true
	}

	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}
	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}
	divisionID := uuid.Nil
	if body.DivisionID != "" {
		divisionID, err = parseUUID(body.DivisionID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "division_id tidak valid")
			return
		}
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	dispatched, err := qtx.InsertDispatch(ctx, &db.InsertDispatchParams{
		BranchID:     pgtype.UUID{Bytes: branchID, Valid: true},
		DivisionID:   pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		WarehouseID:  pgtype.UUID{Bytes: warehouseID, Valid: true},
		Notes:        pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		DispatchedBy: pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat pengiriman")
		return
	}
	dispatchID := dispatched.ID.Bytes

	effectiveDate := dispatched.DispatchedAt.Time
	if hasEntryDate {
		effectiveDate = entryDate
		if _, err := tx.Exec(ctx, `UPDATE dispatches SET dispatched_at = $1 WHERE id = $2`,
			pgtype.Timestamptz{Time: entryDate, Valid: true}, pgUUID(dispatchID)); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan tanggal pengiriman")
			return
		}
	}

	type itemResult struct {
		itemID        uuid.UUID
		quantity      float64
		unitIndex     int32
		unitName      string
		factor        float64
		valueDeducted int64
	}
	results := make([]itemResult, 0, len(body.Items))
	var totalDispatchValue int64

	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		conv, err := dispatchLineConversion(ctx, qtx, itemID, it)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		baseQty := conv.BaseQty(it.Quantity)

		valueDeducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, baseQty,
			service.LotSource{Type: service.SourceDispatch, ID: dispatchID, Date: entryDate})
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal mengurangi stok")
			return
		}

		if err := qtx.InsertDispatchItem(ctx, &db.InsertDispatchItemParams{
			DispatchID:       pgtype.UUID{Bytes: dispatchID, Valid: true},
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
			Quantity:         floatToNumeric(it.Quantity),
			UnitIndex:        it.UnitIndex,
			UnitName:         it.UnitName,
			ConversionFactor: floatToNumeric(conv.Factor),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item pengiriman")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: -baseQty,
			UnitName:       conv.BaseUnitName,
			Type:           "dispatch",
			Date:           effectiveDate,
			Value:          -valueDeducted,
			SourceID:       dispatchID,
			SourceType:     "dispatch",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		results = append(results, itemResult{
			itemID:        itemID,
			quantity:      it.Quantity,
			unitIndex:     it.UnitIndex,
			unitName:      it.UnitName,
			factor:        conv.Factor,
			valueDeducted: valueDeducted,
		})
		totalDispatchValue += valueDeducted
	}

	// Dr division expense (parent, no category), Cr warehouse inventory, at FIFO value.
	expAcctID, err := invoiceExpenseAccountID(ctx, qtx, uuid.Nil, divisionID, branchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun beban")
		return
	}
	var invAcctIDValue uuid.UUID
	if invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true}); err == nil && invAcctID.Valid {
		invAcctIDValue = invAcctID.Bytes
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        effectiveDate,
		SourceType:  service.SourceDispatch,
		SourceID:    dispatchID,
		Description: "Pengiriman ke cabang/divisi",
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(expAcctID, totalDispatchValue),
			service.Cr(invAcctIDValue, totalDispatchValue),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pengiriman")
		return
	}

	invNumRaw, err := qtx.GetNextInvoiceNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor faktur")
		return
	}
	invoiceNumber := fmt.Sprintf("%v", invNumRaw)

	invoiceDate := effectiveDate

	invoice, err := qtx.CreateInvoice(ctx, &db.CreateInvoiceParams{
		InvoiceNumber: invoiceNumber,
		Date:          pgtype.Date{Time: invoiceDate, Valid: true},
		InvoiceType:   "expense",
		PaymentStatus: "dispatched",
		BranchID:      pgtype.UUID{Bytes: branchID, Valid: true},
		DivisionID:    pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat faktur otomatis")
		return
	}

	if err := qtx.SetInvoiceDispatchID(ctx, &db.SetInvoiceDispatchIDParams{
		DispatchID: pgtype.UUID{Bytes: dispatchID, Valid: true},
		ID:         invoice.ID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghubungkan faktur ke pengiriman")
		return
	}

	for _, res := range results {
		unitPrice := int64(0)
		if res.quantity > 0 {
			unitPrice = int64(float64(res.valueDeducted) / res.quantity)
		}
		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:        invoice.ID,
			ItemID:           pgtype.UUID{Bytes: res.itemID, Valid: true},
			Quantity:         floatToNumeric(res.quantity),
			UnitIndex:        pgtype.Int4{Int32: res.unitIndex, Valid: true},
			Price:            unitPrice,
			ConversionFactor: floatToNumeric(res.factor),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item faktur")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengiriman")
		return
	}

	dispatch, _ := h.queries.GetDispatchByID(ctx, pgtype.UUID{Bytes: dispatchID, Valid: true})
	items, _ := h.queries.GetDispatchItems(ctx, pgtype.UUID{Bytes: dispatchID, Valid: true})

	branchName := ""
	if dispatch != nil {
		branchName = dispatch.BranchName
		if dispatch.DivisionName.Valid {
			branchName += "/" + dispatch.DivisionName.String
		}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Dispatch",
		EntityID:    dispatchID,
		Description: fmt.Sprintf("Pengiriman barang ke %s", branchName),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"dispatch": dispatch,
		"items":    items,
	})
}

func dispatchLineConversion(ctx context.Context, qtx *db.Queries, itemID uuid.UUID, in dispatchItemInput) (lineConversion, error) {
	item, err := qtx.GetItemByID(ctx, pgUUID(itemID))
	if err != nil {
		return lineConversion{}, fmt.Errorf("item tidak ditemukan: %s", in.ItemID)
	}
	return resolveLineConversion(item.Units, in.UnitIndex, in.ConversionFactor)
}
