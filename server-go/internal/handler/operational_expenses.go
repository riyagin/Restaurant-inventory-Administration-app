package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// Beban Operasional: one balanced entry per row (Dr category sub-account, Cr payer).
// Both accounts are frozen on the row so repointing a branch never rewrites history.
type OperationalExpensesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewOperationalExpensesHandler(pool *pgxpool.Pool, queries *db.Queries) *OperationalExpensesHandler {
	return &OperationalExpensesHandler{pool: pool, queries: queries}
}

// seededOperationalCategories exist in every branch. Keep in step with migration 073.
var seededOperationalCategories = []string{
	"Listrik", "Air", "Sewa", "Internet", "Telepon",
	"Gas", "Kebersihan", "Keamanan", "Perbaikan", "Lain-lain",
}

// parseMonthParam accepts YYYY-MM or YYYY-MM-DD and returns the first of the month.
func parseMonthParam(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("bulan tagihan wajib diisi")
	}
	if t, err := time.Parse("2006-01", raw); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

const operationalExpenseSelect = `
	SELECT oe.id::text AS id, oe.number, oe.date, oe.period_month, oe.amount, oe.reference, oe.notes, oe.photo_path,
	       oe.billing, oe.due_date, oe.term_months,
	       oe.status, oe.created_at, oe.cancelled_at, oe.cancel_reason,
	       oe.branch_id::text AS branch_id, b.name AS branch_name,
	       oe.category_id::text AS category_id, oec.name AS category_name,
	       oe.debit_account_id::text AS debit_account_id, da.account_number AS debit_account_number, da.name AS debit_account_name,
	       oe.credit_account_id::text AS credit_account_id, ca.account_number AS credit_account_number, ca.name AS credit_account_name,
	       oe.vendor_id::text AS vendor_id, v.name AS vendor_name,
	       cu.username AS created_by_name, xu.username AS cancelled_by_name
	FROM operational_expenses oe
	JOIN branches b ON b.id = oe.branch_id
	JOIN operational_expense_categories oec ON oec.id = oe.category_id
	JOIN accounts da ON da.id = oe.debit_account_id
	JOIN accounts ca ON ca.id = oe.credit_account_id
	LEFT JOIN vendors v ON v.id = oe.vendor_id
	LEFT JOIN users cu ON cu.id = oe.created_by
	LEFT JOIN users xu ON xu.id = oe.cancelled_by`

// List — GET /api/operational-expenses?branch_id=&category_id=&from=&to=&status=
func (h *OperationalExpensesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{}
	var params []any

	add := func(clause string, value any) {
		params = append(params, value)
		where = append(where, fmt.Sprintf(clause, len(params)))
	}

	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		add("oe.branch_id = $%d", pgUUID(id))
	}
	if v := q.Get("category_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "category_id tidak valid")
			return
		}
		add("oe.category_id = $%d", pgUUID(id))
	}
	if v := q.Get("from"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
			return
		}
		add("oe.date >= $%d", pgtype.Date{Time: d, Valid: true})
	}
	if v := q.Get("to"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
			return
		}
		add("oe.date <= $%d", pgtype.Date{Time: d, Valid: true})
	}
	if v := q.Get("status"); v != "" {
		add("oe.status = $%d", v)
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, operationalExpenseSelect+clause+` ORDER BY oe.date DESC, oe.created_at DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data beban operasional")
		return
	}
	expenses, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data beban operasional")
		return
	}

	// Cancelled rows are listed but excluded from totals.
	totalClause := clause
	if totalClause == "" {
		totalClause = " WHERE oe.status = 'posted'"
	} else {
		totalClause += " AND oe.status = 'posted'"
	}
	sumRows, err := h.pool.Query(ctx, `
		SELECT oec.id::text AS category_id, oec.name AS category_name, b.name AS branch_name,
		       SUM(oe.amount)::BIGINT AS total, COUNT(*)::INT AS entries
		FROM operational_expenses oe
		JOIN operational_expense_categories oec ON oec.id = oe.category_id
		JOIN branches b ON b.id = oe.branch_id`+totalClause+`
		GROUP BY oec.id, oec.name, b.name
		ORDER BY total DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung total beban operasional")
		return
	}
	byCategory, err := pgx.CollectRows(sumRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses total beban operasional")
		return
	}

	var total int64
	for _, row := range byCategory {
		total += toInt64(row["total"])
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"expenses":    nilToEmpty(expenses),
		"by_category": nilToEmpty(byCategory),
		"total":       total,
	})
}

// Create — POST /api/operational-expenses
func (h *OperationalExpensesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date            string `json:"date"`
		PeriodMonth     string `json:"period_month"`
		BranchID        string `json:"branch_id"`
		CategoryID      string `json:"category_id"`
		CreditAccountID string `json:"credit_account_id"`
		VendorID        string `json:"vendor_id"`
		Amount          int64  `json:"amount"`
		Reference       string `json:"reference"`
		Notes           string `json:"notes"`
		// "monthly" (default) or "annual": rent paid up front, expensed a twelfth a month.
		Billing string `json:"billing"`
		// Required for annual billing: the first day of the lease year.
		DueDate string `json:"due_date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
		return
	}
	date, err := parseDayParam(body.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}
	dueDate := pgtype.Date{}
	if strings.TrimSpace(body.DueDate) != "" {
		d, err := parseDayParam(body.DueDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal jatuh tempo tidak valid")
			return
		}
		dueDate = pgtype.Date{Time: d, Valid: true}
	}
	billing := strings.TrimSpace(body.Billing)
	if billing == "" {
		billing = billingMonthly
	}
	var period time.Time
	switch billing {
	case billingMonthly:
		period, err = parseMonthParam(body.PeriodMonth)
		if err != nil {
			respondError(w, http.StatusBadRequest, "bulan tagihan tidak valid")
			return
		}
	case billingAnnual:
		// An annual lease starts on its due date; period_month is ignored.
		if !dueDate.Valid {
			respondError(w, http.StatusBadRequest, "tanggal jatuh tempo wajib diisi untuk sewa tahunan")
			return
		}
		period = time.Date(dueDate.Time.Year(), dueDate.Time.Month(), 1, 0, 0, 0, 0, time.UTC)
		if body.Amount < service.AnnualTermMonths {
			respondError(w, http.StatusBadRequest, "harga sewa tahunan terlalu kecil untuk dibagi 12 bulan")
			return
		}
	default:
		respondError(w, http.StatusBadRequest, "jenis pembayaran tidak valid (monthly | annual)")
		return
	}
	categoryID, err := parseUUID(body.CategoryID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "kategori tidak valid")
		return
	}
	creditID, err := parseUUID(body.CreditAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak valid")
		return
	}

	ctx := r.Context()

	// Branch comes from the category, never from the request.
	var branchID, divisionID, debitID pgtype.UUID
	var categoryName, branchName, debitName string
	err = h.pool.QueryRow(ctx, `
		SELECT d.branch_id, oec.division_id, oec.account_id, oec.name, b.name, a.name
		FROM operational_expense_categories oec
		JOIN divisions d ON d.id = oec.division_id
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = oec.account_id
		WHERE oec.id = $1`, pgUUID(categoryID)).
		Scan(&branchID, &divisionID, &debitID, &categoryName, &branchName, &debitName)
	if err != nil {
		respondError(w, http.StatusNotFound, "kategori beban operasional tidak ditemukan")
		return
	}
	if debitID.Bytes == creditID {
		respondError(w, http.StatusBadRequest, "akun beban dan akun sumber dana tidak boleh sama")
		return
	}
	// Annual billing is rent only.
	if billing == billingAnnual && !strings.EqualFold(strings.TrimSpace(categoryName), "sewa") {
		respondError(w, http.StatusBadRequest, "pembayaran tahunan hanya tersedia untuk Sewa")
		return
	}
	expenseAccountID := debitID

	credit, err := h.queries.GetAccountByID(ctx, pgUUID(creditID))
	if err != nil {
		respondError(w, http.StatusNotFound, "akun sumber dana tidak ditemukan")
		return
	}

	vendorID := pgtype.UUID{}
	if strings.TrimSpace(body.VendorID) != "" {
		id, err := parseUUID(body.VendorID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "vendor tidak valid")
			return
		}
		vendorID = pgUUID(id)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	termMonths := 1
	if billing == billingAnnual {
		termMonths = service.AnnualTermMonths
		// Prepaid rent is an asset until used.
		prepaidID, err := ensurePrepaidRentAccount(ctx, tx, branchID, branchName)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyiapkan akun sewa dibayar di muka")
			return
		}
		if prepaidID.Bytes == creditID {
			respondError(w, http.StatusBadRequest, "akun sewa dibayar di muka tidak dapat menjadi sumber dana")
			return
		}
		debitID = prepaidID
	}

	var number string
	if err := tx.QueryRow(ctx,
		`SELECT 'BO-' || LPAD(nextval('operational_expense_number_seq')::TEXT, 6, '0')`).Scan(&number); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor beban operasional")
		return
	}

	var id pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO operational_expenses
		  (number, date, period_month, branch_id, category_id, debit_account_id, credit_account_id,
		   amount, vendor_id, reference, notes, created_by, billing, due_date, term_months)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`,
		number, pgtype.Date{Time: date, Valid: true}, pgtype.Date{Time: period, Valid: true},
		branchID, pgUUID(categoryID), debitID, pgUUID(creditID), body.Amount, vendorID,
		strings.TrimSpace(body.Reference), strings.TrimSpace(body.Notes), pgUserID(ctx),
		billing, dueDate, termMonths).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan beban operasional")
		return
	}

	qtx := h.queries.WithTx(tx)
	description := fmt.Sprintf("Beban operasional %s: %s - %s %s (%s)",
		number, branchName, categoryName, period.Format("Jan 2006"), credit.Name)
	if billing == billingAnnual {
		last := period.AddDate(0, service.AnnualTermMonths-1, 0)
		description = fmt.Sprintf("Sewa tahunan %s: %s - %s %s s/d %s (%s)",
			number, branchName, categoryName, period.Format("Jan 2006"), last.Format("Jan 2006"), credit.Name)
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        date,
		SourceType:  service.SourceOperationalExpense,
		SourceID:    id.Bytes,
		Description: description,
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(debitID.Bytes, body.Amount),
			service.Cr(creditID, body.Amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal beban operasional")
		return
	}

	amortized := 0
	if billing == billingAnnual {
		for _, s := range service.AmortizationSchedule(body.Amount, period, service.AnnualTermMonths) {
			if _, err := tx.Exec(ctx, `
				INSERT INTO operational_expense_amortizations
				  (expense_id, seq, period_month, amount, expense_account_id, prepaid_account_id)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				id, s.Seq, pgtype.Date{Time: s.PeriodMonth, Valid: true}, s.Amount, expenseAccountID, debitID); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan jadwal amortisasi")
				return
			}
		}
		// Months already started are expensed now, not at the next sweep.
		amortized, err = service.PostDueAmortizationsFor(ctx, tx, qtx, id.Bytes, time.Now(), middleware.UserIDFromCtx(ctx))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat amortisasi sewa")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "operational_expense",
		EntityID:    id.Bytes,
		Description: fmt.Sprintf("%s sebesar %d", description, body.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan beban operasional")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":               uuidBytesToString(id.Bytes),
		"number":           number,
		"period_month":     period.Format("2006-01-02"),
		"billing":          billing,
		"months_amortized": amortized,
	})
}

const (
	billingMonthly = "monthly"
	billingAnnual  = "annual"
)

// Cancel — POST /api/operational-expenses/{id}/cancel (reversal dated today)
func (h *OperationalExpensesHandler) Cancel(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	// Locked: the amortization poster checks this status before posting a month.
	var number, status, billing string
	var amount int64
	var debitID, creditID pgtype.UUID
	err = tx.QueryRow(ctx, `
		SELECT number, status, billing, amount, debit_account_id, credit_account_id
		FROM operational_expenses WHERE id = $1 FOR UPDATE`, pgUUID(id)).
		Scan(&number, &status, &billing, &amount, &debitID, &creditID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "beban operasional tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data beban operasional")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusConflict, "beban operasional ini sudah dibatalkan")
		return
	}

	// Annual: credit expensed months back to expense and the rest to prepaid rent.
	lines := []service.Line{
		service.Dr(creditID.Bytes, amount),
		service.Cr(debitID.Bytes, amount),
	}
	if billing == billingAnnual {
		rows, err := tx.Query(ctx, `
			SELECT expense_account_id, status, amount
			FROM operational_expense_amortizations
			WHERE expense_id = $1 FOR UPDATE`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
			return
		}
		var postedTotal int64
		postedByAccount := map[uuid.UUID]int64{}
		var order []uuid.UUID
		for rows.Next() {
			var acct uuid.UUID
			var rowStatus string
			var rowAmount int64
			if err := rows.Scan(&acct, &rowStatus, &rowAmount); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
				return
			}
			if rowStatus != "posted" {
				continue
			}
			if _, seen := postedByAccount[acct]; !seen {
				order = append(order, acct)
			}
			postedByAccount[acct] += rowAmount
			postedTotal += rowAmount
		}
		rows.Close()
		if rows.Err() != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
			return
		}

		lines = []service.Line{
			service.Dr(creditID.Bytes, amount),
			service.Cr(debitID.Bytes, amount-postedTotal),
		}
		for _, acct := range order {
			lines = append(lines, service.Cr(acct, postedByAccount[acct]))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE operational_expense_amortizations SET status = 'cancelled'
			WHERE expense_id = $1 AND status = 'scheduled'`, pgUUID(id)); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membatalkan jadwal amortisasi")
			return
		}
	}

	qtx := h.queries.WithTx(tx)
	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceOperationalExpense,
		SourceID:    id,
		Description: fmt.Sprintf("Pembatalan beban operasional %s", number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines:       lines,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan")
		return
	}

	if _, err := tx.Exec(ctx, `
		UPDATE operational_expenses
		SET status = 'cancelled', cancelled_by = $1, cancelled_at = now(), cancel_reason = $2
		WHERE id = $3`, pgUserID(ctx), body.Reason, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan beban operasional")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CANCEL",
		EntityType:  "operational_expense",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan beban operasional %s: %s", number, body.Reason),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "beban operasional berhasil dibatalkan"})
}
