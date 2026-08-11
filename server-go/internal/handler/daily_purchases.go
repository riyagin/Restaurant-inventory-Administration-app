package handler

import (
	"context"
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

// Pembelanjaan Harian — the branch's daily shopping, paid out of its cash box.
//
// Mechanically a purchase: stock arrives, cost is booked, FIFO lots are created,
// stock_history records the movement. What differs is settlement — cash changes
// hands at the stall, so there is no payable, no due date and no payment status.
// One journal entry does the whole thing:
//
//	Dr Persediaan - <Gudang>        (stock lines, at cost)
//	Dr Beban - <Divisi>/<Kategori>  (non-stock lines)
//	Cr Kas Kecil - <Cabang>         (the total)
//
// The credit is what makes the day's count checkable: the box should hold the
// opening amount, plus top-ups, minus the sum of these rows.

type DailyPurchasesHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewDailyPurchasesHandler(pool *pgxpool.Pool, queries *db.Queries) *DailyPurchasesHandler {
	return &DailyPurchasesHandler{pool: pool, queries: queries}
}

func (h *DailyPurchasesHandler) SetUploadsDir(dir string) { h.uploadsDir = dir }

type dailyPurchaseItemInput struct {
	ItemID           string  `json:"item_id"`
	Description      string  `json:"description"`
	Quantity         float64 `json:"quantity"`
	UnitIndex        int32   `json:"unit_index"`
	Price            int64   `json:"price"`
	ConversionFactor float64 `json:"conversion_factor"`
}

// List — GET /api/daily-purchases?branch_id=&from=&to=&status=
func (h *DailyPurchasesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := &db.ListDailyPurchasesParams{}
	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params.Column1 = pgtype.UUID{Bytes: id, Valid: true}
	}
	if v := q.Get("from"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
			return
		}
		params.Column2 = pgtype.Date{Time: d, Valid: true}
	}
	if v := q.Get("to"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
			return
		}
		params.Column3 = pgtype.Date{Time: d, Valid: true}
	}
	if v := q.Get("status"); v != "" {
		params.Column4 = pgtype.Text{String: v, Valid: true}
	}

	rows, err := h.queries.ListDailyPurchases(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pembelanjaan harian")
		return
	}
	if rows == nil {
		rows = []*db.ListDailyPurchasesRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// Get — GET /api/daily-purchases/{id}
func (h *DailyPurchasesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	purchase, err := h.queries.GetDailyPurchaseByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pembelanjaan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pembelanjaan")
		return
	}

	items, err := h.queries.GetDailyPurchaseItems(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pembelanjaan")
		return
	}
	if items == nil {
		items = []*db.GetDailyPurchaseItemsRow{}
	}

	respondJSON(w, http.StatusOK, map[string]any{"purchase": purchase, "items": items})
}

// Create — POST /api/daily-purchases
func (h *DailyPurchasesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date              string                   `json:"date"`
		BranchID          string                   `json:"branch_id"`
		DivisionID        string                   `json:"division_id"`
		WarehouseID       string                   `json:"warehouse_id"`
		VendorID          string                   `json:"vendor_id"`
		ExpenseCategoryID string                   `json:"expense_category_id"`
		Notes             string                   `json:"notes"`
		Items             []dailyPurchaseItemInput `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	if body.BranchID == "" {
		respondError(w, http.StatusBadRequest, "cabang wajib dipilih")
		return
	}

	ctx := r.Context()

	warehouseID, branchID, divisionID, vendorID, err := parseInvoiceUUIDs(
		body.WarehouseID, body.BranchID, body.DivisionID, body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	purchaseDate := time.Now()
	if body.Date != "" {
		purchaseDate, err = time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal tidak valid")
			return
		}
	}

	// The box is resolved from the branch, then frozen onto the row. Repointing
	// a branch at a different account later must not rewrite where last month's
	// money came from.
	pettyAcct, err := h.queries.GetBranchPettyCashAccountID(ctx, pgtype.UUID{Bytes: branchID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
		return
	}
	if !pettyAcct.Valid {
		respondError(w, http.StatusBadRequest,
			"cabang ini belum punya akun kas kecil — buat ulang cabang atau hubungi admin")
		return
	}

	categoryID, err := resolveExpenseCategory(ctx, h.queries, body.ExpenseCategoryID, divisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	number, err := qtx.NextDailyPurchaseNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor pembelanjaan")
		return
	}

	purchase, err := qtx.CreateDailyPurchase(ctx, &db.CreateDailyPurchaseParams{
		Number:             number,
		Date:               pgtype.Date{Time: purchaseDate, Valid: true},
		BranchID:           pgtype.UUID{Bytes: branchID, Valid: true},
		DivisionID:         pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		WarehouseID:        pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil},
		ExpenseCategoryID:  pgtype.UUID{Bytes: categoryID, Valid: categoryID != uuid.Nil},
		PettyCashAccountID: pettyAcct,
		VendorID:           pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		Notes:              strings.TrimSpace(body.Notes),
		CreatedBy:          pgUserID(ctx),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembelanjaan")
		return
	}
	purchaseID := purchase.ID.Bytes

	// The two halves of the debit. A single shopping trip routinely mixes both —
	// two crates of eggs into the warehouse and a bag of ice consumed on the
	// spot — so they are accumulated separately and posted as two debit legs of
	// one entry rather than forcing the operator to split the trip in two.
	var stockTotal, expenseTotal int64
	vendorName := ""
	if vendorID != uuid.Nil {
		if v, err := qtx.GetVendorByID(ctx, pgtype.UUID{Bytes: vendorID, Valid: true}); err == nil {
			vendorName = v.Name
		}
	}

	for _, in := range body.Items {
		if in.Quantity <= 0 {
			respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
			return
		}

		itemID := uuid.Nil
		isStockLine := false
		conv := lineConversion{Factor: 1, BaseIndex: in.UnitIndex}

		if in.ItemID != "" {
			itemID, err = parseUUID(in.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("barang tidak ditemukan: %s", in.ItemID))
				return
			}
			isStockLine = item.IsStock
			if isStockLine {
				if warehouseID == uuid.Nil {
					respondError(w, http.StatusBadRequest,
						fmt.Sprintf("gudang wajib dipilih karena %q adalah barang stok", item.Name))
					return
				}
				conv, err = resolveLineConversion(item.Units, in.UnitIndex, in.ConversionFactor)
				if err != nil {
					respondError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		} else if strings.TrimSpace(in.Description) == "" {
			// Half of daily shopping has no catalogue entry, which is fine — but
			// an unnamed, un-itemised line is just a number nobody can audit.
			respondError(w, http.StatusBadRequest, "baris tanpa barang harus diberi keterangan")
			return
		}

		lineValue := int64(float64(in.Price) * in.Quantity)
		if isStockLine {
			stockTotal += lineValue
		} else {
			expenseTotal += lineValue
		}

		if err := qtx.CreateDailyPurchaseItem(ctx, &db.CreateDailyPurchaseItemParams{
			PurchaseID:       purchase.ID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: itemID != uuid.Nil},
			Description:      strings.TrimSpace(in.Description),
			Quantity:         floatToNumeric(in.Quantity),
			UnitIndex:        pgtype.Int4{Int32: in.UnitIndex, Valid: itemID != uuid.Nil},
			Price:            in.Price,
			ConversionFactor: floatToNumeric(conv.Factor),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item pembelanjaan")
			return
		}

		if !isStockLine {
			continue
		}

		baseQty := conv.BaseQty(in.Quantity)
		if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, baseQty, conv.BaseIndex, lineValue, purchaseDate); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menambah stok")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: baseQty,
			UnitName:       conv.BaseUnitName,
			Vendor:         vendorName,
			Type:           "daily_purchase",
			Reference:      number,
			Date:           purchaseDate,
			Value:          lineValue,
			SourceID:       purchaseID,
			SourceType:     service.SourceDailyPurchase,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
	}

	total := stockTotal + expenseTotal
	if total <= 0 {
		respondError(w, http.StatusBadRequest, "total pembelanjaan harus lebih dari 0")
		return
	}
	if err := qtx.SetDailyPurchaseTotal(ctx, &db.SetDailyPurchaseTotalParams{
		TotalAmount: total,
		ID:          purchase.ID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan total pembelanjaan")
		return
	}

	// Where the debits land. Either may be unresolvable (a warehouse with no
	// inventory account, a branch with no expense account); Post routes those to
	// suspense rather than dropping the leg, which is what keeps the entry
	// balanced instead of silently one-sided.
	var inventoryAcctID, expenseAcctID uuid.UUID
	if stockTotal > 0 {
		if a, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true}); err == nil && a.Valid {
			inventoryAcctID = a.Bytes
		}
	}
	if expenseTotal > 0 {
		if a, err := invoiceExpenseAccountID(ctx, qtx, categoryID, divisionID, branchID); err == nil {
			expenseAcctID = a
		}
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        purchaseDate,
		SourceType:  service.SourceDailyPurchase,
		SourceID:    purchaseID,
		Description: fmt.Sprintf("Pembelanjaan harian %s", number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(inventoryAcctID, stockTotal),
			service.Dr(expenseAcctID, expenseTotal),
			service.Cr(pettyAcct.Bytes, total),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembelanjaan")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "daily_purchase",
		EntityID:    purchaseID,
		Description: fmt.Sprintf("Mencatat pembelanjaan harian %s sebesar %d", number, total),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembelanjaan")
		return
	}

	// The resulting box balance travels with the response. Spending is never
	// blocked for want of a recorded top-up — refusing would make the money
	// invisible, which is worse than a negative balance the UI can flag — so the
	// caller gets the number it needs to warn on.
	balance := int64(0)
	if acct, err := h.queries.GetAccountByID(ctx, pettyAcct); err == nil {
		balance = acct.Balance
	}

	purchase.TotalAmount = total
	respondJSON(w, http.StatusCreated, map[string]any{
		"purchase":           purchase,
		"petty_cash_balance": balance,
	})
}

// Cancel — POST /api/daily-purchases/{id}/cancel
//
// The row is kept rather than deleted: a day that reconciled once must keep
// reconciling the same way, and a vanished number is a hole nobody can explain.
// Stock and the ledger are unwound by appending reversals, never by rewriting
// what was posted.
func (h *DailyPurchasesHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = parseBody(r, &body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		respondError(w, http.StatusBadRequest, "alasan pembatalan wajib diisi")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	purchase, err := h.queries.GetDailyPurchaseRow(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pembelanjaan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pembelanjaan")
		return
	}
	if purchase.Status == "cancelled" {
		respondError(w, http.StatusConflict, "pembelanjaan ini sudah dibatalkan")
		return
	}

	items, err := h.queries.GetDailyPurchaseItems(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pembelanjaan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	warehouseID := purchase.WarehouseID.Bytes
	now := time.Now()
	var stockTotal, expenseTotal int64

	for _, it := range items {
		lineValue := int64(numericToFloat64(it.Quantity) * float64(it.Price))
		isStockLine := it.ItemID.Valid && it.IsStock.Valid && it.IsStock.Bool
		if !isStockLine {
			expenseTotal += lineValue
			continue
		}
		stockTotal += lineValue

		// Unwind at the factor that was *booked*, not today's catalogue figure —
		// a supplier's dus that held 20 when this was recorded still held 20.
		baseQty := numericToFloat64(it.Quantity) * storedConversionFactor(it.ConversionFactor)
		if _, err := service.FIFODeduct(ctx, qtx, it.ItemID.Bytes, warehouseID, baseQty); err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("tidak dapat dibatalkan: stok %q sudah terpakai", it.ItemName.String))
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal membalik stok")
			return
		}

		// A reversing row rather than deleting the original: the item history
		// reconstructs on-hand by summing these, and a cancellation that leaves
		// no trace is indistinguishable from goods that never arrived.
		unitName := ""
		if it.ItemUnits != nil {
			if conv, err := resolveLineConversion(it.ItemUnits, it.UnitIndex.Int32, 0); err == nil {
				unitName = conv.BaseUnitName
			}
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         it.ItemID.Bytes,
			WarehouseID:    warehouseID,
			QuantityChange: -baseQty,
			UnitName:       unitName,
			Type:           "daily_purchase_cancel",
			Reference:      purchase.Number,
			Date:           now,
			Value:          -lineValue,
			SourceID:       id,
			SourceType:     service.SourceDailyPurchase,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
	}

	// Mirror of the original entry with the legs flipped. Dated today, so a
	// cancellation never reaches back into a period already reported on.
	var inventoryAcctID, expenseAcctID uuid.UUID
	if stockTotal > 0 {
		if a, err := qtx.GetWarehouseInventoryAccountID(ctx, purchase.WarehouseID); err == nil && a.Valid {
			inventoryAcctID = a.Bytes
		}
	}
	if expenseTotal > 0 {
		categoryID := uuid.Nil
		if purchase.ExpenseCategoryID.Valid {
			categoryID = purchase.ExpenseCategoryID.Bytes
		}
		if a, err := invoiceExpenseAccountID(ctx, qtx, categoryID,
			purchase.DivisionID.Bytes, purchase.BranchID.Bytes); err == nil {
			expenseAcctID = a
		}
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        now,
		SourceType:  service.SourceDailyPurchase,
		SourceID:    id,
		Description: fmt.Sprintf("Pembatalan pembelanjaan harian %s", purchase.Number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Cr(inventoryAcctID, stockTotal),
			service.Cr(expenseAcctID, expenseTotal),
			service.Dr(purchase.PettyCashAccountID.Bytes, stockTotal+expenseTotal),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan")
		return
	}

	if err := qtx.CancelDailyPurchase(ctx, &db.CancelDailyPurchaseParams{
		CancelledBy:  pgUserID(ctx),
		CancelReason: body.Reason,
		ID:           pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pembelanjaan")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CANCEL",
		EntityType:  "daily_purchase",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan pembelanjaan harian %s: %s", purchase.Number, body.Reason),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "pembelanjaan berhasil dibatalkan"})
}

// pgUserID is the acting user as a pgtype.UUID, NULL when the request carries
// no identity (a device key, a background sweep).
func pgUserID(ctx context.Context) pgtype.UUID {
	uid := middleware.UserIDFromCtx(ctx)
	return pgtype.UUID{Bytes: uid, Valid: uid != uuid.Nil}
}
