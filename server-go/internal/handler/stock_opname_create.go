package handler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

func (h *StockOpnameHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarehouseID  string `json:"warehouse_id"`
		Notes        string `json:"notes"`
		OperatorName string `json:"operator_name"`
		PicName      string `json:"pic_name"`
		DraftID      string `json:"draft_id"`
		Items        []struct {
			ItemID         string  `json:"item_id"`
			UnitIndex      int32   `json:"unit_index"`
			UnitName       string  `json:"unit_name"`
			ActualQuantity float64 `json:"actual_quantity"`
		} `json:"items"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.WarehouseID == "" || len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "gudang dan item diperlukan")
		return
	}

	warehouseID, err := parseUUID(body.WarehouseID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "warehouse_id tidak valid")
		return
	}

	ctx := r.Context()
	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	wh, err := h.queries.GetWarehouseByID(ctx, pgtype.UUID{Bytes: warehouseID, Valid: true})
	if err != nil {
		respondError(w, http.StatusBadRequest, "gudang tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	today := time.Now()

	// Get or create the inventory variance account.
	varianceAccountID, err := qtx.GetInventoryVarianceAccountID(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusInternalServerError, "gagal mencari akun stok waste")
			return
		}
		created, err := qtx.CreateAccount(ctx, &db.CreateAccountParams{
			Name:        "Selisih Persediaan",
			AccountType: "expense",
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membuat akun stok waste")
			return
		}
		varianceAccountID = created.ID
	}

	// Both directions of a count difference post against the same pair, so an
	// over-count nets against an under-count instead of accumulating in
	// unrelated accounts:
	//   loss    Dr Selisih Persediaan  Cr inventory
	//   surplus Dr inventory           Cr Selisih Persediaan
	// Posting either leg alone would create or destroy inventory value with no
	// counterpart.
	varianceAcctID := uuidFromPg(varianceAccountID)
	invAcctID := uuidFromPg(wh.InventoryAccountID)

	// Insert stock_opname header
	opname, err := qtx.InsertStockOpname(ctx, &db.InsertStockOpnameParams{
		WarehouseID:  pgtype.UUID{Bytes: warehouseID, Valid: true},
		Notes:        pgtype.Text{String: body.Notes, Valid: body.Notes != ""},
		PerformedBy:  pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil},
		OperatorName: pgtype.Text{String: body.OperatorName, Valid: body.OperatorName != ""},
		PicName:      pgtype.Text{String: body.PicName, Valid: body.PicName != ""},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat stok opname")
		return
	}

	// Process each item
	for _, it := range body.Items {
		itemID, err := parseUUID(it.ItemID)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("item_id tidak valid: %s", it.ItemID))
			return
		}

		rawQty, err := qtx.GetCurrentInventoryQuantity(ctx, &db.GetCurrentInventoryQuantityParams{
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca kuantitas inventori")
			return
		}
		recorded := anyNumericToFloat64(rawQty)
		actual := it.ActualQuantity
		diff := actual - recorded

		if math.Abs(diff) < 0.001 {
			_ = qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
				OpnameID:         opname.ID,
				ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
				UnitIndex:        it.UnitIndex,
				UnitName:         it.UnitName,
				RecordedQuantity: floatToNumeric(recorded),
				ActualQuantity:   floatToNumeric(actual),
				Difference:       floatToNumeric(0.0),
				WasteValue:       0,
			})
			continue
		}

		// Two different numbers, tracked separately on purpose:
		//   wasteValue   — a magnitude, for stock_opname_items.waste_value and
		//                  the waste journal; zero on a surplus.
		//   historyValue — signed like quantity_change (positive = stock in,
		//                  negative = stock out), for stock_history.value.
		// Every other movement type already follows the signed convention, and
		// the flow reports bucket on it (`value > 0` → masuk, `< 0` → keluar).
		// Writing the waste magnitude here made a shrinkage count as inbound
		// value, and leaving a surplus at 0 hid the added value entirely.
		var wasteValue, historyValue int64
		if diff < 0 {
			// Loss: FIFO deduct
			deducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, math.Abs(diff))
			if err != nil {
				respondError(w, http.StatusInternalServerError,
					fmt.Sprintf("gagal mengurangi stok opname: %v", err))
				return
			}
			wasteValue = deducted
			historyValue = -deducted
		} else {
			// Surplus: add stock at latest known price (prorated for qty)
			surplusValue := latestItemValue(ctx, qtx, itemID, it.UnitIndex, diff)
			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, diff, it.UnitIndex, surplusValue, today); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok opname")
				return
			}
			wasteValue = 0
			historyValue = surplusValue
			if surplusValue > 0 {
				if _, err := service.Post(ctx, qtx, service.Entry{
					Date:        today,
					SourceType:  service.SourceOpname,
					SourceID:    opname.ID.Bytes,
					Description: "Selisih lebih stok opname",
					CreatedBy:   middleware.UserIDFromCtx(ctx),
					Lines: []service.Line{
						service.Dr(invAcctID, surplusValue),
						service.Cr(varianceAcctID, surplusValue),
					},
				}); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal selisih lebih")
					return
				}
			}
		}

		if err := qtx.InsertStockOpnameItem(ctx, &db.InsertStockOpnameItemParams{
			OpnameID:         opname.ID,
			ItemID:           pgtype.UUID{Bytes: itemID, Valid: true},
			UnitIndex:        it.UnitIndex,
			UnitName:         it.UnitName,
			RecordedQuantity: floatToNumeric(recorded),
			ActualQuantity:   floatToNumeric(actual),
			Difference:       floatToNumeric(diff),
			WasteValue:       wasteValue,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan item opname")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: diff,
			UnitName:       it.UnitName,
			Type:           "opname",
			Reference:      "Stok Opname",
			Date:           today,
			Value:          historyValue,
			SourceID:       opname.ID.Bytes,
			SourceType:     "opname",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		if wasteValue > 0 {
			if _, err := service.Post(ctx, qtx, service.Entry{
				Date:        today,
				SourceType:  service.SourceOpname,
				SourceID:    opname.ID.Bytes,
				Description: "Selisih kurang stok opname (waste)",
				CreatedBy:   middleware.UserIDFromCtx(ctx),
				Lines: []service.Line{
					service.Dr(varianceAcctID, wasteValue),
					service.Cr(invAcctID, wasteValue),
				},
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal waste")
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	// Best-effort: a leftover draft is harmless once the opname has committed.
	if body.DraftID != "" {
		_, _ = h.pool.Exec(ctx, `DELETE FROM stock_opname_drafts WHERE id = $1`, body.DraftID)
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "CREATE",
		EntityType:  "StockOpname",
		EntityID:    opname.ID.Bytes,
		Description: fmt.Sprintf("Stok opname di gudang %s", wh.Name),
	})

	respondJSON(w, http.StatusCreated, opname)
}
