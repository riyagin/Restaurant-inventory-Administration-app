package handler

import (
	"context"
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

type InvoicesHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewInvoicesHandler(pool *pgxpool.Pool, queries *db.Queries) *InvoicesHandler {
	return &InvoicesHandler{pool: pool, queries: queries}
}

type invoiceListRow struct {
	ID              pgtype.UUID        `json:"id"`
	InvoiceNumber   string             `json:"invoice_number"`
	Date            pgtype.Date        `json:"date"`
	DueDate         pgtype.Date        `json:"due_date"`
	InvoiceType     string             `json:"invoice_type"`
	PaymentMethod   pgtype.Text        `json:"payment_method"`
	PaymentStatus   string             `json:"payment_status"`
	AmountPaid      int64              `json:"amount_paid"`
	ReferenceNumber pgtype.Text        `json:"reference_number"`
	PhotoPath       pgtype.Text        `json:"photo_path"`
	CreatedAt       pgtype.Timestamptz `json:"created_at"`
	VendorID        pgtype.UUID        `json:"vendor_id"`
	VendorName      pgtype.Text        `json:"vendor_name"`
	WarehouseID     pgtype.UUID        `json:"warehouse_id"`
	WarehouseName   pgtype.Text        `json:"warehouse_name"`
	BranchID        pgtype.UUID        `json:"branch_id"`
	BranchName      pgtype.Text        `json:"branch_name"`
	DivisionID      pgtype.UUID        `json:"division_id"`
	DivisionName    pgtype.Text        `json:"division_name"`
	AccountID       pgtype.UUID        `json:"account_id"`
	AccountName     pgtype.Text        `json:"account_name"`
	DispatchID      pgtype.UUID        `json:"dispatch_id"`
	TotalAmount     int64              `json:"total_amount"`
}

// List — GET /api/invoices
func (h *InvoicesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	status   := q.Get("status")
	invType  := q.Get("type")
	search   := q.Get("search")
	dateFrom := q.Get("date_from")
	dateTo   := q.Get("date_to")
	branchID := q.Get("branch_id")
	divName  := q.Get("division_name")

	pageNum, pageSize := 1, 25
	if p := q.Get("page");  p != "" { if v, err := strconv.Atoi(p); err == nil && v > 0 { pageNum = v } }
	if l := q.Get("limit"); l != "" { if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 { pageSize = v } }

	var args []any
	var conds []string

	if status != "" && status != "all" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("inv.payment_status = $%d", len(args)))
	}
	if invType != "" && invType != "all" {
		args = append(args, invType)
		conds = append(conds, fmt.Sprintf("inv.invoice_type = $%d", len(args)))
	}
	if dateFrom != "" {
		args = append(args, dateFrom)
		conds = append(conds, fmt.Sprintf("inv.date >= $%d::date", len(args)))
	}
	if dateTo != "" {
		args = append(args, dateTo)
		conds = append(conds, fmt.Sprintf("inv.date <= $%d::date", len(args)))
	}
	if search != "" {
		args = append(args, "%"+search+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("(inv.invoice_number ILIKE $%d OR inv.reference_number ILIKE $%d OR v.name ILIKE $%d)", n, n, n))
	}
	if branchID != "" {
		args = append(args, branchID)
		conds = append(conds, fmt.Sprintf("inv.branch_id = $%d::uuid", len(args)))
	}
	if divName != "" {
		args = append(args, divName)
		conds = append(conds, fmt.Sprintf("dv.name = $%d", len(args)))
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	args = append(args, pageSize)
	limitIdx := len(args)
	args = append(args, (pageNum-1)*pageSize)
	offsetIdx := len(args)

	sqlQuery := fmt.Sprintf(`
		WITH filtered AS (
			SELECT inv.id, inv.invoice_number, inv.date, inv.due_date, inv.invoice_type,
			       inv.payment_method, inv.payment_status, inv.amount_paid, inv.reference_number,
			       inv.photo_path, inv.created_at,
			       inv.vendor_id, v.name AS vendor_name,
			       inv.warehouse_id, w.name AS warehouse_name,
			       inv.branch_id, b.name AS branch_name,
			       inv.division_id, dv.name AS division_name,
			       inv.account_id, a.name AS account_name,
			       inv.dispatch_id,
			       COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
			FROM invoices inv
			LEFT JOIN warehouses w  ON w.id  = inv.warehouse_id
			LEFT JOIN accounts a    ON a.id  = inv.account_id
			LEFT JOIN branches b    ON b.id  = inv.branch_id
			LEFT JOIN divisions dv  ON dv.id = inv.division_id
			LEFT JOIN vendors v     ON v.id  = inv.vendor_id
			LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
			%s
			GROUP BY inv.id, v.name, w.name, b.name, dv.name, a.name
		)
		SELECT *, COUNT(*) OVER()::INT AS total_count
		FROM filtered
		ORDER BY date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, limitIdx, offsetIdx)

	rows, err := h.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	defer rows.Close()

	var invoices []invoiceListRow
	totalCount := 0
	for rows.Next() {
		var row invoiceListRow
		var tc int
		if err := rows.Scan(
			&row.ID, &row.InvoiceNumber, &row.Date, &row.DueDate, &row.InvoiceType,
			&row.PaymentMethod, &row.PaymentStatus, &row.AmountPaid, &row.ReferenceNumber,
			&row.PhotoPath, &row.CreatedAt,
			&row.VendorID, &row.VendorName,
			&row.WarehouseID, &row.WarehouseName,
			&row.BranchID, &row.BranchName,
			&row.DivisionID, &row.DivisionName,
			&row.AccountID, &row.AccountName,
			&row.DispatchID,
			&row.TotalAmount,
			&tc,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data faktur")
			return
		}
		totalCount = tc
		invoices = append(invoices, row)
	}
	if invoices == nil {
		invoices = []invoiceListRow{}
	}

	var outstandingTotal int64
	var outstandingCount int
	_ = h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT,
		       COUNT(DISTINCT inv.id)::INT
		FROM invoices inv
		LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
		WHERE inv.payment_status IN ('unpaid', 'partial')
	`).Scan(&outstandingTotal, &outstandingCount)

	respondJSON(w, http.StatusOK, map[string]any{
		"invoices":          invoices,
		"total":             totalCount,
		"page":              pageNum,
		"limit":             pageSize,
		"outstanding_total": outstandingTotal,
		"outstanding_count": outstandingCount,
	})
}

// Get — GET /api/invoices/:id
func (h *InvoicesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	invoice, err := h.queries.GetInvoiceByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}

	items, err := h.queries.GetInvoiceItems(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item faktur")
		return
	}
	if items == nil {
		items = []*db.GetInvoiceItemsRow{}
	}

	type invoiceDetail struct {
		*db.GetInvoiceByIDRow
		Items []*db.GetInvoiceItemsRow `json:"items"`
	}
	respondJSON(w, http.StatusOK, invoiceDetail{
		GetInvoiceByIDRow: invoice,
		Items:             items,
	})
}

// Create — POST /api/invoices
func (h *InvoicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date            string `json:"date"`
		DueDate         string `json:"due_date"`
		InvoiceType     string `json:"invoice_type"`
		PaymentStatus   string `json:"payment_status"`
		PaymentMethod   string `json:"payment_method"`
		AccountID       string `json:"account_id"`
		WarehouseID     string `json:"warehouse_id"`
		BranchID        string `json:"branch_id"`
		DivisionID      string `json:"division_id"`
		VendorID        string `json:"vendor_id"`
		ReferenceNumber string `json:"reference_number"`
		Items           []struct {
			ItemID      string  `json:"item_id"`
			VendorID    string  `json:"vendor_id"`
			Quantity    float64 `json:"quantity"`
			UnitIndex   int32   `json:"unit_index"`
			Price       int64   `json:"price"`
			Description string  `json:"description"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}
	invoiceType := body.InvoiceType
	if invoiceType == "" {
		invoiceType = "purchase"
	}
	if invoiceType == "purchase" && body.WarehouseID == "" {
		respondError(w, http.StatusBadRequest, "warehouse_id diperlukan untuk faktur pembelian")
		return
	}

	ctx := r.Context()

	warehouseID, branchID, divisionID, vendorID, err := parseInvoiceUUIDs(body.WarehouseID, body.BranchID, body.DivisionID, body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	invoiceDate, dueDate, err := parseInvoiceDates(body.Date, body.DueDate)
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

	// 1. Get next invoice number
	invNumRaw, err := qtx.GetNextInvoiceNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor faktur")
		return
	}
	invoiceNumber := fmt.Sprintf("%v", invNumRaw)

	// 2. Create invoice header (always unpaid, amount_paid = 0)
	invoice, err := qtx.CreateInvoice(ctx, &db.CreateInvoiceParams{
		InvoiceNumber:   invoiceNumber,
		Date:            pgtype.Date{Time: invoiceDate, Valid: true},
		DueDate:         dueDate,
		InvoiceType:     invoiceType,
		PaymentMethod:   pgtype.Text{String: body.PaymentMethod, Valid: body.PaymentMethod != ""},
		PaymentStatus:   "unpaid",
		AccountID:       pgtype.UUID{},
		WarehouseID:     pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil},
		BranchID:        pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		DivisionID:      pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		VendorID:        pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		ReferenceNumber: pgtype.Text{String: body.ReferenceNumber, Valid: body.ReferenceNumber != ""},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat faktur")
		return
	}

	invoiceID := invoice.ID.Bytes
	var grandTotal int64

	// 3. Process items
	for _, it := range body.Items {
		var itemID uuid.UUID
		hasItemID := it.ItemID != ""
		if hasItemID {
			parsed, err := parseUUID(it.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			itemID = parsed
		}
		itemVendorID := uuid.Nil
		if it.VendorID != "" {
			id, err := parseUUID(it.VendorID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "vendor_id item tidak valid")
				return
			}
			itemVendorID = id
		}

		lineValue := int64(float64(it.Price) * it.Quantity)
		grandTotal += lineValue

		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:   invoice.ID,
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: hasItemID},
			VendorID:    pgtype.UUID{Bytes: itemVendorID, Valid: itemVendorID != uuid.Nil},
			Quantity:    floatToNumeric(it.Quantity),
			UnitIndex:   pgtype.Int4{Int32: it.UnitIndex, Valid: hasItemID},
			Price:       it.Price,
			Description: pgtype.Text{String: it.Description, Valid: it.Description != ""},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item faktur")
			return
		}

		if invoiceType == "purchase" && hasItemID {
			item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
				return
			}
			unitName := getUnitName(item.Units, it.UnitIndex)

			vendorName := ""
			if itemVendorID != uuid.Nil {
				if v, err := h.queries.GetVendorByID(ctx, pgtype.UUID{Bytes: itemVendorID, Valid: true}); err == nil {
					vendorName = v.Name
				}
			}

			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, it.Quantity, it.UnitIndex, lineValue, invoiceDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok")
				return
			}

			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID:         itemID,
				WarehouseID:    warehouseID,
				QuantityChange: it.Quantity,
				UnitName:       unitName,
				Vendor:         vendorName,
				Type:           "invoice",
				Reference:      invoiceNumber,
				Date:           invoiceDate,
				Value:          lineValue,
				SourceID:       invoiceID,
				SourceType:     "invoice",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
				return
			}
		}
	}

	// 4. Update account balances
	if invoiceType == "purchase" {
		invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
		if err == nil && invAcctID.Valid {
			if err := service.UpdateBalance(ctx, qtx, invAcctID.Bytes, grandTotal); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun inventaris")
				return
			}
		}
	} else {
		expAcctID, err := invoiceExpenseAccountID(ctx, qtx, divisionID, branchID)
		if err == nil && expAcctID != uuid.Nil {
			if err := service.UpdateBalance(ctx, qtx, expAcctID, grandTotal); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban")
				return
			}
		}
	}

	apAcct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: 20100, Valid: true})
	if err == nil {
		if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, grandTotal); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo hutang usaha")
			return
		}
	}

	// 5. If already paid/partial at creation, settle immediately within the same transaction
	if body.PaymentStatus == "paid" || body.PaymentStatus == "partial" {
		if body.AccountID == "" {
			respondError(w, http.StatusBadRequest, "account_id diperlukan untuk status pembayaran ini")
			return
		}
		cashAcctID, err := parseUUID(body.AccountID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
		cashAcct, err := qtx.GetAccountByID(ctx, pgtype.UUID{Bytes: cashAcctID, Valid: true})
		if err != nil {
			respondError(w, http.StatusBadRequest, "akun pembayaran tidak ditemukan")
			return
		}
		if cashAcct.Balance < grandTotal {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("saldo akun \"%s\" tidak cukup", cashAcct.Name))
			return
		}
		// Debit AP (liability decreases — already credited above)
		if apAcct != nil {
			if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, -grandTotal); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo hutang usaha")
				return
			}
		}
		// Credit cash (asset decreases)
		if err := service.UpdateBalance(ctx, qtx, cashAcctID, -grandTotal); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo kas")
			return
		}
		if _, err := qtx.UpdateInvoicePayment(ctx, &db.UpdateInvoicePaymentParams{
			AmountPaid:    grandTotal,
			PaymentStatus: body.PaymentStatus,
			AccountID:     pgtype.UUID{Bytes: cashAcctID, Valid: true},
			ID:            invoice.ID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan faktur")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "Invoice",
		EntityID:    invoiceID,
		Description: fmt.Sprintf("Buat faktur %s %s", invoiceType, invoiceNumber),
	})

	respondJSON(w, http.StatusCreated, invoice)
}

// Update — PUT /api/invoices/:id (only if unpaid)
func (h *InvoicesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Date            string `json:"date"`
		DueDate         string `json:"due_date"`
		PaymentMethod   string `json:"payment_method"`
		AccountID       string `json:"account_id"`
		WarehouseID     string `json:"warehouse_id"`
		BranchID        string `json:"branch_id"`
		DivisionID      string `json:"division_id"`
		VendorID        string `json:"vendor_id"`
		ReferenceNumber string `json:"reference_number"`
		Items           []struct {
			ItemID      string  `json:"item_id"`
			VendorID    string  `json:"vendor_id"`
			Quantity    float64 `json:"quantity"`
			UnitIndex   int32   `json:"unit_index"`
			Price       int64   `json:"price"`
			Description string  `json:"description"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	// 1. Get current invoice — must be unpaid
	old, err := h.queries.GetInvoiceWithTotal(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	if old.PaymentStatus != "unpaid" {
		respondError(w, http.StatusConflict, "faktur yang sudah dibayar tidak dapat diubah")
		return
	}

	oldItems, err := h.queries.GetInvoiceItems(ctx, invoiceUUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item lama")
		return
	}

	warehouseID, branchID, divisionID, vendorID, err := parseInvoiceUUIDs(body.WarehouseID, body.BranchID, body.DivisionID, body.VendorID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	accountID := uuid.Nil
	if body.AccountID != "" {
		aid, err := parseUUID(body.AccountID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "account_id tidak valid")
			return
		}
		accountID = aid
	}

	invoiceDate, dueDate, err := parseInvoiceDates(body.Date, body.DueDate)
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

	oldWarehouseID := old.WarehouseID.Bytes
	oldBranchID := old.BranchID.Bytes
	oldDivisionID := old.DivisionID.Bytes

	// 2. Reverse old account balances
	if old.InvoiceType == "purchase" && old.WarehouseID.Valid {
		for _, it := range oldItems {
			if !it.ItemID.Valid {
				continue
			}
			qty := numericToFloat64(it.Quantity)
			if _, err := service.FIFODeduct(ctx, qtx, it.ItemID.Bytes, oldWarehouseID, qty); err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity,
						fmt.Sprintf("stok tidak mencukupi untuk membalik item: %s", it.ItemName.String))
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal membalik stok lama")
				return
			}
		}
		if err := qtx.DeleteStockHistoryBySource(ctx, &db.DeleteStockHistoryBySourceParams{
			SourceID:   pgtype.UUID{Bytes: id, Valid: true},
			SourceType: pgtype.Text{String: "invoice", Valid: true},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghapus riwayat stok lama")
			return
		}
		oldInvAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: oldWarehouseID, Valid: old.WarehouseID.Valid})
		if err == nil && oldInvAcctID.Valid {
			if err := service.UpdateBalance(ctx, qtx, oldInvAcctID.Bytes, -old.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun inventaris")
				return
			}
		}
	} else if old.InvoiceType == "expense" {
		expAcctID, err := invoiceExpenseAccountID(ctx, qtx, oldDivisionID, oldBranchID)
		if err == nil && expAcctID != uuid.Nil {
			if err := service.UpdateBalance(ctx, qtx, expAcctID, -old.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun beban")
				return
			}
		}
	}

	apAcct, _ := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: 20100, Valid: true})
	if apAcct != nil {
		if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, -old.TotalAmount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membalik saldo hutang usaha")
			return
		}
	}

	// 3. Delete old items
	if err := qtx.DeleteInvoiceItems(ctx, invoiceUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item lama")
		return
	}

	// 4. Update invoice header
	if _, err := qtx.UpdateInvoice(ctx, &db.UpdateInvoiceParams{
		Date:            pgtype.Date{Time: invoiceDate, Valid: true},
		DueDate:         dueDate,
		PaymentMethod:   pgtype.Text{String: body.PaymentMethod, Valid: body.PaymentMethod != ""},
		AccountID:       pgtype.UUID{Bytes: accountID, Valid: accountID != uuid.Nil},
		VendorID:        pgtype.UUID{Bytes: vendorID, Valid: vendorID != uuid.Nil},
		ReferenceNumber: pgtype.Text{String: body.ReferenceNumber, Valid: body.ReferenceNumber != ""},
		WarehouseID:     pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil},
		BranchID:        pgtype.UUID{Bytes: branchID, Valid: branchID != uuid.Nil},
		DivisionID:      pgtype.UUID{Bytes: divisionID, Valid: divisionID != uuid.Nil},
		ID:              invoiceUUID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui faktur")
		return
	}

	// 5. Re-insert items and compute new total
	var grandTotal int64
	for _, it := range body.Items {
		var itemID uuid.UUID
		hasItemID := it.ItemID != ""
		if hasItemID {
			parsed, err := parseUUID(it.ItemID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "item_id tidak valid")
				return
			}
			itemID = parsed
		}
		itemVendorID := uuid.Nil
		if it.VendorID != "" {
			ivid, err := parseUUID(it.VendorID)
			if err != nil {
				respondError(w, http.StatusBadRequest, "vendor_id item tidak valid")
				return
			}
			itemVendorID = ivid
		}

		lineValue := int64(float64(it.Price) * it.Quantity)
		grandTotal += lineValue

		if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
			InvoiceID:   invoiceUUID,
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: hasItemID},
			VendorID:    pgtype.UUID{Bytes: itemVendorID, Valid: itemVendorID != uuid.Nil},
			Quantity:    floatToNumeric(it.Quantity),
			UnitIndex:   pgtype.Int4{Int32: it.UnitIndex, Valid: hasItemID},
			Price:       it.Price,
			Description: pgtype.Text{String: it.Description, Valid: it.Description != ""},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item baru")
			return
		}

		if old.InvoiceType == "purchase" && hasItemID {
			item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
				return
			}
			unitName := getUnitName(item.Units, it.UnitIndex)

			vendorName := ""
			if itemVendorID != uuid.Nil {
				if v, err := h.queries.GetVendorByID(ctx, pgtype.UUID{Bytes: itemVendorID, Valid: true}); err == nil {
					vendorName = v.Name
				}
			}

			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, it.Quantity, it.UnitIndex, lineValue, invoiceDate); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok baru")
				return
			}

			if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
				ItemID:         itemID,
				WarehouseID:    warehouseID,
				QuantityChange: it.Quantity,
				UnitName:       unitName,
				Vendor:         vendorName,
				Type:           "invoice",
				Reference:      old.InvoiceNumber,
				Date:           invoiceDate,
				Value:          lineValue,
				SourceID:       id,
				SourceType:     "invoice",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok baru")
				return
			}
		}
	}

	// 6. Apply new account balances
	if old.InvoiceType == "purchase" {
		newInvAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: warehouseID != uuid.Nil})
		if err == nil && newInvAcctID.Valid {
			if err := service.UpdateBalance(ctx, qtx, newInvAcctID.Bytes, grandTotal); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun inventaris")
				return
			}
		}
	} else {
		expAcctID, err := invoiceExpenseAccountID(ctx, qtx, divisionID, branchID)
		if err == nil && expAcctID != uuid.Nil {
			if err := service.UpdateBalance(ctx, qtx, expAcctID, grandTotal); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun beban")
				return
			}
		}
	}
	if apAcct != nil {
		if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, grandTotal); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo hutang usaha")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Update faktur %s", old.InvoiceNumber),
	})

	respondJSON(w, http.StatusOK, map[string]any{"id": id})
}

// Delete — DELETE /api/invoices/:id (admin only)
func (h *InvoicesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	inv, err := h.queries.GetInvoiceWithTotal(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}

	items, err := h.queries.GetInvoiceItems(ctx, invoiceUUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item faktur")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	warehouseID := inv.WarehouseID.Bytes
	branchID := inv.BranchID.Bytes
	divisionID := inv.DivisionID.Bytes

	// Reverse inventory lots + account (purchase)
	if inv.InvoiceType == "purchase" && inv.WarehouseID.Valid {
		for _, it := range items {
			if !it.ItemID.Valid {
				continue
			}
			qty := numericToFloat64(it.Quantity)
			if _, err := service.FIFODeduct(ctx, qtx, it.ItemID.Bytes, warehouseID, qty); err != nil {
				if strings.Contains(err.Error(), "stok tidak mencukupi") {
					respondError(w, http.StatusUnprocessableEntity,
						fmt.Sprintf("tidak dapat menghapus: stok tidak mencukupi untuk item %s", it.ItemName.String))
					return
				}
				respondError(w, http.StatusInternalServerError, "gagal membalik stok")
				return
			}
		}
		if err := qtx.DeleteStockHistoryBySource(ctx, &db.DeleteStockHistoryBySourceParams{
			SourceID:   pgtype.UUID{Bytes: id, Valid: true},
			SourceType: pgtype.Text{String: "invoice", Valid: true},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghapus riwayat stok")
			return
		}
		invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
		if err == nil && invAcctID.Valid {
			if err := service.UpdateBalance(ctx, qtx, invAcctID.Bytes, -inv.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun inventaris")
				return
			}
		}
	} else if inv.InvoiceType == "expense" {
		expAcctID, err := invoiceExpenseAccountID(ctx, qtx, divisionID, branchID)
		if err == nil && expAcctID != uuid.Nil {
			if err := service.UpdateBalance(ctx, qtx, expAcctID, -inv.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo akun beban")
				return
			}
		}
	}

	// Reverse cash/AP based on payment_status
	apAcct, _ := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: 20100, Valid: true})
	switch inv.PaymentStatus {
	case "paid":
		if inv.AccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, inv.AccountID.Bytes, inv.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memulihkan saldo kas")
				return
			}
		}
	case "partial":
		if inv.AccountID.Valid && inv.AmountPaid > 0 {
			if err := service.UpdateBalance(ctx, qtx, inv.AccountID.Bytes, inv.AmountPaid); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memulihkan saldo kas")
				return
			}
		}
		unpaid := inv.TotalAmount - inv.AmountPaid
		if apAcct != nil && unpaid > 0 {
			if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, -unpaid); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo hutang")
				return
			}
		}
	case "unpaid":
		if apAcct != nil {
			if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, -inv.TotalAmount); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membalik saldo hutang")
				return
			}
		}
	}

	if err := qtx.DeleteInvoiceItems(ctx, invoiceUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus item faktur")
		return
	}
	if err := qtx.DeleteInvoice(ctx, invoiceUUID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus faktur")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "DELETE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus faktur %s", inv.InvoiceNumber),
	})

	w.WriteHeader(http.StatusNoContent)
}

// Pay — POST /api/invoices/:id/pay
func (h *InvoicesHandler) Pay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		CashAccountID string  `json:"cash_account_id"`
		Amount        float64 `json:"amount"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.CashAccountID == "" {
		respondError(w, http.StatusBadRequest, "cash_account_id diperlukan")
		return
	}
	cashAcctID, err := parseUUID(body.CashAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cash_account_id tidak valid")
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	inv, err := h.queries.GetInvoiceWithTotal(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	if inv.PaymentStatus == "paid" {
		respondError(w, http.StatusBadRequest, "faktur sudah lunas")
		return
	}

	remaining := inv.TotalAmount - inv.AmountPaid
	payAmount := int64(body.Amount)
	if payAmount <= 0 || payAmount > remaining {
		payAmount = remaining
	}
	if payAmount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah pembayaran tidak valid")
		return
	}

	cashAcct, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: cashAcctID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun kas tidak ditemukan")
		return
	}
	if cashAcct.Balance < payAmount {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("saldo akun \"%s\" tidak cukup", cashAcct.Name))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	// Debit AP (liability decreases)
	apAcct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: 20100, Valid: true})
	if err == nil {
		if err := service.UpdateBalance(ctx, qtx, apAcct.ID.Bytes, -payAmount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo hutang")
			return
		}
	}

	// Credit cash account (asset decreases)
	if err := service.UpdateBalance(ctx, qtx, cashAcctID, -payAmount); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo kas")
		return
	}

	newAmountPaid := inv.AmountPaid + payAmount
	newStatus := "partial"
	if newAmountPaid >= inv.TotalAmount {
		newStatus = "paid"
	}

	updated, err := qtx.UpdateInvoicePayment(ctx, &db.UpdateInvoicePaymentParams{
		AmountPaid:    newAmountPaid,
		PaymentStatus: newStatus,
		AccountID:     pgtype.UUID{Bytes: cashAcctID, Valid: true},
		ID:            invoiceUUID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembayaran")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Bayar faktur %s sebesar %d via \"%s\"", inv.InvoiceNumber, payAmount, cashAcct.Name),
	})

	respondJSON(w, http.StatusOK, updated)
}

// invoiceExpenseAccountID returns the expense account for a division (priority) or branch.
func invoiceExpenseAccountID(ctx context.Context, qtx *db.Queries, divisionID, branchID uuid.UUID) (uuid.UUID, error) {
	if divisionID != uuid.Nil {
		aid, err := qtx.GetDivisionExpenseAccountID(ctx, pgtype.UUID{Bytes: divisionID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	if branchID != uuid.Nil {
		aid, err := qtx.GetBranchExpenseAccountID(ctx, pgtype.UUID{Bytes: branchID, Valid: true})
		if err == nil && aid.Valid {
			return aid.Bytes, nil
		}
	}
	return uuid.Nil, nil
}

// parseInvoiceUUIDs parses the four optional UUID strings; returns an error if any is malformed.
func parseInvoiceUUIDs(warehouseStr, branchStr, divisionStr, vendorStr string) (warehouseID, branchID, divisionID, vendorID uuid.UUID, err error) {
	if warehouseStr != "" {
		warehouseID, err = parseUUID(warehouseStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("warehouse_id tidak valid")
		}
	}
	if branchStr != "" {
		branchID, err = parseUUID(branchStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("branch_id tidak valid")
		}
	}
	if divisionStr != "" {
		divisionID, err = parseUUID(divisionStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("division_id tidak valid")
		}
	}
	if vendorStr != "" {
		vendorID, err = parseUUID(vendorStr)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("vendor_id tidak valid")
		}
	}
	return warehouseID, branchID, divisionID, vendorID, nil
}

// parseInvoiceDates parses the date and due_date strings; defaults date to today.
func parseInvoiceDates(dateStr, dueDateStr string) (invoiceDate time.Time, dueDate pgtype.Date, err error) {
	invoiceDate = time.Now()
	if dateStr != "" {
		invoiceDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return invoiceDate, dueDate, fmt.Errorf("format tanggal tidak valid")
		}
	}
	if dueDateStr != "" {
		t, e := time.Parse("2006-01-02", dueDateStr)
		if e != nil {
			return invoiceDate, dueDate, fmt.Errorf("format due_date tidak valid")
		}
		dueDate = pgtype.Date{Time: t, Valid: true}
	}
	return invoiceDate, dueDate, nil
}
