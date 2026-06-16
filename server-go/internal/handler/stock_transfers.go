package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type StockTransfersHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewStockTransfersHandler(pool *pgxpool.Pool, queries *db.Queries) *StockTransfersHandler {
	return &StockTransfersHandler{pool: pool, queries: queries}
}

func (h *StockTransfersHandler) List(w http.ResponseWriter, r *http.Request) {
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

	rows, err := h.queries.ListStockTransfers(ctx, &db.ListStockTransfersParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if rows == nil {
		rows = []*db.ListStockTransfersRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

func (h *StockTransfersHandler) ListByGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID, err := parseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "groupId tidak valid")
		return
	}

	rows, err := h.queries.ListStockTransfersByGroup(ctx, pgtype.UUID{Bytes: groupID, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data transfer")
		return
	}
	if rows == nil {
		rows = []*db.ListStockTransfersByGroupRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

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
		ID        pgtype.UUID
		GroupID   pgtype.UUID
		ItemName  string
		Quantity  float64
		UnitName  string
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

		// 1. FIFO deduct from source
		valueDeducted, err := service.FIFODeduct(ctx, qtx, itemID, fromID, it.Quantity)
		if err != nil {
			if strings.Contains(err.Error(), "stok tidak mencukupi") {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("stok tidak mencukupi untuk item: %s", item.Name))
				return
			}
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("gagal mengurangi stok: %v", err))
			return
		}

		// 2. FIFO add to destination
		if err := service.FIFOAdd(ctx, qtx, itemID, toID, it.Quantity, it.UnitIndex, valueDeducted, today); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menambah stok di tujuan")
			return
		}

		// 3. Stock history for source
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

		// 4. Stock history for destination
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

		// 5 & 6. Update source warehouse account balance
		if fromWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, fromWH.InventoryAccountID.Bytes, -valueDeducted); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun asal")
				return
			}
		}

		// 7. Update destination warehouse account balance
		if toWH.InventoryAccountID.Valid {
			if err := service.UpdateBalance(ctx, qtx, toWH.InventoryAccountID.Bytes, valueDeducted); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal memperbarui saldo akun tujuan")
				return
			}
		}

		// 8. Insert stock_transfer record
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

	// 9. Log activity after commit
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
		"group_id": groupID,
		"transfers": records,
	})
}
