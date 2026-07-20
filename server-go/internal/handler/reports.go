package handler

import (
	"fmt"
	"net/http"
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
// Params: start_date (or from), end_date (or to), branch_id (optional)
func (h *ReportsHandler) Financial(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startDate := firstNonEmpty(q.Get("start_date"), q.Get("from"))
	endDate := firstNonEmpty(q.Get("end_date"), q.Get("to"))
	branchID := q.Get("branch_id")
	usePeriod := startDate != "" && endDate != ""
	hasBranch := branchID != ""
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
		var sql string
		var params []any
		if hasBranch {
			params = []any{startDate, endDate, branchID}
			sql = `
WITH pos_branch_imps AS (
  SELECT DISTINCT pil.import_id
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id AND pi.date BETWEEN $1 AND $2
  JOIN divisions dv ON dv.revenue_account_id = pil.account_id AND dv.branch_id = $3
  WHERE pil.line_type = 'revenue'
),
sales_rev AS (
  SELECT COALESCE(dv.revenue_account_id, b.revenue_account_id) AS account_id,
         SUM(s.amount) AS total
  FROM sales s
  LEFT JOIN divisions dv ON dv.id = s.division_id
  LEFT JOIN branches b ON b.id = s.branch_id
  WHERE s.date BETWEEN $1 AND $2
    AND COALESCE(dv.revenue_account_id, b.revenue_account_id) IS NOT NULL
    AND COALESCE(s.branch_id, dv.branch_id) = $3
  GROUP BY 1
),
pos_rev AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  JOIN pos_branch_imps pbi ON pbi.import_id = pil.import_id
  WHERE pil.line_type = 'revenue' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
inv_exp AS (
  SELECT COALESCE(dv.expense_account_id, b.expense_account_id) AS account_id,
         COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
  FROM invoices inv
  LEFT JOIN divisions dv ON dv.id = inv.division_id
  LEFT JOIN branches b ON b.id = inv.branch_id
  LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
  WHERE inv.invoice_type = 'expense' AND inv.date BETWEEN $1 AND $2
    AND COALESCE(dv.expense_account_id, b.expense_account_id) IS NOT NULL
    AND COALESCE(inv.branch_id, dv.branch_id) = $3
  GROUP BY 1
),
pos_exp AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  JOIN pos_branch_imps pbi ON pbi.import_id = pil.import_id
  WHERE pil.line_type = 'expense' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
pos_disc AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  JOIN pos_branch_imps pbi ON pbi.import_id = pil.import_id
  WHERE pil.line_type = 'discount' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
combined AS (
  SELECT account_id, total FROM sales_rev
  UNION ALL SELECT account_id, total FROM pos_rev
  UNION ALL SELECT account_id, total FROM inv_exp
  UNION ALL SELECT account_id, total FROM pos_exp
  UNION ALL SELECT account_id, total FROM pos_disc
)
SELECT account_id, SUM(total)::BIGINT AS period_balance
FROM combined GROUP BY account_id`
		} else {
			params = []any{startDate, endDate}
			sql = `
WITH sales_rev AS (
  SELECT COALESCE(dv.revenue_account_id, b.revenue_account_id) AS account_id,
         SUM(s.amount) AS total
  FROM sales s
  LEFT JOIN divisions dv ON dv.id = s.division_id
  LEFT JOIN branches b ON b.id = s.branch_id
  WHERE s.date BETWEEN $1 AND $2
    AND COALESCE(dv.revenue_account_id, b.revenue_account_id) IS NOT NULL
  GROUP BY 1
),
pos_rev AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  WHERE pil.line_type = 'revenue' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
inv_exp AS (
  SELECT COALESCE(dv.expense_account_id, b.expense_account_id) AS account_id,
         COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
  FROM invoices inv
  LEFT JOIN divisions dv ON dv.id = inv.division_id
  LEFT JOIN branches b ON b.id = inv.branch_id
  LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
  WHERE inv.invoice_type = 'expense' AND inv.date BETWEEN $1 AND $2
    AND COALESCE(dv.expense_account_id, b.expense_account_id) IS NOT NULL
  GROUP BY 1
),
pos_exp AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  WHERE pil.line_type = 'expense' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
pos_disc AS (
  SELECT pil.account_id, SUM(pil.amount) AS total
  FROM pos_import_lines pil
  JOIN pos_imports pi ON pi.id = pil.import_id
  WHERE pil.line_type = 'discount' AND pi.date BETWEEN $1 AND $2
  GROUP BY pil.account_id
),
adj_period AS (
  SELECT account_id, SUM(amount) AS total
  FROM account_adjustments
  WHERE created_at::date BETWEEN $1 AND $2
  GROUP BY account_id
),
combined AS (
  SELECT account_id, total FROM sales_rev
  UNION ALL SELECT account_id, total FROM pos_rev
  UNION ALL SELECT account_id, total FROM inv_exp
  UNION ALL SELECT account_id, total FROM pos_exp
  UNION ALL SELECT account_id, total FROM pos_disc
  UNION ALL SELECT account_id, total FROM adj_period
)
SELECT account_id, SUM(total)::BIGINT AS period_balance
FROM combined GROUP BY account_id`
		}

		pRows, err := h.pool.Query(ctx, sql, params...)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung periode keuangan")
			return
		}
		defer pRows.Close()
		for pRows.Next() {
			var accountID pgtype.UUID
			var periodBalance int64
			if err := pRows.Scan(&accountID, &periodBalance); err == nil && accountID.Valid {
				periodMap[uuidBytesToString(accountID.Bytes)] = periodBalance
			}
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
	expenses, err := scalar(`
		SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT
		FROM invoices inv
		JOIN invoice_items ii ON ii.invoice_id = inv.id
		WHERE inv.invoice_type = 'expense' AND inv.date BETWEEN $1 AND $2`)
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
		SELECT pi.id, pi.description, pi.date, pi.total_amount, pi.source_file,
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
		SELECT inv.id, inv.invoice_number, inv.date, inv.invoice_type, inv.payment_status,
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
		SELECT d.id, d.dispatched_at, d.notes, b.name AS branch_name, dv.name AS division_name,
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
		SELECT so.id, so.performed_at, so.notes, so.operator_name, so.pic_name,
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
		SELECT st.group_id, MIN(st.transferred_at) AS transferred_at,
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
		SELECT s.id, s.date, s.amount, s.description,
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

	respondJSON(w, http.StatusOK, map[string]any{
		"date":        date,
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
		merged["invoices"] = invList
		merged["item_usage"] = itList
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
