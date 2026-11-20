package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Update — PUT /api/dispatches/:id
// Changes are appended as signed correction lines (invoice + stock_history), never rewritten.
func (h *DispatchesHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		BranchID     string              `json:"branch_id"`
		DivisionID   string              `json:"division_id"`
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
	if body.DivisionID == "" {
		respondError(w, http.StatusBadRequest, "division_id diperlukan")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	newBranchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}
	newDivisionID, err := parseUUID(body.DivisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "division_id tidak valid")
		return
	}
	var newEntryDate time.Time
	hasNewEntryDate := false
	if body.DispatchDate != "" {
		t, err := time.Parse("2006-01-02", body.DispatchDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal pengiriman tidak valid")
			return
		}
		newEntryDate, hasNewEntryDate = t, true
	}

	// lineState: quantity as typed, baseQty in base units (rows merged per key may use different factors).
	type lineState struct {
		itemID    uuid.UUID
		quantity  float64
		baseQty   float64
		unitIndex int32
		unitName  string
		baseIndex int32
		baseName  string
	}
	storedFactor := func(l *lineState) float64 {
		if l.quantity == 0 {
			return 1
		}
		return l.baseQty / l.quantity
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	newByKey := map[string]*lineState{}
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}
		if it.Quantity <= 0 {
			respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
			return
		}
		conv, err := dispatchLineConversion(ctx, qtx, itemID, it)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		key := itemKey(itemID, it.UnitIndex)
		if ex, ok := newByKey[key]; ok {
			ex.quantity += it.Quantity
			ex.baseQty += conv.BaseQty(it.Quantity)
		} else {
			newByKey[key] = &lineState{
				itemID: itemID, quantity: it.Quantity, baseQty: conv.BaseQty(it.Quantity),
				unitIndex: it.UnitIndex, unitName: it.UnitName,
				baseIndex: conv.BaseIndex, baseName: conv.BaseUnitName,
			}
		}
	}

	var curBranch, curDivision, curWarehouse pgtype.UUID
	var curDispatchedAt pgtype.Timestamptz
	var status string
	err = tx.QueryRow(ctx,
		`SELECT branch_id, division_id, warehouse_id, dispatched_at, status FROM dispatches WHERE id = $1`,
		pgUUID(id)).Scan(&curBranch, &curDivision, &curWarehouse, &curDispatchedAt, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "pengiriman sudah dibatalkan dan tidak dapat diubah")
		return
	}
	warehouseID := curWarehouse.Bytes

	effectiveDate := curDispatchedAt.Time
	if hasNewEntryDate {
		effectiveDate = newEntryDate
	}

	oldByKey := map[string]*lineState{}
	{
		rows, err := tx.Query(ctx,
			`SELECT di.item_id, di.quantity, di.unit_index, di.unit_name, di.conversion_factor, i.units
			 FROM dispatch_items di JOIN items i ON i.id = di.item_id
			 WHERE di.dispatch_id = $1`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty, factor pgtype.Numeric
			var unitIndex int32
			var unitName string
			var unitsJSON []byte
			if err := rows.Scan(&itemID, &qty, &unitIndex, &unitName, &factor, &unitsJSON); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
				return
			}
			// Unwound at the stored factor, not the item's current units.
			f := storedConversionFactor(factor)
			enteredQty := numericToFloat64(qty)
			key := itemKey(itemID.Bytes, unitIndex)
			oldByKey[key] = &lineState{
				itemID: itemID.Bytes, quantity: enteredQty, baseQty: enteredQty * f,
				unitIndex: unitIndex, unitName: unitName,
				baseIndex: baseUnitIndex(unitsJSON, unitIndex), baseName: baseUnitName(unitsJSON),
			}
		}
		rows.Close()
	}

	var invID pgtype.UUID
	_ = tx.QueryRow(ctx,
		`SELECT id FROM invoices WHERE dispatch_id = $1 ORDER BY created_at LIMIT 1`, pgUUID(id)).Scan(&invID)

	// Summed in base units, so the derived cost is per base unit.
	type costRec struct {
		qty   float64
		value int64
	}
	costByItem := map[uuid.UUID]costRec{}
	var currentBookedValue int64
	if invID.Valid {
		rows, err := tx.Query(ctx,
			`SELECT item_id, COALESCE(SUM(quantity * conversion_factor),0), COALESCE(SUM(quantity*price),0)::bigint
			 FROM invoice_items WHERE invoice_id = $1 GROUP BY item_id`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil nilai faktur")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty pgtype.Numeric
			var value int64
			if err := rows.Scan(&itemID, &qty, &value); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca nilai faktur")
				return
			}
			costByItem[itemID.Bytes] = costRec{qty: numericToFloat64(qty), value: value}
			currentBookedValue += value
		}
		rows.Close()
	}

	var netExpenseDelta int64 // positive = more expense booked

	keys := map[string]struct{}{}
	for k := range oldByKey {
		keys[k] = struct{}{}
	}
	for k := range newByKey {
		keys[k] = struct{}{}
	}

	// Differences are in base units: a changed factor moves stock even when the typed quantity did not.
	for key := range keys {
		var itemID uuid.UUID
		var baseIndex int32
		var baseName string
		oldQty, newQty := 0.0, 0.0
		if o, ok := oldByKey[key]; ok {
			oldQty = o.baseQty
			itemID, baseIndex, baseName = o.itemID, o.baseIndex, o.baseName
		}
		if n, ok := newByKey[key]; ok {
			newQty = n.baseQty
			itemID, baseIndex, baseName = n.itemID, n.baseIndex, n.baseName
		}
		unitIndex, unitName := baseIndex, baseName

		switch {
		case newQty > oldQty+dispatchEpsilon:
			delta := newQty - oldQty
			extraValue, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, delta)
			if err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity, err.Error())
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal mengurangi stok")
				return
			}
			price := int64(0)
			if delta > 0 {
				price = int64(float64(extraValue) / delta)
			}
			if invID.Valid {
				if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
					InvoiceID:   invID,
					ItemID:      pgUUID(itemID),
					Quantity:    floatToNumeric(delta),
					UnitIndex:   pgtype.Int4{Int32: unitIndex, Valid: true},
					Price:       price,
					Description: pgtype.Text{String: "Koreksi pengiriman (tambah)", Valid: true},
				}); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi faktur")
					return
				}
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: warehouseID, QuantityChange: -delta, UnitName: unitName,
				Type: "dispatch_edit", Date: effectiveDate, Value: -extraValue, SourceID: id, SourceType: "dispatch",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			netExpenseDelta += extraValue

		case newQty < oldQty-dispatchEpsilon:
			ret := oldQty - newQty
			c := costByItem[itemID]
			unitCost := 0.0
			if c.qty > 0 {
				unitCost = float64(c.value) / c.qty
			}
			retValue := int64(unitCost * ret)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, ret, unitIndex, retValue, effectiveDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok")
				return
			}
			if invID.Valid {
				if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
					InvoiceID:   invID,
					ItemID:      pgUUID(itemID),
					Quantity:    floatToNumeric(-ret),
					UnitIndex:   pgtype.Int4{Int32: unitIndex, Valid: true},
					Price:       int64(unitCost),
					Description: pgtype.Text{String: "Koreksi pengiriman (kurang)", Valid: true},
				}); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi faktur")
					return
				}
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: warehouseID, QuantityChange: ret, UnitName: unitName,
				Type: "dispatch_edit", Date: effectiveDate, Value: retValue, SourceID: id, SourceType: "dispatch",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			netExpenseDelta -= retValue
		}
	}

	oldExpAcct, _ := invoiceExpenseAccountID(ctx, qtx, uuid.Nil, uuidFromPg(curDivision), uuidFromPg(curBranch))
	newExpAcct, _ := invoiceExpenseAccountID(ctx, qtx, uuid.Nil, newDivisionID, newBranchID)
	invAcct, _ := qtx.GetWarehouseInventoryAccountID(ctx, curWarehouse)

	// Relocation between divisions and the net item delta are separate entries.
	if oldExpAcct != newExpAcct && currentBookedValue != 0 {
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        effectiveDate,
			SourceType:  service.SourceDispatch,
			SourceID:    id,
			Description: "Pemindahan beban pengiriman antar cabang/divisi",
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Cr(oldExpAcct, currentBookedValue),
				service.Dr(newExpAcct, currentBookedValue),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pemindahan beban")
			return
		}
	}

	if netExpenseDelta != 0 {
		var invAcctIDValue uuid.UUID
		if invAcct.Valid {
			invAcctIDValue = invAcct.Bytes
		}
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        effectiveDate,
			SourceType:  service.SourceDispatch,
			SourceID:    id,
			Description: "Penyesuaian pengiriman (revisi)",
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Dr(newExpAcct, netExpenseDelta),
				service.Cr(invAcctIDValue, netExpenseDelta),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal penyesuaian pengiriman")
			return
		}
	}

	if invID.Valid {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices SET branch_id = $1, division_id = $2,
			        date = COALESCE($3::date, date) WHERE id = $4`,
			pgUUID(newBranchID), pgUUID(newDivisionID),
			pgDateOrNull(hasNewEntryDate, newEntryDate), invID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui faktur")
			return
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM dispatch_items WHERE dispatch_id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui item pengiriman")
		return
	}
	for _, n := range newByKey {
		if err := qtx.InsertDispatchItem(ctx, &db.InsertDispatchItemParams{
			DispatchID:       pgUUID(id),
			ItemID:           pgUUID(n.itemID),
			Quantity:         floatToNumeric(n.quantity),
			UnitIndex:        n.unitIndex,
			UnitName:         n.unitName,
			ConversionFactor: floatToNumeric(storedFactor(n)),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item pengiriman")
			return
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE dispatches SET branch_id = $1, division_id = $2, notes = $3,
		        dispatched_at = COALESCE($4::timestamptz, dispatched_at), updated_at = now()
		 WHERE id = $5`,
		pgUUID(newBranchID), pgUUID(newDivisionID),
		pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		pgTimestamptzOrNull(hasNewEntryDate, newEntryDate), pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui pengiriman")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan pengiriman")
		return
	}

	dispatch, _ := h.queries.GetDispatchByID(ctx, pgUUID(id))
	updatedItems, _ := h.queries.GetDispatchItems(ctx, pgUUID(id))

	branchName := ""
	if dispatch != nil {
		branchName = dispatch.BranchName
		if dispatch.DivisionName.Valid {
			branchName += "/" + dispatch.DivisionName.String
		}
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: userID, Username: username, Action: "UPDATE", EntityType: "Dispatch",
		EntityID: id, Description: fmt.Sprintf("Perubahan pengiriman barang ke %s", branchName),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"dispatch": dispatch,
		"items":    updatedItems,
	})
}
