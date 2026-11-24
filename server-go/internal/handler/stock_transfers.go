package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
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

	// Status lives on a column added after the sqlc query was generated (migration
	// 020), so fetch it separately and attach it to each row.
	statusByID := map[pgtype.UUID]string{}
	if statusRows, err := h.pool.Query(ctx, `SELECT id, status FROM stock_transfers`); err == nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var stID pgtype.UUID
			var st string
			if statusRows.Scan(&stID, &st) == nil {
				statusByID[stID] = st
			}
		}
	}

	type transferWithStatus struct {
		*db.ListStockTransfersRow
		Status string `json:"status"`
	}
	result := make([]transferWithStatus, len(rows))
	for i, row := range rows {
		status := statusByID[row.ID]
		if status == "" {
			status = "active"
		}
		result[i] = transferWithStatus{ListStockTransfersRow: row, Status: status}
	}

	respondJSON(w, http.StatusOK, result)
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

// transferItemInput mirrors the item payload used by Update.
type transferItemInput struct {
	ItemID    string  `json:"item_id"`
	Quantity  float64 `json:"quantity"`
	UnitIndex int32   `json:"unit_index"`
	UnitName  string  `json:"unit_name"`
}

// postInventoryMove records value moving between two warehouse inventory
// accounts — the shape every transfer, transfer edit and transfer cancellation
// takes. Either side may be unset (a warehouse with no inventory account
// configured); service.Post routes those legs to the suspense account so the
// entry still balances instead of silently losing one side.
func postInventoryMove(
	ctx context.Context,
	qtx *db.Queries,
	date time.Time,
	groupID, userID uuid.UUID,
	description string,
	fromAcct, toAcct pgtype.UUID,
	value int64,
) error {
	var from, to uuid.UUID
	if fromAcct.Valid {
		from = fromAcct.Bytes
	}
	if toAcct.Valid {
		to = toAcct.Bytes
	}
	_, err := service.Post(ctx, qtx, service.Entry{
		Date:        date,
		SourceType:  service.SourceTransfer,
		SourceID:    groupID,
		Description: description,
		CreatedBy:   userID,
		Lines: []service.Line{
			service.Cr(from, value),
			service.Dr(to, value),
		},
	})
	return err
}
