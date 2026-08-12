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
)

// Pelacakan Kas — the branch till.
//
// Distinct from Kas Kecil, and deliberately so: they are two different piles of
// money with two different failure modes.
//
//	Kas Kecil  a float the branch buys from. Filled by top-up, emptied by
//	           Pembelanjaan Harian. Nothing is ever sold out of it.
//	Kas laci   the till. Filled by customers paying cash, emptied by setoran to
//	           the owner and by anything paid out in notes.
//
// The day is:
//
//	opening + penjualan tunai (POS) + setoran masuk
//	        − setoran keluar − pengeluaran tunai  =  seharusnya
//
// Every term on the income side already exists as data — POS imports and
// cash_deposits — so the only things anyone types are the two counts. The POS
// side is broken out by payment method, not just cash: the non-cash rows are
// what let a branch's takings be checked against what the platforms and the EDC
// actually settled.

type CashTrackingHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewCashTrackingHandler(pool *pgxpool.Pool, queries *db.Queries) *CashTrackingHandler {
	return &CashTrackingHandler{pool: pool, queries: queries}
}

// settlementLine is one payment method's take for a branch on a day.
type settlementLine struct {
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	IsCash      bool   `json:"is_cash"`
	Amount      int64  `json:"amount"`
}

type cashTrackingDay struct {
	BranchID   string `json:"branch_id"`
	BranchName string `json:"branch_name"`
	Date       string `json:"date"`

	HasOpening    bool  `json:"has_opening"`
	OpeningAmount int64 `json:"opening_amount"`
	HasClosing    bool  `json:"has_closing"`
	ClosingAmount int64 `json:"closing_amount"`

	// The POS take, split by how it was paid. CashSales is the subtotal of the
	// methods flagged as a physical drawer; NonCashSales is everything else and
	// is reported for completeness, never reconciled against a count.
	Settlement   []settlementLine `json:"settlement"`
	CashSales    int64            `json:"cash_sales"`
	NonCashSales int64            `json:"non_cash_sales"`
	HasPOSImport bool             `json:"has_pos_import"`

	CashIn       int64 `json:"cash_in"`
	CashOut      int64 `json:"cash_out"`
	CashExpenses int64 `json:"cash_expenses"`

	ExpectedClosing int64 `json:"expected_closing"`
	Variance        int64 `json:"variance"`

	RecordedExpected *int64 `json:"recorded_expected,omitempty"`
	RecordedVariance *int64 `json:"recorded_variance,omitempty"`
	VarianceNote     string `json:"variance_note,omitempty"`

	SuggestedOpening *int64 `json:"suggested_opening,omitempty"`
}

// dayFigures gathers everything that moved through one branch's till on a date.
func (h *CashTrackingHandler) dayFigures(r *http.Request, branchID pgtype.UUID, date pgtype.Date, settlement []settlementLine) (cashSales, nonCash, cashIn, cashOut, expenses int64) {
	ctx := r.Context()

	for _, s := range settlement {
		if s.IsCash {
			cashSales += s.Amount
		} else {
			nonCash += s.Amount
		}
	}

	if mv, err := h.queries.SumCashDrawerMovementsForDay(ctx, &db.SumCashDrawerMovementsForDayParams{
		BranchID: branchID,
		Date:     date,
	}); err == nil {
		cashIn, cashOut = mv.CashIn, mv.CashOut
	}

	// Invoices settled out of a drawer account. Pembelanjaan Harian is
	// deliberately excluded — it comes out of Kas Kecil, which reconciles on its
	// own page. Counting it here would deduct the same money from two tills.
	if total, err := h.queries.SumCashInvoicesForBranchDay(ctx, &db.SumCashInvoicesForBranchDayParams{
		BranchID: branchID,
		Date:     date,
	}); err == nil {
		expenses = total
	}
	return
}

// Day — GET /api/cash-tracking?date=YYYY-MM-DD
func (h *CashTrackingHandler) Day(w http.ResponseWriter, r *http.Request) {
	date, err := parseDayParam(r.URL.Query().Get("date"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}
	pgDate := pgtype.Date{Time: date, Valid: true}
	ctx := r.Context()

	rows, err := h.queries.ListCashDayStatus(ctx, pgDate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kas")
		return
	}

	// The whole day's POS settlement in one query, grouped in memory. A query per
	// branch would be one round trip per branch for a table that is already tiny.
	settlements, err := h.queries.GetPOSSettlementForDay(ctx, pgDate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil setelmen POS")
		return
	}
	byBranch := map[string][]settlementLine{}
	for _, s := range settlements {
		key := uuidText(s.BranchID)
		byBranch[key] = append(byBranch[key], settlementLine{
			AccountID:   uuidText(s.AccountID),
			AccountName: s.AccountName,
			IsCash:      s.IsCashDrawer,
			Amount:      s.Amount,
		})
	}

	out := make([]cashTrackingDay, 0, len(rows))
	for _, row := range rows {
		branchKey := uuidText(row.BranchID)
		lines := byBranch[branchKey]
		if lines == nil {
			lines = []settlementLine{}
		}

		d := cashTrackingDay{
			BranchID:      branchKey,
			BranchName:    row.BranchName,
			Date:          date.Format("2006-01-02"),
			HasOpening:    row.OpeningAmount.Valid,
			OpeningAmount: row.OpeningAmount.Int64,
			HasClosing:    row.ClosingAmount.Valid,
			ClosingAmount: row.ClosingAmount.Int64,
			Settlement:    lines,
			HasPOSImport:  len(lines) > 0,
			VarianceNote:  row.VarianceNote.String,
		}

		d.CashSales, d.NonCashSales, d.CashIn, d.CashOut, d.CashExpenses =
			h.dayFigures(r, row.BranchID, pgDate, lines)

		d.ExpectedClosing = d.OpeningAmount + d.CashSales + d.CashIn - d.CashOut - d.CashExpenses
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

		if !d.HasOpening {
			if prev, err := h.queries.GetPreviousCashDayClosing(ctx, &db.GetPreviousCashDayClosingParams{
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

// RecordOpening — POST /api/cash-tracking/opening
func (h *CashTrackingHandler) RecordOpening(w http.ResponseWriter, r *http.Request) {
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
	count, err := h.queries.UpsertCashDayOpening(ctx, &db.UpsertCashDayOpeningParams{
		BranchID:      pgtype.UUID{Bytes: branchID, Valid: true},
		CountDate:     pgtype.Date{Time: date, Valid: true},
		OpeningAmount: body.Amount,
		OpeningBy:     pgUserID(ctx),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kas awal")
		return
	}

	logMutation(r, h.queries, "CREATE", "cash_day_count", count.ID.Bytes,
		fmt.Sprintf("Mencatat kas awal %s sebesar %d", date.Format("2006-01-02"), body.Amount))

	respondJSON(w, http.StatusOK, count)
}

// RecordClosing — POST /api/cash-tracking/closing
func (h *CashTrackingHandler) RecordClosing(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusBadRequest, "kas akhir tidak boleh negatif")
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

	existing, err := h.queries.GetCashDayCount(ctx, &db.GetCashDayCountParams{
		BranchID:  pgBranchID,
		CountDate: pgDate,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusBadRequest, "kas awal hari ini belum dicatat")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kas")
		return
	}

	settlements, err := h.queries.GetPOSSettlementForDay(ctx, pgDate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil setelmen POS")
		return
	}
	var lines []settlementLine
	for _, s := range settlements {
		if s.BranchID.Bytes != branchID {
			continue
		}
		lines = append(lines, settlementLine{IsCash: s.IsCashDrawer, Amount: s.Amount})
	}

	cashSales, _, cashIn, cashOut, expenses := h.dayFigures(r, pgBranchID, pgDate, lines)
	expected := existing.OpeningAmount + cashSales + cashIn - cashOut - expenses
	variance := body.Amount - expected

	body.Note = strings.TrimSpace(body.Note)
	if variance != 0 && body.Note == "" {
		respondError(w, http.StatusBadRequest,
			fmt.Sprintf("selisih %d harus dijelaskan sebelum disimpan", variance))
		return
	}

	// The POS import for a day usually lands the following morning. Closing
	// before it arrives would score the whole day's cash takings as missing, so
	// say what is going on rather than recording a fictional shortfall.
	if len(lines) == 0 && variance > 0 {
		respondError(w, http.StatusBadRequest,
			"penjualan POS hari ini belum diimpor, jadi selisihnya belum bisa dihitung — impor dulu lalu tutup kas")
		return
	}

	count, err := h.queries.SetCashDayClosing(ctx, &db.SetCashDayClosingParams{
		ClosingAmount:   pgtype.Int8{Int64: body.Amount, Valid: true},
		ClosingBy:       pgUserID(ctx),
		ExpectedClosing: pgtype.Int8{Int64: expected, Valid: true},
		Variance:        pgtype.Int8{Int64: variance, Valid: true},
		VarianceNote:    body.Note,
		CashSales:       cashSales,
		CashIn:          cashIn,
		CashOut:         cashOut,
		CashExpenses:    expenses,
		BranchID:        pgBranchID,
		CountDate:       pgDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kas akhir")
		return
	}

	logMutation(r, h.queries, "UPDATE", "cash_day_count", count.ID.Bytes,
		fmt.Sprintf("Mencatat kas akhir %s sebesar %d (selisih %d)",
			date.Format("2006-01-02"), body.Amount, variance))

	respondJSON(w, http.StatusOK, map[string]any{
		"count":            count,
		"expected_closing": expected,
		"variance":         variance,
	})
}

// History — GET /api/cash-tracking/history?branch_id=&from=&to=
func (h *CashTrackingHandler) History(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := &db.ListCashDayCountsParams{}

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

	rows, err := h.queries.ListCashDayCounts(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat kas")
		return
	}
	if rows == nil {
		rows = []*db.ListCashDayCountsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Settlement — GET /api/cash-tracking/settlement?from=&to=&branch_id=
//
// The POS payment layer on its own: every method, every day. Cash is what gets
// counted; the rest is what should have arrived from the EDC and the delivery
// platforms, and is worth being able to look at without a reconciliation
// wrapped around it.
func (h *CashTrackingHandler) Settlement(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	from, err := parseDayParam(q.Get("from"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
		return
	}
	to, err := parseDayParam(q.Get("to"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
		return
	}

	params := &db.GetPOSSettlementRangeParams{
		Date:   pgtype.Date{Time: from, Valid: true},
		Date_2: pgtype.Date{Time: to, Valid: true},
	}
	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params.Column3 = pgtype.UUID{Bytes: id, Valid: true}
	}

	rows, err := h.queries.GetPOSSettlementRange(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil setelmen POS")
		return
	}
	if rows == nil {
		rows = []*db.GetPOSSettlementRangeRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// DrawerAccounts — GET /api/cash-tracking/drawer-accounts
func (h *CashTrackingHandler) DrawerAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListCashDrawerAccounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun kas fisik")
		return
	}
	if rows == nil {
		rows = []*db.ListCashDrawerAccountsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// SetDrawerAccount — PUT /api/cash-tracking/drawer-accounts/{id}
//
// Which accounts count as physical cash is a property of the chart of accounts,
// not a constant in the code: a second till or a renamed account should need no
// deploy.
func (h *CashTrackingHandler) SetDrawerAccount(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		IsCashDrawer bool `json:"is_cash_drawer"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	if err := h.queries.SetAccountCashDrawer(r.Context(), &db.SetAccountCashDrawerParams{
		IsCashDrawer: body.IsCashDrawer,
		ID:           pgtype.UUID{Bytes: id, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui akun")
		return
	}

	logMutation(r, h.queries, "UPDATE", "account", id,
		fmt.Sprintf("Menandai akun sebagai kas fisik: %t", body.IsCashDrawer))
	respondJSON(w, http.StatusOK, map[string]bool{"is_cash_drawer": body.IsCashDrawer})
}
