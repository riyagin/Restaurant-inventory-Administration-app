package handler

import (
	"errors"
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

// Setoran — cash moving from one place to another.
//
// Two things the business does constantly, which had no home before: the branch
// hands its takings to the owner (till → bank), and the office refills a
// branch's petty cash box (till/bank → Kas Kecil). Both are the same event —
// money between two accounts, attributed to a branch, against a slip somebody
// signed — so they share one table with a type rather than being two features.
//
// Each posted row is one balanced journal entry, Dr destination / Cr source.
// That is also what makes the petty cash day reconcile: a top-up recorded here
// is the "+ cash in" term the closing count is measured against.

type CashDepositsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewCashDepositsHandler(pool *pgxpool.Pool, queries *db.Queries) *CashDepositsHandler {
	return &CashDepositsHandler{pool: pool, queries: queries}
}

var cashMovementTypes = map[string]string{
	"setoran":                "Setoran ke rekening",
	"pengisian_kas_kecil":    "Pengisian kas kecil",
	"pengembalian_kas_kecil": "Pengembalian kas kecil",
	"lainnya":                "Perpindahan kas",
}

// List — GET /api/cash-deposits?branch_id=&from=&to=&type=&status=
func (h *CashDepositsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := &db.ListCashDepositsParams{}

	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params.Column1 = pgtype.UUID{Bytes: id, Valid: true}
	}
	if v := q.Get("from"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
			return
		}
		params.Column2 = pgtype.Date{Time: d, Valid: true}
	}
	if v := q.Get("to"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
			return
		}
		params.Column3 = pgtype.Date{Time: d, Valid: true}
	}
	if v := q.Get("type"); v != "" {
		params.Column4 = pgtype.Text{String: v, Valid: true}
	}
	if v := q.Get("status"); v != "" {
		params.Column5 = pgtype.Text{String: v, Valid: true}
	}

	rows, err := h.queries.ListCashDeposits(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data setoran")
		return
	}
	if rows == nil {
		rows = []*db.ListCashDepositsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Create — POST /api/cash-deposits
func (h *CashDepositsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date          string `json:"date"`
		BranchID      string `json:"branch_id"`
		MovementType  string `json:"movement_type"`
		FromAccountID string `json:"from_account_id"`
		ToAccountID   string `json:"to_account_id"`
		Amount        int64  `json:"amount"`
		Reference     string `json:"reference"`
		HandedTo      string `json:"handed_to"`
		Notes         string `json:"notes"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	if _, ok := cashMovementTypes[body.MovementType]; !ok {
		respondError(w, http.StatusBadRequest, "jenis perpindahan tidak dikenal")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
		return
	}
	fromID, err := parseUUID(body.FromAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun asal tidak valid")
		return
	}
	toID, err := parseUUID(body.ToAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun tujuan tidak valid")
		return
	}
	if fromID == toID {
		respondError(w, http.StatusBadRequest, "akun asal dan tujuan tidak boleh sama")
		return
	}

	date, err := parseDayParam(body.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}

	branchID := pgtype.UUID{}
	if strings.TrimSpace(body.BranchID) != "" {
		id, err := parseUUID(body.BranchID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "cabang tidak valid")
			return
		}
		branchID = pgtype.UUID{Bytes: id, Valid: true}
	}

	ctx := r.Context()

	from, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fromID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun asal tidak ditemukan")
		return
	}
	to, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: toID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tujuan tidak ditemukan")
		return
	}
	// Handing over more than the source holds means one of the two figures is
	// wrong. Unlike a Pembelanjaan Harian — where the spend happened whether or
	// not the system knows about the top-up — a setoran is recorded by the person
	// doing it, so getting it right at entry is both possible and cheaper.
	if from.Balance < body.Amount {
		respondError(w, http.StatusBadRequest,
			fmt.Sprintf("saldo akun %q hanya %d", from.Name, from.Balance))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	number, err := qtx.NextCashDepositNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor setoran")
		return
	}

	deposit, err := qtx.CreateCashDeposit(ctx, &db.CreateCashDepositParams{
		Number:        number,
		Date:          pgtype.Date{Time: date, Valid: true},
		BranchID:      branchID,
		MovementType:  body.MovementType,
		FromAccountID: pgtype.UUID{Bytes: fromID, Valid: true},
		ToAccountID:   pgtype.UUID{Bytes: toID, Valid: true},
		Amount:        body.Amount,
		Reference:     strings.TrimSpace(body.Reference),
		HandedTo:      strings.TrimSpace(body.HandedTo),
		Notes:         strings.TrimSpace(body.Notes),
		CreatedBy:     pgUserID(ctx),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan setoran")
		return
	}

	description := fmt.Sprintf("%s %s: %s → %s",
		cashMovementTypes[body.MovementType], number, from.Name, to.Name)

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        date,
		SourceType:  service.SourceCashDeposit,
		SourceID:    deposit.ID.Bytes,
		Description: description,
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(toID, body.Amount),
			service.Cr(fromID, body.Amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal setoran")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "cash_deposit",
		EntityID:    deposit.ID.Bytes,
		Description: fmt.Sprintf("%s sebesar %d", description, body.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan setoran")
		return
	}
	respondJSON(w, http.StatusCreated, deposit)
}

// Cancel — POST /api/cash-deposits/{id}/cancel
//
// Kept and reversed rather than deleted: a setoran that reconciled a day once
// must keep reconciling it, and a number that vanishes is a hole nobody can
// explain to the owner.
func (h *CashDepositsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = parseBody(r, &body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		respondError(w, http.StatusBadRequest, "alasan pembatalan wajib diisi")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	deposit, err := h.queries.GetCashDepositRow(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "setoran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data setoran")
		return
	}
	if deposit.Status == "cancelled" {
		respondError(w, http.StatusConflict, "setoran ini sudah dibatalkan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	// Dated today, so a cancellation never reaches back into a period that has
	// already been reported on.
	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceCashDeposit,
		SourceID:    id,
		Description: fmt.Sprintf("Pembatalan setoran %s", deposit.Number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(deposit.FromAccountID.Bytes, deposit.Amount),
			service.Cr(deposit.ToAccountID.Bytes, deposit.Amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan")
		return
	}

	if err := qtx.CancelCashDeposit(ctx, &db.CancelCashDepositParams{
		CancelledBy:  pgUserID(ctx),
		CancelReason: body.Reason,
		ID:           pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan setoran")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CANCEL",
		EntityType:  "cash_deposit",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan setoran %s: %s", deposit.Number, body.Reason),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "setoran berhasil dibatalkan"})
}
