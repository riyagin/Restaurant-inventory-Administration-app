package handler

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
)

// ExpenseSummary — GET /api/reports/expense-summary?date_from=&date_to=
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

// ExpenseReport — GET /api/expense-report?branch_id=&division_id=&date_from=&date_to=
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

	type vendorAgg struct {
		vendorID   string
		vendorName string
		total      int64
		items      []map[string]any
	}
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
