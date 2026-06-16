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

	type dispatchWithItems struct {
		*db.ListDispatchesRow
		Items []listItem `json:"items"`
	}
	result := make([]dispatchWithItems, len(rows))
	for i, row := range rows {
		items := byDispatch[row.ID]
		if items == nil {
			items = []listItem{}
		}
		result[i] = dispatchWithItems{ListDispatchesRow: row, Items: items}
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

	respondJSON(w, http.StatusOK, map[string]any{
		"dispatch": dispatch,
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

