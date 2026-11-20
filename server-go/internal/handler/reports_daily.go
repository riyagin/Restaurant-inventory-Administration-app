package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Daily — GET /api/reports/daily?date=&branch_id=
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

	// Opname and transfers have no branch filter.
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

// dailyPerformance reads the day's P&L from the journal (plActivityByAccount), split by
// branch and division owner. Summing the source tables below would double-count dispatches.
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

	// Every branch is listed, even with zero activity.
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

	// Divisions are listed only when they moved.
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
