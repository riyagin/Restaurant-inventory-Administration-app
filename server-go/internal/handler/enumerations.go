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
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type EnumerationsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewEnumerationsHandler(pool *pgxpool.Pool, queries *db.Queries) *EnumerationsHandler {
	return &EnumerationsHandler{pool: pool, queries: queries}
}

// List — GET /api/enumerations
func (h *EnumerationsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	params := &db.ListEnumerationsParams{}

	if s := r.URL.Query().Get("warehouse_id"); s != "" {
		id, err := parseUUID(s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
			return
		}
		params.Column1 = pgtype.UUID{Bytes: id, Valid: true}
	}
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		params.Column2 = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		params.Column3 = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListEnumerations(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data enumerasi")
		return
	}
	if rows == nil {
		rows = []*db.ListEnumerationsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Create — POST /api/enumerations
func (h *EnumerationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarehouseID  string  `json:"warehouse_id"`
		SourceItemID string  `json:"source_item_id"`
		SourceQty    float64 `json:"source_qty"`
		SourceUnitIdx int32  `json:"source_unit_idx"`
		OutputItemID string  `json:"output_item_id"`
		OutputQty    float64 `json:"output_qty"`
		OutputUnitIdx int32  `json:"output_unit_idx"`
		Date         string  `json:"date"`
		Notes        string  `json:"notes"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.WarehouseID == "" || body.SourceItemID == "" || body.OutputItemID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id, source_item_id, dan output_item_id diperlukan")
		return
	}
	if body.SourceQty <= 0 || body.OutputQty <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah sumber dan hasil harus lebih dari 0")
		return
	}

	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}
	sourceItemID, err := parseUUID(body.SourceItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "source_item_id tidak valid")
		return
	}
	outputItemID, err := parseUUID(body.OutputItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "output_item_id tidak valid")
		return
	}

	enumDate := time.Now()
	if body.Date != "" {
		enumDate, err = time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid")
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

	// 1. Deduct source item
	transferredValue, err := service.FIFODeduct(ctx, qtx, sourceItemID, warehouseID, body.SourceQty)
	if err != nil {
		if strings.Contains(err.Error(), "stok tidak mencukupi") {
			respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengurangi stok sumber")
		return
	}

	// 2. Add output item
	if err := service.FIFOAdd(ctx, qtx, outputItemID, warehouseID, body.OutputQty, body.OutputUnitIdx, transferredValue, enumDate); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menambah stok hasil")
		return
	}

	// 3. Stock history for source (deduction)
	sourceItem, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: sourceItemID, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data item sumber")
		return
	}
	outputItem, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: outputItemID, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data item hasil")
		return
	}

	sourceUnitName := getUnitName(sourceItem.Units, body.SourceUnitIdx)
	outputUnitName := getUnitName(outputItem.Units, body.OutputUnitIdx)

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         sourceItemID,
		WarehouseID:    warehouseID,
		QuantityChange: -body.SourceQty,
		UnitName:       sourceUnitName,
		Type:           "enumeration",
		Date:           enumDate,
		Value:          -transferredValue,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok sumber")
		return
	}

	// 4. Stock history for output (addition)
	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         outputItemID,
		WarehouseID:    warehouseID,
		QuantityChange: body.OutputQty,
		UnitName:       outputUnitName,
		Type:           "enumeration",
		Date:           enumDate,
		Value:          transferredValue,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok hasil")
		return
	}

	// 5. Insert enumeration record
	var srcQtyNumeric, outQtyNumeric pgtype.Numeric
	_ = srcQtyNumeric.Scan(body.SourceQty)
	_ = outQtyNumeric.Scan(body.OutputQty)

	result, err := qtx.InsertEnumeration(ctx, &db.InsertEnumerationParams{
		WarehouseID:      pgtype.UUID{Bytes: warehouseID, Valid: true},
		SourceItemID:     pgtype.UUID{Bytes: sourceItemID, Valid: true},
		OutputItemID:     pgtype.UUID{Bytes: outputItemID, Valid: true},
		SourceQty:        srcQtyNumeric,
		SourceUnitIdx:    body.SourceUnitIdx,
		OutputQty:        outQtyNumeric,
		OutputUnitIdx:    body.OutputUnitIdx,
		TransferredValue: transferredValue,
		Date:             pgtype.Date{Time: enumDate, Valid: true},
		Notes:            pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		CreatedBy:        pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan enumerasi")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan transaksi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Enumeration",
		EntityID:    result.ID.Bytes,
		Description: fmt.Sprintf("Enumerasi %s → %s", sourceItem.Name, outputItem.Name),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":                result.ID,
		"transferred_value": transferredValue,
		"created_at":        result.CreatedAt,
	})
}

// Delete — DELETE /api/enumerations/:id (admin only)
func (h *EnumerationsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	enum, err := h.queries.GetEnumerationByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "enumerasi tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data enumerasi")
		return
	}

	warehouseID := enum.WarehouseID.Bytes
	sourceItemID := enum.SourceItemID.Bytes
	outputItemID := enum.OutputItemID.Bytes
	sourceQty := numericToFloat64(enum.SourceQty)
	outputQty := numericToFloat64(enum.OutputQty)
	transferredValue := enum.TransferredValue

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	today := time.Now()

	// 1. Reverse the output (deduct output item)
	if _, err := service.FIFODeduct(ctx, qtx, outputItemID, warehouseID, outputQty); err != nil {
		if strings.Contains(err.Error(), "stok tidak mencukupi") {
			respondError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("tidak dapat membalik: stok hasil tidak mencukupi (%s)", err.Error()))
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membalik stok hasil")
		return
	}

	// 2. Restore the source (add source item back)
	if err := service.FIFOAdd(ctx, qtx, sourceItemID, warehouseID, sourceQty, enum.SourceUnitIdx, transferredValue, today); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulihkan stok sumber")
		return
	}

	// 3. Reversal stock history rows
	outputUnitName := getUnitName(enum.OutputItemUnits, enum.OutputUnitIdx)
	sourceUnitName := getUnitName(enum.SourceItemUnits, enum.SourceUnitIdx)

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         outputItemID,
		WarehouseID:    warehouseID,
		QuantityChange: -outputQty,
		UnitName:       outputUnitName,
		Type:           "enumeration_reversal",
		Date:           today,
		Value:          -transferredValue,
		SourceID:       id,
		SourceType:     "enumeration",
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat pembalikan stok hasil")
		return
	}

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         sourceItemID,
		WarehouseID:    warehouseID,
		QuantityChange: sourceQty,
		UnitName:       sourceUnitName,
		Type:           "enumeration_reversal",
		Date:           today,
		Value:          transferredValue,
		SourceID:       id,
		SourceType:     "enumeration",
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat pembalikan stok sumber")
		return
	}

	// 4. Delete enumeration record
	if err := qtx.DeleteEnumeration(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus enumerasi")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembalikan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "DELETE",
		EntityType:  "Enumeration",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus enumerasi %s → %s", enum.SourceItemName, enum.OutputItemName),
	})

	w.WriteHeader(http.StatusNoContent)
}
