package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type VendorsHandler struct {
	queries *db.Queries
}

func NewVendorsHandler(queries *db.Queries) *VendorsHandler {
	return &VendorsHandler{queries: queries}
}

func (h *VendorsHandler) List(w http.ResponseWriter, r *http.Request) {
	vendors, err := h.queries.ListVendors(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data vendor")
		return
	}
	if vendors == nil {
		vendors = []*db.Vendor{}
	}
	respondJSON(w, http.StatusOK, vendors)
}

func (h *VendorsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama vendor wajib diisi")
		return
	}

	vendor, err := h.queries.CreateVendor(r.Context(), body.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat vendor")
		return
	}
	respondJSON(w, http.StatusCreated, vendor)
}

func (h *VendorsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama vendor wajib diisi")
		return
	}

	vendor, err := h.queries.UpdateVendor(r.Context(), &db.UpdateVendorParams{
		Name: body.Name,
		ID:   pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui vendor")
		return
	}
	respondJSON(w, http.StatusOK, vendor)
}

func (h *VendorsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := h.queries.DeleteVendor(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus vendor")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "vendor berhasil dihapus"})
}

func (h *VendorsHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	history, err := h.queries.GetVendorHistory(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat vendor")
		return
	}
	if history == nil {
		history = []*db.GetVendorHistoryRow{}
	}
	respondJSON(w, http.StatusOK, history)
}
