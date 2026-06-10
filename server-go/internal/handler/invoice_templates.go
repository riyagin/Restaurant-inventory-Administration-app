package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

type InvoiceTemplatesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewInvoiceTemplatesHandler(pool *pgxpool.Pool, queries *db.Queries) *InvoiceTemplatesHandler {
	return &InvoiceTemplatesHandler{pool: pool, queries: queries}
}

type templateItemInput struct {
	ItemID      string `json:"item_id"`
	Description string `json:"description"`
	UnitIndex   int32  `json:"unit_index"`
	SortOrder   int32  `json:"sort_order"`
}

type templateItemResponse struct {
	ID          pgtype.UUID     `json:"id"`
	TemplateID  pgtype.UUID     `json:"template_id"`
	ItemID      pgtype.UUID     `json:"item_id"`
	Description pgtype.Text     `json:"description"`
	UnitIndex   int32           `json:"unit_index"`
	SortOrder   int32           `json:"sort_order"`
	ItemName    pgtype.Text     `json:"item_name"`
	ItemUnits   json.RawMessage `json:"item_units"`
}

func toTemplateItemResponse(ti *db.GetInvoiceTemplateItemsRow) templateItemResponse {
	units := json.RawMessage(ti.ItemUnits)
	if len(units) == 0 {
		units = json.RawMessage("[]")
	}
	return templateItemResponse{
		ID:          ti.ID,
		TemplateID:  ti.TemplateID,
		ItemID:      ti.ItemID,
		Description: ti.Description,
		UnitIndex:   ti.UnitIndex,
		SortOrder:   ti.SortOrder,
		ItemName:    ti.ItemName,
		ItemUnits:   units,
	}
}

func (h *InvoiceTemplatesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.pool.Query(ctx, `
		SELECT t.id, t.name, t.invoice_type, t.vendor_id, t.warehouse_id, t.created_at,
		       v.name AS vendor_name, w.name AS warehouse_name,
		       COALESCE(
		         json_agg(
		           json_build_object(
		             'id',          ti.id,
		             'item_id',     ti.item_id,
		             'item_name',   i.name,
		             'description', ti.description,
		             'unit_index',  ti.unit_index,
		             'sort_order',  ti.sort_order
		           ) ORDER BY ti.sort_order
		         ) FILTER (WHERE ti.id IS NOT NULL),
		         '[]'
		       ) AS items
		FROM invoice_templates t
		LEFT JOIN invoice_template_items ti ON ti.template_id = t.id
		LEFT JOIN items i ON i.id = ti.item_id
		LEFT JOIN vendors v ON v.id = t.vendor_id
		LEFT JOIN warehouses w ON w.id = t.warehouse_id
		GROUP BY t.id, v.name, w.name
		ORDER BY t.name
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data template")
		return
	}
	defer rows.Close()

	type templateRow struct {
		ID            pgtype.UUID        `json:"id"`
		Name          string             `json:"name"`
		InvoiceType   string             `json:"invoice_type"`
		VendorID      pgtype.UUID        `json:"vendor_id"`
		WarehouseID   pgtype.UUID        `json:"warehouse_id"`
		CreatedAt     pgtype.Timestamptz `json:"created_at"`
		VendorName    pgtype.Text        `json:"vendor_name"`
		WarehouseName pgtype.Text        `json:"warehouse_name"`
		Items         json.RawMessage    `json:"items"`
	}

	result := []templateRow{}
	for rows.Next() {
		var t templateRow
		if err := rows.Scan(&t.ID, &t.Name, &t.InvoiceType, &t.VendorID, &t.WarehouseID,
			&t.CreatedAt, &t.VendorName, &t.WarehouseName, &t.Items); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data template")
			return
		}
		if len(t.Items) == 0 {
			t.Items = json.RawMessage("[]")
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data template")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *InvoiceTemplatesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	tmpl, err := h.queries.GetInvoiceTemplateByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}

	rows, err := h.queries.GetInvoiceTemplateItems(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item template")
		return
	}

	items := make([]templateItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, toTemplateItemResponse(row))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id":             tmpl.ID,
		"name":           tmpl.Name,
		"invoice_type":   tmpl.InvoiceType,
		"vendor_id":      tmpl.VendorID,
		"warehouse_id":   tmpl.WarehouseID,
		"created_at":     tmpl.CreatedAt,
		"vendor_name":    tmpl.VendorName,
		"warehouse_name": tmpl.WarehouseName,
		"items":          items,
	})
}

func (h *InvoiceTemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string              `json:"name"`
		InvoiceType string              `json:"invoice_type"`
		VendorID    *string             `json:"vendor_id"`
		WarehouseID *string             `json:"warehouse_id"`
		Items       []templateItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	params := &db.CreateInvoiceTemplateParams{
		Name:        body.Name,
		InvoiceType: body.InvoiceType,
	}
	if body.VendorID != nil && *body.VendorID != "" {
		vid, err := parseUUID(*body.VendorID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "vendor_id tidak valid")
			return
		}
		params.VendorID = pgtype.UUID{Bytes: vid, Valid: true}
	}
	if body.WarehouseID != nil && *body.WarehouseID != "" {
		wid, err := parseUUID(*body.WarehouseID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
			return
		}
		params.WarehouseID = pgtype.UUID{Bytes: wid, Valid: true}
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	tmpl, err := qtx.CreateInvoiceTemplate(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat template")
		return
	}

	createdItems := make([]*db.InvoiceTemplateItem, 0, len(body.Items))
	for i, inp := range body.Items {
		itemUUID := pgtype.UUID{Valid: false}
		if inp.ItemID != "" {
			itemID, err := parseUUID(inp.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			itemUUID = pgtype.UUID{Bytes: itemID, Valid: true}
		}
		ti, err := qtx.CreateInvoiceTemplateItem(ctx, &db.CreateInvoiceTemplateItemParams{
			TemplateID:  tmpl.ID,
			ItemID:      itemUUID,
			Description: pgtype.Text{String: inp.Description, Valid: inp.Description != ""},
			UnitIndex:   inp.UnitIndex,
			SortOrder:   int32(i),
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat item template")
			return
		}
		createdItems = append(createdItems, ti)
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":           tmpl.ID,
		"name":         tmpl.Name,
		"invoice_type": tmpl.InvoiceType,
		"vendor_id":    tmpl.VendorID,
		"warehouse_id": tmpl.WarehouseID,
		"created_at":   tmpl.CreatedAt,
		"items":        createdItems,
	})
}

func (h *InvoiceTemplatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		Name        string              `json:"name"`
		InvoiceType string              `json:"invoice_type"`
		VendorID    *string             `json:"vendor_id"`
		WarehouseID *string             `json:"warehouse_id"`
		Items       []templateItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	params := &db.UpdateInvoiceTemplateParams{
		Name:        body.Name,
		InvoiceType: body.InvoiceType,
		ID:          pgID,
	}
	if body.VendorID != nil && *body.VendorID != "" {
		vid, err := parseUUID(*body.VendorID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "vendor_id tidak valid")
			return
		}
		params.VendorID = pgtype.UUID{Bytes: vid, Valid: true}
	}
	if body.WarehouseID != nil && *body.WarehouseID != "" {
		wid, err := parseUUID(*body.WarehouseID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
			return
		}
		params.WarehouseID = pgtype.UUID{Bytes: wid, Valid: true}
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	tmpl, err := qtx.UpdateInvoiceTemplate(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui template")
		return
	}

	if err := qtx.DeleteInvoiceTemplateItems(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item lama")
		return
	}

	updatedItems := make([]*db.InvoiceTemplateItem, 0, len(body.Items))
	for i, inp := range body.Items {
		itemUUID := pgtype.UUID{Valid: false}
		if inp.ItemID != "" {
			itemID, err := parseUUID(inp.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			itemUUID = pgtype.UUID{Bytes: itemID, Valid: true}
		}
		ti, err := qtx.CreateInvoiceTemplateItem(ctx, &db.CreateInvoiceTemplateItemParams{
			TemplateID:  pgID,
			ItemID:      itemUUID,
			Description: pgtype.Text{String: inp.Description, Valid: inp.Description != ""},
			UnitIndex:   inp.UnitIndex,
			SortOrder:   int32(i),
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat item template")
			return
		}
		updatedItems = append(updatedItems, ti)
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id":           tmpl.ID,
		"name":         tmpl.Name,
		"invoice_type": tmpl.InvoiceType,
		"vendor_id":    tmpl.VendorID,
		"warehouse_id": tmpl.WarehouseID,
		"created_at":   tmpl.CreatedAt,
		"items":        updatedItems,
	})
}

func (h *InvoiceTemplatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := h.queries.DeleteInvoiceTemplate(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus template")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "template berhasil dihapus"})
}
