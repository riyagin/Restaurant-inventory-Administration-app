package handler

import (
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

type WarehousesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewWarehousesHandler(pool *pgxpool.Pool, queries *db.Queries) *WarehousesHandler {
	return &WarehousesHandler{pool: pool, queries: queries}
}

func (h *WarehousesHandler) List(w http.ResponseWriter, r *http.Request) {
	warehouses, err := h.queries.ListWarehouses(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data gudang")
		return
	}
	if warehouses == nil {
		warehouses = []*db.ListWarehousesRow{}
	}
	respondJSON(w, http.StatusOK, warehouses)
}

func (h *WarehousesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama gudang wajib diisi")
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

	nextNum, err := qtx.GetNextInventoryAccountNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mendapatkan nomor akun")
		return
	}

	accountName := "Persediaan - " + body.Name
	acctID, err := qtx.CreateAccountForWarehouse(ctx, &db.CreateAccountForWarehouseParams{
		Name:          accountName,
		AccountNumber: pgtype.Int4{Int32: nextNum, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun persediaan")
		return
	}

	warehouse, err := qtx.CreateWarehouse(ctx, &db.CreateWarehouseParams{
		Name:               body.Name,
		InventoryAccountID: acctID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat gudang")
		return
	}

	// Logged inside the transaction so the entry cannot outlive a rolled-back
	// warehouse (creating one also creates its inventory account).
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "warehouse",
		EntityID:    warehouse.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan gudang %q dengan akun %q", warehouse.Name, accountName),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":                   warehouse.ID,
		"name":                 warehouse.Name,
		"inventory_account_id": warehouse.InventoryAccountID,
		"account_name":         accountName,
	})
}

func (h *WarehousesHandler) Update(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusBadRequest, "nama gudang wajib diisi")
		return
	}

	before, err := h.queries.GetWarehouseByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "gudang tidak ditemukan")
		return
	}

	warehouse, err := h.queries.UpdateWarehouse(r.Context(), &db.UpdateWarehouseParams{
		Name: body.Name,
		ID:   pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui gudang")
		return
	}

	logMutation(r, h.queries, "UPDATE", "warehouse", id,
		fmt.Sprintf("Mengubah nama gudang %q → %q", before.Name, warehouse.Name))

	respondJSON(w, http.StatusOK, warehouse)
}

func (h *WarehousesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	existing, err := h.queries.GetWarehouseByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "gudang tidak ditemukan")
		return
	}

	if err := h.queries.DeleteWarehouse(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusConflict, "gagal menghapus gudang: "+err.Error())
		return
	}

	logMutation(r, h.queries, "DELETE", "warehouse", id,
		fmt.Sprintf("Menghapus gudang %q", existing.Name))

	respondJSON(w, http.StatusOK, map[string]string{"message": "gudang berhasil dihapus"})
}
