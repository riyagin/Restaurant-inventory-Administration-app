package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type AccountAdjustmentsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewAccountAdjustmentsHandler(pool *pgxpool.Pool, queries *db.Queries) *AccountAdjustmentsHandler {
	return &AccountAdjustmentsHandler{pool: pool, queries: queries}
}

// List — GET /api/account-adjustments
func (h *AccountAdjustmentsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var accountID pgtype.UUID
	if s := q.Get("account_id"); s != "" {
		id, err := parseUUID(s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
		accountID = pgtype.UUID{Bytes: id, Valid: true}
	}

	var fromDate, toDate pgtype.Date
	if s := q.Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListAccountAdjustments(ctx, &db.ListAccountAdjustmentsParams{
		Column1: accountID,
		Column2: fromDate,
		Column3: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data penyesuaian akun")
		return
	}
	if rows == nil {
		rows = []*db.ListAccountAdjustmentsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Create — POST /api/account-adjustments
func (h *AccountAdjustmentsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID   string `json:"account_id"`
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.AccountID == "" {
		respondError(w, http.StatusBadRequest, "account_id diperlukan")
		return
	}
	if body.Description == "" {
		respondError(w, http.StatusBadRequest, "deskripsi diperlukan")
		return
	}

	accountID, err := parseUUID(body.AccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "account_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	account, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: accountID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	adjustment, err := qtx.InsertAccountAdjustment(ctx, &db.InsertAccountAdjustmentParams{
		AccountID:     pgtype.UUID{Bytes: accountID, Valid: true},
		Amount:        body.Amount,
		Description:   body.Description,
		CreatedBy:     pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
		CreatedByName: pgtype.Text{String: username, Valid: username != ""},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat penyesuaian akun")
		return
	}

	if err := service.UpdateBalance(ctx, qtx, accountID, body.Amount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan transaksi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "AccountAdjustment",
		EntityID:    adjustment.ID.Bytes,
		Description: fmt.Sprintf("Penyesuaian akun %s: %d", account.Name, body.Amount),
	})

	respondJSON(w, http.StatusCreated, adjustment)
}

// Transfer — POST /api/account-adjustments/transfer
func (h *AccountAdjustmentsHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FromAccountID string `json:"from_account_id"`
		ToAccountID   string `json:"to_account_id"`
		Amount        int64  `json:"amount"`
		Description   string `json:"description"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.FromAccountID == "" || body.ToAccountID == "" {
		respondError(w, http.StatusBadRequest, "from_account_id dan to_account_id diperlukan")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "amount harus lebih dari 0")
		return
	}
	if body.Description == "" {
		respondError(w, http.StatusBadRequest, "deskripsi diperlukan")
		return
	}

	fromAccountID, err := parseUUID(body.FromAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "from_account_id tidak valid")
		return
	}
	toAccountID, err := parseUUID(body.ToAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "to_account_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	fromAccount, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fromAccountID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun sumber tidak ditemukan")
		return
	}
	toAccount, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: toAccountID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tujuan tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	transferID := uuid.New()
	pgTransferID := pgtype.UUID{Bytes: transferID, Valid: true}
	pgCreatedBy := pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil}
	pgCreatedByName := pgtype.Text{String: username, Valid: username != ""}

	_, err = qtx.InsertAccountAdjustmentWithTransfer(ctx, &db.InsertAccountAdjustmentWithTransferParams{
		AccountID:     pgtype.UUID{Bytes: fromAccountID, Valid: true},
		Amount:        -body.Amount,
		Description:   body.Description,
		CreatedBy:     pgCreatedBy,
		CreatedByName: pgCreatedByName,
		TransferID:    pgTransferID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat entri transfer (sumber)")
		return
	}

	_, err = qtx.InsertAccountAdjustmentWithTransfer(ctx, &db.InsertAccountAdjustmentWithTransferParams{
		AccountID:     pgtype.UUID{Bytes: toAccountID, Valid: true},
		Amount:        body.Amount,
		Description:   body.Description,
		CreatedBy:     pgCreatedBy,
		CreatedByName: pgCreatedByName,
		TransferID:    pgTransferID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat entri transfer (tujuan)")
		return
	}

	if err := service.UpdateBalance(ctx, qtx, fromAccountID, -body.Amount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun sumber")
		return
	}
	if err := service.UpdateBalance(ctx, qtx, toAccountID, body.Amount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan transaksi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "AccountTransfer",
		EntityID:    transferID,
		Description: fmt.Sprintf("Transfer saldo %d dari %s ke %s", body.Amount, fromAccount.Name, toAccount.Name),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"transfer_id": transferID,
		"message":     "transfer berhasil",
	})
}
