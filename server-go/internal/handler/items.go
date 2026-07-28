package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type ItemsHandler struct {
	queries *db.Queries
}

func NewItemsHandler(queries *db.Queries) *ItemsHandler {
	return &ItemsHandler{queries: queries}
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
	respondJSON(w, http.StatusCreated, itemToResponse(item))
}

func (h *ItemsHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	item, err := h.queries.UpdateItem(r.Context(), &db.UpdateItemParams{
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
	respondJSON(w, http.StatusOK, itemToResponse(item))
}

func (h *ItemsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := h.queries.DeleteItem(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item")
		return
	}
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
