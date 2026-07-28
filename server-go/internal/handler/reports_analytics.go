package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

// AnalyticsHandler serves the two period-comparison reports:
//   - PriceChanges: how purchase prices moved between two dates (weekly index)
//   - UsageTrend:   how item usage moved between two dates (daily breakdown)
type AnalyticsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewAnalyticsHandler(pool *pgxpool.Pool, queries *db.Queries) *AnalyticsHandler {
	return &AnalyticsHandler{pool: pool, queries: queries}
}

const dateLayout = "2006-01-02"

// parseRange reads date_from/date_to (or start_date/end_date) and validates them.
func parseRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	fromStr := firstNonEmpty(q.Get("date_from"), q.Get("start_date"), q.Get("from"))
	toStr := firstNonEmpty(q.Get("date_to"), q.Get("end_date"), q.Get("to"))
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("parameter 'date_from' dan 'date_to' diperlukan (YYYY-MM-DD)")
	}
	from, err := time.Parse(dateLayout, fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("format 'date_from' tidak valid (YYYY-MM-DD)")
	}
	to, err := time.Parse(dateLayout, toStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("format 'date_to' tidak valid (YYYY-MM-DD)")
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("'date_to' tidak boleh lebih awal dari 'date_from'")
	}
	return from, to, nil
}

// pctChange returns the percentage change from base to now. A zero base has no
// meaningful percentage, so the caller gets nil and renders "—".
func pctChange(base, now float64) *float64 {
	if base == 0 {
		return nil
	}
	v := (now - base) / math.Abs(base) * 100
	return &v
}

// ── Report 1: purchase price changes ─────────────────────────────────────────

// PriceChanges — GET /api/reports/price-changes
// Params: date_from, date_to (required), is_stock (optional: "true"/"false")
//
// Prices for different units of the same item are not comparable, so every
// rollup keys on item_id + unit_index. The headline number is a fixed-basket
// (Laspeyres) index: each item's price is weighted by the quantity purchased
// over the whole period, so a cheap item bought once cannot swamp the result.
func (h *AnalyticsHandler) PriceChanges(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()

	params := []any{from.Format(dateLayout), to.Format(dateLayout)}
	stockFilter := ""
	switch r.URL.Query().Get("is_stock") {
	case "true":
		stockFilter = " AND it.is_stock = TRUE"
	case "false":
		stockFilter = " AND it.is_stock = FALSE"
	}

	sql := `
		SELECT
		  (date_trunc('week', inv.date)::date)::text AS week_start,
		  ii.item_id::text                           AS item_id,
		  it.name                                    AS item_name,
		  COALESCE(it.code, '')                      AS item_code,
		  it.is_stock                                AS is_stock,
		  ii.unit_index                              AS unit_index,
		  COALESCE(it.units->ii.unit_index->>'name', '') AS unit_name,
		  SUM(ii.quantity)::float8                   AS quantity,
		  SUM(ii.quantity * ii.price)::bigint        AS spend,
		  COUNT(*)::int                              AS purchase_count,
		  MIN(inv.date)::text                        AS first_date,
		  MAX(inv.date)::text                        AS last_date
		FROM invoice_items ii
		JOIN invoices inv ON inv.id = ii.invoice_id
		JOIN items it     ON it.id  = ii.item_id
		WHERE inv.date BETWEEN $1 AND $2
		  AND ii.quantity > 0
		  AND ii.price > 0` + stockFilter + `
		GROUP BY 1, 2, 3, 4, 5, 6, 7
		ORDER BY 1`

	rows, err := h.pool.Query(ctx, sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data perubahan harga")
		return
	}
	defer rows.Close()

	type weekPoint struct {
		week     string
		avgPrice float64
		quantity float64
		spend    int64
	}
	type itemAgg struct {
		itemID     string
		itemName   string
		itemCode   string
		isStock    bool
		unitIndex  int32
		unitName   string
		quantity   float64
		spend      int64
		count      int
		firstDate  string
		lastDate   string
		byWeek     map[string]weekPoint
		firstWeek  string
		lastWeek   string
		firstPrice float64
		lastPrice  float64
	}

	items := map[string]*itemAgg{}
	weekSet := map[string]bool{}

	for rows.Next() {
		var (
			weekStart, itemID, itemName, itemCode, unitName string
			firstDate, lastDate                             string
			isStock                                         bool
			unitIndex                                       int32
			quantity                                        float64
			spend                                           int64
			count                                           int32
		)
		if err := rows.Scan(&weekStart, &itemID, &itemName, &itemCode, &isStock, &unitIndex,
			&unitName, &quantity, &spend, &count, &firstDate, &lastDate); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data perubahan harga")
			return
		}
		if quantity <= 0 {
			continue
		}
		weekSet[weekStart] = true
		key := fmt.Sprintf("%s|%d", itemID, unitIndex)
		agg := items[key]
		if agg == nil {
			agg = &itemAgg{
				itemID: itemID, itemName: itemName, itemCode: itemCode, isStock: isStock,
				unitIndex: unitIndex, unitName: unitName,
				firstDate: firstDate, lastDate: lastDate,
				byWeek: map[string]weekPoint{},
			}
			items[key] = agg
		}
		agg.quantity += quantity
		agg.spend += spend
		agg.count += int(count)
		if firstDate < agg.firstDate {
			agg.firstDate = firstDate
		}
		if lastDate > agg.lastDate {
			agg.lastDate = lastDate
		}
		avg := float64(spend) / quantity
		agg.byWeek[weekStart] = weekPoint{week: weekStart, avgPrice: avg, quantity: quantity, spend: spend}
		if agg.firstWeek == "" || weekStart < agg.firstWeek {
			agg.firstWeek, agg.firstPrice = weekStart, avg
		}
		if weekStart > agg.lastWeek {
			agg.lastWeek, agg.lastPrice = weekStart, avg
		}
	}
	if rows.Err() != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data perubahan harga")
		return
	}

	weeks := make([]string, 0, len(weekSet))
	for wk := range weekSet {
		weeks = append(weeks, wk)
	}
	sort.Strings(weeks)

	// Basket = every item/unit priced in at least two different weeks; only those
	// have a start and an end price to compare.
	type basketEntry struct {
		key        string
		weight     float64 // quantity purchased across the period
		firstPrice float64
		lastPrice  float64
	}
	basket := make([]basketEntry, 0, len(items))
	var baseCost, endCost, totalSpend float64
	var up, down, flat int

	type itemOut struct {
		ItemID     string   `json:"item_id"`
		ItemName   string   `json:"item_name"`
		ItemCode   string   `json:"item_code"`
		IsStock    bool     `json:"is_stock"`
		UnitIndex  int32    `json:"unit_index"`
		UnitName   string   `json:"unit_name"`
		Quantity   float64  `json:"quantity"`
		Spend      int64    `json:"spend"`
		Purchases  int      `json:"purchase_count"`
		FirstDate  string   `json:"first_date"`
		LastDate   string   `json:"last_date"`
		FirstPrice float64  `json:"first_price"`
		LastPrice  float64  `json:"last_price"`
		AvgPrice   float64  `json:"avg_price"`
		ChangePct  *float64 `json:"change_pct"`
		Impact     float64  `json:"impact"` // Rp effect on the basket: qty × (last − first)
		InBasket   bool     `json:"in_basket"`
	}

	out := make([]itemOut, 0, len(items))
	for key, a := range items {
		totalSpend += float64(a.spend)
		inBasket := a.firstWeek != a.lastWeek
		var changePct *float64
		var impact float64
		if inBasket {
			changePct = pctChange(a.firstPrice, a.lastPrice)
			impact = a.quantity * (a.lastPrice - a.firstPrice)
			basket = append(basket, basketEntry{key: key, weight: a.quantity, firstPrice: a.firstPrice, lastPrice: a.lastPrice})
			baseCost += a.quantity * a.firstPrice
			endCost += a.quantity * a.lastPrice
			switch {
			case a.lastPrice > a.firstPrice:
				up++
			case a.lastPrice < a.firstPrice:
				down++
			default:
				flat++
			}
		}
		avgPrice := 0.0
		if a.quantity > 0 {
			avgPrice = float64(a.spend) / a.quantity
		}
		out = append(out, itemOut{
			ItemID: a.itemID, ItemName: a.itemName, ItemCode: a.itemCode, IsStock: a.isStock,
			UnitIndex: a.unitIndex, UnitName: a.unitName,
			Quantity: a.quantity, Spend: a.spend, Purchases: a.count,
			FirstDate: a.firstDate, LastDate: a.lastDate,
			FirstPrice: a.firstPrice, LastPrice: a.lastPrice, AvgPrice: avgPrice,
			ChangePct: changePct, Impact: impact, InBasket: inBasket,
		})
	}
	// Biggest rupiah impact first; items outside the basket sink to the bottom.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].InBasket != out[j].InBasket {
			return out[i].InBasket
		}
		return math.Abs(out[i].Impact) > math.Abs(out[j].Impact)
	})

	// Weekly index: cost of the fixed basket priced at each week. Items not
	// purchased in a week carry their last known price forward (and their first
	// price backward), so the curve stays continuous instead of dropping to zero.
	type weekOut struct {
		WeekStart  string   `json:"week_start"`
		WeekEnd    string   `json:"week_end"`
		BasketCost float64  `json:"basket_cost"`
		IndexPct   *float64 `json:"index_pct"`  // 100 = start-of-period basket
		ChangePct  *float64 `json:"change_pct"` // vs the period's first week
		Spend      int64    `json:"spend"`
		Quantity   float64  `json:"quantity"`
		ItemCount  int      `json:"item_count"`
		PricedPct  float64  `json:"priced_pct"` // share of basket actually repriced this week
	}

	carried := make(map[string]float64, len(basket))
	for _, b := range basket {
		carried[b.key] = b.firstPrice
	}
	weekRows := make([]weekOut, 0, len(weeks))
	for _, wk := range weeks {
		var spend int64
		var quantity float64
		itemCount := 0
		var repricedWeight float64
		for key, a := range items {
			if p, ok := a.byWeek[wk]; ok {
				spend += p.spend
				quantity += p.quantity
				itemCount++
				if _, inB := carried[key]; inB {
					carried[key] = p.avgPrice
					repricedWeight += a.quantity
				}
			}
		}
		var cost float64
		for _, b := range basket {
			cost += b.weight * carried[b.key]
		}
		wkEnd := wk
		if t, err := time.Parse(dateLayout, wk); err == nil {
			end := t.AddDate(0, 0, 6)
			if end.After(to) {
				end = to
			}
			wkEnd = end.Format(dateLayout)
		}
		var indexPct, changePct *float64
		if baseCost > 0 {
			idx := cost / baseCost * 100
			indexPct = &idx
			ch := idx - 100
			changePct = &ch
		}
		pricedPct := 0.0
		if baseCost > 0 {
			var totalWeight float64
			for _, b := range basket {
				totalWeight += b.weight
			}
			if totalWeight > 0 {
				pricedPct = repricedWeight / totalWeight * 100
			}
		}
		weekRows = append(weekRows, weekOut{
			WeekStart: wk, WeekEnd: wkEnd, BasketCost: cost,
			IndexPct: indexPct, ChangePct: changePct,
			Spend: spend, Quantity: quantity, ItemCount: itemCount, PricedPct: pricedPct,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date_from": from.Format(dateLayout),
		"date_to":   to.Format(dateLayout),
		"summary": map[string]any{
			"basket_item_count": len(basket),
			"tracked_items":     len(items),
			"base_cost":         baseCost,
			"end_cost":          endCost,
			"change_pct":        pctChange(baseCost, endCost),
			"total_spend":       int64(totalSpend),
			"items_up":          up,
			"items_down":        down,
			"items_flat":        flat,
			"week_count":        len(weeks),
		},
		"weeks": weekRows,
		"items": out,
	})
}

// ── Report 2: item usage over time ───────────────────────────────────────────

// UsageTrend — GET /api/reports/usage-trend
// Params: date_from, date_to (required)
//
// "Usage" means two different things depending on the item:
//   - stock items:     value/quantity dispatched out of a warehouse
//     (stock_history rows sourced from a dispatch; edit and
//     cancel reversals post the opposite sign and net out)
//   - non-stock items: invoice lines, since a non-stock item is consumed at
//     the moment it is bought — it never sits in inventory
func (h *AnalyticsHandler) UsageTrend(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	fromStr, toStr := from.Format(dateLayout), to.Format(dateLayout)

	sql := `
		SELECT d::text AS day, item_id::text, item_name, item_code, is_stock, unit_name,
		       SUM(quantity)::float8 AS quantity,
		       SUM(value)::bigint    AS value
		FROM (
		  -- stock items: dispatched out of a warehouse
		  SELECT sh.date AS d, it.id AS item_id, it.name AS item_name,
		         COALESCE(it.code, '') AS item_code, TRUE AS is_stock,
		         COALESCE(it.units->0->>'name', '') AS unit_name,
		         -sh.quantity_change AS quantity,
		         COALESCE(-sh.value, 0) AS value
		  FROM stock_history sh
		  JOIN items it ON it.id = sh.item_id
		  WHERE sh.source_type = 'dispatch' AND sh.date BETWEEN $1 AND $2

		  UNION ALL

		  -- non-stock items: consumed when purchased
		  SELECT inv.date AS d, it.id AS item_id, it.name AS item_name,
		         COALESCE(it.code, '') AS item_code, FALSE AS is_stock,
		         COALESCE(it.units->ii.unit_index->>'name', '') AS unit_name,
		         ii.quantity AS quantity,
		         (ii.quantity * ii.price) AS value
		  FROM invoice_items ii
		  JOIN invoices inv ON inv.id = ii.invoice_id
		  JOIN items it     ON it.id  = ii.item_id
		  WHERE it.is_stock = FALSE AND inv.date BETWEEN $1 AND $2
		) u
		GROUP BY d, item_id, item_name, item_code, is_stock, unit_name
		ORDER BY d`

	rows, err := h.pool.Query(ctx, sql, fromStr, toStr)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pemakaian barang")
		return
	}
	defer rows.Close()

	type point struct {
		quantity float64
		value    int64
	}
	type itemAgg struct {
		itemID   string
		name     string
		code     string
		isStock  bool
		unitName string
		byDay    map[string]point
		totalQty float64
		totalVal int64
		days     int
	}

	items := map[string]*itemAgg{}
	dayTotals := map[string]struct {
		stockQty, nonQty float64
		stockVal, nonVal int64
	}{}

	for rows.Next() {
		var day, itemID, name, code, unitName string
		var isStock bool
		var quantity float64
		var value int64
		if err := rows.Scan(&day, &itemID, &name, &code, &isStock, &unitName, &quantity, &value); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data pemakaian barang")
			return
		}
		a := items[itemID]
		if a == nil {
			a = &itemAgg{itemID: itemID, name: name, code: code, isStock: isStock, unitName: unitName, byDay: map[string]point{}}
			items[itemID] = a
		}
		p := a.byDay[day]
		p.quantity += quantity
		p.value += value
		a.byDay[day] = p
		a.totalQty += quantity
		a.totalVal += value

		t := dayTotals[day]
		if isStock {
			t.stockQty += quantity
			t.stockVal += value
		} else {
			t.nonQty += quantity
			t.nonVal += value
		}
		dayTotals[day] = t
	}
	if rows.Err() != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data pemakaian barang")
		return
	}

	// Dense daily series — days with no movement stay in the series as zeros so
	// the chart keeps a true time axis.
	type dayOut struct {
		Date        string  `json:"date"`
		StockQty    float64 `json:"stock_quantity"`
		StockValue  int64   `json:"stock_value"`
		NonStockQty float64 `json:"nonstock_quantity"`
		NonStockVal int64   `json:"nonstock_value"`
		TotalQty    float64 `json:"total_quantity"`
		TotalValue  int64   `json:"total_value"`
		ActiveItems int     `json:"active_items"`
	}
	activePerDay := map[string]int{}
	for _, a := range items {
		for day := range a.byDay {
			activePerDay[day]++
		}
	}
	days := make([]dayOut, 0)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(dateLayout)
		t := dayTotals[key]
		days = append(days, dayOut{
			Date:     key,
			StockQty: t.stockQty, StockValue: t.stockVal,
			NonStockQty: t.nonQty, NonStockVal: t.nonVal,
			TotalQty:    t.stockQty + t.nonQty,
			TotalValue:  t.stockVal + t.nonVal,
			ActiveItems: activePerDay[key],
		})
	}

	// Per item: the fixed endpoints of the range, plus the first and last day the
	// item actually moved (the endpoints are often zero for slow-moving items, so
	// the UI lets the user compare either pair).
	type itemOut struct {
		ItemID          string   `json:"item_id"`
		ItemName        string   `json:"item_name"`
		ItemCode        string   `json:"item_code"`
		IsStock         bool     `json:"is_stock"`
		UnitName        string   `json:"unit_name"`
		StartQty        float64  `json:"start_quantity"`
		StartValue      int64    `json:"start_value"`
		EndQty          float64  `json:"end_quantity"`
		EndValue        int64    `json:"end_value"`
		QtyChangePct    *float64 `json:"qty_change_pct"`
		ValueChangePct  *float64 `json:"value_change_pct"`
		FirstActiveDate string   `json:"first_active_date"`
		FirstActiveQty  float64  `json:"first_active_quantity"`
		FirstActiveVal  int64    `json:"first_active_value"`
		LastActiveDate  string   `json:"last_active_date"`
		LastActiveQty   float64  `json:"last_active_quantity"`
		LastActiveVal   int64    `json:"last_active_value"`
		ActiveQtyPct    *float64 `json:"active_qty_change_pct"`
		ActiveValuePct  *float64 `json:"active_value_change_pct"`
		TotalQty        float64  `json:"total_quantity"`
		TotalValue      int64    `json:"total_value"`
		ActiveDays      int      `json:"active_days"`
		AvgDailyValue   float64  `json:"avg_daily_value"`
	}

	totalDays := len(days)
	out := make([]itemOut, 0, len(items))
	var sumTotalValue int64
	for _, a := range items {
		startP := a.byDay[fromStr]
		endP := a.byDay[toStr]

		dayKeys := make([]string, 0, len(a.byDay))
		for d := range a.byDay {
			dayKeys = append(dayKeys, d)
		}
		sort.Strings(dayKeys)
		a.days = len(dayKeys)

		var firstDate, lastDate string
		var firstP, lastP point
		if len(dayKeys) > 0 {
			firstDate, lastDate = dayKeys[0], dayKeys[len(dayKeys)-1]
			firstP, lastP = a.byDay[firstDate], a.byDay[lastDate]
		}
		var activeQtyPct, activeValPct *float64
		if firstDate != lastDate {
			activeQtyPct = pctChange(firstP.quantity, lastP.quantity)
			activeValPct = pctChange(float64(firstP.value), float64(lastP.value))
		}
		avgDaily := 0.0
		if totalDays > 0 {
			avgDaily = float64(a.totalVal) / float64(totalDays)
		}
		sumTotalValue += a.totalVal
		out = append(out, itemOut{
			ItemID: a.itemID, ItemName: a.name, ItemCode: a.code, IsStock: a.isStock, UnitName: a.unitName,
			StartQty: startP.quantity, StartValue: startP.value,
			EndQty: endP.quantity, EndValue: endP.value,
			QtyChangePct:    pctChange(startP.quantity, endP.quantity),
			ValueChangePct:  pctChange(float64(startP.value), float64(endP.value)),
			FirstActiveDate: firstDate, FirstActiveQty: firstP.quantity, FirstActiveVal: firstP.value,
			LastActiveDate: lastDate, LastActiveQty: lastP.quantity, LastActiveVal: lastP.value,
			ActiveQtyPct: activeQtyPct, ActiveValuePct: activeValPct,
			TotalQty: a.totalQty, TotalValue: a.totalVal, ActiveDays: a.days, AvgDailyValue: avgDaily,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TotalValue > out[j].TotalValue })

	var startDay, endDay dayOut
	if len(days) > 0 {
		startDay, endDay = days[0], days[len(days)-1]
	}
	// Comparable window for lumpy data: the first and last N days of the range,
	// where N is a seventh of the range (capped at a week).
	windowLen := len(days) / 7
	if windowLen < 1 {
		windowLen = 1
	}
	if windowLen > 7 {
		windowLen = 7
	}
	if windowLen*2 > len(days) {
		windowLen = len(days) / 2
	}
	var firstWinQty, lastWinQty float64
	var firstWinVal, lastWinVal int64
	for i := 0; i < windowLen; i++ {
		firstWinQty += days[i].TotalQty
		firstWinVal += days[i].TotalValue
		j := len(days) - 1 - i
		lastWinQty += days[j].TotalQty
		lastWinVal += days[j].TotalValue
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date_from": fromStr,
		"date_to":   toStr,
		"summary": map[string]any{
			"item_count":         len(items),
			"day_count":          totalDays,
			"total_value":        sumTotalValue,
			"start_quantity":     startDay.TotalQty,
			"start_value":        startDay.TotalValue,
			"end_quantity":       endDay.TotalQty,
			"end_value":          endDay.TotalValue,
			"qty_change_pct":     pctChange(startDay.TotalQty, endDay.TotalQty),
			"value_change_pct":   pctChange(float64(startDay.TotalValue), float64(endDay.TotalValue)),
			"window_days":        windowLen,
			"window_start_qty":   firstWinQty,
			"window_start_value": firstWinVal,
			"window_end_qty":     lastWinQty,
			"window_end_value":   lastWinVal,
			"window_qty_pct":     pctChange(firstWinQty, lastWinQty),
			"window_value_pct":   pctChange(float64(firstWinVal), float64(lastWinVal)),
		},
		"days":  days,
		"items": out,
	})
}
