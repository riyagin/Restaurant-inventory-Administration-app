package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

// The daily petty cash count.
//
// Twice a day someone opens the box and counts it. Those two numbers bracket the
// day, and the difference between them must equal what the system says moved:
//
//	expected closing = opening + top-ups in - transfers out - Pembelanjaan Harian
//
// Anything left over is a variance — real money that is missing or unexplained —
// and it has to carry a note before it can be saved.
//
// Nothing here writes to the ledger. A count is an observation of a physical
// box, not a financial event; letting a typo post a journal entry would mean a
// miscount could rewrite the books. Correcting a variance stays a deliberate act
// on the Setoran or account-adjustment screen.

type PettyCashHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPettyCashHandler(pool *pgxpool.Pool, queries *db.Queries) *PettyCashHandler {
	return &PettyCashHandler{pool: pool, queries: queries}
}

// pettyCashDay is everything known about one branch's box on one date: the two
// counts, the movements that should explain the gap between them, and what the
// ledger independently believes is in there.
type pettyCashDay struct {
	BranchID   string `json:"branch_id"`
	BranchName string `json:"branch_name"`
	Date       string `json:"date"`

	AccountID     string `json:"account_id"`
	LedgerBalance int64  `json:"ledger_balance"`

	HasOpening    bool   `json:"has_opening"`
	OpeningAmount int64  `json:"opening_amount"`
	OpeningBy     string `json:"opening_by,omitempty"`

	HasClosing    bool  `json:"has_closing"`
	ClosingAmount int64 `json:"closing_amount"`

	// The movements the day should account for.
	CashIn        int64 `json:"cash_in"`
	CashOut       int64 `json:"cash_out"`
	Spending      int64 `json:"spending"`
	SpendingCount int32 `json:"spending_count"`

	// Live figures, recomputed on every read so the form can show the operator
	// what closing it is aiming at before anything is saved.
	ExpectedClosing int64 `json:"expected_closing"`
	Variance        int64 `json:"variance"`

	// Frozen figures, as recorded when the closing count was taken. These are the
	// ones that were signed off; they can differ from the live pair if something
	// was backdated into the day afterwards, and that difference is worth seeing.
	RecordedExpected *int64 `json:"recorded_expected,omitempty"`
	RecordedVariance *int64 `json:"recorded_variance,omitempty"`
	VarianceNote     string `json:"variance_note,omitempty"`

	// The previous day's closing count, offered as this morning's default so the
	// two ends of consecutive days are linked rather than typed twice.
	SuggestedOpening *int64 `json:"suggested_opening,omitempty"`
}

// dayMovements adds up everything that should have moved through one box on one
// date: top-ups and withdrawals from Setoran, spending from Pembelanjaan Harian.
func (h *PettyCashHandler) dayMovements(r *http.Request, accountID pgtype.UUID, branchID pgtype.UUID, date pgtype.Date) (cashIn, cashOut, spending int64, spendCount int32) {
	ctx := r.Context()
	if accountID.Valid {
		if sums, err := h.queries.SumCashDepositsForAccountDay(ctx, &db.SumCashDepositsForAccountDayParams{
			ToAccountID: accountID,
			Date:        date,
		}); err == nil {
			cashIn, cashOut = sums.CashIn, sums.CashOut
		}
	}
	if spend, err := h.queries.SumDailyPurchasesForDay(ctx, &db.SumDailyPurchasesForDayParams{
		BranchID: branchID,
		Date:     date,
	}); err == nil {
		spending, spendCount = spend.Total, spend.Count
	}
	return
}

// Day — GET /api/petty-cash?date=YYYY-MM-DD
//
// One row per branch: the board the admin works from each morning and evening.
func (h *PettyCashHandler) Day(w http.ResponseWriter, r *http.Request) {
	date, err := parseDayParam(r.URL.Query().Get("date"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}
	pgDate := pgtype.Date{Time: date, Valid: true}

	rows, err := h.queries.ListPettyCashDayStatus(r.Context(), pgDate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kas kecil")
		return
	}

	out := make([]pettyCashDay, 0, len(rows))
	for _, row := range rows {
		d := pettyCashDay{
			BranchID:      uuidText(row.BranchID),
			BranchName:    row.BranchName,
			Date:          date.Format("2006-01-02"),
			AccountID:     uuidText(row.PettyCashAccountID),
			LedgerBalance: row.LedgerBalance,
			HasOpening:    row.OpeningAmount.Valid,
			OpeningAmount: row.OpeningAmount.Int64,
			HasClosing:    row.ClosingAmount.Valid,
			ClosingAmount: row.ClosingAmount.Int64,
		}

		d.CashIn, d.CashOut, d.Spending, d.SpendingCount =
			h.dayMovements(r, row.PettyCashAccountID, row.BranchID, pgDate)
		d.ExpectedClosing = d.OpeningAmount + d.CashIn - d.CashOut - d.Spending
		if d.HasClosing {
			d.Variance = d.ClosingAmount - d.ExpectedClosing
		}

		if row.ExpectedClosing.Valid {
			v := row.ExpectedClosing.Int64
			d.RecordedExpected = &v
		}
		if row.Variance.Valid {
			v := row.Variance.Int64
			d.RecordedVariance = &v
		}
		d.VarianceNote = row.VarianceNote.String

		if !d.HasOpening {
			if prev, err := h.queries.GetPreviousPettyCashClosing(r.Context(), &db.GetPreviousPettyCashClosingParams{
				BranchID:  row.BranchID,
				CountDate: pgDate,
			}); err == nil && prev.ClosingAmount.Valid {
				v := prev.ClosingAmount.Int64
				d.SuggestedOpening = &v
			}
		}

		out = append(out, d)
	}
	respondJSON(w, http.StatusOK, out)
}

// RecordOpening — POST /api/petty-cash/opening
//
// Recording an opening twice on the same date is a correction of a miscount, not
// a second observation, so it overwrites — and it clears that day's closing with
// it, because a variance computed against the old opening is now meaningless.
func (h *PettyCashHandler) RecordOpening(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID string `json:"branch_id"`
		Date     string `json:"date"`
		Amount   int64  `json:"amount"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	if body.Amount < 0 {
		respondError(w, http.StatusBadRequest, "saldo awal tidak boleh negatif")
		return
	}
	date, err := parseDayParam(body.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}

	ctx := r.Context()
	count, err := h.queries.UpsertPettyCashOpening(ctx, &db.UpsertPettyCashOpeningParams{
		BranchID:      pgtype.UUID{Bytes: branchID, Valid: true},
		CountDate:     pgtype.Date{Time: date, Valid: true},
		OpeningAmount: body.Amount,
		OpeningBy:     pgUserID(ctx),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan saldo awal kas kecil")
		return
	}

	logMutation(r, h.queries, "CREATE", "petty_cash_count", count.ID.Bytes,
		fmt.Sprintf("Mencatat saldo awal kas kecil %s sebesar %d", date.Format("2006-01-02"), body.Amount))

	respondJSON(w, http.StatusOK, count)
}

// RecordClosing — POST /api/petty-cash/closing
//
// The expected figure is computed here and frozen onto the row rather than
// recomputed at read time. A spend backdated into a day that has already been
// signed off must not quietly change a variance someone has explained.
func (h *PettyCashHandler) RecordClosing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID string `json:"branch_id"`
		Date     string `json:"date"`
		Amount   int64  `json:"amount"`
		Note     string `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}
	if body.Amount < 0 {
		respondError(w, http.StatusBadRequest, "saldo akhir tidak boleh negatif")
		return
	}
	date, err := parseDayParam(body.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}

	ctx := r.Context()
	pgBranchID := pgtype.UUID{Bytes: branchID, Valid: true}
	pgDate := pgtype.Date{Time: date, Valid: true}

	// Closing without an opening has nothing to be measured against, and
	// inventing one would bury the omission rather than surface it.
	existing, err := h.queries.GetPettyCashCount(ctx, &db.GetPettyCashCountParams{
		BranchID:  pgBranchID,
		CountDate: pgDate,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusBadRequest, "saldo awal hari ini belum dicatat")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kas kecil")
		return
	}

	acctID, err := h.queries.GetBranchPettyCashAccountID(ctx, pgBranchID)
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
		return
	}

	cashIn, cashOut, spending, _ := h.dayMovements(r, acctID, pgBranchID, pgDate)
	expected := existing.OpeningAmount + cashIn - cashOut - spending
	variance := body.Amount - expected

	body.Note = strings.TrimSpace(body.Note)
	if variance != 0 && body.Note == "" {
		respondError(w, http.StatusBadRequest, fmt.Sprintf(
			"selisih %d harus dijelaskan sebelum disimpan", variance))
		return
	}

	count, err := h.queries.SetPettyCashClosing(ctx, &db.SetPettyCashClosingParams{
		ClosingAmount:   pgtype.Int8{Int64: body.Amount, Valid: true},
		ClosingBy:       pgUserID(ctx),
		ExpectedClosing: pgtype.Int8{Int64: expected, Valid: true},
		Variance:        pgtype.Int8{Int64: variance, Valid: true},
		VarianceNote:    body.Note,
		BranchID:        pgBranchID,
		CountDate:       pgDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan saldo akhir kas kecil")
		return
	}

	logMutation(r, h.queries, "UPDATE", "petty_cash_count", count.ID.Bytes,
		fmt.Sprintf("Mencatat saldo akhir kas kecil %s sebesar %d (selisih %d)",
			date.Format("2006-01-02"), body.Amount, variance))

	respondJSON(w, http.StatusOK, map[string]any{
		"count":            count,
		"expected_closing": expected,
		"variance":         variance,
		"cash_in":          cashIn,
		"cash_out":         cashOut,
		"spending":         spending,
	})
}

// History — GET /api/petty-cash/history?branch_id=&from=&to=
func (h *PettyCashHandler) History(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := &db.ListPettyCashCountsParams{}

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

	rows, err := h.queries.ListPettyCashCounts(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat kas kecil")
		return
	}
	if rows == nil {
		rows = []*db.ListPettyCashCountsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Accounts — GET /api/petty-cash/accounts
//
// The branch → box mapping with live balances, for the Setoran form's account
// pickers and anywhere else that needs to name a branch's cash account.
func (h *PettyCashHandler) Accounts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListBranchPettyCash(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun kas kecil")
		return
	}
	if rows == nil {
		rows = []*db.ListBranchPettyCashRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// parseDayParam reads a YYYY-MM-DD parameter, defaulting to today when blank.
func parseDayParam(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Parse("2006-01-02", raw)
}

// uuidText renders a pgtype.UUID, mapping NULL to "" so a missing account and an
// absent row read the same way on the client.
func uuidText(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
