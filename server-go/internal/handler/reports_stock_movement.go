package handler

import (
	"fmt"
	"net/http"
	"sort"
)

// stockMovementMaxDays bounds the item × day matrix. The page renders one column
// per day, so an unbounded range would be unreadable long before it was slow.
const stockMovementMaxDays = 93

// StockMovement — GET /api/reports/stock-movement
// Params: date_from, date_to (required), mode (opname | opname_dispatch,
// default opname), warehouse_id (optional)
//
// Day-by-day change in every stock item, read straight from stock_history.
// Two views:
//   - opname:          only what stock opname corrected — the shrinkage (or
//     surplus) found when the shelf was counted
//   - opname_dispatch: the same plus what was dispatched to the branches, i.e.
//     everything that took stock out of the warehouse for use
//
// Rows are selected on source_type, not type: older rows carry legacy type
// labels ("SO" for opname, "pemakaian" for dispatch) under the same source.
// Dispatch edits and cancellations post opposite-signed rows under the dispatch
// source and net out here, as do opname corrections.
//
// Quantities are reported in each item's base unit (the last entry of
// items.units), since inventory — and so every stock_history quantity — is held
// in it. stock_history.unit_name is not used: opname rows often leave it blank.
func (h *AnalyticsHandler) StockMovement(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if int(to.Sub(from).Hours()/24)+1 > stockMovementMaxDays {
		respondError(w, http.StatusBadRequest, "rentang tanggal maksimal 93 hari")
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	fromStr, toStr := from.Format(dateLayout), to.Format(dateLayout)

	mode := q.Get("mode")
	var sources []string
	switch mode {
	case "", "opname":
		mode = "opname"
		sources = []string{"opname"}
	case "opname_dispatch":
		sources = []string{"opname", "dispatch"}
	default:
		respondError(w, http.StatusBadRequest, "mode tidak valid (opname | opname_dispatch)")
		return
	}

	// The warehouse filter is the last parameter of both queries below, which
	// differ in how many come before it.
	var warehouseArg []any
	warehouseFilter := func(n int) string { return "" }
	warehouseID := q.Get("warehouse_id")
	if warehouseID != "" {
		id, err := parseUUID(warehouseID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
			return
		}
		warehouseArg = []any{id}
		warehouseFilter = func(n int) string { return fmt.Sprintf(" AND sh.warehouse_id = $%d", n) }
	}

	sql := `
		SELECT sh.date::text, it.id::text, it.name, COALESCE(it.code, ''),
		       COALESCE(it.units->(jsonb_array_length(it.units) - 1)->>'name', '') AS base_unit,
		       sh.source_type,
		       SUM(sh.quantity_change)::float8 AS quantity,
		       COALESCE(SUM(sh.value), 0)::bigint AS value
		FROM stock_history sh
		JOIN items it ON it.id = sh.item_id
		WHERE sh.date BETWEEN $1 AND $2
		  AND sh.source_type = ANY($3)` + warehouseFilter(4) + `
		GROUP BY 1, 2, 3, 4, 5, 6
		ORDER BY 1`

	rows, err := h.pool.Query(ctx, sql, append([]any{fromStr, toStr, sources}, warehouseArg...)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pergerakan stok")
		return
	}
	defer rows.Close()

	type cell struct {
		OpnameQty     float64 `json:"opname_quantity"`
		OpnameValue   int64   `json:"opname_value"`
		DispatchQty   float64 `json:"dispatch_quantity"`
		DispatchValue int64   `json:"dispatch_value"`
	}
	type itemAgg struct {
		ItemID        string           `json:"item_id"`
		ItemName      string           `json:"item_name"`
		ItemCode      string           `json:"item_code"`
		UnitName      string           `json:"unit_name"`
		Days          map[string]*cell `json:"days"`
		OpnameQty     float64          `json:"opname_quantity"`
		OpnameValue   int64            `json:"opname_value"`
		DispatchQty   float64          `json:"dispatch_quantity"`
		DispatchValue int64            `json:"dispatch_value"`
		TotalQty      float64          `json:"total_quantity"`
		TotalValue    int64            `json:"total_value"`
		ActiveDays    int              `json:"active_days"`
	}

	items := map[string]*itemAgg{}
	for rows.Next() {
		var day, itemID, name, code, unit, source string
		var qty float64
		var value int64
		if err := rows.Scan(&day, &itemID, &name, &code, &unit, &source, &qty, &value); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data pergerakan stok")
			return
		}
		a := items[itemID]
		if a == nil {
			a = &itemAgg{ItemID: itemID, ItemName: name, ItemCode: code, UnitName: unit, Days: map[string]*cell{}}
			items[itemID] = a
		}
		c := a.Days[day]
		if c == nil {
			c = &cell{}
			a.Days[day] = c
		}
		switch source {
		case "opname":
			c.OpnameQty += qty
			c.OpnameValue += value
			a.OpnameQty += qty
			a.OpnameValue += value
		case "dispatch":
			c.DispatchQty += qty
			c.DispatchValue += value
			a.DispatchQty += qty
			a.DispatchValue += value
		}
		a.TotalQty += qty
		a.TotalValue += value
	}
	if rows.Err() != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data pergerakan stok")
		return
	}

	// Per-day totals. Opname gain and loss are split per item-day after netting,
	// so a correction made the same day cancels the original instead of showing
	// up as both a loss and a gain.
	type dayAgg struct {
		Date            string `json:"date"`
		OpnameGainValue int64  `json:"opname_gain_value"`
		OpnameLossValue int64  `json:"opname_loss_value"`
		DispatchValue   int64  `json:"dispatch_value"`
		NetValue        int64  `json:"net_value"`
		ItemsMoved      int    `json:"items_moved"`
		OpnameItems     int    `json:"opname_items"`
	}
	dayMap := map[string]*dayAgg{}
	for _, a := range items {
		for day, c := range a.Days {
			d := dayMap[day]
			if d == nil {
				d = &dayAgg{Date: day}
				dayMap[day] = d
			}
			if c.OpnameValue > 0 {
				d.OpnameGainValue += c.OpnameValue
			} else {
				d.OpnameLossValue += c.OpnameValue
			}
			d.DispatchValue += c.DispatchValue
			d.NetValue += c.OpnameValue + c.DispatchValue
			if c.OpnameQty != 0 || c.DispatchQty != 0 {
				d.ItemsMoved++
				a.ActiveDays++
			}
			if c.OpnameQty != 0 {
				d.OpnameItems++
			}
		}
	}

	// Dense series so the chart keeps a true time axis.
	days := make([]dayAgg, 0)
	var sumGain, sumLoss, sumDispatch int64
	opnameDays := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(dateLayout)
		if agg := dayMap[key]; agg != nil {
			days = append(days, *agg)
			sumGain += agg.OpnameGainValue
			sumLoss += agg.OpnameLossValue
			sumDispatch += agg.DispatchValue
			if agg.OpnameItems > 0 {
				opnameDays++
			}
		} else {
			days = append(days, dayAgg{Date: key})
		}
	}

	out := make([]*itemAgg, 0, len(items))
	for _, a := range items {
		if a.ActiveDays == 0 {
			continue // every movement in the range netted to zero
		}
		out = append(out, a)
	}
	// Largest value moved first; ties (and zero-valued rows) by name.
	sort.SliceStable(out, func(i, j int) bool {
		vi, vj := abs64(out[i].TotalValue), abs64(out[j].TotalValue)
		if vi != vj {
			return vi > vj
		}
		return out[i].ItemName < out[j].ItemName
	})

	// The opname sessions behind the opname rows, so the page can link a day
	// back to the count that produced it. Legacy rows may not reference a
	// session, which the LEFT JOIN tolerates.
	type opnameRef struct {
		ID            string `json:"id"`
		Date          string `json:"date"`
		WarehouseName string `json:"warehouse_name"`
		ItemCount     int    `json:"item_count"`
	}
	opRows, err := h.pool.Query(ctx, `
		SELECT sh.source_id::text, sh.date::text, COALESCE(w.name, ''), COUNT(DISTINCT sh.item_id)::int
		FROM stock_history sh
		JOIN stock_opname o ON o.id = sh.source_id
		LEFT JOIN warehouses w ON w.id = sh.warehouse_id
		WHERE sh.source_type = 'opname' AND sh.date BETWEEN $1 AND $2`+warehouseFilter(3)+`
		GROUP BY 1, 2, 3
		ORDER BY 2, 3`, append([]any{fromStr, toStr}, warehouseArg...)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar stok opname")
		return
	}
	defer opRows.Close()
	opnames := make([]opnameRef, 0)
	for opRows.Next() {
		var o opnameRef
		if err := opRows.Scan(&o.ID, &o.Date, &o.WarehouseName, &o.ItemCount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses daftar stok opname")
			return
		}
		opnames = append(opnames, o)
	}
	if opRows.Err() != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca daftar stok opname")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date_from":    fromStr,
		"date_to":      toStr,
		"mode":         mode,
		"warehouse_id": warehouseID,
		"summary": map[string]any{
			"opname_gain_value": sumGain,
			"opname_loss_value": sumLoss,
			"opname_net_value":  sumGain + sumLoss,
			"dispatch_value":    sumDispatch,
			"net_value":         sumGain + sumLoss + sumDispatch,
			"item_count":        len(out),
			"day_count":         len(days),
			"opname_days":       opnameDays,
		},
		"days":    days,
		"items":   out,
		"opnames": opnames,
	})
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
