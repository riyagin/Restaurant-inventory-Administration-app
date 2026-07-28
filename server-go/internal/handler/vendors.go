package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

type VendorsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewVendorsHandler(pool *pgxpool.Pool, queries *db.Queries) *VendorsHandler {
	return &VendorsHandler{pool: pool, queries: queries}
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

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	vendor, err := qtx.CreateVendor(ctx, body.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat vendor")
		return
	}

	// Every vendor carries its own payable sub-account under "Utang Usaha".
	acctID, err := service.CreateVendorPayableAccount(ctx, qtx, vendor.ID.Bytes, vendor.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun hutang vendor")
		return
	}
	vendor.AccountID = pgtype.UUID{Bytes: acctID, Valid: true}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan vendor")
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

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	vendor, err := qtx.UpdateVendor(ctx, &db.UpdateVendorParams{
		Name: body.Name,
		ID:   pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui vendor")
		return
	}

	// Keep the payable sub-account label in step with the vendor name.
	if vendor.AccountID.Valid {
		if err := service.RenameVendorPayableAccount(ctx, qtx, vendor.AccountID.Bytes, vendor.Name); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui akun hutang vendor")
			return
		}
	} else if acctID, err := service.CreateVendorPayableAccount(ctx, qtx, id, vendor.Name); err == nil {
		vendor.AccountID = pgtype.UUID{Bytes: acctID, Valid: true}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
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

	ctx := r.Context()
	vendorUUID := pgtype.UUID{Bytes: id, Valid: true}

	vendor, err := h.queries.GetVendorByID(ctx, vendorUUID)
	if err != nil {
		respondError(w, http.StatusNotFound, "vendor tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.DeleteVendor(ctx, vendorUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus vendor")
		return
	}

	// Drop the vendor's payable sub-account with it; any leftover balance moves
	// to the shared bucket so total liabilities stay unchanged.
	if vendor.AccountID.Valid {
		acct, err := qtx.GetAccountByID(ctx, vendor.AccountID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil akun hutang vendor")
			return
		}
		if acct.Balance != 0 {
			otherID := service.PayableOtherAccountID(ctx, qtx)
			if otherID == uuid.Nil {
				respondError(w, http.StatusConflict, "vendor masih memiliki saldo hutang")
				return
			}
			if err := service.UpdateBalance(ctx, qtx, otherID, acct.Balance); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memindahkan saldo hutang vendor")
				return
			}
		}
		if err := qtx.DeleteAccount(ctx, vendor.AccountID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghapus akun hutang vendor")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
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
