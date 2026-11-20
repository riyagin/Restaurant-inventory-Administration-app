package handler

import (
	"errors"
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

// Delete — DELETE /api/dispatches/:id
// Cancels rather than erases: stock returns, postings reverse, and the mirror invoice nets to zero.
func (h *DispatchesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	var branchID, divisionID, warehouseID pgtype.UUID
	var dispatchedAt pgtype.Timestamptz
	var status string
	err = tx.QueryRow(ctx,
		`SELECT branch_id, division_id, warehouse_id, dispatched_at, status FROM dispatches WHERE id = $1`,
		pgUUID(id)).Scan(&branchID, &divisionID, &warehouseID, &dispatchedAt, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusUnprocessableEntity, "pengiriman sudah dibatalkan")
		return
	}
	whID := warehouseID.Bytes

	unitByItem := map[uuid.UUID]int32{}
	nameByItem := map[uuid.UUID]string{}
	{
		rows, err := tx.Query(ctx,
			`SELECT di.item_id, di.unit_index, i.units
			 FROM dispatch_items di JOIN items i ON i.id = di.item_id
			 WHERE di.dispatch_id = $1`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var unitIndex int32
			var unitsJSON []byte
			if err := rows.Scan(&itemID, &unitIndex, &unitsJSON); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
				return
			}
			if _, ok := unitByItem[itemID.Bytes]; !ok {
				unitByItem[itemID.Bytes] = baseUnitIndex(unitsJSON, unitIndex)
				nameByItem[itemID.Bytes] = baseUnitName(unitsJSON)
			}
		}
		rows.Close()
	}

	var invID pgtype.UUID
	_ = tx.QueryRow(ctx,
		`SELECT id FROM invoices WHERE dispatch_id = $1 ORDER BY created_at LIMIT 1`, pgUUID(id)).Scan(&invID)

	type agg struct {
		qty   float64
		value int64
	}
	perItem := map[uuid.UUID]*agg{}
	var totalBooked int64
	if invID.Valid {
		rows, err := tx.Query(ctx,
			`SELECT item_id, COALESCE(SUM(quantity * conversion_factor),0), COALESCE(SUM(quantity*price),0)::bigint
			 FROM invoice_items WHERE invoice_id = $1 GROUP BY item_id`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil nilai faktur")
			return
		}
		for rows.Next() {
			var itemID pgtype.UUID
			var qty pgtype.Numeric
			var value int64
			if err := rows.Scan(&itemID, &qty, &value); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca nilai faktur")
				return
			}
			perItem[itemID.Bytes] = &agg{qty: numericToFloat64(qty), value: value}
			totalBooked += value
		}
		rows.Close()
	}

	// On the dispatch's own date, so it nets out within the same period.
	reversalDate := dispatchedAt.Time
	if !dispatchedAt.Valid {
		reversalDate = time.Now()
	}

	for itemID, a := range perItem {
		if a.qty <= dispatchEpsilon {
			continue
		}
		unitIndex := unitByItem[itemID]
		unitName := nameByItem[itemID]
		if err := service.FIFOAdd(ctx, qtx, itemID, whID, a.qty, unitIndex, a.value, reversalDate); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengembalikan stok")
			return
		}
		if err := service.InsertStockHistory(ctx, qtx, service.StockHistoryParams{
			ItemID: itemID, WarehouseID: whID, QuantityChange: a.qty, UnitName: unitName,
			Type: "dispatch_cancel", Date: reversalDate, Value: a.value, SourceID: id, SourceType: "dispatch",
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat riwayat stok")
			return
		}
	}

	if invID.Valid {
		type invLine struct {
			itemID    pgtype.UUID
			quantity  pgtype.Numeric
			unitIndex pgtype.Int4
			price     int64
			factor    pgtype.Numeric
		}
		var lines []invLine
		rows, err := tx.Query(ctx,
			`SELECT item_id, quantity, unit_index, price, conversion_factor FROM invoice_items WHERE invoice_id = $1`, invID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil item faktur")
			return
		}
		for rows.Next() {
			var l invLine
			if err := rows.Scan(&l.itemID, &l.quantity, &l.unitIndex, &l.price, &l.factor); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca item faktur")
				return
			}
			lines = append(lines, l)
		}
		rows.Close()

		for _, l := range lines {
			negQty := floatToNumeric(-numericToFloat64(l.quantity))
			if _, err := qtx.CreateInvoiceItem(ctx, &db.CreateInvoiceItemParams{
				InvoiceID:        invID,
				ItemID:           l.itemID,
				Quantity:         negQty,
				UnitIndex:        l.unitIndex,
				Price:            l.price,
				Description:      pgtype.Text{String: "Pembatalan pengiriman", Valid: true},
				ConversionFactor: floatToNumeric(storedConversionFactor(l.factor)),
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan faktur")
				return
			}
		}
	}

	expAcct, _ := invoiceExpenseAccountID(ctx, qtx, uuid.Nil, uuidFromPg(divisionID), uuidFromPg(branchID))
	invAcct, _ := qtx.GetWarehouseInventoryAccountID(ctx, warehouseID)
	var invAcctIDValue uuid.UUID
	if invAcct.Valid {
		invAcctIDValue = invAcct.Bytes
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceDispatch,
		SourceID:    id,
		Description: "Pembalikan pengiriman (dibatalkan)",
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Cr(expAcct, totalBooked),
			service.Dr(invAcctIDValue, totalBooked),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan pengiriman")
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE dispatches SET status = 'cancelled', updated_at = now() WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengiriman")
		return
	}
	if invID.Valid {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices SET payment_status = 'cancelled' WHERE id = $1`, invID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui status faktur")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengiriman")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: userID, Username: username, Action: "DELETE", EntityType: "Dispatch",
		EntityID: id, Description: "Pembatalan pengiriman barang",
	})

	respondJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}
