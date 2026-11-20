package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

type DispatchesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewDispatchesHandler(pool *pgxpool.Pool, queries *db.Queries) *DispatchesHandler {
	return &DispatchesHandler{pool: pool, queries: queries}
}

// List — GET /api/dispatches
func (h *DispatchesHandler) List(w http.ResponseWriter, r *http.Request) {
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

	rows, err := h.queries.ListDispatches(ctx, &db.ListDispatchesParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}
	if rows == nil {
		rows = []*db.ListDispatchesRow{}
	}

	type listItem struct {
		ID         pgtype.UUID    `json:"id"`
		DispatchID pgtype.UUID    `json:"dispatch_id"`
		ItemID     pgtype.UUID    `json:"item_id"`
		Quantity   pgtype.Numeric `json:"quantity"`
		UnitIndex  int32          `json:"unit_index"`
		UnitName   string         `json:"unit_name"`
		// Factor the line was booked at, so an edit reopens with the same conversion.
		ConversionFactor pgtype.Numeric `json:"conversion_factor"`
		ItemName         string         `json:"item_name"`
		ItemCode         pgtype.Text    `json:"item_code"`
	}
	itemRows, err := h.pool.Query(ctx, `
		SELECT di.id, di.dispatch_id, di.item_id, di.quantity, di.unit_index, di.unit_name,
		       di.conversion_factor, i.name AS item_name, i.code AS item_code
		FROM dispatch_items di
		JOIN items i ON i.id = di.item_id
		ORDER BY i.name
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
		return
	}
	defer itemRows.Close()

	byDispatch := map[pgtype.UUID][]listItem{}
	for itemRows.Next() {
		var it listItem
		if err := itemRows.Scan(&it.ID, &it.DispatchID, &it.ItemID, &it.Quantity,
			&it.UnitIndex, &it.UnitName, &it.ConversionFactor, &it.ItemName, &it.ItemCode); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca item pengiriman")
			return
		}
		byDispatch[it.DispatchID] = append(byDispatch[it.DispatchID], it)
	}

	statusByID := map[pgtype.UUID]string{}
	if statusRows, err := h.pool.Query(ctx, `SELECT id, status FROM dispatches`); err == nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var did pgtype.UUID
			var st string
			if statusRows.Scan(&did, &st) == nil {
				statusByID[did] = st
			}
		}
	}

	type dispatchWithItems struct {
		*db.ListDispatchesRow
		Status string     `json:"status"`
		Items  []listItem `json:"items"`
	}
	result := make([]dispatchWithItems, len(rows))
	for i, row := range rows {
		items := byDispatch[row.ID]
		if items == nil {
			items = []listItem{}
		}
		status := statusByID[row.ID]
		if status == "" {
			status = "active"
		}
		result[i] = dispatchWithItems{ListDispatchesRow: row, Status: status, Items: items}
	}

	respondJSON(w, http.StatusOK, result)
}

// Get — GET /api/dispatches/:id
func (h *DispatchesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	dispatch, err := h.queries.GetDispatchByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengiriman tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data pengiriman")
		return
	}

	items, err := h.queries.GetDispatchItems(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil item pengiriman")
		return
	}
	if items == nil {
		items = []*db.GetDispatchItemsRow{}
	}

	status := "active"
	_ = h.pool.QueryRow(ctx, `SELECT status FROM dispatches WHERE id = $1`,
		pgtype.UUID{Bytes: id, Valid: true}).Scan(&status)

	respondJSON(w, http.StatusOK, map[string]any{
		"dispatch": dispatch,
		"status":   status,
		"items":    items,
	})
}

// ConversionFactor overrides, for this line only, how many base units the unit holds; 0 uses the item's units.
type dispatchItemInput struct {
	ItemID           string  `json:"item_id"`
	Quantity         float64 `json:"quantity"`
	UnitIndex        int32   `json:"unit_index"`
	UnitName         string  `json:"unit_name"`
	ConversionFactor float64 `json:"conversion_factor"`
}

func itemKey(itemID uuid.UUID, unitIndex int32) string {
	return itemID.String() + "|" + strconv.Itoa(int(unitIndex))
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

// pgDateOrNull returns NULL when absent, so a COALESCE keeps the stored value.
func pgDateOrNull(present bool, t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: present}
}

func pgTimestamptzOrNull(present bool, t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: present}
}

const dispatchEpsilon = 0.0001

func uuidFromPg(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return p.Bytes
}
