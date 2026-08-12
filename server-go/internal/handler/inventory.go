package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

const (
	defaultInventoryPageSize = 25
	maxInventoryPageSize     = 200
)

// Movement labels for manual lot entry. A hand-entered lot is an adjustment, not
// a purchase: it has no invoice, no vendor and no price behind it. Labelling it
// 'purchase' (as Create used to) made it indistinguishable from real buying in
// the per-type flow breakdown on the item detail page. 'manual_in'/'manual_out'
// are the labels the legacy backend used and the ones already in the data.
const (
	manualStockInType     = "manual_in"
	manualStockOutType    = "manual_out"
	manualStockReference  = "Penyesuaian manual"
	manualStockSourceType = "inventory"
)

// recordManualStockMove writes the stock_history row behind a manual lot change.
//
// Every other movement in the system leaves a stock_history row, which is what
// lets the item history reconstruct on-hand and what the flow reports read.
// Manual lot edits and deletions used to skip it entirely, so stock could change
// with no movement to show for it — the quantity simply differed from the sum of
// its own history. Quantity and value are signed the same way as everywhere
// else: positive is stock (and value) coming in.
func recordManualStockMove(
	ctx context.Context,
	qtx *db.Queries,
	itemID, warehouseID uuid.UUID,
	qtyChange float64,
	valueChange int64,
	unitName string,
	date time.Time,
) error {
	if qtyChange == 0 && valueChange == 0 {
		return nil
	}
	moveType := manualStockInType
	// A pure revaluation leaves quantity alone, so fall back to the value's
	// direction to decide which way the movement reads.
	if qtyChange < 0 || (qtyChange == 0 && valueChange < 0) {
		moveType = manualStockOutType
	}
	return service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         itemID,
		WarehouseID:    warehouseID,
		QuantityChange: qtyChange,
		UnitName:       unitName,
		Type:           moveType,
		Reference:      manualStockReference,
		Date:           date,
		Value:          valueChange,
		SourceID:       itemID,
		SourceType:     manualStockSourceType,
	})
}

// inventoryFilters builds the common filter params shared by the list and count queries.
func inventoryFilters(q map[string][]string) (warehouseFilter, itemFilter pgtype.UUID, search pgtype.Text, dateFromPg, dateToPg pgtype.Date) {
	get := func(key string) string {
		if v, ok := q[key]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}

	if idStr := get("warehouse_id"); idStr != "" && idStr != "all" {
		if id, err := parseUUID(idStr); err == nil {
			warehouseFilter = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	if idStr := get("item_id"); idStr != "" && idStr != "all" {
		if id, err := parseUUID(idStr); err == nil {
			itemFilter = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	if s := strings.TrimSpace(get("search")); s != "" {
		search = pgtype.Text{String: s, Valid: true}
	}
	if dateFrom := get("date_from"); dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			dateFromPg = pgtype.Date{Time: t, Valid: true}
		}
	}
	if dateTo := get("date_to"); dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			dateToPg = pgtype.Date{Time: t, Valid: true}
		}
	}
	return
}

type InventoryHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewInventoryHandler(pool *pgxpool.Pool, queries *db.Queries) *InventoryHandler {
	return &InventoryHandler{pool: pool, queries: queries}
}

func (h *InventoryHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	warehouseFilter, itemFilter, search, dateFromPg, dateToPg := inventoryFilters(q)

	pageSize := defaultInventoryPageSize
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		pageSize = v
	}
	if pageSize > maxInventoryPageSize {
		pageSize = maxInventoryPageSize
	}

	page := 1
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		page = v
	}

	sort, dir := inventorySort(q)

	rows, err := h.queries.ListInventoryPage(ctx, &db.ListInventoryPageParams{
		WarehouseID:  warehouseFilter,
		ItemID:       itemFilter,
		Search:       search,
		DateFrom:     dateFromPg,
		DateTo:       dateToPg,
		IncludeEmpty: inventoryIncludeEmpty(q),
		Sort:         sort,
		Dir:          dir,
		Limit:        int32(pageSize),
		Offset:       int32((page - 1) * pageSize),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data inventori")
		return
	}
	if rows == nil {
		rows = []*db.ListInventoryPageRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Count returns the total number of inventory lots matching the given filters,
// independent of the page of rows fetched by List.
func (h *InventoryHandler) Count(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	warehouseFilter, itemFilter, search, dateFromPg, dateToPg := inventoryFilters(q)

	count, err := h.queries.CountInventory(ctx, &db.CountInventoryParams{
		WarehouseID:  warehouseFilter,
		ItemID:       itemFilter,
		Search:       search,
		DateFrom:     dateFromPg,
		DateTo:       dateToPg,
		IncludeEmpty: inventoryIncludeEmpty(q),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung data inventori")
		return
	}
	respondJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// inventoryIncludeEmpty widens the list to depleted lots and stocked items that
// have no lot at all. Default **on**: the item you are about to run out of is
// the one worth seeing, and the old behaviour hid it precisely when it mattered.
// `?include_empty=false` restores the stock-only view.
func inventoryIncludeEmpty(q url.Values) bool {
	return q.Get("include_empty") != "false"
}

// inventorySort validates the requested ordering against a closed set. The SQL
// compares these values inside CASE expressions rather than interpolating them,
// so an unknown key cannot reach the query — but rejecting it here means the
// list falls back to a sensible order instead of an arbitrary one.
func inventorySort(q url.Values) (sort, dir string) {
	allowed := map[string]bool{"item": true, "code": true, "quantity": true, "warehouse": true, "date": true}
	sort = q.Get("sort")
	if !allowed[sort] {
		sort = "item"
	}
	dir = q.Get("dir")
	if dir != "desc" {
		dir = "asc"
	}
	return sort, dir
}

// LotHistory — GET /api/inventory/lots/{id}/history
//
// One delivery's whole life: what arrived, everything that was taken out of it,
// and what (if anything) is left. Distinct from the item's stock history, which
// interleaves every delivery of that item and cannot answer "did *this* batch
// get used up, and where did it go?".
func (h *InventoryHandler) LotHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	lot, err := h.queries.GetInventoryLotDetail(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "lot tidak ditemukan")
		return
	}

	consumptions, err := h.queries.GetLotConsumptions(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pemakaian lot")
		return
	}
	if consumptions == nil {
		consumptions = []*db.GetLotConsumptionsRow{}
	}

	// Lots consumed before per-lot tracing existed were deleted outright, so
	// their usage cannot be reconstructed. Saying so is better than showing an
	// empty list that reads as "never used".
	traced := len(consumptions) > 0 ||
		numericToFloat64(lot.ConsumedQuantity) > 0 ||
		!lot.DepletedAt.Valid

	respondJSON(w, http.StatusOK, map[string]any{
		"lot":          lot,
		"consumptions": consumptions,
		"traced":       traced,
	})
}

func (h *InventoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	row, err := h.queries.GetInventoryByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}
	respondJSON(w, http.StatusOK, row)
}

func (h *InventoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemID      string  `json:"item_id"`
		WarehouseID string  `json:"warehouse_id"`
		Quantity    float64 `json:"quantity"`
		UnitIndex   int32   `json:"unit_index"`
		UnitName    string  `json:"unit_name"`
		Value       int64   `json:"value"`
		Date        string  `json:"date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	itemID, err := parseUUID(body.ItemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "item_id tidak valid")
		return
	}
	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}
	if body.Quantity <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
		return
	}

	date := time.Now()
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		date = parsed
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	accountID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tidak ditemukan")
		return
	}

	item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "item tidak ditemukan")
		return
	}

	// Derive unit name from item if not provided in request
	unitName := body.UnitName
	if unitName == "" {
		unitName = getUnitName(item.Units, body.UnitIndex)
	}

	warehouseName := ""
	if wh, err := qtx.GetWarehouseByID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true}); err == nil {
		warehouseName = wh.Name
	}

	if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, body.Quantity, body.UnitIndex, body.Value, date); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat lot inventori")
		return
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        date,
		SourceType:  service.SourceInventory,
		SourceID:    itemID,
		Description: fmt.Sprintf("Penambahan lot manual (%s)", unitName),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(uuidFromPg(accountID), body.Value),
			service.Cr(inventoryAdjustmentAccountID(ctx, qtx), body.Value),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal penyesuaian inventori")
		return
	}

	if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
		ItemID:         itemID,
		WarehouseID:    warehouseID,
		QuantityChange: body.Quantity,
		UnitName:       unitName,
		Type:           manualStockInType,
		Reference:      manualStockReference,
		Date:           date,
		Value:          body.Value,
		SourceID:       itemID,
		SourceType:     manualStockSourceType,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	// FIFOAdd may merge into an existing lot rather than insert one, so there is
	// no single lot ID to point at — the entity is the item, matching the source
	// ID the journal entry above uses.
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:     userID,
		Username:   username,
		Action:     "CREATE",
		EntityType: "inventory",
		EntityID:   itemID,
		Description: fmt.Sprintf("Menambahkan %s %s %q di %s (nilai %s)",
			formatQty(body.Quantity), unitName, item.Name, warehouseName, formatRupiahShort(body.Value)),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"message": "lot inventori berhasil ditambahkan",
	})
}

func (h *InventoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Quantity float64 `json:"quantity"`
		Value    int64   `json:"value"`
		Date     string  `json:"date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	existing, err := qtx.GetInventoryByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}

	date := existing.Date.Time
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		date = parsed
	}

	if err := qtx.UpdateInventoryLot(ctx, &db.UpdateInventoryLotParams{
		ID:       pgtype.UUID{Bytes: id, Valid: true},
		Quantity: floatToNumeric(body.Quantity),
		Value:    body.Value,
		Date:     pgtype.Date{Time: date, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui lot")
		return
	}

	// The lot's stock moved, so the movement log has to say so — the ledger leg
	// below only covers the value, and a quantity-only edit would otherwise leave
	// no trace at all.
	if err := recordManualStockMove(ctx, qtx,
		uuidFromPg(existing.ItemID), uuidFromPg(existing.WarehouseID),
		body.Quantity-numericToFloat64(existing.Quantity),
		body.Value-existing.Value,
		getUnitName(existing.ItemUnits, existing.UnitIndex),
		date,
	); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
		return
	}

	accountID, _ := qtx.GetWarehouseInventoryAccountID(ctx, existing.WarehouseID)
	if delta := body.Value - existing.Value; delta != 0 {
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        date,
			SourceType:  service.SourceInventory,
			SourceID:    id,
			Description: "Perubahan nilai lot manual",
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: []service.Line{
				service.Dr(uuidFromPg(accountID), delta),
				service.Cr(inventoryAdjustmentAccountID(ctx, qtx), delta),
			},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal penyesuaian inventori")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:     middleware.UserIDFromCtx(ctx),
		Username:   middleware.UsernameFromCtx(ctx),
		Action:     "UPDATE",
		EntityType: "inventory",
		EntityID:   id,
		Description: fmt.Sprintf("Mengubah lot %q di %s%s",
			existing.ItemName, existing.WarehouseName,
			inventoryLotChanges(existing, body.Quantity, body.Value, date)),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "lot inventori berhasil diperbarui"})
}

func (h *InventoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	existing, err := qtx.GetInventoryByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "lot inventori tidak ditemukan")
		return
	}

	if err := qtx.DeleteInventoryLot(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus lot inventori")
		return
	}

	// Deleting a lot removes real stock; record it as an outbound movement so the
	// item's history still adds up to what is on hand.
	deletedAt := time.Now()
	if err := recordManualStockMove(ctx, qtx,
		uuidFromPg(existing.ItemID), uuidFromPg(existing.WarehouseID),
		-numericToFloat64(existing.Quantity), -existing.Value,
		getUnitName(existing.ItemUnits, existing.UnitIndex),
		deletedAt,
	); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
		return
	}

	accountID, _ := qtx.GetWarehouseInventoryAccountID(ctx, existing.WarehouseID)
	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        deletedAt,
		SourceType:  service.SourceInventory,
		SourceID:    id,
		Description: "Penghapusan lot manual",
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Cr(uuidFromPg(accountID), existing.Value),
			service.Dr(inventoryAdjustmentAccountID(ctx, qtx), existing.Value),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal penyesuaian inventori")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:     userID,
		Username:   username,
		Action:     "DELETE",
		EntityType: "inventory",
		EntityID:   id,
		Description: fmt.Sprintf("Menghapus lot %s %q di %s (nilai %s)",
			formatQty(numericToFloat64(existing.Quantity)), existing.ItemName,
			existing.WarehouseName, formatRupiahShort(existing.Value)),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "lot inventori berhasil dihapus"})
}

// formatQty renders a stock quantity without trailing zeros, so whole numbers
// read as "12" rather than "12.000000" in log descriptions.
func formatQty(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

// inventoryLotChanges describes what a lot edit changed, as a trailing clause
// like ` — jumlah: 10 → 8; nilai: Rp 1.000 → Rp 800`. A manual lot edit moves
// both stock on hand and the inventory account, so the before/after numbers are
// the point of the log entry. Returns "" when nothing changed.
func inventoryLotChanges(before *db.GetInventoryByIDRow, qty float64, value int64, date time.Time) string {
	var changes []string
	if oldQty := numericToFloat64(before.Quantity); oldQty != qty {
		changes = append(changes, fmt.Sprintf("jumlah: %s → %s", formatQty(oldQty), formatQty(qty)))
	}
	if before.Value != value {
		changes = append(changes, fmt.Sprintf("nilai: %s → %s", formatRupiahShort(before.Value), formatRupiahShort(value)))
	}
	if !before.Date.Time.Equal(date) {
		changes = append(changes, fmt.Sprintf("tanggal: %s → %s",
			before.Date.Time.Format("2006-01-02"), date.Format("2006-01-02")))
	}
	if len(changes) == 0 {
		return ""
	}
	return " — " + strings.Join(changes, "; ")
}

// getUnitName parses item units JSON and returns the unit name at the given index.
func getUnitName(unitsJSON []byte, idx int32) string {
	var units []struct {
		Name  string `json:"name"`
		Ratio any    `json:"ratio"`
	}
	if err := json.Unmarshal(unitsJSON, &units); err != nil || int(idx) >= len(units) {
		return ""
	}
	return units[idx].Name
}

// inventoryAdjustmentAccountID returns the account that carries inventory value
// appearing or disappearing outside a purchase or a dispatch — the same
// "Selisih Persediaan" bucket stock opname posts its count differences to.
//
// Manual lot create/edit/delete used to move the warehouse inventory account on
// its own, conjuring asset value with no counterpart. They now post against this
// account, so a manual stock correction reads as what it is: a difference
// charged to (or credited back from) shrinkage.
func inventoryAdjustmentAccountID(ctx context.Context, qtx *db.Queries) uuid.UUID {
	acctID, err := qtx.GetInventoryVarianceAccountID(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil
		}
		created, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{
			Name:        "Selisih Persediaan",
			AccountType: "expense",
		})
		if err != nil {
			return uuid.Nil
		}
		return created.ID.Bytes
	}
	return uuidFromPg(acctID)
}
