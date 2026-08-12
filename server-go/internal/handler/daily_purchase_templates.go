package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Templates for Pembelanjaan Harian.
//
// The same shopping run repeats — the Tuesday market list, one branch's weekly
// gas and ice. A template stores the skeleton: branch, division, receiving
// warehouse, vendor, and the lines in the units they are normally bought in.
//
// It stores no quantities and no prices, matching dispatch_templates. A template
// that remembered "12 kg at 18.000" would invite someone to accept last month's
// price unread, and a stale price here lands straight in inventory value and the
// branch's expenses. The repetitive typing is saved; the two fields that must be
// checked every single time are left blank on purpose.

type templateLineInput struct {
	ItemID      string `json:"item_id"`
	Description string `json:"description"`
	UnitIndex   int32  `json:"unit_index"`
}

type dailyPurchaseTemplateBody struct {
	Name              string              `json:"name"`
	BranchID          string              `json:"branch_id"`
	DivisionID        string              `json:"division_id"`
	WarehouseID       string              `json:"warehouse_id"`
	VendorID          string              `json:"vendor_id"`
	ExpenseCategoryID string              `json:"expense_category_id"`
	Notes             string              `json:"notes"`
	Items             []templateLineInput `json:"items"`
}

// ListTemplates — GET /api/daily-purchase-templates
func (h *DailyPurchasesHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	templates, err := h.queries.ListDailyPurchaseTemplates(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil template pembelanjaan")
		return
	}

	// Lines travel with the list. A template is a handful of rows and the page
	// shows them inline, so fetching them per template on click would be a
	// round trip for data we could have sent once.
	out := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		lines, err := h.queries.GetDailyPurchaseTemplateItems(ctx, t.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil baris template")
			return
		}
		if lines == nil {
			lines = []*db.GetDailyPurchaseTemplateItemsRow{}
		}
		out = append(out, map[string]any{"template": t, "items": lines})
	}
	respondJSON(w, http.StatusOK, out)
}

// CreateTemplate — POST /api/daily-purchase-templates
func (h *DailyPurchasesHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var body dailyPurchaseTemplateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	ids, err := parseTemplateRefs(&body)
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

	template, err := qtx.CreateDailyPurchaseTemplate(ctx, &db.CreateDailyPurchaseTemplateParams{
		Name:              body.Name,
		BranchID:          ids.branch,
		DivisionID:        ids.division,
		WarehouseID:       ids.warehouse,
		VendorID:          ids.vendor,
		ExpenseCategoryID: ids.category,
		Notes:             strings.TrimSpace(body.Notes),
		CreatedBy:         pgUserID(ctx),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama template sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template")
		return
	}

	if err := h.writeTemplateItems(r, qtx, template.ID, body.Items); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "daily_purchase_template",
		EntityID:    template.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan template pembelanjaan %q", template.Name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template")
		return
	}
	respondJSON(w, http.StatusCreated, template)
}

// UpdateTemplate — PUT /api/daily-purchase-templates/{id}
func (h *DailyPurchasesHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body dailyPurchaseTemplateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama template wajib diisi")
		return
	}

	ids, err := parseTemplateRefs(&body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	template, err := qtx.UpdateDailyPurchaseTemplate(ctx, &db.UpdateDailyPurchaseTemplateParams{
		Name:              body.Name,
		BranchID:          ids.branch,
		DivisionID:        ids.division,
		WarehouseID:       ids.warehouse,
		VendorID:          ids.vendor,
		ExpenseCategoryID: ids.category,
		Notes:             strings.TrimSpace(body.Notes),
		ID:                pgID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama template sudah digunakan")
			return
		}
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}

	// Replaced wholesale rather than diffed: a template is a handful of rows,
	// and reconciling adds/moves/removes buys nothing but a chance to get the
	// ordering wrong.
	if err := qtx.DeleteDailyPurchaseTemplateItems(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui baris template")
		return
	}
	if err := h.writeTemplateItems(r, qtx, pgID, body.Items); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "daily_purchase_template",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah template pembelanjaan %q", template.Name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template")
		return
	}
	respondJSON(w, http.StatusOK, template)
}

// DeleteTemplate — DELETE /api/daily-purchase-templates/{id}
func (h *DailyPurchasesHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	// Deleting a template destroys no history: nothing references it once a
	// purchase has been recorded from it, by design — the purchase copies the
	// lines rather than pointing at them.
	if err := h.queries.DeleteDailyPurchaseTemplate(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus template")
		return
	}

	logMutation(r, h.queries, "DELETE", "daily_purchase_template", id, "Menghapus template pembelanjaan")
	respondJSON(w, http.StatusOK, map[string]string{"message": "template berhasil dihapus"})
}

type templateRefs struct {
	branch, division, warehouse, vendor, category pgtype.UUID
}

func parseTemplateRefs(body *dailyPurchaseTemplateBody) (templateRefs, error) {
	var refs templateRefs
	parse := func(raw, label string) (pgtype.UUID, error) {
		if strings.TrimSpace(raw) == "" {
			return pgtype.UUID{}, nil
		}
		id, err := parseUUID(raw)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("%s tidak valid", label)
		}
		return pgtype.UUID{Bytes: id, Valid: true}, nil
	}

	var err error
	if refs.branch, err = parse(body.BranchID, "cabang"); err != nil {
		return refs, err
	}
	if refs.division, err = parse(body.DivisionID, "divisi"); err != nil {
		return refs, err
	}
	if refs.warehouse, err = parse(body.WarehouseID, "gudang"); err != nil {
		return refs, err
	}
	if refs.vendor, err = parse(body.VendorID, "vendor"); err != nil {
		return refs, err
	}
	if refs.category, err = parse(body.ExpenseCategoryID, "kategori beban"); err != nil {
		return refs, err
	}
	return refs, nil
}

// writeTemplateItems stores the lines in the order they were sent. Sort order is
// taken from position rather than from a client-supplied field so the list can
// never come back scrambled or with duplicate positions.
func (h *DailyPurchasesHandler) writeTemplateItems(r *http.Request, qtx *db.Queries, templateID pgtype.UUID, lines []templateLineInput) error {
	for i, line := range lines {
		itemID := uuid.Nil
		if strings.TrimSpace(line.ItemID) != "" {
			parsed, err := parseUUID(line.ItemID)
			if err != nil {
				return fmt.Errorf("item pada baris %d tidak valid", i+1)
			}
			itemID = parsed
		}
		description := strings.TrimSpace(line.Description)
		if itemID == uuid.Nil && description == "" {
			return fmt.Errorf("baris %d harus punya barang atau keterangan", i+1)
		}

		if err := qtx.CreateDailyPurchaseTemplateItem(r.Context(), &db.CreateDailyPurchaseTemplateItemParams{
			TemplateID:  templateID,
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: itemID != uuid.Nil},
			Description: description,
			UnitIndex:   line.UnitIndex,
			SortOrder:   int32(i),
		}); err != nil {
			return fmt.Errorf("gagal menyimpan baris %d", i+1)
		}
	}
	return nil
}
