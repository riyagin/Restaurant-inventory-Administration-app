package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type SalesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewSalesHandler(pool *pgxpool.Pool, queries *db.Queries) *SalesHandler {
	return &SalesHandler{pool: pool, queries: queries}
}

// List — GET /api/sales
func (h *SalesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var fromDate, toDate pgtype.Date
	var branchID pgtype.UUID

	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("branch_id"); s != "" {
		id, err := parseUUID(s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		branchID = pgtype.UUID{Bytes: id, Valid: true}
	}

	rows, err := h.queries.ListSales(ctx, &db.ListSalesParams{
		Column1: fromDate,
		Column2: toDate,
		Column3: branchID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data penjualan")
		return
	}
	if rows == nil {
		rows = []*db.ListSalesRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Create — POST /api/sales
func (h *SalesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID   string `json:"account_id"`
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
		Date        string `json:"date"`
		BranchID    string `json:"branch_id"`
		DivisionID  string `json:"division_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.AccountID == "" || body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "account_id dan jumlah positif diperlukan")
		return
	}

	accountID, err := parseUUID(body.AccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "account_id tidak valid")
		return
	}

	branchID := uuid.Nil
	if body.BranchID != "" {
		branchID, err = parseUUID(body.BranchID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
	}

	divisionID := uuid.Nil
	if body.DivisionID != "" {
		divisionID, err = parseUUID(body.DivisionID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "division_id tidak valid")
			return
		}
	}

	saleDate := time.Now()
	if body.Date != "" {
		saleDate, err = time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid")
			return
		}
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	sale, err := qtx.InsertSale(ctx, &db.InsertSaleParams{
		AccountID:   pgtype.UUID{Bytes: accountID, Valid: true},
		Amount:      body.Amount,
		Description: pgtype.Text{String: body.Description, Valid: body.Description != ""},
		Date:        pgtype.Date{Time: saleDate, Valid: true},
		BranchID:    pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		DivisionID:  pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		CreatedBy:   pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan penjualan")
		return
	}

	// Dr cash account (asset increases)
	if err := service.UpdateBalance(ctx, qtx, accountID, body.Amount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun kas")
		return
	}

	// Cr revenue account (revenue increases)
	revAcctID, _ := saleRevenueAccountID(ctx, qtx, divisionID, branchID)
	if revAcctID != uuid.Nil {
		if err := service.UpdateBalance(ctx, qtx, revAcctID, body.Amount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun pendapatan")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan penjualan")
		return
	}

	branchName := body.BranchID
	if branchID != uuid.Nil {
		if br, err := h.queries.GetBranchByID(ctx, pgtype.UUID{Bytes: branchID, Valid: true}); err == nil {
			branchName = br.Name
		}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Sale",
		EntityID:    sale.ID.Bytes,
		Description: fmt.Sprintf("Catat penjualan %d di %s", body.Amount, branchName),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":         sale.ID,
		"created_at": sale.CreatedAt,
		"account_id": pgtype.UUID{Bytes: accountID, Valid: true},
		"amount":     body.Amount,
		"description": body.Description,
		"date":       body.Date,
		"branch_id":  pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		"division_id": pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
	})
}

// Delete — DELETE /api/sales/:id (admin)
func (h *SalesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	sale, err := h.queries.GetSaleByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "penjualan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data penjualan")
		return
	}

	branchID := uuid.Nil
	if sale.BranchID.Valid {
		branchID = sale.BranchID.Bytes
	}
	divisionID := uuid.Nil
	if sale.DivisionID.Valid {
		divisionID = sale.DivisionID.Bytes
	}
	accountID := sale.AccountID.Bytes

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	// Reverse cash account
	if err := service.UpdateBalance(ctx, qtx, accountID, -sale.Amount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun kas")
		return
	}

	// Reverse revenue account
	revAcctID, _ := saleRevenueAccountID(ctx, qtx, divisionID, branchID)
	if revAcctID != uuid.Nil {
		if err := service.UpdateBalance(ctx, qtx, revAcctID, -sale.Amount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun pendapatan")
			return
		}
	}

	if err := qtx.DeleteSale(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus penjualan")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus penjualan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "DELETE",
		EntityType:  "Sale",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus penjualan %d", sale.Amount),
	})

	w.WriteHeader(http.StatusNoContent)
}

// saleRevenueAccountID returns the revenue account ID for a division (priority) or branch.
func saleRevenueAccountID(ctx context.Context, qtx *db.Queries, divisionID, branchID uuid.UUID) (uuid.UUID, error) {
	if divisionID != uuid.Nil {
		aid, err := qtx.GetDivisionRevenueAccountID(ctx, pgtype.UUID{Bytes: divisionID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	if branchID != uuid.Nil {
		aid, err := qtx.GetBranchRevenueAccountID(ctx, pgtype.UUID{Bytes: branchID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	return uuid.Nil, nil
}
