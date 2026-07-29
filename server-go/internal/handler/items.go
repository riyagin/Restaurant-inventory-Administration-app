package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// logItem records an item mutation on the activity log. Item edits change how
// every downstream purchase and stock movement is priced and converted, so they
// need the same audit trail as the transactions themselves.
func (h *ItemsHandler) logItem(r *http.Request, action string, id [16]byte, description string) {
	ctx := r.Context()
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      action,
		EntityType:  "item",
		EntityID:    id,
		Description: description,
	})
}

// itemLabel renders an item as `"Nama" (KODE)`, or just `"Nama"` when it has no code.
func itemLabel(name, code string) string {
	if strings.TrimSpace(code) == "" {
		return fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("%q (%s)", name, code)
}

// itemChangeSummary describes what an update changed, as a trailing clause like
// ` — nama: "A" → "B"; satuan diubah`. Returns "" when nothing changed, so the
// log entry degrades to a plain "Mengubah item …" rather than a dangling dash.
func itemChangeSummary(before, after *db.Item) string {
	var changes []string
	if before.Name != after.Name {
		changes = append(changes, fmt.Sprintf("nama: %q → %q", before.Name, after.Name))
	}
	if before.Code != after.Code {
		changes = append(changes, fmt.Sprintf("kode: %q → %q", before.Code, after.Code))
	}
	if before.IsStock != after.IsStock {
		changes = append(changes, fmt.Sprintf("tipe: %s → %s", stockLabel(before.IsStock), stockLabel(after.IsStock)))
	}
	// Compare the raw JSONB: a ratio change reprices existing stock, so it is
	// worth flagging even though the units themselves are too verbose to inline.
	if !bytes.Equal(before.Units, after.Units) {
		changes = append(changes, "satuan diubah")
	}
	if len(changes) == 0 {
		return ""
	}
	return " — " + strings.Join(changes, "; ")
}

func stockLabel(isStock bool) string {
	if isStock {
		return "barang stok"
	}
	return "non-stok"
}

type ItemsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewItemsHandler(pool *pgxpool.Pool, queries *db.Queries) *ItemsHandler {
	return &ItemsHandler{pool: pool, queries: queries}
}

type itemResponse struct {
	ID      pgtype.UUID     `json:"id"`
	Name    string          `json:"name"`
	Code    string          `json:"code"`
	Units   json.RawMessage `json:"units"`
	IsStock bool            `json:"is_stock"`
}

func itemToResponse(i *db.Item) itemResponse {
	units := json.RawMessage(i.Units)
	if len(units) == 0 {
		units = json.RawMessage("[]")
	}
	return itemResponse{
		ID:      i.ID,
		Name:    i.Name,
		Code:    i.Code,
		Units:   units,
		IsStock: i.IsStock,
	}
}

func (h *ItemsHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.queries.ListItems(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data item")
		return
	}

	search      := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	isStockStr  := r.URL.Query().Get("is_stock")
	result := make([]itemResponse, 0, len(items))
	for _, item := range items {
		if isStockStr != "" {
			wantStock := isStockStr == "true"
			if item.IsStock != wantStock {
				continue
			}
		}
		if search != "" {
			if !strings.Contains(strings.ToLower(item.Name), search) &&
				!strings.Contains(strings.ToLower(item.Code), search) {
				continue
			}
		}
		result = append(result, itemToResponse(item))
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *ItemsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	item, err := h.queries.GetItemByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "item tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil item")
		return
	}
	respondJSON(w, http.StatusOK, itemToResponse(item))
}

func (h *ItemsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string          `json:"name"`
		Code    string          `json:"code"`
		Units   json.RawMessage `json:"units"`
		IsStock bool            `json:"is_stock"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama item wajib diisi")
		return
	}
	if len(body.Units) == 0 {
		body.Units = json.RawMessage("[]")
	}

	item, err := h.queries.CreateItem(r.Context(), &db.CreateItemParams{
		Name:    body.Name,
		Code:    body.Code,
		Units:   []byte(body.Units),
		IsStock: body.IsStock,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat item")
		return
	}

	h.logItem(r, "CREATE", item.ID.Bytes,
		fmt.Sprintf("Menambahkan item %s", itemLabel(item.Name, item.Code)))

	respondJSON(w, http.StatusCreated, itemToResponse(item))
}

func (h *ItemsHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name    string          `json:"name"`
		Code    string          `json:"code"`
		Units   json.RawMessage `json:"units"`
		IsStock bool            `json:"is_stock"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama item wajib diisi")
		return
	}
	if len(body.Units) == 0 {
		body.Units = json.RawMessage("[]")
	}

	// Read the row first so the log can name what actually changed. A unit-ratio
	// edit silently reprices existing stock, so "diubah" alone is not enough to
	// audit from.
	before, err := h.queries.GetItemByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "item tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil item")
		return
	}

	// A units edit and the inventory it restates have to land together: a lot
	// left at the old index is read as the wrong unit by every later deduction.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	rescaled := 0
	if !bytes.Equal(before.Units, []byte(body.Units)) {
		oldUnits, errOld := parseUnitDefs(before.Units)
		newUnits, errNew := parseUnitDefs([]byte(body.Units))
		if errOld != nil || errNew != nil {
			respondError(w, http.StatusBadRequest, "format satuan tidak valid")
			return
		}
		if len(newUnits) > 0 && len(oldUnits) > 0 {
			rescaled, err = rescaleInventoryForUnits(ctx, tx, id, oldUnits, newUnits)
			if err != nil {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("gagal menyesuaikan stok ke satuan baru: %v", err))
				return
			}
		}
	}

	item, err := qtx.UpdateItem(ctx, &db.UpdateItemParams{
		Name:    body.Name,
		Code:    body.Code,
		Units:   []byte(body.Units),
		IsStock: body.IsStock,
		ID:      pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui item")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	summary := itemChangeSummary(before, item)
	if rescaled > 0 {
		summary += fmt.Sprintf("; %d lot stok dikonversi ke satuan %s",
			rescaled, baseUnitName([]byte(body.Units)))
	}
	h.logItem(r, "UPDATE", item.ID.Bytes,
		fmt.Sprintf("Mengubah item %s%s", itemLabel(before.Name, before.Code), summary))

	respondJSON(w, http.StatusOK, itemToResponse(item))
}

func (h *ItemsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	// Read before deleting: once the row is gone its name and code cannot be
	// recovered for the log entry.
	existing, err := h.queries.GetItemByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "item tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil item")
		return
	}

	if err := h.queries.DeleteItem(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item")
		return
	}

	h.logItem(r, "DELETE", id,
		fmt.Sprintf("Menghapus item %s", itemLabel(existing.Name, existing.Code)))

	respondJSON(w, http.StatusOK, map[string]string{"message": "item berhasil dihapus"})
}

func (h *ItemsHandler) GetLastPrice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	row, err := h.queries.GetItemLastPrice(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "harga terakhir tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil harga terakhir")
		return
	}
	respondJSON(w, http.StatusOK, row)
}

// GetHistory returns every invoice line that ever bought this item. Used by the
// non-stock item detail page, where invoices are the only history that exists.
func (h *ItemsHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	history, err := h.queries.GetItemPurchaseHistory(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat pembelian")
		return
	}
	if history == nil {
		history = []*db.GetItemPurchaseHistoryRow{}
	}
	respondJSON(w, http.StatusOK, history)
}

// GetPriceHistory returns how the purchase price of an item has moved over
// time: rolled up per unit, per vendor, and per month. Prices are always kept
// split by unit — a price per dus and a price per pcs are not the same number.
func (h *ItemsHandler) GetPriceHistory(w http.ResponseWriter, r *http.Request) {
	rawID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	id := pgtype.UUID{Bytes: rawID, Valid: true}
	ctx := r.Context()

	item, err := h.queries.GetItemByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "item tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil item")
		return
	}

	byUnit, err := h.queries.GetItemPriceByUnit(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan harga")
		return
	}
	byVendor, err := h.queries.GetItemPriceByVendor(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil harga per vendor")
		return
	}
	trend, err := h.queries.GetItemPriceTrend(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil tren harga")
		return
	}
	purchases, err := h.queries.GetItemPurchaseHistory(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat pembelian")
		return
	}

	if byUnit == nil {
		byUnit = []*db.GetItemPriceByUnitRow{}
	}
	if byVendor == nil {
		byVendor = []*db.GetItemPriceByVendorRow{}
	}
	if trend == nil {
		trend = []*db.GetItemPriceTrendRow{}
	}
	if purchases == nil {
		purchases = []*db.GetItemPurchaseHistoryRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"item":      itemToResponse(item),
		"by_unit":   byUnit,
		"by_vendor": byVendor,
		"monthly":   trend,
		"purchases": purchases,
	})
}

// GetStockHistory returns the raw stock_history rows for an item, optionally
// bounded by ?from=/?to= dates.
func (h *ItemsHandler) GetStockHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	params := &db.GetItemStockHistoryParams{
		ItemID: pgtype.UUID{Bytes: id, Valid: true},
	}

	if from := r.URL.Query().Get("from"); from != "" {
		t, err := time.Parse("2006-01-02", from)
		if err == nil {
			params.Column2 = pgtype.Date{Time: t, Valid: true}
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		t, err := time.Parse("2006-01-02", to)
		if err == nil {
			params.Column3 = pgtype.Date{Time: t, Valid: true}
		}
	}

	history, err := h.queries.GetItemStockHistory(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat stok")
		return
	}
	if history == nil {
		history = []*db.GetItemStockHistoryRow{}
	}
	respondJSON(w, http.StatusOK, history)
}

// GetStockDetail bundles everything the stock-item history page needs in one
// round trip: the item itself, on-hand stock per warehouse, purchase invoice
// lines, dispatch usage per destination, and monthly / per-type flow rollups.
func (h *ItemsHandler) GetStockDetail(w http.ResponseWriter, r *http.Request) {
	rawID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	id := pgtype.UUID{Bytes: rawID, Valid: true}
	ctx := r.Context()

	item, err := h.queries.GetItemByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			respondError(w, http.StatusNotFound, "item tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil item")
		return
	}

	stock, err := h.queries.GetItemStockByWarehouse(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil stok per gudang")
		return
	}
	purchases, err := h.queries.GetItemPurchaseHistory(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat pembelian")
		return
	}
	usage, err := h.queries.GetItemUsageByDestination(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pemakaian")
		return
	}
	monthly, err := h.queries.GetItemMonthlyFlow(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan bulanan")
		return
	}
	byType, err := h.queries.GetItemFlowByType(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan per tipe")
		return
	}

	if stock == nil {
		stock = []*db.GetItemStockByWarehouseRow{}
	}
	if purchases == nil {
		purchases = []*db.GetItemPurchaseHistoryRow{}
	}
	if usage == nil {
		usage = []*db.GetItemUsageByDestinationRow{}
	}
	if monthly == nil {
		monthly = []*db.GetItemMonthlyFlowRow{}
	}
	if byType == nil {
		byType = []*db.GetItemFlowByTypeRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"item":                 itemToResponse(item),
		"stock_by_warehouse":   stock,
		"purchases":            purchases,
		"usage_by_destination": usage,
		"monthly_flow":         monthly,
		"flow_by_type":         byType,
	})
}
