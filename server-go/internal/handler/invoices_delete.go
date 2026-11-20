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

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

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

	distributed, err := isDistributedInvoice(ctx, h.pool, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	var allocations []storedAllocation
	if distributed {
		if allocations, err = loadInvoiceAllocations(ctx, h.pool, id); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil alokasi faktur")
			return
		}
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
	categoryID := uuid.Nil
	if inv.ExpenseCategoryID.Valid {
		categoryID = inv.ExpenseCategoryID.Bytes
	}

	var debitAcctID uuid.UUID
	if inv.InvoiceType == "purchase" && inv.WarehouseID.Valid {
		for _, it := range items {
			if !it.ItemID.Valid {
				continue
			}
			qty := numericToFloat64(it.Quantity) * storedConversionFactor(it.ConversionFactor)
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
		if invAcctID, err := qtx.GetWarehouseInventoryAccountID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true}); err == nil && invAcctID.Valid {
			debitAcctID = invAcctID.Bytes
		}
	} else if inv.InvoiceType == "expense" {
		if expAcctID, err := invoiceExpenseAccountID(ctx, qtx, categoryID, divisionID, branchID); err == nil {
			debitAcctID = expAcctID
		}
	}

	credits := []service.Line{service.Cr(debitAcctID, inv.TotalAmount)}
	if distributed {
		if err := reverseDistributedStock(ctx, qtx, id, allocations); err != nil {
			var ins errInsufficientStock
			if errors.As(err, &ins) {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("tidak dapat menghapus: stok tidak mencukupi untuk item %s", ins.item))
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal membalik stok")
			return
		}
		credits = debitLinesFor(storedToResolved(allocations), true)
	}

	apAcctID, err := service.VendorPayableAccountID(ctx, qtx, inv.VendorID.Bytes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun hutang vendor")
		return
	}

	// Dated today so a deletion never reaches into an already-reported period.
	reversal := credits
	switch inv.PaymentStatus {
	case "paid":
		reversal = append(reversal, service.Dr(inv.AccountID.Bytes, inv.TotalAmount))
	case "partial":
		reversal = append(reversal,
			service.Dr(inv.AccountID.Bytes, inv.AmountPaid),
			service.Dr(apAcctID, inv.TotalAmount-inv.AmountPaid),
		)
	default: // unpaid
		reversal = append(reversal, service.Dr(apAcctID, inv.TotalAmount))
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceInvoice,
		SourceID:    id,
		Description: fmt.Sprintf("Pembalikan faktur %s (dihapus)", inv.InvoiceNumber),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines:       reversal,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembalikan faktur")
		return
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
