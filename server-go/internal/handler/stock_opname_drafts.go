package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/middleware"
)

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
