package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
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

type StockOpnameHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewStockOpnameHandler(pool *pgxpool.Pool, queries *db.Queries) *StockOpnameHandler {
	return &StockOpnameHandler{pool: pool, queries: queries}
}

// ─── List submitted opnames ───────────────────────────────────────────────────

func (h *StockOpnameHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.queries.ListStockOpname(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data stok opname")
		return
	}
	if rows == nil {
		rows = []*db.ListStockOpnameRow{}
	}

	type listItem struct {
		ID               pgtype.UUID    `json:"id"`
		OpnameID         pgtype.UUID    `json:"opname_id"`
		ItemID           pgtype.UUID    `json:"item_id"`
		UnitIndex        int32          `json:"unit_index"`
		UnitName         string         `json:"unit_name"`
		RecordedQuantity pgtype.Numeric `json:"recorded_quantity"`
		ActualQuantity   pgtype.Numeric `json:"actual_quantity"`
		Difference       pgtype.Numeric `json:"difference"`
		WasteValue       int64          `json:"waste_value"`
		IsCorrection     bool           `json:"is_correction"`
		ItemName         string         `json:"item_name"`
	}
	itemRows, err := h.pool.Query(ctx, `
		SELECT soi.id, soi.opname_id, soi.item_id, soi.unit_index, soi.unit_name,
		       soi.recorded_quantity, soi.actual_quantity, soi.difference, soi.waste_value,
		       soi.is_correction, i.name AS item_name
		FROM stock_opname_items soi
		JOIN items i ON i.id = soi.item_id
		ORDER BY soi.is_correction, soi.created_at, i.name
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item opname")
		return
	}
	defer itemRows.Close()

	byOpname := map[pgtype.UUID][]listItem{}
	for itemRows.Next() {
		var it listItem
		if err := itemRows.Scan(&it.ID, &it.OpnameID, &it.ItemID, &it.UnitIndex, &it.UnitName,
			&it.RecordedQuantity, &it.ActualQuantity, &it.Difference, &it.WasteValue, &it.IsCorrection, &it.ItemName); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca item opname")
			return
		}
		byOpname[it.OpnameID] = append(byOpname[it.OpnameID], it)
	}

	type opnameWithItems struct {
		*db.ListStockOpnameRow
		Items []listItem `json:"items"`
	}
	result := make([]opnameWithItems, len(rows))
	for i, row := range rows {
		items := byOpname[row.ID]
		if items == nil {
			items = []listItem{}
		}
		result[i] = opnameWithItems{ListStockOpnameRow: row, Items: items}
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *StockOpnameHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	opname, err := h.queries.GetStockOpnameByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "stok opname tidak ditemukan")
		return
	}

	items, err := h.fetchOpnameItems(ctx, opname.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item opname")
		return
	}

	// updated_at lives on a column added after the sqlc query was generated.
	var updatedAt pgtype.Timestamptz
	_ = h.pool.QueryRow(ctx, `SELECT updated_at FROM stock_opname WHERE id = $1`, opname.ID).Scan(&updatedAt)

	respondJSON(w, http.StatusOK, map[string]any{
		"id":                opname.ID,
		"warehouse_id":      opname.WarehouseID,
		"warehouse_name":    opname.WarehouseName,
		"notes":             opname.Notes,
		"performed_at":      opname.PerformedAt,
		"updated_at":        updatedAt,
		"operator_name":     opname.OperatorName,
		"pic_name":          opname.PicName,
		"performed_by_name": opname.PerformedByName,
		"items":             items,
	})
}

// opnameItem is the item shape returned by Get, including the is_correction
// flag (a column added after the sqlc query was generated, so read raw).
type opnameItem struct {
	ID               pgtype.UUID    `json:"id"`
	OpnameID         pgtype.UUID    `json:"opname_id"`
	ItemID           pgtype.UUID    `json:"item_id"`
	UnitIndex        int32          `json:"unit_index"`
	UnitName         string         `json:"unit_name"`
	RecordedQuantity pgtype.Numeric `json:"recorded_quantity"`
	ActualQuantity   pgtype.Numeric `json:"actual_quantity"`
	Difference       pgtype.Numeric `json:"difference"`
	WasteValue       int64          `json:"waste_value"`
	IsCorrection     bool           `json:"is_correction"`
	CreatedAt        pgtype.Timestamptz `json:"created_at"`
	ItemName         string         `json:"item_name"`
	ItemCode         pgtype.Text    `json:"item_code"`
}

// fetchOpnameItems reads the items for an opname, original rows first then
// corrections in the order they were booked.
func (h *StockOpnameHandler) fetchOpnameItems(ctx context.Context, opnameID pgtype.UUID) ([]opnameItem, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT soi.id, soi.opname_id, soi.item_id, soi.unit_index, soi.unit_name,
		       soi.recorded_quantity, soi.actual_quantity, soi.difference, soi.waste_value,
		       soi.is_correction, soi.created_at,
		       i.name AS item_name, i.code AS item_code
		FROM stock_opname_items soi
		JOIN items i ON i.id = soi.item_id
		WHERE soi.opname_id = $1
		ORDER BY soi.is_correction, soi.created_at, i.name
	`, opnameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []opnameItem{}
	for rows.Next() {
		var it opnameItem
		if err := rows.Scan(&it.ID, &it.OpnameID, &it.ItemID, &it.UnitIndex, &it.UnitName,
			&it.RecordedQuantity, &it.ActualQuantity, &it.Difference, &it.WasteValue,
			&it.IsCorrection, &it.CreatedAt, &it.ItemName, &it.ItemCode); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ─── Draft endpoints ──────────────────────────────────────────────────────────

type draftItem struct {
	ItemID    string  `json:"item_id"`
	UnitIndex int32   `json:"unit_index"`
	UnitName  string  `json:"unit_name"`
	ActualQty float64 `json:"actual_qty"`
}

func (h *StockOpnameHandler) ListDrafts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.pool.Query(ctx, `
		SELECT d.id::text, d.warehouse_id::text, w.name,
		       d.pic_name, d.operator_name, d.notes, d.items,
		       d.created_at, d.updated_at
		FROM stock_opname_drafts d
		JOIN warehouses w ON w.id = d.warehouse_id
		ORDER BY d.updated_at DESC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil draft opname")
		return
	}
	defer rows.Close()

	type draftRow struct {
		ID            string          `json:"id"`
		WarehouseID   string          `json:"warehouse_id"`
		WarehouseName string          `json:"warehouse_name"`
		PicName       *string         `json:"pic_name"`
		OperatorName  *string         `json:"operator_name"`
		Notes         *string         `json:"notes"`
		Items         json.RawMessage `json:"items"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
	}

	var result []draftRow
	for rows.Next() {
		var d draftRow
		if err := rows.Scan(&d.ID, &d.WarehouseID, &d.WarehouseName,
			&d.PicName, &d.OperatorName, &d.Notes, &d.Items,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca draft")
			return
		}
		result = append(result, d)
	}
	if result == nil {
		result = []draftRow{}
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *StockOpnameHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarehouseID  string      `json:"warehouse_id"`
		PicName      string      `json:"pic_name"`
		OperatorName string      `json:"operator_name"`
		Notes        string      `json:"notes"`
		Items        []draftItem `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.WarehouseID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id diperlukan")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)

	items := body.Items
	if items == nil {
		items = []draftItem{}
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan item draft")
		return
	}

	var id string
	err = h.pool.QueryRow(ctx, `
		INSERT INTO stock_opname_drafts (warehouse_id, pic_name, operator_name, notes, items, created_by)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), $5, $6)
		RETURNING id::text
	`,
		body.WarehouseID,
		body.PicName, body.OperatorName, body.Notes,
		itemsJSON,
		pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat draft opname")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *StockOpnameHandler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(draftID); err != nil {
		respondError(w, http.StatusBadRequest, "ID draft tidak valid")
		return
	}

	var body struct {
		PicName      string      `json:"pic_name"`
		OperatorName string      `json:"operator_name"`
		Notes        string      `json:"notes"`
		Items        []draftItem `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	items := body.Items
	if items == nil {
		items = []draftItem{}
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan item draft")
		return
	}

	ctx := r.Context()
	tag, err := h.pool.Exec(ctx, `
		UPDATE stock_opname_drafts
		SET pic_name = NULLIF($1,''), operator_name = NULLIF($2,''),
		    notes = NULLIF($3,''), items = $4, updated_at = NOW()
		WHERE id = $5
	`,
		body.PicName, body.OperatorName, body.Notes,
		itemsJSON, draftID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		respondError(w, http.StatusNotFound, "draft tidak ditemukan")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"id": draftID})
}

func (h *StockOpnameHandler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(draftID); err != nil {
		respondError(w, http.StatusBadRequest, "ID draft tidak valid")
		return
	}

	ctx := r.Context()
	_, err := h.pool.Exec(ctx, `DELETE FROM stock_opname_drafts WHERE id = $1`, draftID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus draft")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── Create (submit final opname) ────────────────────────────────────────────

func (h *StockOpnameHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarehouseID  string `json:"warehouse_id"`
		Notes        string `json:"notes"`
		OperatorName string `json:"operator_name"`
		PicName      string `json:"pic_name"`
		DraftID      string `json:"draft_id"`
		Items        []struct {
			ItemID         string  `json:"item_id"`
			UnitIndex      int32   `json:"unit_index"`
			UnitName       string  `json:"unit_name"`
			ActualQuantity float64 `json:"actual_quantity"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.WarehouseID == "" || len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "gudang dan item diperlukan")
		return
	}

	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	wh, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	today := time.Now()

	// Get or create Stock Waste account
	wasteAccountID, err := qtx.GetStockWasteAccountID(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusInternalServerError, "gagal mencari akun stok waste")
			return
		}
		created, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{
			Name:        "Stock Waste",
			AccountType: "expense",
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat akun stok waste")
			return
		}
		wasteAccountID = created.ID
	}

	// Insert stock_opname header
	opname, err := qtx.InsertStockOpname(ctx, &db.InsertStockOpnameParams{
		WarehouseID:  pgtype.UUID{Bytes: warehouseID, Valid: true},
		Notes:        pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		PerformedBy:  pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
		OperatorName: pgtype.Text{String: body.OperatorName, Valid: body.OperatorName != ""},
		PicName:      pgtype.Text{String: body.PicName, Valid: body.PicName != ""},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat stok opname")
		return
	}

	// Process each item
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		rawQty, err := qtx.GetCurrentInventoryQuantity(ctx, &db.GetCurrentInventoryQuantityParams{
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca kuantitas inventori")
			return
		}
		recorded := anyNumericToFloat64(rawQty)
		actual := it.ActualQuantity
		diff := actual - recorded

		if math.Abs(diff) < 0.001 {
			_ = qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
				OpnameID:         opname.ID,
				ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
				UnitIndex:        it.UnitIndex,
				UnitName:         it.UnitName,
				RecordedQuantity: floatToNumeric(recorded),
				ActualQuantity:   floatToNumeric(actual),
				Difference:       floatToNumeric(0.0),
				WasteValue:       0,
			})
			continue
		}

		var wasteValue int64
		if diff < 0 {
			// Loss: FIFO deduct
			deducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, math.Abs(diff))
			if err != nil {
				respondError(w, http.StatusInternalServerError,
					fmt.Sprintf("gagal mengurangi stok opname: %v", err))
				return
			}
			wasteValue = deducted
		} else {
			// Surplus: add stock at latest known price (prorated for qty)
			surplusValue := latestItemValue(ctx, qtx, itemID, it.UnitIndex, diff)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, diff, it.UnitIndex, surplusValue, today); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok opname")
				return
			}
			wasteValue = 0
			// Credit the inventory asset account for the value added
			if surplusValue > 0 && wh.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wh.InventoryAccountID.Bytes, surplusValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun inventori")
					return
				}
			}
		}

		if err := qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
			OpnameID:         opname.ID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
			UnitIndex:        it.UnitIndex,
			UnitName:         it.UnitName,
			RecordedQuantity: floatToNumeric(recorded),
			ActualQuantity:   floatToNumeric(actual),
			Difference:       floatToNumeric(diff),
			WasteValue:       wasteValue,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item opname")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: diff,
			UnitName:       it.UnitName,
			Type:           "opname",
			Reference:      "Stok Opname",
			Date:           today,
			Value:          wasteValue,
			SourceID:       opname.ID.Bytes,
			SourceType:     "opname",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		if wasteValue > 0 {
			if wasteAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wasteAccountID.Bytes, wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun waste")
					return
				}
			}
			if wh.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wh.InventoryAccountID.Bytes, -wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun inventori")
					return
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	// Delete the draft now that the opname is finalised
	if body.DraftID != "" {
		_, _ = h.pool.Exec(ctx, `DELETE FROM stock_opname_drafts WHERE id = $1`, body.DraftID)
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "StockOpname",
		EntityID:    opname.ID.Bytes,
		Description: fmt.Sprintf("Stok opname di gudang %s", wh.Name),
	})

	respondJSON(w, http.StatusCreated, opname)
}

// Update — PUT /api/stock-opname/{id}
//
// Edits (corrects) a submitted opname. Instead of rewriting the already-posted
// rows, every corrected item is reconciled against the CURRENT system quantity
// and the difference is appended as a NEW stock_opname_items row
// (is_correction = true) plus a matching stock_history row, with the waste and
// inventory account balances moved by the difference. The original rows stay
// intact as the audit trail, and reports net correctly because they sum the
// rows. See the accounting-edit convention (dispatches Update is the reference).
func (h *StockOpnameHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Items []struct {
			ItemID         string  `json:"item_id"`
			UnitIndex      int32   `json:"unit_index"`
			UnitName       string  `json:"unit_name"`
			ActualQuantity float64 `json:"actual_quantity"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	opname, err := h.queries.GetStockOpnameByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "stok opname tidak ditemukan")
		return
	}
	warehouseID := opname.WarehouseID.Bytes

	wh, err := h.queries.GetWarehouseByID(ctx, opname.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	now := time.Now()

	// Get or create Stock Waste account (same as Create).
	wasteAccountID, err := qtx.GetStockWasteAccountID(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusInternalServerError, "gagal mencari akun stok waste")
			return
		}
		created, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{
			Name:        "Stock Waste",
			AccountType: "expense",
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat akun stok waste")
			return
		}
		wasteAccountID = created.ID
	}

	applied := 0
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		rawQty, err := qtx.GetCurrentInventoryQuantity(ctx, &db.GetCurrentInventoryQuantityParams{
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca kuantitas inventori")
			return
		}
		recorded := anyNumericToFloat64(rawQty)
		actual := it.ActualQuantity
		diff := actual - recorded

		// No change against the current system quantity → nothing to book.
		if math.Abs(diff) < 0.001 {
			continue
		}

		var wasteValue int64
		if diff < 0 {
			deducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, math.Abs(diff))
			if err != nil {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("gagal mengoreksi stok opname: %v", err))
				return
			}
			wasteValue = deducted
		} else {
			surplusValue := latestItemValue(ctx, qtx, itemID, it.UnitIndex, diff)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, diff, it.UnitIndex, surplusValue, now); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok opname")
				return
			}
			if surplusValue > 0 && wh.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wh.InventoryAccountID.Bytes, surplusValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun inventori")
					return
				}
			}
		}

		// Append the correction as a NEW row (raw insert to set is_correction,
		// a column added after the sqlc query was generated).
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_opname_items (
				id, opname_id, item_id, unit_index, unit_name,
				recorded_quantity, actual_quantity, difference, waste_value, is_correction
			)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, true)`,
			pgUUID(id), pgUUID(itemID), it.UnitIndex, it.UnitName,
			floatToNumeric(recorded), floatToNumeric(actual), floatToNumeric(diff), wasteValue,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi opname")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: diff,
			UnitName:       it.UnitName,
			Type:           "opname",
			Reference:      "Koreksi Stok Opname",
			Date:           now,
			Value:          wasteValue,
			SourceID:       id,
			SourceType:     "opname",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		if wasteValue > 0 {
			if wasteAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wasteAccountID.Bytes, wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun waste")
					return
				}
			}
			if wh.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wh.InventoryAccountID.Bytes, -wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun inventori")
					return
				}
			}
		}
		applied++
	}

	if applied == 0 {
		respondError(w, http.StatusUnprocessableEntity, "tidak ada perubahan untuk dicatat")
		return
	}

	if _, err := tx.Exec(ctx, `UPDATE stock_opname SET updated_at = now() WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui stok opname")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "StockOpname",
		EntityID:    id,
		Description: fmt.Sprintf("Koreksi stok opname di gudang %s", wh.Name),
	})

	items, err := h.fetchOpnameItems(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item opname")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"id":    id,
		"items": items,
	})
}

// latestItemValue returns the total IDR value to assign to `qty` surplus units
// using the most recent purchase price for the item+unitIndex. Returns 0 if unknown.
func latestItemValue(ctx context.Context, qtx *db.Queries, itemID uuid.UUID, unitIndex int32, qty float64) int64 {
	row, err := qtx.GetItemLastPrice(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
	if err != nil {
		return 0
	}
	// price is per unit at the recorded unit_index; use it directly
	return int64(math.Round(float64(row.Price) * qty))
}
