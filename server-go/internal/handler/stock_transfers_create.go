package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

func (h *StockTransfersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FromWarehouseID string `json:"from_warehouse_id"`
		ToWarehouseID   string `json:"to_warehouse_id"`
		Notes           string `json:"notes"`
		Items           []struct {
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
	if body.FromWarehouseID == "" || body.ToWarehouseID == "" {
		respondError(w, http.StatusBadRequest, "gudang asal dan tujuan diperlukan")
		return
	}
	if body.FromWarehouseID == body.ToWarehouseID {
		respondError(w, http.StatusBadRequest, "gudang asal dan tujuan harus berbeda")
		return
	}
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	fromID, err := parseUUID(body.FromWarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "from_warehouse_id tidak valid")
		return
	}
	toID, err := parseUUID(body.ToWarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "to_warehouse_id tidak valid")
		return
	}

	ctx := r.Context()

	// Read warehouse names and account IDs before starting the transaction
	fromWH, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: fromID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang asal tidak ditemukan")
		return
	}
	toWH, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: toID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tujuan tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	groupID := uuid.New()
	today := time.Now()

	type transferRecord struct {
		ID       pgtype.UUID
		GroupID  pgtype.UUID
		ItemName string
		Quantity float64
		UnitName string
	}
	var records []transferRecord

	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		// Get item name for error messages
		item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item tidak ditemukan: %s", it.ItemID))
			return
		}

		valueDeducted, err := service.FIFODeduct(ctx, qtx, itemID, fromID, it.Quantity,
			service.LotSource{Type: service.SourceTransfer, ID: groupID, Date: today})
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("stok tidak mencukupi untuk item: %s", item.Name))
				return
			}
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("gagal mengurangi stok: %v", err))
			return
		}

		if err := service.FIFOAdd(ctx, qtx, itemID, toID, it.Quantity, it.UnitIndex, valueDeducted, today); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menambah stok di tujuan")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    fromID,
			QuantityChange: -it.Quantity,
			UnitName:       it.UnitName,
			Type:           "transfer",
			Reference:      fmt.Sprintf("Transfer → %s", toWH.Name),
			Date:           today,
			Value:          -valueDeducted,
			SourceID:       groupID,
			SourceType:     "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    toID,
			QuantityChange: it.Quantity,
			UnitName:       it.UnitName,
			Type:           "transfer",
			Reference:      fmt.Sprintf("Transfer ← %s", fromWH.Name),
			Date:           today,
			Value:          valueDeducted,
			SourceID:       groupID,
			SourceType:     "transfer",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		// 5-7. Value moves from the source warehouse's inventory account to the
		// destination's, as one balanced entry.
		if err := postInventoryMove(ctx, qtx, today, groupID, middleware.UserIDFromCtx(ctx),
			fmt.Sprintf("Transfer stok %s → %s", fromWH.Name, toWH.Name),
			fromWH.InventoryAccountID, toWH.InventoryAccountID, valueDeducted); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal transfer")
			return
		}

		rec, err := qtx.InsertStockTransfer(ctx, &db.InsertStockTransferParams{
			ItemID:          pgtype.UUID{Bytes: itemID, Valid: true},
			FromWarehouseID: pgtype.UUID{Bytes: fromID, Valid: true},
			ToWarehouseID:   pgtype.UUID{Bytes: toID, Valid: true},
			Quantity:        floatToNumeric(it.Quantity),
			UnitIndex:       it.UnitIndex,
			UnitName:        it.UnitName,
			Notes:           pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
			TransferredBy:   pgtype.UUID{Bytes: middleware.UserIDFromCtx(ctx), Valid: middleware.UserIDFromCtx(ctx) != uuid.Nil},
			GroupID:         pgtype.UUID{Bytes: groupID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan data transfer")
			return
		}

		records = append(records, transferRecord{
			ID:       rec.ID,
			GroupID:  rec.GroupID,
			ItemName: item.Name,
			Quantity: it.Quantity,
			UnitName: it.UnitName,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "StockTransfer",
		Description: fmt.Sprintf("Transfer stok dari %s ke %s", fromWH.Name, toWH.Name),
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"group_id":  groupID,
		"transfers": records,
	})
}
