package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Delete — DELETE /api/stock-transfers/group/{groupId} (reverses and flags cancelled)
func (h *StockTransfersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID, err := parseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "groupId tidak valid")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	g, err := h.loadTransferGroup(ctx, tx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "transfer tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if g.status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "transfer sudah dibatalkan")
		return
	}

	fromWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.fromID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang asal")
		return
	}
	toWH, err := qtx.GetWarehouseByID(ctx, pgUUID(g.toID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil gudang tujuan")
		return
	}

	// Reverse on the transfer's own date so it nets against the original movement
	// within the same reporting period.
	reversalDate := time.Now()
	if g.transferredAt.Valid {
		reversalDate = g.transferredAt.Time
	}

	for _, ln := range g.netByKey() {
		if ln.quantity <= dispatchEpsilon {
			continue
		}
		v, err := service.FIFODeduct(ctx, qtx, ln.itemID, g.toID, ln.quantity)
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity, "stok di gudang tujuan tidak mencukupi untuk membatalkan transfer")
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal mengurangi stok tujuan")
			return
		}
		if err := service.FIFOAdd(ctx, qtx, ln.itemID, g.fromID, ln.quantity, ln.unitIndex, v, reversalDate); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok ke asal")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: ln.itemID, WarehouseID: g.toID, QuantityChange: -ln.quantity, UnitName: ln.unitName,
			Type: "transfer_cancel", Reference: fmt.Sprintf("Pembatalan transfer → %s", fromWH.Name),
			Date: reversalDate, Value: -v, SourceID: groupID, SourceType: "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: ln.itemID, WarehouseID: g.fromID, QuantityChange: ln.quantity, UnitName: ln.unitName,
			Type: "transfer_cancel", Reference: fmt.Sprintf("Pembatalan transfer ← %s", toWH.Name),
			Date: reversalDate, Value: v, SourceID: groupID, SourceType: "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
		if err := postInventoryMove(ctx, qtx, reversalDate, groupID, middleware.UserIDFromCtx(ctx),
			fmt.Sprintf("Pembatalan transfer %s → %s", toWH.Name, fromWH.Name),
			toWH.InventoryAccountID, fromWH.InventoryAccountID, v); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan transfer")
			return
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE stock_transfers SET status = 'cancelled', updated_at = now() WHERE group_id = $1`, pgUUID(groupID)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan transfer")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan transfer")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "StockTransfer",
		EntityID:    groupID,
		Description: fmt.Sprintf("Pembatalan transfer stok dari %s ke %s", fromWH.Name, toWH.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}
