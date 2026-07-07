package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type StockTransfersHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewStockTransfersHandler(pool *pgxpool.Pool, queries *db.Queries) *StockTransfersHandler {
	return &StockTransfersHandler{pool: pool, queries: queries}
}

func (h *StockTransfersHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var fromDate, toDate pgtype.Date
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListStockTransfers(ctx, &db.ListStockTransfersParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if rows == nil {
		rows = []*db.ListStockTransfersRow{}
	}

	// Status lives on a column added after the sqlc query was generated (migration
	// 020), so fetch it separately and attach it to each row.
	statusByID := map[pgtype.UUID]string{}
	if statusRows, err := h.pool.Query(ctx, `SELECT id, status FROM stock_transfers`); err == nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var stID pgtype.UUID
			var st string
			if statusRows.Scan(&stID, &st) == nil {
				statusByID[stID] = st
			}
		}
	}

	type transferWithStatus struct {
		*db.ListStockTransfersRow
		Status string `json:"status"`
	}
	result := make([]transferWithStatus, len(rows))
	for i, row := range rows {
		status := statusByID[row.ID]
		if status == "" {
			status = "active"
		}
		result[i] = transferWithStatus{ListStockTransfersRow: row, Status: status}
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *StockTransfersHandler) ListByGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID, err := parseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "groupId tidak valid")
		return
	}

	rows, err := h.queries.ListStockTransfersByGroup(ctx, pgtype.UUID{Bytes: groupID, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if rows == nil {
		rows = []*db.ListStockTransfersByGroupRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

func (h *StockTransfersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FromWarehouseID string `json:"from_warehouse_id"`
		ToWarehouseID   string `json:"to_warehouse_id"`
		Notes           string `json:"notes"`
		Items           []struct {
			ItemID    string  `json:"item_id"`
			Quantity  float64 `json:"quantity"`
			UnitIndex int32   `json:"unit_index"`
			UnitName  string  `json:"unit_name"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.FromWarehouseID == "" || body.ToWarehouseID == "" {
		respondError(w, http.StatusBadRequest, "gudang asal dan tujuan diperlukan")
		return
	}
	if body.FromWarehouseID == body.ToWarehouseID {
		respondError(w, http.StatusBadRequest, "gudang asal dan tujuan harus berbeda")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	fromID, err := parseUUID(body.FromWarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "from_warehouse_id tidak valid")
		return
	}
	toID, err := parseUUID(body.ToWarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "to_warehouse_id tidak valid")
		return
	}

	ctx := r.Context()

	// Read warehouse names and account IDs before starting the transaction
	fromWH, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: fromID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang asal tidak ditemukan")
		return
	}
	toWH, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: toID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tujuan tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	groupID := uuid.New()
	today := time.Now()

	type transferRecord struct {
		ID        pgtype.UUID
		GroupID   pgtype.UUID
		ItemName  string
		Quantity  float64
		UnitName  string
	}
	var records []transferRecord

	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		// Get item name for error messages
		item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
			return
		}

		// 1. FIFO deduct from source
		valueDeducted, err := service.FIFODeduct(ctx, qtx, itemID, fromID, it.Quantity)
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("stok tidak mencukupi untuk item: %s", item.Name))
				return
			}
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("gagal mengurangi stok: %v", err))
			return
		}

		// 2. FIFO add to destination
		if err := service.FIFOAdd(ctx, qtx, itemID, toID, it.Quantity, it.UnitIndex, valueDeducted, today); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menambah stok di tujuan")
			return
		}

		// 3. Stock history for source
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    fromID,
			QuantityChange: -it.Quantity,
			UnitName:       it.UnitName,
			Type:           "transfer",
			Reference:      fmt.Sprintf("Transfer → %s", toWH.Name),
			Date:           today,
			Value:          -valueDeducted,
			SourceID:       groupID,
			SourceType:     "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		// 4. Stock history for destination
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    toID,
			QuantityChange: it.Quantity,
			UnitName:       it.UnitName,
			Type:           "transfer",
			Reference:      fmt.Sprintf("Transfer ← %s", fromWH.Name),
			Date:           today,
			Value:          valueDeducted,
			SourceID:       groupID,
			SourceType:     "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		// 5 & 6. Update source warehouse account balance
		if fromWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, fromWH.InventoryAccountID.Bytes, -valueDeducted); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun asal")
				return
			}
		}

		// 7. Update destination warehouse account balance
		if toWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, toWH.InventoryAccountID.Bytes, valueDeducted); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
				return
			}
		}

		// 8. Insert stock_transfer record
		rec, err := qtx.InsertStockTransfer(ctx, &db.InsertStockTransferParams{
			ItemID:          pgtype.UUID{Bytes: itemID, Valid: true},
			FromWarehouseID: pgtype.UUID{Bytes: fromID, Valid: true},
			ToWarehouseID:   pgtype.UUID{Bytes: toID, Valid: true},
			Quantity:        floatToNumeric(it.Quantity),
			UnitIndex:       it.UnitIndex,
			UnitName:        it.UnitName,
			Notes:           pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
			TransferredBy:   pgtype.UUID{Bytes: middleware.UserIDFromCtx(ctx), Valid: middleware.UserIDFromCtx(ctx) != uuid.Nil},
			GroupID:         pgtype.UUID{Bytes: groupID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan data transfer")
			return
		}

		records = append(records, transferRecord{
			ID:       rec.ID,
			GroupID:  rec.GroupID,
			ItemName: item.Name,
			Quantity: it.Quantity,
			UnitName: it.UnitName,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	// 9. Log activity after commit
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "StockTransfer",
		Description: fmt.Sprintf("Transfer stok dari %s ke %s", fromWH.Name, toWH.Name),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"group_id": groupID,
		"transfers": records,
	})
}

// transferItemInput mirrors the item payload used by Update.
type transferItemInput struct {
	ItemID    string  `json:"item_id"`
	Quantity  float64 `json:"quantity"`
	UnitIndex int32   `json:"unit_index"`
	UnitName  string  `json:"unit_name"`
}

// transferLine is one item+unit line within a transfer group.
type transferLine struct {
	itemID    uuid.UUID
	quantity  float64
	unitIndex int32
	unitName  string
}

// transferGroup is the loaded state of a transfer group (its shared header
// fields plus one entry per existing stock_transfers row).
type transferGroup struct {
	fromID        uuid.UUID
	toID          uuid.UUID
	transferredBy pgtype.UUID
	transferredAt pgtype.Timestamptz
	status        string
	lines         []*transferLine
}

// loadTransferGroup reads every stock_transfers row sharing groupID. The header
// fields (warehouses, who/when, status) are identical across a group, so they
// are taken from the first row. Returns pgx.ErrNoRows when the group is empty.
func (h *StockTransfersHandler) loadTransferGroup(ctx context.Context, tx pgx.Tx, groupID uuid.UUID) (*transferGroup, error) {
	rows, err := tx.Query(ctx,
		`SELECT item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name,
		        transferred_by, transferred_at, status
		 FROM stock_transfers WHERE group_id = $1 ORDER BY transferred_at`, pgUUID(groupID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	g := &transferGroup{}
	found := false
	for rows.Next() {
		var itemID, fromID, toID, transferredBy pgtype.UUID
		var qty pgtype.Numeric
		var unitIndex int32
		var unitName, status string
		var transferredAt pgtype.Timestamptz
		if err := rows.Scan(&itemID, &fromID, &toID, &qty, &unitIndex, &unitName,
			&transferredBy, &transferredAt, &status); err != nil {
			return nil, err
		}
		if !found {
			g.fromID = fromID.Bytes
			g.toID = toID.Bytes
			g.transferredBy = transferredBy
			g.transferredAt = transferredAt
			g.status = status
			found = true
		}
		g.lines = append(g.lines, &transferLine{
			itemID: itemID.Bytes, quantity: numericToFloat64(qty), unitIndex: unitIndex, unitName: unitName,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, pgx.ErrNoRows
	}
	return g, nil
}

// netByKey collapses the group's rows (originals plus any appended corrections)
// into the current net quantity per item+unit.
func (g *transferGroup) netByKey() map[string]*transferLine {
	out := map[string]*transferLine{}
	for _, ln := range g.lines {
		key := itemKey(ln.itemID, ln.unitIndex)
		if ex, ok := out[key]; ok {
			ex.quantity += ln.quantity
		} else {
			cp := *ln
			out[key] = &cp
		}
	}
	return out
}

// Update — PUT /api/stock-transfers/group/{groupId}
//
// Edits an active transfer group. Each quantity change is reconciled physically
// as a delta: extra qty is moved source→destination (FIFO), reduced qty is moved
// back destination→source, and both warehouses' inventory account balances shift
// by the value moved. The group's rows are then rewritten to the new state,
// keeping the original group_id and timestamp for the audit trail.
func (h *StockTransfersHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID, err := parseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "groupId tidak valid")
		return
	}

	var body struct {
		Notes string              `json:"notes"`
		Items []transferItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	// Desired new state keyed by item+unit (duplicate lines are summed).
	newByKey := map[string]*transferLine{}
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
		key := itemKey(itemID, it.UnitIndex)
		if ex, ok := newByKey[key]; ok {
			ex.quantity += it.Quantity
		} else {
			newByKey[key] = &transferLine{itemID: itemID, quantity: it.Quantity, unitIndex: it.UnitIndex, unitName: it.UnitName}
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	g, err := h.loadTransferGroup(ctx, tx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "transfer tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if g.status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "transfer sudah dibatalkan dan tidak dapat diubah")
		return
	}

	fromWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.fromID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang asal")
		return
	}
	toWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.toID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang tujuan")
		return
	}

	// Current net state keyed by item+unit (originals plus any prior corrections).
	oldByKey := g.netByKey()

	// Corrections are booked on the transfer's own date so they land in the same
	// reporting period as the original movement.
	effectiveDate := time.Now()
	if g.transferredAt.Valid {
		effectiveDate = g.transferredAt.Time
	}

	// The note is descriptive metadata shared by the group; appended correction
	// rows carry it and the group's rows are refreshed to it at the end.
	notes := pgtype.Text{String: strings.TrimSpace(body.Notes), Valid: strings.TrimSpace(body.Notes) != ""}

	// appendCorrection records a signed quantity delta as a NEW stock_transfers
	// row (positive = extra transferred, negative = returned) rather than
	// rewriting the originals, so the full movement history is preserved.
	appendCorrection := func(itemID uuid.UUID, unitIndex int32, unitName string, qty float64) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO stock_transfers
			   (id, item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name,
			    notes, transferred_by, transferred_at, group_id, status, updated_at)
			 VALUES (gen_random_uuid(), $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'active', now())`,
			pgUUID(itemID), pgUUID(g.fromID), pgUUID(g.toID),
			floatToNumeric(qty), unitIndex, unitName,
			notes, g.transferredBy, g.transferredAt, pgUUID(groupID))
		return err
	}

	keys := map[string]struct{}{}
	for k := range oldByKey {
		keys[k] = struct{}{}
	}
	for k := range newByKey {
		keys[k] = struct{}{}
	}

	for key := range keys {
		var itemID uuid.UUID
		var unitIndex int32
		var unitName string
		oldQty, newQty := 0.0, 0.0
		if o, ok := oldByKey[key]; ok {
			oldQty = o.quantity
			itemID, unitIndex, unitName = o.itemID, o.unitIndex, o.unitName
		}
		if n, ok := newByKey[key]; ok {
			newQty = n.quantity
			itemID, unitIndex, unitName = n.itemID, n.unitIndex, n.unitName
		}

		switch {
		case newQty > oldQty+dispatchEpsilon:
			// Move the extra quantity source → destination.
			delta := newQty - oldQty
			v, err := service.FIFODeduct(ctx, qtx, itemID, g.fromID, delta)
			if err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity, "stok tidak mencukupi di gudang asal")
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal mengurangi stok")
				return
			}
			if err := service.FIFOAdd(ctx, qtx, itemID, g.toID, delta, unitIndex, v, effectiveDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok di tujuan")
				return
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: g.fromID, QuantityChange: -delta, UnitName: unitName,
				Type: "transfer_edit", Reference: fmt.Sprintf("Koreksi transfer → %s", toWH.Name),
				Date: effectiveDate, Value: -v, SourceID: groupID, SourceType: "transfer",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: g.toID, QuantityChange: delta, UnitName: unitName,
				Type: "transfer_edit", Reference: fmt.Sprintf("Koreksi transfer ← %s", fromWH.Name),
				Date: effectiveDate, Value: v, SourceID: groupID, SourceType: "transfer",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			if fromWH.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, fromWH.InventoryAccountID.Bytes, -v); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun asal")
					return
				}
			}
			if toWH.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, toWH.InventoryAccountID.Bytes, v); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
					return
				}
			}
			if err := appendCorrection(itemID, unitIndex, unitName, delta); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi transfer")
				return
			}

		case newQty < oldQty-dispatchEpsilon:
			// Move the reduced quantity back destination → source.
			ret := oldQty - newQty
			v, err := service.FIFODeduct(ctx, qtx, itemID, g.toID, ret)
			if err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity, "stok di gudang tujuan tidak mencukupi untuk mengurangi transfer")
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal mengurangi stok tujuan")
				return
			}
			if err := service.FIFOAdd(ctx, qtx, itemID, g.fromID, ret, unitIndex, v, effectiveDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok ke asal")
				return
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: g.toID, QuantityChange: -ret, UnitName: unitName,
				Type: "transfer_edit", Reference: fmt.Sprintf("Koreksi transfer → %s", fromWH.Name),
				Date: effectiveDate, Value: -v, SourceID: groupID, SourceType: "transfer",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: g.fromID, QuantityChange: ret, UnitName: unitName,
				Type: "transfer_edit", Reference: fmt.Sprintf("Koreksi transfer ← %s", toWH.Name),
				Date: effectiveDate, Value: v, SourceID: groupID, SourceType: "transfer",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			if toWH.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, toWH.InventoryAccountID.Bytes, -v); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
					return
				}
			}
			if fromWH.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, fromWH.InventoryAccountID.Bytes, v); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun asal")
					return
				}
			}
			if err := appendCorrection(itemID, unitIndex, unitName, -ret); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi transfer")
				return
			}
		}
	}

	// Refresh the descriptive note across the group's rows. Quantity/movement rows
	// are never rewritten — corrections were appended as new rows above — so the
	// full history is preserved for the audit trail.
	if _, err := tx.Exec(ctx,
		`UPDATE stock_transfers SET notes = $1, updated_at = now() WHERE group_id = $2`,
		notes, pgUUID(groupID)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui catatan transfer")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan transfer")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "StockTransfer",
		EntityID:    groupID,
		Description: fmt.Sprintf("Perubahan transfer stok dari %s ke %s", fromWH.Name, toWH.Name),
	})

	updated, _ := h.queries.ListStockTransfersByGroup(ctx, pgUUID(groupID))
	if updated == nil {
		updated = []*db.ListStockTransfersByGroupRow{}
	}
	respondJSON(w, http.StatusOK, updated)
}

// Delete — DELETE /api/stock-transfers/group/{groupId}
//
// Cancels a transfer group without erasing it: for every line the movement is
// reversed (stock pulled back from destination via FIFO and returned to source),
// both warehouses' inventory account balances are moved back, and the rows are
// flagged 'cancelled' and kept for the audit trail.
func (h *StockTransfersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID, err := parseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "groupId tidak valid")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	g, err := h.loadTransferGroup(ctx, tx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "transfer tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if g.status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "transfer sudah dibatalkan")
		return
	}

	fromWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.fromID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang asal")
		return
	}
	toWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.toID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang tujuan")
		return
	}

	// Reverse on the transfer's own date so it nets against the original movement
	// within the same reporting period.
	reversalDate := time.Now()
	if g.transferredAt.Valid {
		reversalDate = g.transferredAt.Time
	}

	for _, ln := range g.netByKey() {
		if ln.quantity <= dispatchEpsilon {
			continue
		}
		v, err := service.FIFODeduct(ctx, qtx, ln.itemID, g.toID, ln.quantity)
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity, "stok di gudang tujuan tidak mencukupi untuk membatalkan transfer")
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal mengurangi stok tujuan")
			return
		}
		if err := service.FIFOAdd(ctx, qtx, ln.itemID, g.fromID, ln.quantity, ln.unitIndex, v, reversalDate); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok ke asal")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: ln.itemID, WarehouseID: g.toID, QuantityChange: -ln.quantity, UnitName: ln.unitName,
			Type: "transfer_cancel", Reference: fmt.Sprintf("Pembatalan transfer → %s", fromWH.Name),
			Date: reversalDate, Value: -v, SourceID: groupID, SourceType: "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: ln.itemID, WarehouseID: g.fromID, QuantityChange: ln.quantity, UnitName: ln.unitName,
			Type: "transfer_cancel", Reference: fmt.Sprintf("Pembatalan transfer ← %s", toWH.Name),
			Date: reversalDate, Value: v, SourceID: groupID, SourceType: "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
		if toWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, toWH.InventoryAccountID.Bytes, -v); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
				return
			}
		}
		if fromWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, fromWH.InventoryAccountID.Bytes, v); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun asal")
				return
			}
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE stock_transfers SET status = 'cancelled', updated_at = now() WHERE group_id = $1`, pgUUID(groupID)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan transfer")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan transfer")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "StockTransfer",
		EntityID:    groupID,
		Description: fmt.Sprintf("Pembatalan transfer stok dari %s ke %s", fromWH.Name, toWH.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}
