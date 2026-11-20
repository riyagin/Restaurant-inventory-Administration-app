package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
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
	PaymentDate     pgtype.Date        `json:"payment_date"`
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
	IsDistributed   bool               `json:"is_distributed"`
	TotalAmount     int64              `json:"total_amount"`
}

// Known payment statuses; anything else is dropped before reaching the query.
var invoicePaymentStatuses = map[string]bool{
	"unpaid":     true,
	"partial":    true,
	"paid":       true,
	"dispatched": true,
	"cancelled":  true,
}

func parseStatusList(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if invoicePaymentStatuses[s] && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// List — GET /api/invoices
func (h *InvoicesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	status := q.Get("status")
	excludeStatus := parseStatusList(q.Get("exclude_status"))
	invType := q.Get("type")
	search := q.Get("search")
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")
	branchID := q.Get("branch_id")
	divName := q.Get("division_name")
	vendorID := q.Get("vendor_id")

	pageNum, pageSize := 1, 25
	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			pageNum = v
		}
	}
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	var args []any
	var conds []string

	if status != "" && status != "all" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("inv.payment_status = $%d", len(args)))
	}
	if len(excludeStatus) > 0 {
		args = append(args, excludeStatus)
		conds = append(conds, fmt.Sprintf("NOT (inv.payment_status = ANY($%d::text[]))", len(args)))
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
	if vendorID == "none" {
		conds = append(conds, "inv.vendor_id IS NULL")
	} else if vendorID != "" {
		args = append(args, vendorID)
		conds = append(conds, fmt.Sprintf("inv.vendor_id = $%d::uuid", len(args)))
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
			       inv.payment_method, inv.payment_status, inv.amount_paid, inv.payment_date, inv.reference_number,
			       inv.photo_path, inv.created_at,
			       inv.vendor_id, v.name AS vendor_name,
			       inv.warehouse_id, w.name AS warehouse_name,
			       inv.branch_id, b.name AS branch_name,
			       inv.division_id, dv.name AS division_name,
			       inv.account_id, a.name AS account_name,
			       inv.dispatch_id, inv.is_distributed,
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
			&row.PaymentMethod, &row.PaymentStatus, &row.AmountPaid, &row.PaymentDate, &row.ReferenceNumber,
			&row.PhotoPath, &row.CreatedAt,
			&row.VendorID, &row.VendorName,
			&row.WarehouseID, &row.WarehouseName,
			&row.BranchID, &row.BranchName,
			&row.DivisionID, &row.DivisionName,
			&row.AccountID, &row.AccountName,
			&row.DispatchID, &row.IsDistributed,
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

	distributed, err := isDistributedInvoice(ctx, h.pool, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	allocations := []invoiceAllocationView{}
	if distributed {
		if allocations, err = listInvoiceAllocationViews(ctx, h.pool, id); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil alokasi faktur")
			return
		}
	}

	type invoiceDetail struct {
		*db.GetInvoiceByIDRow
		IsDistributed bool                     `json:"is_distributed"`
		Items         []*db.GetInvoiceItemsRow `json:"items"`
		Allocations   []invoiceAllocationView  `json:"allocations"`
	}
	respondJSON(w, http.StatusOK, invoiceDetail{
		GetInvoiceByIDRow: invoice,
		IsDistributed:     distributed,
		Items:             items,
		Allocations:       allocations,
	})
}

// ConversionFactor overrides, for this line only, how many base units the chosen unit holds; 0 uses the item's units.
type invoiceItemInput struct {
	ItemID           string                   `json:"item_id"`
	VendorID         string                   `json:"vendor_id"`
	Quantity         float64                  `json:"quantity"`
	UnitIndex        int32                    `json:"unit_index"`
	Price            int64                    `json:"price"`
	Description      string                   `json:"description"`
	ConversionFactor float64                  `json:"conversion_factor"`
	Allocations      []invoiceAllocationInput `json:"allocations"`
}

type invoiceUpdateRequest struct {
	Date              string             `json:"date"`
	DueDate           string             `json:"due_date"`
	PaymentMethod     string             `json:"payment_method"`
	AccountID         string             `json:"account_id"`
	WarehouseID       string             `json:"warehouse_id"`
	BranchID          string             `json:"branch_id"`
	DivisionID        string             `json:"division_id"`
	VendorID          string             `json:"vendor_id"`
	ReferenceNumber   string             `json:"reference_number"`
	ExpenseCategoryID string             `json:"expense_category_id"`
	Items             []invoiceItemInput `json:"items"`
}

type resolvedInvoiceLine struct {
	in        invoiceItemInput
	itemID    uuid.UUID
	hasItemID bool
	vendorID  uuid.UUID
	conv      lineConversion
	value     int64
}
