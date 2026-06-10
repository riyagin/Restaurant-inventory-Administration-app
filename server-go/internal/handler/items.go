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

func (h *ItemsHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
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
