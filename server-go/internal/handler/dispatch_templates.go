package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type DispatchTemplatesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewDispatchTemplatesHandler(pool *pgxpool.Pool, queries *db.Queries) *DispatchTemplatesHandler {
	return &DispatchTemplatesHandler{pool: pool, queries: queries}
}

// dispatchTemplateItemInput is a row on a saved template. Quantity is absent by
// design — it is entered fresh on every dispatch.
type dispatchTemplateItemInput struct {
	ItemID    string `json:"item_id"`
	UnitIndex int32  `json:"unit_index"`
	SortOrder int32  `json:"sort_order"`
}

type dispatchTemplateItemResponse struct {
	ID         pgtype.UUID     `json:"id"`
	TemplateID pgtype.UUID     `json:"template_id"`
	ItemID     pgtype.UUID     `json:"item_id"`
	UnitIndex  int32           `json:"unit_index"`
	SortOrder  int32           `json:"sort_order"`
	ItemName   pgtype.Text     `json:"item_name"`
	ItemUnits  json.RawMessage `json:"item_units"`
}

func toDispatchTemplateItemResponse(ti *db.GetDispatchTemplateItemsRow) dispatchTemplateItemResponse {
	units := json.RawMessage(ti.ItemUnits)
	if len(units) == 0 {
		units = json.RawMessage("[]")
	}
	return dispatchTemplateItemResponse{
		ID:         ti.ID,
		TemplateID: ti.TemplateID,
		ItemID:     ti.ItemID,
		UnitIndex:  ti.UnitIndex,
		SortOrder:  ti.SortOrder,
		ItemName:   ti.ItemName,
		ItemUnits:  units,
	}
}

// dispatchTemplateBody is the shared create/update payload.
type dispatchTemplateBody struct {
	Name        string                      `json:"name"`
	WarehouseID *string                     `json:"warehouse_id"`
	BranchID    *string                     `json:"branch_id"`
	DivisionID  *string                     `json:"division_id"`
	Notes       string                      `json:"notes"`
	Items       []dispatchTemplateItemInput `json:"items"`
}

// optionalUUID converts an optional id string into a pgtype.UUID, leaving it
// NULL when absent or empty.
func optionalUUID(s *string, field string) (pgtype.UUID, error) {
	if s == nil || *s == "" {
		return pgtype.UUID{Valid: false}, nil
	}
	id, err := parseUUID(*s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%s tidak valid", field)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

// List — GET /api/dispatch-templates
func (h *DispatchTemplatesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.pool.Query(ctx, `
		SELECT t.id, t.name, t.warehouse_id, t.branch_id, t.division_id, t.notes, t.created_at,
		       wh.name AS warehouse_name, b.name AS branch_name, d.name AS division_name,
		       COALESCE(
		         json_agg(
		           json_build_object(
		             'id',         ti.id,
		             'item_id',    ti.item_id,
		             'item_name',  i.name,
		             'unit_index', ti.unit_index,
		             'sort_order', ti.sort_order
		           ) ORDER BY ti.sort_order
		         ) FILTER (WHERE ti.id IS NOT NULL),
		         '[]'
		       ) AS items
		FROM dispatch_templates t
		LEFT JOIN dispatch_template_items ti ON ti.template_id = t.id
		LEFT JOIN items i ON i.id = ti.item_id
		LEFT JOIN warehouses wh ON wh.id = t.warehouse_id
		LEFT JOIN branches b ON b.id = t.branch_id
		LEFT JOIN divisions d ON d.id = t.division_id
		GROUP BY t.id, wh.name, b.name, d.name
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
		WarehouseID   pgtype.UUID        `json:"warehouse_id"`
		BranchID      pgtype.UUID        `json:"branch_id"`
		DivisionID    pgtype.UUID        `json:"division_id"`
		Notes         pgtype.Text        `json:"notes"`
		CreatedAt     pgtype.Timestamptz `json:"created_at"`
		WarehouseName pgtype.Text        `json:"warehouse_name"`
		BranchName    pgtype.Text        `json:"branch_name"`
		DivisionName  pgtype.Text        `json:"division_name"`
		Items         json.RawMessage    `json:"items"`
	}

	result := []templateRow{}
	for rows.Next() {
		var t templateRow
		if err := rows.Scan(&t.ID, &t.Name, &t.WarehouseID, &t.BranchID, &t.DivisionID,
			&t.Notes, &t.CreatedAt, &t.WarehouseName, &t.BranchName, &t.DivisionName, &t.Items); err != nil {
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

// Get — GET /api/dispatch-templates/:id
func (h *DispatchTemplatesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	tmpl, err := h.queries.GetDispatchTemplateByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}

	rows, err := h.queries.GetDispatchTemplateItems(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item template")
		return
	}

	items := make([]dispatchTemplateItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDispatchTemplateItemResponse(row))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id":             tmpl.ID,
		"name":           tmpl.Name,
		"warehouse_id":   tmpl.WarehouseID,
		"branch_id":      tmpl.BranchID,
		"division_id":    tmpl.DivisionID,
		"notes":          tmpl.Notes,
		"created_at":     tmpl.CreatedAt,
		"warehouse_name": tmpl.WarehouseName,
		"branch_name":    tmpl.BranchName,
		"division_name":  tmpl.DivisionName,
		"items":          items,
	})
}

// Create — POST /api/dispatch-templates
func (h *DispatchTemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body dispatchTemplateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	warehouseID, err := optionalUUID(body.WarehouseID, "warehouse_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	branchID, err := optionalUUID(body.BranchID, "branch_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	divisionID, err := optionalUUID(body.DivisionID, "division_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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

	tmpl, err := qtx.CreateDispatchTemplate(ctx, &db.CreateDispatchTemplateParams{
		Name:        body.Name,
		WarehouseID: warehouseID,
		BranchID:    branchID,
		DivisionID:  divisionID,
		Notes:       pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat template")
		return
	}

	createdItems, herr := h.replaceItems(ctx, qtx, tmpl.ID, body.Items)
	if herr != nil {
		respondError(w, herr.status, herr.message)
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "dispatch_template",
		EntityID:    tmpl.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan template pengiriman %q (%d item)", tmpl.Name, len(createdItems)),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":           tmpl.ID,
		"name":         tmpl.Name,
		"warehouse_id": tmpl.WarehouseID,
		"branch_id":    tmpl.BranchID,
		"division_id":  tmpl.DivisionID,
		"notes":        tmpl.Notes,
		"created_at":   tmpl.CreatedAt,
		"items":        createdItems,
	})
}

// Update — PUT /api/dispatch-templates/:id
//
// The whole item list is replaced: a template carries no history, so rewriting
// it is simpler than diffing and cannot drift from what the user sees.
func (h *DispatchTemplatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body dispatchTemplateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	warehouseID, err := optionalUUID(body.WarehouseID, "warehouse_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	branchID, err := optionalUUID(body.BranchID, "branch_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	divisionID, err := optionalUUID(body.DivisionID, "division_id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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

	tmpl, err := qtx.UpdateDispatchTemplate(ctx, &db.UpdateDispatchTemplateParams{
		Name:        body.Name,
		WarehouseID: warehouseID,
		BranchID:    branchID,
		DivisionID:  divisionID,
		Notes:       pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		ID:          pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui template")
		return
	}

	if err := qtx.DeleteDispatchTemplateItems(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item lama")
		return
	}

	updatedItems, herr := h.replaceItems(ctx, qtx, pgID, body.Items)
	if herr != nil {
		respondError(w, herr.status, herr.message)
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "dispatch_template",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah template pengiriman %q (%d item)", tmpl.Name, len(updatedItems)),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id":           tmpl.ID,
		"name":         tmpl.Name,
		"warehouse_id": tmpl.WarehouseID,
		"branch_id":    tmpl.BranchID,
		"division_id":  tmpl.DivisionID,
		"notes":        tmpl.Notes,
		"created_at":   tmpl.CreatedAt,
		"items":        updatedItems,
	})
}

// Delete — DELETE /api/dispatch-templates/:id
func (h *DispatchTemplatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	existing, err := h.queries.GetDispatchTemplateByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}

	if err := h.queries.DeleteDispatchTemplate(r.Context(), pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus template")
		return
	}

	logMutation(r, h.queries, "DELETE", "dispatch_template", id,
		fmt.Sprintf("Menghapus template pengiriman %q", existing.Name))

	respondJSON(w, http.StatusOK, map[string]string{"message": "template berhasil dihapus"})
}

// handlerError carries the status/message pair back to the calling handler so
// the shared item-writing step can fail with the right response.
type handlerError struct {
	status  int
	message string
}

func (e *handlerError) Error() string { return e.message }

// replaceItems writes the template's item rows in payload order. Rows without an
// item are skipped: an empty row is how the UI represents "not filled in yet".
func (h *DispatchTemplatesHandler) replaceItems(ctx context.Context, qtx *db.Queries, templateID pgtype.UUID, inputs []dispatchTemplateItemInput) ([]*db.DispatchTemplateItem, *handlerError) {
	items := make([]*db.DispatchTemplateItem, 0, len(inputs))
	for _, inp := range inputs {
		if strings.TrimSpace(inp.ItemID) == "" {
			continue
		}
		itemID, err := parseUUID(inp.ItemID)
		if err != nil {
			return nil, &handlerError{http.StatusBadRequest, "item_id tidak valid"}
		}
		ti, err := qtx.CreateDispatchTemplateItem(ctx, &db.CreateDispatchTemplateItemParams{
			TemplateID: templateID,
			ItemID:     pgtype.UUID{Bytes: itemID, Valid: true},
			UnitIndex:  inp.UnitIndex,
			SortOrder:  int32(len(items)),
		})
		if err != nil {
			return nil, &handlerError{http.StatusInternalServerError, "gagal membuat item template"}
		}
		items = append(items, ti)
	}
	return items, nil
}
