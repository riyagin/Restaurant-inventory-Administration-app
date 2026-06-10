package handler

import (
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
		ItemName         string         `json:"item_name"`
	}
	itemRows, err := h.pool.Query(ctx, `
		SELECT soi.id, soi.opname_id, soi.item_id, soi.unit_index, soi.unit_name,
		       soi.recorded_quantity, soi.actual_quantity, soi.difference, soi.waste_value,
		       i.name AS item_name
		FROM stock_opname_items soi
		JOIN items i ON i.id = soi.item_id
		ORDER BY i.name
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
			&it.RecordedQuantity, &it.ActualQuantity, &it.Difference, &it.WasteValue, &it.ItemName); err != nil {
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

	items, err := h.queries.GetStockOpnameItems(ctx, opname.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item opname")
		return
	}
	if items == nil {
		items = []*db.GetStockOpnameItemsRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id":                opname.ID,
		"warehouse_id":      opname.WarehouseID,
		"warehouse_name":    opname.WarehouseName,
		"notes":             opname.Notes,
		"performed_at":      opname.PerformedAt,
		"operator_name":     opname.OperatorName,
		"pic_name":          opname.PicName,
		"performed_by_name": opname.PerformedByName,
		"items":             items,
	})
}

func (h *StockOpnameHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarehouseID  string `json:"warehouse_id"`
		Notes        string `json:"notes"`
		OperatorName string `json:"operator_name"`
		PicName      string `json:"pic_name"`
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

	// Read warehouse info before transaction
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
		// Create the Stock Waste account if it doesn't exist
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

	// 1. Insert stock_opname header
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

	// 2. Process each item
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		// a. Get current recorded quantity
		rawQty, err := qtx.GetCurrentInventoryQuantity(ctx, &db.GetCurrentInventoryQuantityParams{
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca kuantitas inventori")
			return
		}
		recorded := anyNumericToFloat64(rawQty)

		// b. Calculate difference
		actual := it.ActualQuantity
		diff := actual - recorded

		// Skip items with no change
		if math.Abs(diff) < 0.001 {
			// Still record for audit
			var recNum, actNum, diffNum pgtype.Numeric
			_ = recNum.Scan(recorded)
			_ = actNum.Scan(actual)
			_ = diffNum.Scan(0.0)
			_ = qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
				OpnameID:         opname.ID,
				ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
				UnitIndex:        it.UnitIndex,
				UnitName:         it.UnitName,
				RecordedQuantity: recNum,
				ActualQuantity:   actNum,
				Difference:       diffNum,
				WasteValue:       0,
			})
			continue
		}

		// c & e. Apply stock adjustment and get waste value
		var wasteValue int64
		if diff < 0 {
			// Loss: FIFO deduct returns the value removed (= waste_value)
			deducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, math.Abs(diff))
			if err != nil {
				respondError(w, http.StatusInternalServerError,
					fmt.Sprintf("gagal mengurangi stok opname: %v", err))
				return
			}
			wasteValue = deducted
		} else {
			// Surplus: add stock with zero value (cost unknown)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, diff, it.UnitIndex, 0, today); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok opname")
				return
			}
			wasteValue = 0
		}

		// d. Insert stock_opname_item
		var recNum, actNum, diffNum pgtype.Numeric
		_ = recNum.Scan(recorded)
		_ = actNum.Scan(actual)
		_ = diffNum.Scan(diff)
		if err := qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
			OpnameID:         opname.ID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
			UnitIndex:        it.UnitIndex,
			UnitName:         it.UnitName,
			RecordedQuantity: recNum,
			ActualQuantity:   actNum,
			Difference:       diffNum,
			WasteValue:       wasteValue,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item opname")
			return
		}

		// f. Insert stock history
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

		// g. Update accounts if there was waste
		if wasteValue > 0 {
			// Expense account increases (waste incurred)
			if wasteAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wasteAccountID.Bytes, wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun waste")
					return
				}
			}
			// Inventory asset account decreases
			if wh.InventoryAccountID.Valid {
				if err := service.UpdateBalance(ctx, qtx, wh.InventoryAccountID.Bytes, -wasteValue); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal memperbarui akun inventori")
					return
				}
			}
		}
	}

	// 3. Commit
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	// 4. Log activity after commit
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
