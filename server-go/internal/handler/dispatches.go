package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

type DispatchesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewDispatchesHandler(pool *pgxpool.Pool, queries *db.Queries) *DispatchesHandler {
	return &DispatchesHandler{pool: pool, queries: queries}
}

// List — GET /api/dispatches
func (h *DispatchesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var fromDate, toDate pgtype.Date
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListDispatches(ctx, &db.ListDispatchesParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if rows == nil {
		rows = []*db.ListDispatchesRow{}
	}

	type listItem struct {
		ID         pgtype.UUID    `json:"id"`
		DispatchID pgtype.UUID    `json:"dispatch_id"`
		ItemID     pgtype.UUID    `json:"item_id"`
		Quantity   pgtype.Numeric `json:"quantity"`
		UnitIndex  int32          `json:"unit_index"`
		UnitName   string         `json:"unit_name"`
		ItemName   string         `json:"item_name"`
		ItemCode   pgtype.Text    `json:"item_code"`
	}
	itemRows, err := h.pool.Query(ctx, `
		SELECT di.id, di.dispatch_id, di.item_id, di.quantity, di.unit_index, di.unit_name,
		       i.name AS item_name, i.code AS item_code
		FROM dispatch_items di
		JOIN items i ON i.id = di.item_id
		ORDER BY i.name
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
		return
	}
	defer itemRows.Close()

	byDispatch := map[pgtype.UUID][]listItem{}
	for itemRows.Next() {
		var it listItem
		if err := itemRows.Scan(&it.ID, &it.DispatchID, &it.ItemID, &it.Quantity,
			&it.UnitIndex, &it.UnitName, &it.ItemName, &it.ItemCode); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
			return
		}
		byDispatch[it.DispatchID] = append(byDispatch[it.DispatchID], it)
	}

	// Status lives on a column added after the sqlc query was generated, so fetch it separately.
	statusByID := map[pgtype.UUID]string{}
	if statusRows, err := h.pool.Query(ctx, `SELECT id, status FROM dispatches`); err == nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var did pgtype.UUID
			var st string
			if statusRows.Scan(&did, &st) == nil {
				statusByID[did] = st
			}
		}
	}

	type dispatchWithItems struct {
		*db.ListDispatchesRow
		Status string     `json:"status"`
		Items  []listItem `json:"items"`
	}
	result := make([]dispatchWithItems, len(rows))
	for i, row := range rows {
		items := byDispatch[row.ID]
		if items == nil {
			items = []listItem{}
		}
		status := statusByID[row.ID]
		if status == "" {
			status = "active"
		}
		result[i] = dispatchWithItems{ListDispatchesRow: row, Status: status, Items: items}
	}

	respondJSON(w, http.StatusOK, result)
}

// Get — GET /api/dispatches/:id
func (h *DispatchesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	dispatch, err := h.queries.GetDispatchByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}

	items, err := h.queries.GetDispatchItems(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
		return
	}
	if items == nil {
		items = []*db.GetDispatchItemsRow{}
	}

	status := "active"
	_ = h.pool.QueryRow(ctx, `SELECT status FROM dispatches WHERE id = $1`,
		pgtype.UUID{Bytes: id, Valid: true}).Scan(&status)

	respondJSON(w, http.StatusOK, map[string]any{
		"dispatch": dispatch,
		"status":   status,
		"items":    items,
	})
}

// Create — POST /api/dispatches
func (h *DispatchesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID   string `json:"branch_id"`
		DivisionID string `json:"division_id"`
		WarehouseID string `json:"warehouse_id"`
		Notes      string `json:"notes"`
		Items      []struct {
			ItemID    string  `json:"item_id"`
			Quantity  float64 `json:"quantity"`
			UnitIndex int32   `json:"unit_index"`
			UnitName  string  `json:"unit_name"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.BranchID == "" {
		respondError(w, http.StatusBadRequest, "branch_id diperlukan")
		return
	}
	if body.WarehouseID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id diperlukan")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}
	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}
	divisionID := uuid.Nil
	if body.DivisionID != "" {
		divisionID, err = parseUUID(body.DivisionID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "division_id tidak valid")
			return
		}
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	// 1. Insert dispatch header
	dispatched, err := qtx.InsertDispatch(ctx, &db.InsertDispatchParams{
		BranchID:    pgtype.UUID{Bytes: branchID, Valid: true},
		DivisionID:  pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		Notes:       pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		DispatchedBy: pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat pengiriman")
		return
	}
	dispatchID := dispatched.ID.Bytes

	// 2. Process each item: FIFO deduct, insert dispatch item, insert stock history
	type itemResult struct {
		itemID       uuid.UUID
		quantity     float64
		unitIndex    int32
		unitName     string
		valueDeducted int64
	}
	results := make([]itemResult, 0, len(body.Items))
	var totalDispatchValue int64

	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		valueDeducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, it.Quantity)
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal mengurangi stok")
			return
		}

		if err := qtx.InsertDispatchItem(ctx, &db.InsertDispatchItemParams{
			DispatchID: pgtype.UUID{Bytes: dispatchID, Valid: true},
			ItemID:     pgtype.UUID{Bytes: itemID, Valid: true},
			Quantity:   floatToNumeric(it.Quantity),
			UnitIndex:  it.UnitIndex,
			UnitName:   it.UnitName,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item pengiriman")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: -it.Quantity,
			UnitName:       it.UnitName,
			Type:           "dispatch",
			Date:           dispatched.DispatchedAt.Time,
			Value:          -valueDeducted,
			SourceID:       dispatchID,
			SourceType:     "dispatch",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		results = append(results, itemResult{
			itemID:       itemID,
			quantity:     it.Quantity,
			unitIndex:    it.UnitIndex,
			unitName:     it.UnitName,
			valueDeducted: valueDeducted,
		})
		totalDispatchValue += valueDeducted
	}

	// 3. Update account balances
	expAcctID, err := invoiceExpenseAccountID(ctx, qtx, divisionID, branchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun beban")
		return
	}
	if expAcctID != uuid.Nil && totalDispatchValue > 0 {
		if err := service.UpdateBalance(ctx, qtx, expAcctID, totalDispatchValue); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban")
			return
		}
	}

	invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	if err == nil && invAcctID.Valid && totalDispatchValue > 0 {
		if err := service.UpdateBalance(ctx, qtx, invAcctID.Bytes, -totalDispatchValue); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun inventaris")
			return
		}
	}

	// 4. Auto-create expense invoice linked to this dispatch
	invNumRaw, err := qtx.GetNextInvoiceNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor faktur")
		return
	}
	invoiceNumber := fmt.Sprintf("%v", invNumRaw)

	invoiceDate := dispatched.DispatchedAt.Time

	invoice, err := qtx.CreateInvoice(ctx, &db.CreateInvoiceParams{
		InvoiceNumber: invoiceNumber,
		Date:          pgtype.Date{Time: invoiceDate, Valid: true},
		InvoiceType:   "expense",
		PaymentStatus: "dispatched",
		BranchID:      pgtype.UUID{Bytes: branchID, Valid: true},
		DivisionID:    pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat faktur otomatis")
		return
	}

	if err := qtx.SetInvoiceDispatchID(ctx, &db.SetInvoiceDispatchIDParams{
		DispatchID: pgtype.UUID{Bytes: dispatchID, Valid: true},
		ID:         invoice.ID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghubungkan faktur ke pengiriman")
		return
	}

	for _, res := range results {
		unitPrice := int64(0)
		if res.quantity > 0 {
			unitPrice = int64(float64(res.valueDeducted) / res.quantity)
		}
		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID: invoice.ID,
			ItemID:    pgtype.UUID{Bytes: res.itemID, Valid: true},
			Quantity:  floatToNumeric(res.quantity),
			UnitIndex: pgtype.Int4{Int32: res.unitIndex, Valid: true},
			Price:     unitPrice,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item faktur")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengiriman")
		return
	}

	// Fetch the full dispatch + items to return
	dispatch, _ := h.queries.GetDispatchByID(ctx, pgtype.UUID{Bytes: dispatchID, Valid: true})
	items, _ := h.queries.GetDispatchItems(ctx, pgtype.UUID{Bytes: dispatchID, Valid: true})

	branchName := ""
	if dispatch != nil {
		branchName = dispatch.BranchName
		if dispatch.DivisionName.Valid {
			branchName += "/" + dispatch.DivisionName.String
		}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Dispatch",
		EntityID:    dispatchID,
		Description: fmt.Sprintf("Pengiriman barang ke %s", branchName),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"dispatch": dispatch,
		"items":    items,
	})
}

// dispatchItemInput mirrors the item payload used by Create/Update.
type dispatchItemInput struct {
	ItemID    string  `json:"item_id"`
	Quantity  float64 `json:"quantity"`
	UnitIndex int32   `json:"unit_index"`
	UnitName  string  `json:"unit_name"`
}

// itemKey uniquely identifies a dispatched line by item + unit so a quantity
// change on the same unit is detected as a delta rather than a remove+add.
func itemKey(itemID uuid.UUID, unitIndex int32) string {
	return itemID.String() + "|" + strconv.Itoa(int(unitIndex))
}

// pgUUID wraps a uuid.UUID as a valid pgtype.UUID.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

const dispatchEpsilon = 0.0001

// Update — PUT /api/dispatches/:id
//
// Edits an active dispatch. Rather than rewriting the original accounting
// entries, every change is booked as a NEW signed line on the dispatch's
// auto-generated expense invoice (and a matching stock_history row), so the
// original figures stay intact as an audit trail. Inventory is physically
// reconciled for the delta (extra qty deducted FIFO, reduced qty returned to a
// new lot) and account balances are moved by the net difference.
func (h *DispatchesHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		BranchID   string              `json:"branch_id"`
		DivisionID string              `json:"division_id"`
		Notes      string              `json:"notes"`
		Items      []dispatchItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.BranchID == "" {
		respondError(w, http.StatusBadRequest, "branch_id diperlukan")
		return
	}
	if body.DivisionID == "" {
		respondError(w, http.StatusBadRequest, "division_id diperlukan")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	newBranchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}
	newDivisionID, err := parseUUID(body.DivisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "division_id tidak valid")
		return
	}

	// Build the desired new state keyed by item+unit.
	type lineState struct {
		itemID    uuid.UUID
		quantity  float64
		unitIndex int32
		unitName  string
	}
	newByKey := map[string]*lineState{}
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}
		if it.Quantity <= 0 {
			respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
			return
		}
		key := itemKey(itemID, it.UnitIndex)
		if ex, ok := newByKey[key]; ok {
			ex.quantity += it.Quantity
		} else {
			newByKey[key] = &lineState{itemID: itemID, quantity: it.Quantity, unitIndex: it.UnitIndex, unitName: it.UnitName}
		}
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Load current dispatch header.
	var curBranch, curDivision, curWarehouse pgtype.UUID
	var status string
	err = tx.QueryRow(ctx,
		`SELECT branch_id, division_id, warehouse_id, status FROM dispatches WHERE id = $1`,
		pgUUID(id)).Scan(&curBranch, &curDivision, &curWarehouse, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "pengiriman sudah dibatalkan dan tidak dapat diubah")
		return
	}
	warehouseID := curWarehouse.Bytes

	// Load current item quantities keyed by item+unit.
	oldByKey := map[string]*lineState{}
	{
		rows, err := tx.Query(ctx,
			`SELECT item_id, quantity, unit_index, unit_name FROM dispatch_items WHERE dispatch_id = $1`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty pgtype.Numeric
			var unitIndex int32
			var unitName string
			if err := rows.Scan(&itemID, &qty, &unitIndex, &unitName); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
				return
			}
			key := itemKey(itemID.Bytes, unitIndex)
			oldByKey[key] = &lineState{itemID: itemID.Bytes, quantity: numericToFloat64(qty), unitIndex: unitIndex, unitName: unitName}
		}
		rows.Close()
	}

	// Linked auto-invoice + booked value per item (used to cost returns).
	var invID pgtype.UUID
	_ = tx.QueryRow(ctx,
		`SELECT id FROM invoices WHERE dispatch_id = $1 ORDER BY created_at LIMIT 1`, pgUUID(id)).Scan(&invID)

	type costRec struct {
		qty   float64
		value int64
	}
	costByItem := map[uuid.UUID]costRec{}
	var currentBookedValue int64
	if invID.Valid {
		rows, err := tx.Query(ctx,
			`SELECT item_id, COALESCE(SUM(quantity),0), COALESCE(SUM(quantity*price),0)::bigint
			 FROM invoice_items WHERE invoice_id = $1 GROUP BY item_id`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil nilai faktur")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty pgtype.Numeric
			var value int64
			if err := rows.Scan(&itemID, &qty, &value); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca nilai faktur")
				return
			}
			costByItem[itemID.Bytes] = costRec{qty: numericToFloat64(qty), value: value}
			currentBookedValue += value
		}
		rows.Close()
	}

	now := time.Now()
	var netExpenseDelta int64 // positive = more expense booked

	// Walk the union of old and new keys and book each difference as a new line.
	keys := map[string]struct{}{}
	for k := range oldByKey {
		keys[k] = struct{}{}
	}
	for k := range newByKey {
		keys[k] = struct{}{}
	}

	for key := range keys {
		var itemID uuid.UUID
		var unitIndex int32
		var unitName string
		oldQty, newQty := 0.0, 0.0
		if o, ok := oldByKey[key]; ok {
			oldQty = o.quantity
			itemID, unitIndex, unitName = o.itemID, o.unitIndex, o.unitName
		}
		if n, ok := newByKey[key]; ok {
			newQty = n.quantity
			itemID, unitIndex, unitName = n.itemID, n.unitIndex, n.unitName
		}

		switch {
		case newQty > oldQty+dispatchEpsilon:
			delta := newQty - oldQty
			extraValue, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, delta)
			if err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity, err.Error())
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal mengurangi stok")
				return
			}
			price := int64(0)
			if delta > 0 {
				price = int64(float64(extraValue) / delta)
			}
			if invID.Valid {
				if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
					InvoiceID:   invID,
					ItemID:      pgUUID(itemID),
					Quantity:    floatToNumeric(delta),
					UnitIndex:   pgtype.Int4{Int32: unitIndex, Valid: true},
					Price:       price,
					Description: pgtype.Text{String: "Koreksi pengiriman (tambah)", Valid: true},
				}); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi faktur")
					return
				}
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: warehouseID, QuantityChange: -delta, UnitName: unitName,
				Type: "dispatch_edit", Date: now, Value: -extraValue, SourceID: id, SourceType: "dispatch",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			netExpenseDelta += extraValue

		case newQty < oldQty-dispatchEpsilon:
			ret := oldQty - newQty
			c := costByItem[itemID]
			unitCost := 0.0
			if c.qty > 0 {
				unitCost = float64(c.value) / c.qty
			}
			retValue := int64(unitCost * ret)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, ret, unitIndex, retValue, now); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok")
				return
			}
			if invID.Valid {
				if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
					InvoiceID:   invID,
					ItemID:      pgUUID(itemID),
					Quantity:    floatToNumeric(-ret),
					UnitIndex:   pgtype.Int4{Int32: unitIndex, Valid: true},
					Price:       int64(unitCost),
					Description: pgtype.Text{String: "Koreksi pengiriman (kurang)", Valid: true},
				}); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi faktur")
					return
				}
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID: itemID, WarehouseID: warehouseID, QuantityChange: ret, UnitName: unitName,
				Type: "dispatch_edit", Date: now, Value: retValue, SourceID: id, SourceType: "dispatch",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
			netExpenseDelta -= retValue
		}
	}

	// Move account balances. If the branch/division changed, relocate the whole
	// booked expense to the new expense account, then apply the net item delta.
	oldExpAcct, _ := invoiceExpenseAccountID(ctx, qtx, uuidFromPg(curDivision), uuidFromPg(curBranch))
	newExpAcct, _ := invoiceExpenseAccountID(ctx, qtx, newDivisionID, newBranchID)
	invAcct, _ := qtx.GetWarehouseInventoryAccountID(ctx, curWarehouse)

	if oldExpAcct != newExpAcct && oldExpAcct != uuid.Nil && newExpAcct != uuid.Nil && currentBookedValue != 0 {
		if err := service.UpdateBalance(ctx, qtx, oldExpAcct, -currentBookedValue); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban lama")
			return
		}
		if err := service.UpdateBalance(ctx, qtx, newExpAcct, currentBookedValue); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban baru")
			return
		}
	}
	if newExpAcct != uuid.Nil && netExpenseDelta != 0 {
		if err := service.UpdateBalance(ctx, qtx, newExpAcct, netExpenseDelta); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban")
			return
		}
	}
	if invAcct.Valid && netExpenseDelta != 0 {
		if err := service.UpdateBalance(ctx, qtx, invAcct.Bytes, -netExpenseDelta); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun inventaris")
			return
		}
	}

	// Re-point the invoice header so period reports attribute lines correctly.
	if invID.Valid {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices SET branch_id = $1, division_id = $2 WHERE id = $3`,
			pgUUID(newBranchID), pgUUID(newDivisionID), invID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui faktur")
			return
		}
	}

	// Replace dispatch_items to reflect the current physical state.
	if _, err := tx.Exec(ctx, `DELETE FROM dispatch_items WHERE dispatch_id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui item pengiriman")
		return
	}
	for _, n := range newByKey {
		if err := qtx.InsertDispatchItem(ctx, &db.InsertDispatchItemParams{
			DispatchID: pgUUID(id),
			ItemID:     pgUUID(n.itemID),
			Quantity:   floatToNumeric(n.quantity),
			UnitIndex:  n.unitIndex,
			UnitName:   n.unitName,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item pengiriman")
			return
		}
	}

	// Update dispatch header.
	if _, err := tx.Exec(ctx,
		`UPDATE dispatches SET branch_id = $1, division_id = $2, notes = $3, updated_at = now() WHERE id = $4`,
		pgUUID(newBranchID), pgUUID(newDivisionID),
		pgtype.Text{String: body.Notes, Valid: body.Notes != ""}, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui pengiriman")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan pengiriman")
		return
	}

	dispatch, _ := h.queries.GetDispatchByID(ctx, pgUUID(id))
	updatedItems, _ := h.queries.GetDispatchItems(ctx, pgUUID(id))

	branchName := ""
	if dispatch != nil {
		branchName = dispatch.BranchName
		if dispatch.DivisionName.Valid {
			branchName += "/" + dispatch.DivisionName.String
		}
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: userID, Username: username, Action: "UPDATE", EntityType: "Dispatch",
		EntityID: id, Description: fmt.Sprintf("Perubahan pengiriman barang ke %s", branchName),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"dispatch": dispatch,
		"items":    updatedItems,
	})
}

// Delete — DELETE /api/dispatches/:id
//
// Cancels a dispatch without erasing it: stock is returned to inventory, the
// account balances are reversed, and reversing lines are appended to the
// auto-invoice (so it nets to zero) — the dispatch is flagged 'cancelled' and
// kept for the audit trail.
func (h *DispatchesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	var branchID, divisionID, warehouseID pgtype.UUID
	var status string
	err = tx.QueryRow(ctx,
		`SELECT branch_id, division_id, warehouse_id, status FROM dispatches WHERE id = $1`,
		pgUUID(id)).Scan(&branchID, &divisionID, &warehouseID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "pengiriman sudah dibatalkan")
		return
	}
	whID := warehouseID.Bytes

	// Unit + name per item, so returned lots keep the dispatched unit.
	unitByItem := map[uuid.UUID]int32{}
	nameByItem := map[uuid.UUID]string{}
	{
		rows, err := tx.Query(ctx,
			`SELECT item_id, unit_index, unit_name FROM dispatch_items WHERE dispatch_id = $1`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var unitIndex int32
			var unitName string
			if err := rows.Scan(&itemID, &unitIndex, &unitName); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
				return
			}
			if _, ok := unitByItem[itemID.Bytes]; !ok {
				unitByItem[itemID.Bytes] = unitIndex
				nameByItem[itemID.Bytes] = unitName
			}
		}
		rows.Close()
	}

	var invID pgtype.UUID
	_ = tx.QueryRow(ctx,
		`SELECT id FROM invoices WHERE dispatch_id = $1 ORDER BY created_at LIMIT 1`, pgUUID(id)).Scan(&invID)

	// Net booked qty/value per item drives the physical return.
	type agg struct {
		qty   float64
		value int64
	}
	perItem := map[uuid.UUID]*agg{}
	var totalBooked int64
	if invID.Valid {
		rows, err := tx.Query(ctx,
			`SELECT item_id, COALESCE(SUM(quantity),0), COALESCE(SUM(quantity*price),0)::bigint
			 FROM invoice_items WHERE invoice_id = $1 GROUP BY item_id`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil nilai faktur")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty pgtype.Numeric
			var value int64
			if err := rows.Scan(&itemID, &qty, &value); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca nilai faktur")
				return
			}
			perItem[itemID.Bytes] = &agg{qty: numericToFloat64(qty), value: value}
			totalBooked += value
		}
		rows.Close()
	}

	now := time.Now()

	// Return stock for each item.
	for itemID, a := range perItem {
		if a.qty <= dispatchEpsilon {
			continue
		}
		unitIndex := unitByItem[itemID]
		unitName := nameByItem[itemID]
		if err := service.FIFOAdd(ctx, qtx, itemID, whID, a.qty, unitIndex, a.value, now); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: itemID, WarehouseID: whID, QuantityChange: a.qty, UnitName: unitName,
			Type: "dispatch_cancel", Date: now, Value: a.value, SourceID: id, SourceType: "dispatch",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
	}

	// Append exact reversing lines so the invoice nets to zero.
	if invID.Valid {
		type invLine struct {
			itemID    pgtype.UUID
			quantity  pgtype.Numeric
			unitIndex pgtype.Int4
			price     int64
		}
		var lines []invLine
		rows, err := tx.Query(ctx,
			`SELECT item_id, quantity, unit_index, price FROM invoice_items WHERE invoice_id = $1`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item faktur")
			return
		}
		for rows.Next() {
			var l invLine
			if err := rows.Scan(&l.itemID, &l.quantity, &l.unitIndex, &l.price); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item faktur")
				return
			}
			lines = append(lines, l)
		}
		rows.Close()

		for _, l := range lines {
			negQty := floatToNumeric(-numericToFloat64(l.quantity))
			if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
				InvoiceID:   invID,
				ItemID:      l.itemID,
				Quantity:    negQty,
				UnitIndex:   l.unitIndex,
				Price:       l.price,
				Description: pgtype.Text{String: "Pembatalan pengiriman", Valid: true},
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan faktur")
				return
			}
		}
	}

	// Reverse account balances.
	expAcct, _ := invoiceExpenseAccountID(ctx, qtx, uuidFromPg(divisionID), uuidFromPg(branchID))
	invAcct, _ := qtx.GetWarehouseInventoryAccountID(ctx, warehouseID)
	if expAcct != uuid.Nil && totalBooked != 0 {
		if err := service.UpdateBalance(ctx, qtx, expAcct, -totalBooked); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban")
			return
		}
	}
	if invAcct.Valid && totalBooked != 0 {
		if err := service.UpdateBalance(ctx, qtx, invAcct.Bytes, totalBooked); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun inventaris")
			return
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE dispatches SET status = 'cancelled', updated_at = now() WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengiriman")
		return
	}
	if invID.Valid {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices SET payment_status = 'cancelled' WHERE id = $1`, invID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status faktur")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengiriman")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: userID, Username: username, Action: "DELETE", EntityType: "Dispatch",
		EntityID: id, Description: "Pembatalan pengiriman barang",
	})

	respondJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

// uuidFromPg converts a pgtype.UUID to uuid.UUID (uuid.Nil when invalid).
func uuidFromPg(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return p.Bytes
}

