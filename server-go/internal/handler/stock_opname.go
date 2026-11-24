package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
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
	ID               pgtype.UUID        `json:"id"`
	OpnameID         pgtype.UUID        `json:"opname_id"`
	ItemID           pgtype.UUID        `json:"item_id"`
	UnitIndex        int32              `json:"unit_index"`
	UnitName         string             `json:"unit_name"`
	RecordedQuantity pgtype.Numeric     `json:"recorded_quantity"`
	ActualQuantity   pgtype.Numeric     `json:"actual_quantity"`
	Difference       pgtype.Numeric     `json:"difference"`
	WasteValue       int64              `json:"waste_value"`
	IsCorrection     bool               `json:"is_correction"`
	CreatedAt        pgtype.Timestamptz `json:"created_at"`
	ItemName         string             `json:"item_name"`
	ItemCode         pgtype.Text        `json:"item_code"`
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

// baseUnitFactor now lives in item_units.go, alongside the unit-rescale logic
// that shares its interpretation of the units array.
