package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

type ReportsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewReportsHandler(pool *pgxpool.Pool, queries *db.Queries) *ReportsHandler {
	return &ReportsHandler{pool: pool, queries: queries}
}

// Financial — GET /api/reports/financial
// Params: start_date (or from), end_date (or to)
//
// Organisation-wide. The per-branch split lives in ProfitLossByBranch, which
// derives its figures from the journal; this endpoint's period figures still come
// from the source tables below.
func (h *ReportsHandler) Financial(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startDate := firstNonEmpty(q.Get("start_date"), q.Get("from"))
	endDate := firstNonEmpty(q.Get("end_date"), q.Get("to"))
	usePeriod := startDate != "" && endDate != ""
	ctx := r.Context()

	// Fetch all accounts
	type accRow struct {
		ID            pgtype.UUID `json:"id"`
		AccountNumber pgtype.Int4 `json:"account_number"`
		Name          string      `json:"name"`
		AccountType   string      `json:"account_type"`
		Balance       int64       `json:"balance"`
		ParentID      pgtype.UUID `json:"parent_id"`
		IsSystem      bool        `json:"is_system"`
	}
	accRows, err := h.pool.Query(ctx,
		`SELECT id, account_number, name, account_type, balance, parent_id, is_system
		 FROM accounts ORDER BY account_type, account_number NULLS LAST, name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data akun")
		return
	}
	accounts, err := pgx.CollectRows(accRows, func(row pgx.CollectableRow) (accRow, error) {
		var a accRow
		return a, row.Scan(&a.ID, &a.AccountNumber, &a.Name, &a.AccountType, &a.Balance, &a.ParentID, &a.IsSystem)
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data akun")
		return
	}

	periodMap := map[string]int64{}
	adjMap := map[string]int64{}

	if usePeriod {
		// Period revenue/expense comes from the journal — the same
		// plActivityByAccount the per-branch split reads, so the two agree by
		// construction instead of by coincidence. See that function for why the
		// source tables cannot be summed into the same answer.
		periodMap, err = plActivityByAccount(ctx, h.pool, startDate, endDate)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung periode keuangan")
			return
		}

		// Adjustments map
		adjRows, err := h.pool.Query(ctx,
			`SELECT account_id, COALESCE(SUM(amount),0)::BIGINT AS total
			 FROM account_adjustments WHERE created_at::date BETWEEN $1 AND $2
			 GROUP BY account_id`,
			startDate, endDate)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil data penyesuaian")
			return
		}
		defer adjRows.Close()
		for adjRows.Next() {
			var accountID pgtype.UUID
			var total int64
			if err := adjRows.Scan(&accountID, &total); err == nil && accountID.Valid {
				adjMap[uuidBytesToString(accountID.Bytes)] = total
			}
		}
	} else {
		adjRows, err := h.pool.Query(ctx,
			`SELECT account_id, COALESCE(SUM(amount),0)::BIGINT AS total
			 FROM account_adjustments GROUP BY account_id`)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil data penyesuaian")
			return
		}
		defer adjRows.Close()
		for adjRows.Next() {
			var accountID pgtype.UUID
			var total int64
			if err := adjRows.Scan(&accountID, &total); err == nil && accountID.Valid {
				adjMap[uuidBytesToString(accountID.Bytes)] = total
			}
		}
	}

	type resultRow struct {
		ID                pgtype.UUID `json:"id"`
		AccountNumber     pgtype.Int4 `json:"account_number"`
		Name              string      `json:"name"`
		AccountType       string      `json:"account_type"`
		Balance           int64       `json:"balance"`
		ParentID          pgtype.UUID `json:"parent_id"`
		IsSystem          bool        `json:"is_system"`
		TotalAdjustments  int64       `json:"total_adjustments"`
	}

	result := make([]resultRow, 0, len(accounts))
	for _, a := range accounts {
		isIncomeStmt := a.AccountType == "revenue" || a.AccountType == "expense"
		balance := a.Balance
		if usePeriod && isIncomeStmt {
			idStr := ""
			if a.ID.Valid {
				idStr = uuidBytesToString(a.ID.Bytes)
			}
			balance = periodMap[idStr]
		}
		idStr := ""
		if a.ID.Valid {
			idStr = uuidBytesToString(a.ID.Bytes)
		}
		result = append(result, resultRow{
			ID:               a.ID,
			AccountNumber:    a.AccountNumber,
			Name:             a.Name,
			AccountType:      a.AccountType,
			Balance:          balance,
			ParentID:         a.ParentID,
			IsSystem:         a.IsSystem,
			TotalAdjustments: adjMap[idStr],
		})
	}

	respondJSON(w, http.StatusOK, result)
}

// CashSummary — GET /api/reports/cash-summary
// Params: start_date (or from), end_date (or to) — both required.
// Returns a simple cash-in / cash-out summary for the period, derived from the
// same operating sources the financial report uses (POS revenue, manual sales,
// purchase & expense invoices) plus payroll disbursements and kasbon payouts.
func (h *ReportsHandler) CashSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startDate := firstNonEmpty(q.Get("start_date"), q.Get("from"))
	endDate := firstNonEmpty(q.Get("end_date"), q.Get("to"))
	if startDate == "" || endDate == "" {
		respondError(w, http.StatusBadRequest, "parameter 'start_date' dan 'end_date' diperlukan (YYYY-MM-DD)")
		return
	}
	ctx := r.Context()

	scalar := func(sql string) (int64, error) {
		var v int64
		err := h.pool.QueryRow(ctx, sql, startDate, endDate).Scan(&v)
		return v, err
	}

	type line struct {
		Label  string `json:"label"`
		Amount int64  `json:"amount"`
	}

	posRevenue, err := scalar(`
		SELECT COALESCE(SUM(pil.amount), 0)::BIGINT
		FROM pos_import_lines pil
		JOIN pos_imports pi ON pi.id = pil.import_id
		WHERE pil.line_type = 'revenue' AND pi.date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung pendapatan POS")
		return
	}
	manualSales, err := scalar(`
		SELECT COALESCE(SUM(amount), 0)::BIGINT
		FROM sales WHERE date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung penjualan manual")
		return
	}
	purchases, err := scalar(`
		SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT
		FROM invoices inv
		JOIN invoice_items ii ON ii.invoice_id = inv.id
		WHERE inv.invoice_type = 'purchase' AND inv.date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung pembelian")
		return
	}
	// dispatch_id IS NULL: a dispatch's auto-invoice moves value from inventory to
	// the division's expense account and never touches cash — the cash for those
	// goods already left as "Pembelian Persediaan" above. Counting it here would
	// charge the same rupiah to cash twice.
	expenses, err := scalar(`
		SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT
		FROM invoices inv
		JOIN invoice_items ii ON ii.invoice_id = inv.id
		WHERE inv.invoice_type = 'expense' AND inv.dispatch_id IS NULL
		  AND inv.date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung beban operasional")
		return
	}
	payroll, err := scalar(`
		SELECT COALESCE(SUM(pl.net_pay), 0)::BIGINT
		FROM payroll_lines pl
		JOIN payroll_periods pp ON pp.id = pl.payroll_period_id
		WHERE pp.status = 'paid' AND pp.paid_at::date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung penggajian")
		return
	}
	kasbon, err := scalar(`
		SELECT COALESCE(SUM(amount), 0)::BIGINT
		FROM kasbons
		WHERE status IN ('processed', 'resolved')
		  AND processed_at IS NOT NULL
		  AND processed_at::date BETWEEN $1 AND $2`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung kasbon")
		return
	}

	inflows := []line{
		{Label: "Penjualan POS", Amount: posRevenue},
		{Label: "Penjualan Manual", Amount: manualSales},
	}
	outflows := []line{
		{Label: "Pembelian Persediaan", Amount: purchases},
		{Label: "Beban Operasional", Amount: expenses},
		{Label: "Penggajian", Amount: payroll},
		{Label: "Pencairan Kasbon", Amount: kasbon},
	}

	var totalIn, totalOut int64
	for _, l := range inflows {
		totalIn += l.Amount
	}
	for _, l := range outflows {
		totalOut += l.Amount
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"start_date":    startDate,
		"end_date":      endDate,
		"inflows":       inflows,
		"outflows":      outflows,
		"total_inflow":  totalIn,
		"total_outflow": totalOut,
		"net_cash_flow": totalIn - totalOut,
	})
}

// Daily — GET /api/reports/daily
// Params: date (required), branch_id (optional)
func (h *ReportsHandler) Daily(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date := q.Get("date")
	if date == "" {
		respondError(w, http.StatusBadRequest, "parameter 'date' diperlukan (YYYY-MM-DD)")
		return
	}
	branchID := q.Get("branch_id")
	hasBranch := branchID != ""
	ctx := r.Context()

	// POS imports
	posSQL := `
		SELECT pi.id::text AS id, pi.description, pi.date, pi.total_amount, pi.source_file,
		       u.username AS created_by_name,
		       json_agg(json_build_object(
		         'label', pil.label, 'amount', pil.amount, 'line_type', pil.line_type,
		         'account_name', a.name, 'account_number', a.account_number
		       ) ORDER BY pil.line_type DESC, pil.amount DESC) AS lines
		FROM pos_imports pi
		LEFT JOIN users u ON u.id = pi.created_by
		LEFT JOIN pos_import_lines pil ON pil.import_id = pi.id
		LEFT JOIN accounts a ON a.id = pil.account_id
		WHERE pi.date = $1`
	if hasBranch {
		posSQL += ` AND pi.id IN (
			SELECT pil2.import_id FROM pos_import_lines pil2
			JOIN divisions dv ON dv.revenue_account_id = pil2.account_id AND dv.branch_id = $2
			WHERE pil2.line_type = 'revenue'
		)`
	}
	posSQL += ` GROUP BY pi.id, u.username ORDER BY pi.created_at`

	// Invoices
	invSQL := `
		SELECT inv.id::text AS id, inv.invoice_number, inv.date, inv.invoice_type, inv.payment_status,
		       inv.amount_paid, v.name AS vendor_name, b.name AS branch_name, dv.name AS division_name,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
		FROM invoices inv
		LEFT JOIN vendors v ON v.id = inv.vendor_id
		LEFT JOIN branches b ON b.id = inv.branch_id
		LEFT JOIN divisions dv ON dv.id = inv.division_id
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		WHERE inv.date = $1`
	if hasBranch {
		invSQL += ` AND inv.branch_id = $2`
	}
	invSQL += ` GROUP BY inv.id, v.name, b.name, dv.name ORDER BY inv.date`

	// Dispatches
	dispSQL := `
		SELECT d.id::text AS id, d.dispatched_at, d.notes, b.name AS branch_name, dv.name AS division_name,
		       w.name AS warehouse_name, u.username AS dispatched_by_name,
		       COUNT(di.id)::INT AS item_count,
		       COUNT(DISTINCT di.item_id)::INT AS distinct_items
		FROM dispatches d
		JOIN branches b ON b.id = d.branch_id
		JOIN divisions dv ON dv.id = d.division_id
		JOIN warehouses w ON w.id = d.warehouse_id
		LEFT JOIN users u ON u.id = d.dispatched_by
		LEFT JOIN dispatch_items di ON di.dispatch_id = d.id
		WHERE d.dispatched_at::date = $1`
	if hasBranch {
		dispSQL += ` AND d.branch_id = $2`
	}
	dispSQL += ` GROUP BY d.id, b.name, dv.name, w.name, u.username ORDER BY d.dispatched_at`

	// Opname (no branch filter)
	opnameSQL := `
		SELECT so.id::text AS id, so.performed_at, so.notes, so.operator_name, so.pic_name,
		       w.name AS warehouse_name, u.username AS performed_by_name,
		       COUNT(soi.id)::INT AS item_count,
		       COALESCE(SUM(ABS(soi.difference)), 0)::BIGINT AS total_diff
		FROM stock_opname so
		JOIN warehouses w ON w.id = so.warehouse_id
		LEFT JOIN users u ON u.id = so.performed_by
		LEFT JOIN stock_opname_items soi ON soi.opname_id = so.id
		WHERE so.performed_at::date = $1
		GROUP BY so.id, w.name, u.username ORDER BY so.performed_at`

	// Transfers (no branch filter)
	transferSQL := `
		SELECT st.group_id::text AS group_id, MIN(st.transferred_at) AS transferred_at,
		       fw.name AS from_warehouse, tw.name AS to_warehouse,
		       u.username AS transferred_by_name,
		       COUNT(st.id)::INT AS item_count,
		       COUNT(DISTINCT st.item_id)::INT AS distinct_items
		FROM stock_transfers st
		JOIN warehouses fw ON fw.id = st.from_warehouse_id
		JOIN warehouses tw ON tw.id = st.to_warehouse_id
		LEFT JOIN users u ON u.id = st.transferred_by
		WHERE st.transferred_at::date = $1
		GROUP BY st.group_id, fw.name, tw.name, u.username ORDER BY transferred_at`

	// Sales
	salesSQL := `
		SELECT s.id::text AS id, s.date, s.amount, s.description,
		       b.name AS branch_name, dv.name AS division_name, u.username AS created_by_name
		FROM sales s
		LEFT JOIN branches b ON b.id = s.branch_id
		LEFT JOIN divisions dv ON dv.id = s.division_id
		LEFT JOIN users u ON u.id = s.created_by
		WHERE s.date = $1`
	if hasBranch {
		salesSQL += ` AND s.branch_id = $2`
	}
	salesSQL += ` ORDER BY s.created_at`

	queryArgs := func(extra ...any) []any {
		args := []any{date}
		return append(args, extra...)
	}
	branchArgs := func() []any {
		if hasBranch {
			return queryArgs(branchID)
		}
		return queryArgs()
	}

	posRows, err := h.pool.Query(ctx, posSQL, branchArgs()...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data POS")
		return
	}
	posImports, err := pgx.CollectRows(posRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data POS")
		return
	}

	invRows, err := h.pool.Query(ctx, invSQL, branchArgs()...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	invoices, err := pgx.CollectRows(invRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data faktur")
		return
	}

	dispRows, err := h.pool.Query(ctx, dispSQL, branchArgs()...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	dispatches, err := pgx.CollectRows(dispRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data pengiriman")
		return
	}

	opnRows, err := h.pool.Query(ctx, opnameSQL, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data opname")
		return
	}
	opnames, err := pgx.CollectRows(opnRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data opname")
		return
	}

	trRows, err := h.pool.Query(ctx, transferSQL, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	transfers, err := pgx.CollectRows(trRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data transfer")
		return
	}

	salRows, err := h.pool.Query(ctx, salesSQL, branchArgs()...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data penjualan")
		return
	}
	sales, err := pgx.CollectRows(salRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data penjualan")
		return
	}

	// Compute summary
	var posRevenue, manualSales, purchases, expenses int64
	for _, p := range posImports {
		if v, ok := p["total_amount"]; ok {
			posRevenue += toInt64(v)
		}
	}
	for _, s := range sales {
		if v, ok := s["amount"]; ok {
			manualSales += toInt64(v)
		}
	}
	for _, inv := range invoices {
		t := toInt64(inv["total"])
		switch inv["invoice_type"] {
		case "purchase":
			purchases += t
		case "expense":
			expenses += t
		}
	}

	// The day at a glance: what each branch earned and spent, and which divisions
	// carried the revenue. Read from the journal through the same helpers the
	// branch P&L uses, so a day's figures and the month's report cannot disagree
	// — and so the overview covers everything that reaches a P&L account, not
	// only what happens to appear in the tables listed below it.
	branchPerf, divisionPerf, perfErr := h.dailyPerformance(ctx, date, branchID)
	if perfErr != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung performa harian")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date":        date,
		"branches":    branchPerf,
		"divisions":   divisionPerf,
		"pos_imports": nilToEmpty(posImports),
		"invoices":    nilToEmpty(invoices),
		"dispatches":  nilToEmpty(dispatches),
		"opnames":     nilToEmpty(opnames),
		"transfers":   nilToEmpty(transfers),
		"sales":       nilToEmpty(sales),
		"summary": map[string]any{
			"pos_revenue":    posRevenue,
			"manual_sales":   manualSales,
			"purchases":      purchases,
			"expenses":       expenses,
			"dispatch_count": len(dispatches),
		},
	})
}

// dailyPerformance answers the two questions the daily report opens with: how
// did each branch do today, and which divisions made the money.
//
// Both come from `plActivityByAccount` over the single day, split with the same
// `accountBranchOwnerSQL` / `accountDivisionOwnerSQL` walks the branch and
// periodic P&L reports use. That matters more than it looks: the tables further
// down the daily report are source rows (invoices, POS imports, dispatches), and
// summing those would have produced a third definition of "what a P&L account
// did today" — one that misses payroll, Pembelanjaan Harian and opname
// write-offs, and double-counts every dispatch against its mirror invoice. The
// journal has all of it exactly once.
//
// Each account is attributed to its own owner, so no parent rollup is needed:
// an expense category and its division parent both appear, each with only its
// own postings.
func (h *ReportsHandler) dailyPerformance(ctx context.Context, date, branchFilter string) ([]map[string]any, []map[string]any, error) {
	amountOf, err := plActivityByAccount(ctx, h.pool, date, date)
	if err != nil {
		return nil, nil, err
	}

	loadOwners := func(sql string) (map[string]string, error) {
		rows, err := h.pool.Query(ctx, sql)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var accountID, ownerID pgtype.UUID
			if err := rows.Scan(&accountID, &ownerID); err != nil {
				return nil, err
			}
			if accountID.Valid && ownerID.Valid {
				out[uuidBytesToString(accountID.Bytes)] = uuidBytesToString(ownerID.Bytes)
			}
		}
		return out, rows.Err()
	}

	branchOf, err := loadOwners(accountBranchOwnerSQL)
	if err != nil {
		return nil, nil, err
	}
	divisionOf, err := loadOwners(accountDivisionOwnerSQL)
	if err != nil {
		return nil, nil, err
	}

	typeRows, err := h.pool.Query(ctx,
		`SELECT id, account_type FROM accounts WHERE account_type IN ('revenue', 'expense')`)
	if err != nil {
		return nil, nil, err
	}
	accountType := map[string]string{}
	for typeRows.Next() {
		var id pgtype.UUID
		var t string
		if err := typeRows.Scan(&id, &t); err != nil {
			typeRows.Close()
			return nil, nil, err
		}
		accountType[uuidBytesToString(id.Bytes)] = t
	}
	typeRows.Close()
	if err := typeRows.Err(); err != nil {
		return nil, nil, err
	}

	type tally struct{ revenue, expense int64 }
	byBranch := map[string]*tally{}
	byDivision := map[string]*tally{}

	for accountID, amount := range amountOf {
		if amount == 0 {
			continue
		}
		isRevenue := accountType[accountID] == "revenue"
		if branchID, ok := branchOf[accountID]; ok {
			t := byBranch[branchID]
			if t == nil {
				t = &tally{}
				byBranch[branchID] = t
			}
			if isRevenue {
				t.revenue += amount
			} else {
				t.expense += amount
			}
		}
		// A division only earns revenue through its own revenue account; its
		// expense side is reported too, since "which divisions made the most
		// money" is a poorer question than it looks if a division's costs are
		// invisible beside its takings.
		if divisionID, ok := divisionOf[accountID]; ok {
			t := byDivision[divisionID]
			if t == nil {
				t = &tally{}
				byDivision[divisionID] = t
			}
			if isRevenue {
				t.revenue += amount
			} else {
				t.expense += amount
			}
		}
	}

	// Every branch is listed, including the ones that did nothing today: a branch
	// missing from the overview reads as an oversight, while a branch showing
	// zero is a fact worth seeing on a trading day.
	branchSQL := `SELECT id, name FROM branches`
	var branchParams []any
	if branchFilter != "" {
		branchSQL += ` WHERE id = $1`
		branchParams = append(branchParams, branchFilter)
	}
	branchRows, err := h.pool.Query(ctx, branchSQL+` ORDER BY name`, branchParams...)
	if err != nil {
		return nil, nil, err
	}
	defer branchRows.Close()

	branches := []map[string]any{}
	for branchRows.Next() {
		var id pgtype.UUID
		var name string
		if err := branchRows.Scan(&id, &name); err != nil {
			return nil, nil, err
		}
		key := uuidBytesToString(id.Bytes)
		t := byBranch[key]
		if t == nil {
			t = &tally{}
		}
		branches = append(branches, map[string]any{
			"id":      key,
			"name":    name,
			"revenue": t.revenue,
			"expense": t.expense,
			"net":     t.revenue - t.expense,
		})
	}
	if err := branchRows.Err(); err != nil {
		return nil, nil, err
	}

	// Divisions, unlike branches, are only listed when they moved: there are two
	// dozen of them and a chart ranking twenty empty bars answers nothing.
	divSQL := `SELECT d.id, d.name, d.branch_id, b.name FROM divisions d JOIN branches b ON b.id = d.branch_id`
	var divParams []any
	if branchFilter != "" {
		divSQL += ` WHERE d.branch_id = $1`
		divParams = append(divParams, branchFilter)
	}
	divRows, err := h.pool.Query(ctx, divSQL+` ORDER BY b.name, d.name`, divParams...)
	if err != nil {
		return nil, nil, err
	}
	defer divRows.Close()

	divisions := []map[string]any{}
	for divRows.Next() {
		var id, branchID pgtype.UUID
		var name, branchName string
		if err := divRows.Scan(&id, &name, &branchID, &branchName); err != nil {
			return nil, nil, err
		}
		key := uuidBytesToString(id.Bytes)
		t := byDivision[key]
		if t == nil || (t.revenue == 0 && t.expense == 0) {
			continue
		}
		divisions = append(divisions, map[string]any{
			"id":          key,
			"name":        name,
			"branch_id":   uuidBytesToString(branchID.Bytes),
			"branch_name": branchName,
			"revenue":     t.revenue,
			"expense":     t.expense,
			"net":         t.revenue - t.expense,
		})
	}
	if err := divRows.Err(); err != nil {
		return nil, nil, err
	}

	return branches, divisions, nil
}

// InventoryValue — GET /api/reports/inventory-value
// Params: warehouse_id (optional), date_from (optional), date_to (optional)
func (h *ReportsHandler) InventoryValue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	warehouseID := q.Get("warehouse_id")
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")
	ctx := r.Context()

	var params []any
	conditions := []string{}
	if warehouseID != "" && warehouseID != "all" {
		params = append(params, warehouseID)
		conditions = append(conditions, "inv.warehouse_id = $"+itoa(len(params)))
	}
	if dateFrom != "" {
		params = append(params, dateFrom)
		conditions = append(conditions, "inv.date >= $"+itoa(len(params)))
	}
	if dateTo != "" {
		params = append(params, dateTo)
		conditions = append(conditions, "inv.date <= $"+itoa(len(params)))
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + joinStrings(conditions, " AND ")
	}

	sql := `
		SELECT
		  w.id AS warehouse_id, w.name AS warehouse_name,
		  COUNT(DISTINCT inv.item_id)::INT AS item_count,
		  COALESCE(SUM(inv.value), 0)::BIGINT AS total_value,
		  json_agg(json_build_object(
		    'item_id',   inv.item_id,
		    'item_name', i.name,
		    'item_code', i.code,
		    'quantity',  inv.quantity,
		    'unit_name', i.units->inv.unit_index->>'name',
		    'value',     inv.value,
		    'date',      inv.date
		  ) ORDER BY inv.value DESC NULLS LAST) AS items
		FROM inventory inv
		JOIN warehouses w ON w.id = inv.warehouse_id
		JOIN items i ON i.id = inv.item_id` + where + `
		GROUP BY w.id, w.name
		ORDER BY total_value DESC`

	rows, err := h.pool.Query(ctx, sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil laporan nilai persediaan")
		return
	}
	result, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses laporan nilai persediaan")
		return
	}

	respondJSON(w, http.StatusOK, nilToEmpty(result))
}

// ExpenseSummary — GET /api/reports/expense-summary
// Params: date_from (optional), date_to (optional)
func (h *ReportsHandler) ExpenseSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")
	ctx := r.Context()

	baseConditions := []string{"inv.invoice_type = 'expense'", "inv.branch_id IS NOT NULL"}
	var params []any
	if dateFrom != "" {
		params = append(params, dateFrom)
		baseConditions = append(baseConditions, "inv.date >= $"+itoa(len(params)))
	}
	if dateTo != "" {
		params = append(params, dateTo)
		baseConditions = append(baseConditions, "inv.date <= $"+itoa(len(params)))
	}
	where := "WHERE " + joinStrings(baseConditions, " AND ")

	branchSQL := `
		SELECT b.id AS branch_id, b.name AS branch_name,
		       COUNT(DISTINCT inv.id)::INT AS invoice_count,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
		FROM invoices inv
		JOIN branches b ON b.id = inv.branch_id
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		` + where + `
		GROUP BY b.id, b.name
		ORDER BY total_amount DESC`

	divSQL := `
		SELECT b.id AS branch_id, b.name AS branch_name,
		       dv.id AS division_id, dv.name AS division_name,
		       COUNT(DISTINCT inv.id)::INT AS invoice_count,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
		FROM invoices inv
		JOIN branches b   ON b.id  = inv.branch_id
		JOIN divisions dv ON dv.id = inv.division_id
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		` + where + `
		GROUP BY b.id, b.name, dv.id, dv.name
		ORDER BY b.name, total_amount DESC`

	branchRows, err := h.pool.Query(ctx, branchSQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan pengeluaran (cabang)")
		return
	}
	branches, err := pgx.CollectRows(branchRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data cabang")
		return
	}

	divRows, err := h.pool.Query(ctx, divSQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan pengeluaran (divisi)")
		return
	}
	divs, err := pgx.CollectRows(divRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data divisi")
		return
	}

	// Group divisions by branch_id
	divsByBranch := map[any][]map[string]any{}
	for _, d := range divs {
		bid := d["branch_id"]
		divsByBranch[bid] = append(divsByBranch[bid], d)
	}

	result := make([]map[string]any, 0, len(branches))
	for _, b := range branches {
		bid := b["branch_id"]
		divList := divsByBranch[bid]
		if divList == nil {
			divList = []map[string]any{}
		}
		merged := make(map[string]any, len(b)+1)
		for k, v := range b {
			merged[k] = v
		}
		merged["divisions"] = divList
		result = append(result, merged)
	}

	respondJSON(w, http.StatusOK, result)
}

// ExpenseReport — GET /api/expense-report
// Params: branch_id, division_id, date_from, date_to (all optional)
func (h *ReportsHandler) ExpenseReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchID := q.Get("branch_id")
	divisionID := q.Get("division_id")
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")
	ctx := r.Context()

	baseConditions := []string{"inv.invoice_type = 'expense'", "inv.branch_id IS NOT NULL"}
	var params []any
	if branchID != "" {
		params = append(params, branchID)
		baseConditions = append(baseConditions, "inv.branch_id = $"+itoa(len(params)))
	}
	if divisionID != "" {
		params = append(params, divisionID)
		baseConditions = append(baseConditions, "inv.division_id = $"+itoa(len(params)))
	}
	if dateFrom != "" {
		params = append(params, dateFrom)
		baseConditions = append(baseConditions, "inv.date >= $"+itoa(len(params)))
	}
	if dateTo != "" {
		params = append(params, dateTo)
		baseConditions = append(baseConditions, "inv.date <= $"+itoa(len(params)))
	}
	where := "WHERE " + joinStrings(baseConditions, " AND ")

	summarySQL := `
		SELECT b.id AS branch_id, b.name AS branch_name,
		       dv.id AS division_id, dv.name AS division_name,
		       COUNT(DISTINCT inv.id)::INT AS invoice_count,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
		FROM invoices inv
		JOIN branches b   ON b.id  = inv.branch_id
		JOIN divisions dv ON dv.id = inv.division_id
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		` + where + `
		GROUP BY b.id, b.name, dv.id, dv.name
		ORDER BY b.name, dv.name`

	invoiceSQL := `
		SELECT inv.id, inv.invoice_number, inv.date, inv.payment_status,
		       inv.branch_id, inv.division_id, inv.photo_path, inv.dispatch_id,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
		FROM invoices inv
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		` + where + `
		GROUP BY inv.id
		ORDER BY inv.date DESC`

	itemSQL := `
		SELECT inv.branch_id, inv.division_id,
		       ii.item_id,
		       COALESCE(it.name, ii.description) AS description,
		       SUM(ii.quantity)::BIGINT AS total_qty,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_value
		FROM invoices inv
		JOIN invoice_items ii ON ii.invoice_id = inv.id
		LEFT JOIN items it ON it.id = ii.item_id
		` + where + `
		GROUP BY inv.branch_id, inv.division_id, ii.item_id, COALESCE(it.name, ii.description)
		ORDER BY inv.branch_id, inv.division_id, total_value DESC`

	// Item usage broken down by the invoice's vendor, within each branch/division.
	vendorItemSQL := `
		SELECT inv.branch_id, inv.division_id,
		       inv.vendor_id::TEXT AS vendor_id,
		       COALESCE(vn.name, 'Tanpa Vendor') AS vendor_name,
		       COALESCE(it.name, ii.description) AS description,
		       SUM(ii.quantity)::BIGINT AS total_qty,
		       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_value
		FROM invoices inv
		JOIN invoice_items ii ON ii.invoice_id = inv.id
		LEFT JOIN items it   ON it.id = ii.item_id
		LEFT JOIN vendors vn ON vn.id = inv.vendor_id
		` + where + `
		GROUP BY inv.branch_id, inv.division_id, inv.vendor_id, vn.name, COALESCE(it.name, ii.description)
		ORDER BY inv.branch_id, inv.division_id, total_value DESC`

	summaryRows, err := h.pool.Query(ctx, summarySQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan laporan pengeluaran")
		return
	}
	summaries, err := pgx.CollectRows(summaryRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses ringkasan pengeluaran")
		return
	}

	invoiceRows, err := h.pool.Query(ctx, invoiceSQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil faktur pengeluaran")
		return
	}
	invoices, err := pgx.CollectRows(invoiceRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses faktur")
		return
	}

	itemRows, err := h.pool.Query(ctx, itemSQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pengeluaran")
		return
	}
	items, err := pgx.CollectRows(itemRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses item")
		return
	}

	vendorRows, err := h.pool.Query(ctx, vendorItemSQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengeluaran per vendor")
		return
	}
	vendorItems, err := pgx.CollectRows(vendorRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses pengeluaran vendor")
		return
	}

	// Group invoices and items by branch_id::division_id key
	invoicesByGroup := map[string][]map[string]any{}
	for _, inv := range invoices {
		key := mapGroupKey(inv, "branch_id", "division_id")
		invoicesByGroup[key] = append(invoicesByGroup[key], inv)
	}
	itemsByGroup := map[string][]map[string]any{}
	for _, it := range items {
		key := mapGroupKey(it, "branch_id", "division_id")
		itemsByGroup[key] = append(itemsByGroup[key], it)
	}

	// Build per-group vendor breakdown: each vendor holds its item list + running total.
	type vendorAgg struct {
		vendorID   string
		vendorName string
		total      int64
		items      []map[string]any
	}
	// group key -> ordered vendor list, plus an index for O(1) lookup
	vendorOrder := map[string][]*vendorAgg{}
	vendorIndex := map[string]map[string]*vendorAgg{}
	for _, vi := range vendorItems {
		gkey := mapGroupKey(vi, "branch_id", "division_id")
		vname := fmt.Sprintf("%v", vi["vendor_name"])
		vid := fmt.Sprintf("%v", vi["vendor_id"])
		if vendorIndex[gkey] == nil {
			vendorIndex[gkey] = map[string]*vendorAgg{}
		}
		va := vendorIndex[gkey][vid]
		if va == nil {
			va = &vendorAgg{vendorID: vid, vendorName: vname}
			vendorIndex[gkey][vid] = va
			vendorOrder[gkey] = append(vendorOrder[gkey], va)
		}
		va.total += toInt64(vi["total_value"])
		va.items = append(va.items, map[string]any{
			"description": vi["description"],
			"total_qty":   vi["total_qty"],
			"total_value": vi["total_value"],
		})
	}
	// Materialize into JSON-ready maps, vendors sorted by spend desc.
	vendorsByGroup := map[string][]map[string]any{}
	for gkey, vs := range vendorOrder {
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].total > vs[j].total })
		list := make([]map[string]any, 0, len(vs))
		for _, va := range vs {
			list = append(list, map[string]any{
				"vendor_id":   va.vendorID,
				"vendor_name": va.vendorName,
				"total_value": va.total,
				"items":       va.items,
			})
		}
		vendorsByGroup[gkey] = list
	}

	result := make([]map[string]any, 0, len(summaries))
	for _, g := range summaries {
		key := mapGroupKey(g, "branch_id", "division_id")
		invList := invoicesByGroup[key]
		if invList == nil {
			invList = []map[string]any{}
		}
		itList := itemsByGroup[key]
		if itList == nil {
			itList = []map[string]any{}
		}
		merged := make(map[string]any, len(g)+2)
		for k, v := range g {
			merged[k] = v
		}
		vendorList := vendorsByGroup[key]
		if vendorList == nil {
			vendorList = []map[string]any{}
		}
		merged["invoices"] = invList
		merged["item_usage"] = itList
		merged["vendor_usage"] = vendorList
		result = append(result, merged)
	}

	respondJSON(w, http.StatusOK, result)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func nilToEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case pgtype.Numeric:
		return int64(numericToFloat64(n))
	}
	return 0
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func joinStrings(ss []string, sep string) string {
	return strings.Join(ss, sep)
}

func mapGroupKey(m map[string]any, keys ...string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%v", m[k])
	}
	return strings.Join(parts, "::")
}
