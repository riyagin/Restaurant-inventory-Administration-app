package handler

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StatsHandler struct {
	pool *pgxpool.Pool
}

func NewStatsHandler(pool *pgxpool.Pool) *StatsHandler {
	return &StatsHandler{pool: pool}
}

func periodRange(period string) (start, end string) {
	today := time.Now()
	end = today.Format("2006-01-02")
	switch period {
	case "weekly":
		start = today.AddDate(0, 0, -6).Format("2006-01-02")
	case "monthly":
		start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()).Format("2006-01-02")
	case "yearly":
		start = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, today.Location()).Format("2006-01-02")
	default:
		start = end
	}
	return
}

// GeneralStats — GET /api/stats
// Params: period (default "daily"), branch_id (optional)
func (h *StatsHandler) GeneralStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "daily"
	}
	branchID := q.Get("branch_id")
	periodStart, periodEnd := periodRange(period)
	ctx := r.Context()

	purchasesParams := []any{periodStart, periodEnd}
	branchClause := ""
	if branchID != "" {
		purchasesParams = append(purchasesParams, branchID)
		branchClause = " AND inv.branch_id = $3"
	}

	// Total items
	var totalItems int64
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM items`).Scan(&totalItems); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung item")
		return
	}

	// Total inventory records
	var totalInventoryRecords int64
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory`).Scan(&totalInventoryRecords); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung inventaris")
		return
	}

	// Total inventory value
	var totalInventoryValue int64
	if err := h.pool.QueryRow(ctx, `SELECT COALESCE(SUM(value), 0) AS total FROM inventory`).Scan(&totalInventoryValue); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung nilai inventaris")
		return
	}

	// Purchases total and count in period
	var purchasesTotal int64
	var purchasesCount int32
	if err := h.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total, COUNT(DISTINCT inv.id)::INT AS count
		 FROM invoices inv
		 LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		 WHERE inv.date BETWEEN $1 AND $2 AND inv.invoice_type = 'purchase'`+branchClause,
		purchasesParams...,
	).Scan(&purchasesTotal, &purchasesCount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung pembelian")
		return
	}

	// Outstanding invoices (unpaid + partial)
	outstandingRows, err := h.pool.Query(ctx,
		`SELECT inv.id, inv.invoice_number, inv.amount_paid,
		        inv.payment_status, inv.due_date, inv.date, inv.invoice_type,
		        v.name AS vendor_name,
		        COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
		 FROM invoices inv
		 LEFT JOIN vendors v ON v.id = inv.vendor_id
		 LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		 WHERE inv.payment_status IN ('unpaid','partial')
		 GROUP BY inv.id, v.name
		 ORDER BY MIN(inv.due_date) ASC NULLS LAST, MIN(inv.date) DESC`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil faktur outstanding")
		return
	}
	outstandingInvoices, err := pgx.CollectRows(outstandingRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses faktur outstanding")
		return
	}

	// Recent activity
	activityRows, err := h.pool.Query(ctx,
		`SELECT id, user_id, username, action, entity_type, description, created_at
		 FROM activity_log ORDER BY created_at DESC LIMIT 5`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil log aktivitas")
		return
	}
	recentActivity, err := pgx.CollectRows(activityRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses log aktivitas")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"totalItems":            totalItems,
		"totalInventoryRecords": totalInventoryRecords,
		"totalInventoryValue":   totalInventoryValue,
		"purchasesTotal":        purchasesTotal,
		"purchasesCount":        purchasesCount,
		"period":                period,
		"outstandingInvoices":   nilToEmpty(outstandingInvoices),
		"recentActivity":        nilToEmpty(recentActivity),
	})
}

// DailySales — GET /api/stats/daily-sales
// Params: date (required)
func (h *StatsHandler) DailySales(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		respondError(w, http.StatusBadRequest, "parameter 'date' diperlukan (YYYY-MM-DD)")
		return
	}
	ctx := r.Context()

	rows, err := h.pool.Query(ctx, `
		SELECT b.id AS branch_id, b.name AS branch_name,
		       COALESCE(s.manual_sales, 0)  AS manual_sales,
		       COALESCE(s.sale_count, 0)    AS sale_count,
		       COALESCE(pi.pos_revenue, 0)  AS pos_revenue,
		       COALESCE(pi.import_count, 0) AS pos_import_count
		FROM branches b
		LEFT JOIN (
		  SELECT branch_id, SUM(amount)::BIGINT AS manual_sales, COUNT(*)::INT AS sale_count
		  FROM sales WHERE date = $1 GROUP BY branch_id
		) s ON s.branch_id = b.id
		LEFT JOIN (
		  SELECT d.branch_id,
		         SUM(pil.amount)::BIGINT    AS pos_revenue,
		         COUNT(DISTINCT pi.id)::INT AS import_count
		  FROM pos_imports pi
		  JOIN pos_import_lines pil ON pil.import_id = pi.id AND pil.line_type = 'revenue'
		  JOIN divisions d ON d.revenue_account_id = pil.account_id
		  WHERE pi.date = $1
		  GROUP BY d.branch_id
		) pi ON pi.branch_id = b.id
		ORDER BY b.name
	`, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data penjualan harian")
		return
	}
	rawBranches, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data penjualan harian")
		return
	}

	// Compute total per branch (manual_sales + pos_revenue)
	branches := make([]map[string]any, 0, len(rawBranches))
	for _, rb := range rawBranches {
		manual := toInt64(rb["manual_sales"])
		pos := toInt64(rb["pos_revenue"])
		merged := make(map[string]any, len(rb)+1)
		for k, v := range rb {
			merged[k] = v
		}
		merged["total"] = manual + pos
		branches = append(branches, merged)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date":     date,
		"branches": branches,
	})
}

// StockFlow — GET /api/stats/stock-flow
// Params: period (default "weekly"), start, end, branch_id (all optional)
func (h *StatsHandler) StockFlow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "weekly"
	}
	startParam := q.Get("start")
	endParam := q.Get("end")
	branchID := q.Get("branch_id")

	var start, end string
	if startParam != "" && endParam != "" {
		start, end = startParam, endParam
	} else {
		start, end = periodRange(period)
	}

	hasBranch := branchID != ""
	var params []any
	if hasBranch {
		params = []any{start, end, branchID}
	} else {
		params = []any{start, end}
	}

	dispatchJoin := ""
	branchExpense := ""
	branchSales := ""
	branchPOS := ""
	if hasBranch {
		dispatchJoin = `JOIN dispatches d ON d.id = sh.source_id AND d.branch_id = $3`
		branchExpense = `AND inv.branch_id = $3`
		branchSales = `AND branch_id = $3`
		branchPOS = `JOIN divisions dv ON dv.revenue_account_id = pil.account_id AND dv.branch_id = $3`
	}

	sql := `
		SELECT
		  day::TEXT AS date,
		  COALESCE(su.stock_usage,   0)::BIGINT AS stock_usage,
		  COALESCE(ei.expense_total, 0)::BIGINT AS expense_total,
		  COALESCE(ms.manual_sales,  0)::BIGINT AS manual_sales,
		  COALESCE(pos.pos_revenue,  0)::BIGINT AS pos_revenue
		FROM (SELECT generate_series($1::date, $2::date, '1 day'::interval)::date AS day) days
		LEFT JOIN (
		  -- Dispatch value leaves stock as a negative sh.value, so -SUM gives the
		  -- outgoing value. Edit/cancel reversals post positive values that net
		  -- against it, keeping period usage correct after corrections.
		  SELECT sh.date, SUM(-sh.value)::BIGINT AS stock_usage
		  FROM stock_history sh
		  ` + dispatchJoin + `
		  WHERE sh.source_type = 'dispatch' AND sh.value IS NOT NULL AND sh.date BETWEEN $1 AND $2
		  GROUP BY sh.date
		) su ON su.date = days.day
		LEFT JOIN (
		  SELECT inv.date, SUM(ii.quantity * ii.price)::BIGINT AS expense_total
		  FROM invoices inv JOIN invoice_items ii ON ii.invoice_id = inv.id
		  WHERE inv.invoice_type = 'expense' AND inv.date BETWEEN $1 AND $2
		  ` + branchExpense + `
		  GROUP BY inv.date
		) ei ON ei.date = days.day
		LEFT JOIN (
		  SELECT date, SUM(amount)::BIGINT AS manual_sales
		  FROM sales WHERE date BETWEEN $1 AND $2
		  ` + branchSales + `
		  GROUP BY date
		) ms ON ms.date = days.day
		LEFT JOIN (
		  SELECT pi.date, SUM(pil.amount)::BIGINT AS pos_revenue
		  FROM pos_imports pi
		  JOIN pos_import_lines pil ON pil.import_id = pi.id AND pil.line_type = 'revenue'
		  ` + branchPOS + `
		  WHERE pi.date BETWEEN $1 AND $2
		  GROUP BY pi.date
		) pos ON pos.date = days.day
		ORDER BY day`

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data aliran stok")
		return
	}
	defer rows.Close()

	type chartRow struct {
		Date     string `json:"date"`
		Spend    int64  `json:"spend"`
		Revenue  int64  `json:"revenue"`
		Margin   int64  `json:"margin"`
	}
	chart := []chartRow{}
	var summarySpend, summaryRevenue, summaryMargin int64

	for rows.Next() {
		var date string
		var stockUsage, expenseTotal, manualSales, posRevenue int64
		if err := rows.Scan(&date, &stockUsage, &expenseTotal, &manualSales, &posRevenue); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data aliran stok")
			return
		}
		spend := stockUsage + expenseTotal
		revenue := manualSales + posRevenue
		margin := revenue - spend
		summarySpend += spend
		summaryRevenue += revenue
		summaryMargin += margin
		chart = append(chart, chartRow{Date: date, Spend: spend, Revenue: revenue, Margin: margin})
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data aliran stok")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"period":  period,
		"start":   start,
		"end":     end,
		"summary": map[string]int64{"spend": summarySpend, "revenue": summaryRevenue, "margin": summaryMargin},
		"chart":   chart,
	})
}
