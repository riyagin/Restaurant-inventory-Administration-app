package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Update — PUT /api/stock-opname/{id}
//
// Edits (corrects) a submitted opname. Instead of rewriting the already-posted
// rows, every corrected item is reconciled against the CURRENT system quantity
// and the difference is appended as a NEW stock_opname_items row
// (is_correction = true) plus a matching stock_history row, with the waste and
// inventory account balances moved by the difference. The original rows stay
// intact as the audit trail, and reports net correctly because they sum the
// rows. See the accounting-edit convention (dispatches Update is the reference).
func (h *StockOpnameHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Items []struct {
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
	if len(body.Items) == 0 {
		respondError(w, http.StatusBadRequest, "minimal satu item diperlukan")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	opname, err := h.queries.GetStockOpnameByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "stok opname tidak ditemukan")
		return
	}
	warehouseID := opname.WarehouseID.Bytes

	wh, err := h.queries.GetWarehouseByID(ctx, opname.WarehouseID)
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
	now := time.Now()

	// Get or create the inventory variance account (same as Create).
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

	// Same posting pair as Create — see the comment there.
	varianceAcctID := uuidFromPg(varianceAccountID)
	invAcctID := uuidFromPg(wh.InventoryAccountID)

	applied := 0
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

		// No change against the current system quantity → nothing to book.
		if math.Abs(diff) < 0.001 {
			continue
		}

		// Same split as Create — see the comment there.
		var wasteValue, historyValue int64
		if diff < 0 {
			deducted, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, math.Abs(diff))
			if err != nil {
				respondError(w, http.StatusUnprocessableEntity,
					fmt.Sprintf("gagal mengoreksi stok opname: %v", err))
				return
			}
			wasteValue = deducted
			historyValue = -deducted
		} else {
			// A correction that puts stock back is first of all a reversal of what
			// THIS opname took away — most often an item mistakenly counted to 0,
			// which deletes its lots and makes it vanish from the warehouse.
			// Valuing that at the latest purchase price would leave a residue in
			// Selisih Persediaan, because the write-off and its reversal would use
			// different costs. So restore at the cost written off, and treat only
			// the quantity beyond it as a genuine surplus.
			removedQty, removedValue := opnameNetWaste(ctx, tx, id, itemID)
			reverseQty := 0.0
			if removedQty > 0 && removedValue > 0 {
				reverseQty = math.Min(diff, removedQty)
			}
			var reversedValue int64
			if reverseQty > 0 {
				reversedValue = int64(math.Round(float64(removedValue) * reverseQty / removedQty))
			}
			surplusValue := reversedValue
			if extra := diff - reverseQty; extra > 0.001 {
				surplusValue += latestItemValue(ctx, qtx, itemID, it.UnitIndex, extra)
			}

			if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, diff, it.UnitIndex, surplusValue, now); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menambah stok opname")
				return
			}
			historyValue = surplusValue
			// Booking the restoration as negative waste nets this opname's recorded
			// loss back down, so every report that sums waste_value — and the next
			// reversal's own arithmetic — sees what is actually still written off.
			wasteValue = -reversedValue
			if surplusValue > 0 {
				if _, err := service.Post(ctx, qtx, service.Entry{
					Date:        now,
					SourceType:  service.SourceOpname,
					SourceID:    id,
					Description: "Koreksi selisih lebih stok opname",
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

		// Append the correction as a NEW row (raw insert to set is_correction,
		// a column added after the sqlc query was generated).
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_opname_items (
				id, opname_id, item_id, unit_index, unit_name,
				recorded_quantity, actual_quantity, difference, waste_value, is_correction
			)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, true)`,
			pgUUID(id), pgUUID(itemID), it.UnitIndex, it.UnitName,
			floatToNumeric(recorded), floatToNumeric(actual), floatToNumeric(diff), wasteValue,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi opname")
			return
		}

		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID:         itemID,
			WarehouseID:    warehouseID,
			QuantityChange: diff,
			UnitName:       it.UnitName,
			Type:           "opname",
			Reference:      "Koreksi Stok Opname",
			Date:           now,
			Value:          historyValue,
			SourceID:       id,
			SourceType:     "opname",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}

		if wasteValue > 0 {
			if _, err := service.Post(ctx, qtx, service.Entry{
				Date:        now,
				SourceType:  service.SourceOpname,
				SourceID:    id,
				Description: "Koreksi selisih kurang stok opname (waste)",
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
		applied++
	}

	if applied == 0 {
		respondError(w, http.StatusUnprocessableEntity, "tidak ada perubahan untuk dicatat")
		return
	}

	if _, err := tx.Exec(ctx, `UPDATE stock_opname SET updated_at = now() WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui stok opname")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "StockOpname",
		EntityID:    id,
		Description: fmt.Sprintf("Koreksi stok opname di gudang %s", wh.Name),
	})

	items, err := h.fetchOpnameItems(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item opname")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"id":    id,
		"items": items,
	})
}

// opnameNetWaste reports how much of an item this opname has written off and
// not yet put back: the net quantity removed and the net rupiah value behind it.
//
// Both figures net across the original rows AND every later correction, which is
// what makes repeated corrections safe — a restoration is booked as a correction
// row carrying a positive difference and a negative waste_value, so it cancels
// out of these sums instead of being reversible twice.
func opnameNetWaste(ctx context.Context, tx pgx.Tx, opnameID, itemID uuid.UUID) (qty float64, value int64) {
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(-difference), 0)::float8, COALESCE(SUM(waste_value), 0)::bigint
		FROM stock_opname_items
		WHERE opname_id = $1 AND item_id = $2`,
		pgUUID(opnameID), pgUUID(itemID)).Scan(&qty, &value); err != nil {
		return 0, 0
	}
	return qty, value
}

// latestItemValue returns the total IDR value to assign to `qty` surplus units,
// priced from the item's most recent purchase. Returns 0 if unknown.
//
// `qty` is a base-unit quantity (inventory lots are stored at the item's lowest
// unit), but the last purchase may have been priced at any unit — a carton
// rather than a can. The price is therefore converted down to the base unit
// before it is applied.
//
// Skipping that conversion is a real overstatement, not a theoretical one: an
// item bought by the dus at 24 cans per dus had its surplus valued at the dus
// price for every single can.
func latestItemValue(ctx context.Context, qtx *db.Queries, itemID uuid.UUID, unitIndex int32, qty float64) int64 {
	row, err := qtx.GetItemLastPrice(ctx, pgtype.UUID{Bytes: itemID, Valid: true})
	if err != nil {
		return 0
	}

	priceUnitIndex := int32(0)
	if row.UnitIndex.Valid {
		priceUnitIndex = row.UnitIndex.Int32
	}

	pricePerBase := float64(row.Price)
	if item, err := qtx.GetItemByID(ctx, pgtype.UUID{Bytes: itemID, Valid: true}); err == nil {
		if factor := baseUnitFactor(item.Units, priceUnitIndex); factor > 0 {
			pricePerBase /= factor
		}
	}

	return int64(math.Round(pricePerBase * qty))
}
