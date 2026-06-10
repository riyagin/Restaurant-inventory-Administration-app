package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type InventoryHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewInventoryHandler(pool *pgxpool.Pool, queries *db.Queries) *InventoryHandler {
	return &InventoryHandler{pool: pool, queries: queries}
}

func (h *InventoryHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	search    := strings.ToLower(strings.TrimSpace(q.Get("search")))
	dateFrom  := q.Get("date_from")
	dateTo    := q.Get("date_to")

	var warehouseFilter pgtype.UUID
	if idStr := q.Get("warehouse_id"); idStr != "" && idStr != "all" {
		id, err := parseUUID(idStr)
		if err == nil {
			warehouseFilter = pgtype.UUID{Bytes: id, Valid: true}
		}
	}

	var itemFilter pgtype.UUID
	if idStr := q.Get("item_id"); idStr != "" && idStr != "all" {
		id, err := parseUUID(idStr)
		if err == nil {
			itemFilter = pgtype.UUID{Bytes: id, Valid: true}
		}
	}

	var dateFromPg, dateToPg pgtype.Date
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			dateFromPg = pgtype.Date{Time: t, Valid: true}
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			dateToPg = pgtype.Date{Time: t, Valid: true}
		}
	}

	rows, err := h.queries.ListInventory(ctx, &db.ListInventoryParams{
		Column1: warehouseFilter,
		Column2: itemFilter,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data inventori")
		return
	}
	if rows == nil {
		rows = []*db.ListInventoryRow{}
	}

	var result []*db.ListInventoryRow
	for _, row := range rows {
		if search != "" {
			if !strings.Contains(strings.ToLower(row.ItemName), search) &&
				!strings.Contains(strings.ToLower(row.ItemCode), search) {
				continue
			}
		}
		if dateFromPg.Valid && row.Date.Valid && row.Date.Time.Before(dateFromPg.Time) {
			continue
		}
		if dateToPg.Valid && row.Date.Valid && row.Date.Time.After(dateToPg.Time) {
			continue
		}
		result = append(result, row)
	}
	if result == nil {
		result = []*db.ListInventoryRow{}
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *InventoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	row, err := h.queries.GetInventoryByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}
	respondJSON(w, http.StatusOK, row)
}

func (h *InventoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemID      string  `json:"item_id"`
		WarehouseID string  `json:"warehouse_id"`
		Quantity    float64 `json:"quantity"`
		UnitIndex   int32   `json:"unit_index"`
		UnitName    string  `json:"unit_name"`
		Value       int64   `json:"value"`
		Date        string  `json:"date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	itemID, err := parseUUID(body.ItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "item_id tidak valid")
		return
	}
	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}
	if body.Quantity <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
		return
	}

	date := time.Now()
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		date = parsed
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	accountID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tidak ditemukan")
		return
	}

	// Derive unit name from item if not provided in request
	unitName := body.UnitName
	if unitName == "" {
		item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
		if err != nil {
			respondError(w, http.StatusBadRequest, "item tidak ditemukan")
			return
		}
		unitName = getUnitName(item.Units, body.UnitIndex)
	}

	if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, body.Quantity, body.UnitIndex, body.Value, date); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat lot inventori")
		return
	}

	if accountID.Valid {
		if err := service.UpdateBalance(ctx, qtx, accountID.Bytes, body.Value); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun")
			return
		}
	}

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         itemID,
		WarehouseID:    warehouseID,
		QuantityChange: body.Quantity,
		UnitName:       unitName,
		Type:           "purchase",
		Date:           date,
		Value:          body.Value,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Inventory",
		Description: "Tambah lot inventori",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"message": "lot inventori berhasil ditambahkan",
	})
}

func (h *InventoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Quantity float64 `json:"quantity"`
		Value    int64   `json:"value"`
		Date     string  `json:"date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	existing, err := qtx.GetInventoryByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}

	date := existing.Date.Time
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		date = parsed
	}

	var newQtyNumeric pgtype.Numeric
	_ = newQtyNumeric.Scan(body.Quantity)

	if err := qtx.UpdateInventoryLot(ctx, &db.UpdateInventoryLotParams{
		ID:       pgtype.UUID{Bytes: id, Valid: true},
		Quantity: newQtyNumeric,
		Value:    body.Value,
		Date:     pgtype.Date{Time: date, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui lot")
		return
	}

	accountID, err := qtx.GetWarehouseInventoryAccountID(ctx, existing.WarehouseID)
	if err == nil && accountID.Valid {
		delta := body.Value - existing.Value
		if delta != 0 {
			if err := service.UpdateBalance(ctx, qtx, accountID.Bytes, delta); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun")
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "lot inventori berhasil diperbarui"})
}

func (h *InventoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	existing, err := qtx.GetInventoryByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}

	if err := qtx.DeleteInventoryLot(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus lot inventori")
		return
	}

	accountID, err := qtx.GetWarehouseInventoryAccountID(ctx, existing.WarehouseID)
	if err == nil && accountID.Valid {
		if err := service.UpdateBalance(ctx, qtx, accountID.Bytes, -existing.Value); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun")
			return
		}
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "DELETE",
		EntityType:  "Inventory",
		Description: "Hapus lot inventori",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "lot inventori berhasil dihapus"})
}

// getUnitName parses item units JSON and returns the unit name at the given index.
func getUnitName(unitsJSON []byte, idx int32) string {
	var units []struct {
		Name  string `json:"name"`
		Ratio any    `json:"ratio"`
	}
	if err := json.Unmarshal(unitsJSON, &units); err != nil || int(idx) >= len(units) {
		return ""
	}
	return units[idx].Name
}
