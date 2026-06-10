package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type StockHistoryHandler struct {
	queries *db.Queries
}

func NewStockHistoryHandler(queries *db.Queries) *StockHistoryHandler {
	return &StockHistoryHandler{queries: queries}
}

func (h *StockHistoryHandler) List(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseUUID(chi.URLParam(r, "itemId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "itemId tidak valid")
		return
	}

	var fromDate pgtype.Date
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}

	var toDate pgtype.Date
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListStockHistoryByItem(r.Context(), &db.ListStockHistoryByItemParams{
		ItemID:  pgtype.UUID{Bytes: itemID, Valid: true},
		Column2: fromDate,
		Column3: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat stok")
		return
	}
	if rows == nil {
		rows = []*db.ListStockHistoryByItemRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}
