package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Faktur distribusi: a purchase invoice whose lines are split across several
// destinations instead of landing in one header warehouse. See migration 076.
//
// A warehouse allocation behaves exactly like a normal purchase line into that
// warehouse (FIFO lot, stock_history, Dr Persediaan). A branch allocation is
// consumed on arrival and debits the division's (or branch's) expense account —
// the account a dispatch to the same place would debit — so no stock moves.

// allocationQtyTolerance absorbs float noise when a line's allocations, typed
// with decimals, are summed back up to the line quantity.
const allocationQtyTolerance = 1e-4

// invoiceAllocationInput is one destination of a line in a Create/Update payload.
// Exactly one of WarehouseID / BranchID is set; DivisionID narrows a branch.
// Quantity is in the line's entered unit.
type invoiceAllocationInput struct {
	WarehouseID string  `json:"warehouse_id"`
	BranchID    string  `json:"branch_id"`
	DivisionID  string  `json:"division_id"`
	Quantity    float64 `json:"quantity"`
}

type resolvedAllocation struct {
	warehouseID uuid.UUID
	branchID    uuid.UUID
	divisionID  uuid.UUID
	quantity    float64
	baseQty     float64
	value       int64
	debitAcctID uuid.UUID
}

// key identifies a destination for one item, so an edit can tell whether the
// goods still go where they went before.
func (a resolvedAllocation) key(itemID uuid.UUID) string {
	if a.warehouseID != uuid.Nil {
		return "w:" + a.warehouseID.String() + "|" + itemID.String()
	}
	return "b:" + a.branchID.String() + ":" + a.divisionID.String() + "|" + itemID.String()
}

type distributedLine struct {
	resolvedInvoiceLine
	itemName string
	isStock  bool
	allocs   []resolvedAllocation
}

// resolveDistributedLines validates a distributed invoice's lines and works out,
// per allocation, its base quantity, its share of the line value and the
// account it debits. Everything that can be refused is refused here, before a
// transaction is opened.
func resolveDistributedLines(ctx context.Context, q *db.Queries, conn db.DBTX, items []invoiceItemInput) ([]distributedLine, error) {
	lines := make([]distributedLine, 0, len(items))
	divisionBranch := map[uuid.UUID]uuid.UUID{}

	for i, it := range items {
		row := i + 1
		if it.ItemID == "" {
			return nil, fmt.Errorf("baris %d: item harus dipilih", row)
		}
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			return nil, fmt.Errorf("baris %d: item_id tidak valid", row)
		}
		if it.Quantity <= 0 {
			return nil, fmt.Errorf("baris %d: kuantitas harus lebih dari 0", row)
		}
		if it.Price < 0 {
			return nil, fmt.Errorf("baris %d: harga tidak boleh negatif", row)
		}
		item, err := q.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("baris %d: item tidak ditemukan", row)
		}
		conv, err := resolveLineConversion(item.Units, it.UnitIndex, it.ConversionFactor)
		if err != nil {
			return nil, fmt.Errorf("baris %d (%s): %s", row, item.Name, err.Error())
		}

		l := distributedLine{
			resolvedInvoiceLine: resolvedInvoiceLine{
				in: it, itemID: itemID, hasItemID: true, conv: conv,
				value: int64(float64(it.Price) * it.Quantity),
			},
			itemName: item.Name,
			isStock:  item.IsStock,
		}
		if it.VendorID != "" {
			if l.vendorID, err = parseUUID(it.VendorID); err != nil {
				return nil, fmt.Errorf("baris %d: vendor_id item tidak valid", row)
			}
		}

		if len(it.Allocations) == 0 {
			return nil, fmt.Errorf("baris %d (%s): belum dialokasikan ke tujuan mana pun", row, item.Name)
		}
		var sum float64
		for _, a := range it.Allocations {
			sum += a.Quantity
		}
		if math.Abs(sum-it.Quantity) > allocationQtyTolerance {
			return nil, fmt.Errorf("baris %d (%s): total alokasi %s tidak sama dengan kuantitas %s",
				row, item.Name, formatQty(sum), formatQty(it.Quantity))
		}

		var allotted int64
		for ai, a := range it.Allocations {
			if a.Quantity <= 0 {
				return nil, fmt.Errorf("baris %d (%s): kuantitas alokasi harus lebih dari 0", row, item.Name)
			}
			ra := resolvedAllocation{quantity: a.Quantity, baseQty: conv.BaseQty(a.Quantity)}

			switch {
			case a.WarehouseID != "" && a.BranchID != "":
				return nil, fmt.Errorf("baris %d (%s): satu alokasi hanya boleh ke gudang atau ke cabang", row, item.Name)
			case a.WarehouseID != "":
				if ra.warehouseID, err = parseUUID(a.WarehouseID); err != nil {
					return nil, fmt.Errorf("baris %d: warehouse_id alokasi tidak valid", row)
				}
				if !item.IsStock {
					return nil, fmt.Errorf("baris %d (%s): item non-stok tidak dapat dialokasikan ke gudang", row, item.Name)
				}
				acct, err := q.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: ra.warehouseID, Valid: true})
				if err != nil || !acct.Valid {
					return nil, fmt.Errorf("baris %d (%s): gudang tujuan tidak ditemukan atau belum punya akun persediaan", row, item.Name)
				}
				ra.debitAcctID = acct.Bytes
			case a.BranchID != "":
				if ra.branchID, err = parseUUID(a.BranchID); err != nil {
					return nil, fmt.Errorf("baris %d: branch_id alokasi tidak valid", row)
				}
				if a.DivisionID != "" {
					if ra.divisionID, err = parseUUID(a.DivisionID); err != nil {
						return nil, fmt.Errorf("baris %d: division_id alokasi tidak valid", row)
					}
					owner, ok := divisionBranch[ra.divisionID]
					if !ok {
						var b pgtype.UUID
						if err := conn.QueryRow(ctx, `SELECT branch_id FROM divisions WHERE id = $1`, ra.divisionID).Scan(&b); err != nil {
							return nil, fmt.Errorf("baris %d (%s): divisi tujuan tidak ditemukan", row, item.Name)
						}
						owner = uuidFromPg(b)
						divisionBranch[ra.divisionID] = owner
					}
					if owner != ra.branchID {
						return nil, fmt.Errorf("baris %d (%s): divisi tujuan bukan milik cabang yang dipilih", row, item.Name)
					}
				}
				acct, err := invoiceExpenseAccountID(ctx, q, uuid.Nil, ra.divisionID, ra.branchID)
				if err != nil || acct == uuid.Nil {
					return nil, fmt.Errorf("baris %d (%s): cabang/divisi tujuan belum punya akun beban", row, item.Name)
				}
				ra.debitAcctID = acct
			default:
				return nil, fmt.Errorf("baris %d (%s): tujuan alokasi belum dipilih", row, item.Name)
			}

			// Each allocation takes its proportional share; the last one takes the
			// rounding remainder so a line's shares add up to its value exactly and
			// the journal entry balances to the rupiah.
			if ai == len(it.Allocations)-1 {
				ra.value = l.value - allotted
			} else {
				ra.value = int64(math.Round(float64(l.value) * a.Quantity / it.Quantity))
			}
			allotted += ra.value
			l.allocs = append(l.allocs, ra)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

// writeDistributedLines inserts the invoice lines and their allocations and,
// for each warehouse allocation, the stock_history row. addStock=false skips
// creating lots: a price-only edit keeps the lots it already made and revalues
// them instead. Returns the invoice total.
func writeDistributedLines(ctx context.Context, qtx *db.Queries, conn db.DBTX, invoiceID uuid.UUID, invoiceNumber string, date time.Time, lines []distributedLine, addStock bool) (int64, error) {
	var total int64
	vendorNames := map[uuid.UUID]string{}
	seq := 0

	for _, l := range lines {
		total += l.value
		lineID, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:        pgtype.UUID{Bytes: invoiceID, Valid: true},
			ItemID:           pgtype.UUID{Bytes: l.itemID, Valid: true},
			VendorID:         pgtype.UUID{Bytes: l.vendorID, Valid: l.vendorID != uuid.Nil},
			Quantity:         floatToNumeric(l.in.Quantity),
			UnitIndex:        pgtype.Int4{Int32: l.in.UnitIndex, Valid: true},
			Price:            l.in.Price,
			Description:      pgtype.Text{String: l.in.Description, Valid: l.in.Description != ""},
			ConversionFactor: floatToNumeric(l.conv.Factor),
		})
		if err != nil {
			return 0, fmt.Errorf("gagal menyimpan item faktur")
		}

		vendorName := ""
		if l.vendorID != uuid.Nil {
			name, ok := vendorNames[l.vendorID]
			if !ok {
				if v, err := qtx.GetVendorByID(ctx, pgtype.UUID{Bytes: l.vendorID, Valid: true}); err == nil {
					name = v.Name
				}
				vendorNames[l.vendorID] = name
			}
			vendorName = name
		}

		for _, a := range l.allocs {
			seq++
			if _, err := conn.Exec(ctx, `
				INSERT INTO invoice_item_allocations
					(invoice_id, invoice_item_id, warehouse_id, branch_id, division_id,
					 quantity, base_quantity, value, debit_account_id, sort_order)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				invoiceID, lineID,
				pgtype.UUID{Bytes: a.warehouseID, Valid: a.warehouseID != uuid.Nil},
				pgtype.UUID{Bytes: a.branchID, Valid: a.branchID != uuid.Nil},
				pgtype.UUID{Bytes: a.divisionID, Valid: a.divisionID != uuid.Nil},
				floatToNumeric(a.quantity), floatToNumeric(a.baseQty), a.value,
				pgtype.UUID{Bytes: a.debitAcctID, Valid: a.debitAcctID != uuid.Nil},
				seq,
			); err != nil {
				return 0, fmt.Errorf("gagal menyimpan alokasi item")
			}

			if a.warehouseID == uuid.Nil {
				continue
			}
			if addStock {
				if err := service.FIFOAdd(ctx, qtx, l.itemID, a.warehouseID, a.baseQty, l.conv.BaseIndex, a.value, date); err != nil {
					return 0, fmt.Errorf("gagal menambah stok")
				}
			}
			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID:         l.itemID,
				WarehouseID:    a.warehouseID,
				QuantityChange: a.baseQty,
				UnitName:       l.conv.BaseUnitName,
				Vendor:         vendorName,
				Type:           "invoice",
				Reference:      invoiceNumber,
				Date:           date,
				Value:          a.value,
				SourceID:       invoiceID,
				SourceType:     "invoice",
			}); err != nil {
				return 0, fmt.Errorf("gagal mencatat riwayat stok")
			}
		}
	}
	return total, nil
}

// debitLinesFor sums allocation values per debited account, in first-seen
// order, so the journal entry has one line per account rather than per row.
func debitLinesFor(allocs []resolvedAllocation, credit bool) []service.Line {
	order := []uuid.UUID{}
	sums := map[uuid.UUID]int64{}
	for _, a := range allocs {
		if _, ok := sums[a.debitAcctID]; !ok {
			order = append(order, a.debitAcctID)
		}
		sums[a.debitAcctID] += a.value
	}
	out := make([]service.Line, 0, len(order))
	for _, acct := range order {
		if credit {
			out = append(out, service.Cr(acct, sums[acct]))
		} else {
			out = append(out, service.Dr(acct, sums[acct]))
		}
	}
	return out
}

func allAllocations(lines []distributedLine) []resolvedAllocation {
	var out []resolvedAllocation
	for _, l := range lines {
		out = append(out, l.allocs...)
	}
	return out
}

// storedAllocation is an allocation as booked, read back for a reversal.
type storedAllocation struct {
	resolvedAllocation
	itemID   uuid.UUID
	itemName string
}

func loadInvoiceAllocations(ctx context.Context, conn db.DBTX, invoiceID uuid.UUID) ([]storedAllocation, error) {
	rows, err := conn.Query(ctx, `
		SELECT ii.item_id, COALESCE(it.name, ''), a.warehouse_id, a.branch_id, a.division_id,
		       a.quantity, a.base_quantity, a.value, a.debit_account_id
		FROM invoice_item_allocations a
		JOIN invoice_items ii ON ii.id = a.invoice_item_id
		LEFT JOIN items it ON it.id = ii.item_id
		WHERE a.invoice_id = $1
		ORDER BY a.sort_order`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedAllocation
	for rows.Next() {
		var s storedAllocation
		var item, wh, br, dv, acct pgtype.UUID
		var qty, base pgtype.Numeric
		if err := rows.Scan(&item, &s.itemName, &wh, &br, &dv, &qty, &base, &s.value, &acct); err != nil {
			return nil, err
		}
		s.itemID, s.warehouseID, s.branchID, s.divisionID, s.debitAcctID =
			uuidFromPg(item), uuidFromPg(wh), uuidFromPg(br), uuidFromPg(dv), uuidFromPg(acct)
		s.quantity, s.baseQty = numericToFloat64(qty), numericToFloat64(base)
		out = append(out, s)
	}
	return out, rows.Err()
}

func storedToResolved(stored []storedAllocation) []resolvedAllocation {
	out := make([]resolvedAllocation, len(stored))
	for i, s := range stored {
		out[i] = s.resolvedAllocation
	}
	return out
}

// errInsufficientStock carries the item a reversal could not take back out.
type errInsufficientStock struct{ item string }

func (e errInsufficientStock) Error() string { return "stok tidak mencukupi: " + e.item }

// reverseDistributedStock takes back out of each warehouse what its allocation
// put in, at the booked base quantity, and drops the invoice's stock_history.
func reverseDistributedStock(ctx context.Context, qtx *db.Queries, invoiceID uuid.UUID, stored []storedAllocation) error {
	for _, s := range stored {
		if s.warehouseID == uuid.Nil || s.itemID == uuid.Nil {
			continue
		}
		if _, err := service.FIFODeduct(ctx, qtx, s.itemID, s.warehouseID, s.baseQty); err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				return errInsufficientStock{item: s.itemName}
			}
			return err
		}
	}
	return qtx.DeleteStockHistoryBySource(ctx, &db.DeleteStockHistoryBySourceParams{
		SourceID:   pgtype.UUID{Bytes: invoiceID, Valid: true},
		SourceType: pgtype.Text{String: "invoice", Valid: true},
	})
}

func isDistributedInvoice(ctx context.Context, conn db.DBTX, invoiceID uuid.UUID) (bool, error) {
	var d bool
	err := conn.QueryRow(ctx, `SELECT is_distributed FROM invoices WHERE id = $1`, invoiceID).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return d, err
}

// invoiceAllocationView is an allocation as the detail page and the edit form
// read it.
type invoiceAllocationView struct {
	ID            pgtype.UUID    `json:"id"`
	InvoiceItemID pgtype.UUID    `json:"invoice_item_id"`
	WarehouseID   pgtype.UUID    `json:"warehouse_id"`
	WarehouseName pgtype.Text    `json:"warehouse_name"`
	BranchID      pgtype.UUID    `json:"branch_id"`
	BranchName    pgtype.Text    `json:"branch_name"`
	DivisionID    pgtype.UUID    `json:"division_id"`
	DivisionName  pgtype.Text    `json:"division_name"`
	Quantity      pgtype.Numeric `json:"quantity"`
	Value         int64          `json:"value"`
}

func listInvoiceAllocationViews(ctx context.Context, conn db.DBTX, invoiceID uuid.UUID) ([]invoiceAllocationView, error) {
	rows, err := conn.Query(ctx, `
		SELECT a.id, a.invoice_item_id, a.warehouse_id, w.name, a.branch_id, b.name,
		       a.division_id, dv.name, a.quantity, a.value
		FROM invoice_item_allocations a
		LEFT JOIN warehouses w ON w.id = a.warehouse_id
		LEFT JOIN branches b   ON b.id = a.branch_id
		LEFT JOIN divisions dv ON dv.id = a.division_id
		WHERE a.invoice_id = $1
		ORDER BY a.sort_order`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []invoiceAllocationView{}
	for rows.Next() {
		var v invoiceAllocationView
		if err := rows.Scan(&v.ID, &v.InvoiceItemID, &v.WarehouseID, &v.WarehouseName, &v.BranchID, &v.BranchName,
			&v.DivisionID, &v.DivisionName, &v.Quantity, &v.Value); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// updateDistributed is Update for a distributed invoice. It mirrors the normal
// path: a full reverse-and-repost, unless the goods, quantities, destinations
// and date are all unchanged — then it is a price correction and only the
// difference is posted, revaluing the lots still on hand rather than pulling
// stock back out that has probably been used already.
func (h *InvoicesHandler) updateDistributed(w http.ResponseWriter, r *http.Request, id uuid.UUID, old *db.GetInvoiceWithTotalRow, oldHeader *db.GetInvoiceByIDRow, body invoiceUpdateRequest) {
	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	_, _, _, vendorID, err := parseInvoiceUUIDs("", "", "", body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if old.AmountPaid > 0 && vendorID != uuidFromPg(old.VendorID) {
		respondError(w, http.StatusConflict, "vendor tidak dapat diganti pada faktur yang sudah ada pembayarannya")
		return
	}
	accountID := uuid.Nil
	if body.AccountID != "" {
		if accountID, err = parseUUID(body.AccountID); err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
	}
	invoiceDate, dueDate, err := parseInvoiceDates(body.Date, body.DueDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	lines, err := resolveDistributedLines(ctx, h.queries, h.pool, body.Items)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	stored, err := loadInvoiceAllocations(ctx, h.pool, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil alokasi lama")
		return
	}

	// Price-only when every (destination, item) receives exactly the base
	// quantity it received before.
	oldQty, oldValue, oldAcct := map[string]float64{}, map[string]int64{}, map[string]uuid.UUID{}
	for _, s := range stored {
		k := s.key(s.itemID)
		oldQty[k] += s.baseQty
		oldValue[k] += s.value
		if _, ok := oldAcct[k]; !ok {
			oldAcct[k] = s.debitAcctID
		}
	}
	qtyDiff := map[string]float64{}
	for k, q := range oldQty {
		qtyDiff[k] = q
	}
	for _, l := range lines {
		for _, a := range l.allocs {
			qtyDiff[a.key(l.itemID)] -= a.baseQty
		}
	}
	repriceOnly := oldHeader.Date.Valid && oldHeader.Date.Time.Format("2006-01-02") == invoiceDate.Format("2006-01-02")
	for _, d := range qtyDiff {
		if math.Abs(d) > allocationQtyTolerance {
			repriceOnly = false
			break
		}
	}
	if repriceOnly {
		// A correction leaves each delta on the account the original went to, so
		// the rows keep naming that account for any later reversal.
		for li := range lines {
			for ai := range lines[li].allocs {
				a := &lines[li].allocs[ai]
				a.debitAcctID = oldAcct[a.key(lines[li].itemID)]
			}
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	oldAPAcctID, err := service.VendorPayableAccountID(ctx, qtx, old.VendorID.Bytes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun hutang vendor lama")
		return
	}

	if repriceOnly {
		if err := qtx.DeleteStockHistoryBySource(ctx, &db.DeleteStockHistoryBySourceParams{
			SourceID:   invoiceUUID,
			SourceType: pgtype.Text{String: "invoice", Valid: true},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghapus riwayat stok lama")
			return
		}
	} else {
		if err := reverseDistributedStock(ctx, qtx, id, stored); err != nil {
			var ins errInsufficientStock
			if errors.As(err, &ins) {
				respondError(w, http.StatusUnprocessableEntity, fmt.Sprintf("stok tidak mencukupi untuk membalik item: %s", ins.item))
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal membalik stok lama")
			return
		}
		// Dated with the old date so it cancels the original in its own period.
		reversalDate := invoiceDate
		if oldHeader.Date.Valid {
			reversalDate = oldHeader.Date.Time
		}
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        reversalDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Pembalikan faktur %s (revisi)", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines: append(debitLinesFor(storedToResolved(stored), true),
				service.Dr(oldAPAcctID, old.TotalAmount)),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembalikan faktur")
			return
		}
	}

	if err := qtx.DeleteInvoiceItems(ctx, invoiceUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item lama")
		return
	}
	if _, err := qtx.UpdateInvoice(ctx, &db.UpdateInvoiceParams{
		Date:            pgtype.Date{Time: invoiceDate, Valid: true},
		DueDate:         dueDate,
		PaymentMethod:   pgtype.Text{String: body.PaymentMethod, Valid: body.PaymentMethod != ""},
		AccountID:       pgtype.UUID{Bytes: accountID, Valid: accountID != uuid.Nil},
		VendorID:        pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		ReferenceNumber: pgtype.Text{String: body.ReferenceNumber, Valid: body.ReferenceNumber != ""},
		ID:              invoiceUUID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui faktur")
		return
	}

	grandTotal, err := writeDistributedLines(ctx, qtx, tx, id, old.InvoiceNumber, invoiceDate, lines, !repriceOnly)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	newAPAcctID, err := service.VendorPayableAccountID(ctx, qtx, vendorID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan akun hutang vendor")
		return
	}

	if !repriceOnly {
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        invoiceDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Faktur distribusi %s (revisi)", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines:       append(debitLinesFor(allAllocations(lines), false), service.Cr(newAPAcctID, grandTotal)),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal faktur revisi")
			return
		}
	} else {
		// Per destination: a warehouse revalues the share of its delivery still on
		// hand (the rest goes to Selisih Persediaan, as on a normal purchase); a
		// branch consumed the goods on arrival, so its expense simply moves.
		newValue := map[string]int64{}
		keyAlloc := map[string]resolvedAllocation{}
		keyItem := map[string]uuid.UUID{}
		var order []string
		for _, l := range lines {
			for _, a := range l.allocs {
				k := a.key(l.itemID)
				if _, ok := newValue[k]; !ok {
					order = append(order, k)
					keyAlloc[k], keyItem[k] = a, l.itemID
				}
				newValue[k] += a.value
			}
		}

		var journal []service.Line
		var varianceDelta int64
		for _, k := range order {
			delta := newValue[k] - oldValue[k]
			if delta == 0 {
				continue
			}
			a := keyAlloc[k]
			if a.warehouseID == uuid.Nil {
				journal = append(journal, service.Dr(a.debitAcctID, delta))
				continue
			}
			applied, err := repriceReceivedStock(ctx, qtx, keyItem[k], a.warehouseID, oldQty[k], delta)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyesuaikan nilai stok")
				return
			}
			journal = append(journal, service.Dr(a.debitAcctID, applied))
			varianceDelta += delta - applied
		}
		if varianceDelta != 0 {
			journal = append(journal, service.Dr(inventoryAdjustmentAccountID(ctx, qtx), varianceDelta).
				WithMemo("selisih harga atas stok yang sudah terpakai"))
		}
		if oldAPAcctID == newAPAcctID {
			journal = append(journal, service.Cr(newAPAcctID, grandTotal-old.TotalAmount))
		} else {
			journal = append(journal, service.Dr(oldAPAcctID, old.TotalAmount), service.Cr(newAPAcctID, grandTotal))
		}
		if _, err := service.Post(ctx, qtx, service.Entry{
			Date:        invoiceDate,
			SourceType:  service.SourceInvoice,
			SourceID:    id,
			Description: fmt.Sprintf("Koreksi harga faktur %s", old.InvoiceNumber),
			CreatedBy:   middleware.UserIDFromCtx(ctx),
			Lines:       journal,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal koreksi harga")
			return
		}
	}

	newStatus := invoicePaymentStatus(old.AmountPaid, grandTotal)
	if newStatus != old.PaymentStatus {
		if _, err := tx.Exec(ctx, `UPDATE invoices SET payment_status = $1 WHERE id = $2`, newStatus, id); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	desc := fmt.Sprintf("Update faktur distribusi %s", old.InvoiceNumber)
	if grandTotal != old.TotalAmount {
		desc += fmt.Sprintf(" (total %d → %d)", old.TotalAmount, grandTotal)
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: desc,
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"id":             id,
		"payment_status": newStatus,
		"total_amount":   grandTotal,
		"amount_paid":    old.AmountPaid,
	})
}
