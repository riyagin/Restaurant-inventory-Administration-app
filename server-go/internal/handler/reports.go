package handler

import (
	"net/http"

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

// Financial — GET /api/reports/financial?start_date=&end_date=
func (h *ReportsHandler) Financial(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startDate := firstNonEmpty(q.Get("start_date"), q.Get("from"))
	endDate := firstNonEmpty(q.Get("end_date"), q.Get("to"))
	usePeriod := startDate != "" && endDate != ""
	ctx := r.Context()

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
		// Period P&L comes from the journal via plActivityByAccount, shared with the branch split.
		periodMap, err = plActivityByAccount(ctx, h.pool, startDate, endDate)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung periode keuangan")
			return
		}

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
		ID               pgtype.UUID `json:"id"`
		AccountNumber    pgtype.Int4 `json:"account_number"`
		Name             string      `json:"name"`
		AccountType      string      `json:"account_type"`
		Balance          int64       `json:"balance"`
		ParentID         pgtype.UUID `json:"parent_id"`
		IsSystem         bool        `json:"is_system"`
		TotalAdjustments int64       `json:"total_adjustments"`
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

// CashSummary — GET /api/reports/cash-summary?start_date=&end_date= (both required)
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
	// Dispatch mirror invoices never touch cash; their goods were paid as purchases.
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

// InventoryValue — GET /api/reports/inventory-value?warehouse_id=&date_from=&date_to=
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
