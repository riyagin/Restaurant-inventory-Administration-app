package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type ActivityLogHandler struct {
	queries *db.Queries
}

func NewActivityLogHandler(queries *db.Queries) *ActivityLogHandler {
	return &ActivityLogHandler{queries: queries}
}

// List — GET /api/activity-log
func (h *ActivityLogHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	entityType := q.Get("entity_type")
	action := q.Get("action")
	search := q.Get("search")

	var fromDate, toDate pgtype.Date
	if s := q.Get("date_from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'date_from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := q.Get("date_to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'date_to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	page := 1
	limit := 50
	if s := q.Get("page"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			page = v
		}
	}
	if s := q.Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}
	offset := (page - 1) * limit

	total, err := h.queries.CountActivityLog(ctx, &db.CountActivityLogParams{
		Column1: entityType,
		Column2: action,
		Column3: search,
		Column4: fromDate,
		Column5: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung log aktivitas")
		return
	}

	rows, err := h.queries.ListActivityLog(ctx, &db.ListActivityLogParams{
		Column1: entityType,
		Column2: action,
		Column3: search,
		Column4: fromDate,
		Column5: toDate,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil log aktivitas")
		return
	}
	if rows == nil {
		rows = []*db.ActivityLog{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"rows":  rows,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Export — GET /api/activity-log/export
func (h *ActivityLogHandler) Export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var fromDate, toDate pgtype.Date
	if s := q.Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'from' tidak valid")
			return
		}
		fromDate = pgtype.Date{Time: t, Valid: true}
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal 'to' tidak valid")
			return
		}
		toDate = pgtype.Date{Time: t, Valid: true}
	}

	rows, err := h.queries.ListActivityLogForExport(ctx, &db.ListActivityLogForExportParams{
		Column1: fromDate,
		Column2: toDate,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil log aktivitas")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="activity-log.csv"`)

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"ID", "Username", "Action", "Entity Type", "Entity ID", "Description", "Created At"})
	for _, row := range rows {
		entityID := ""
		if row.EntityID.Valid {
			b := row.EntityID.Bytes
			entityID = uuidBytesToString(b)
		}
		createdAt := ""
		if row.CreatedAt.Valid {
			createdAt = row.CreatedAt.Time.Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			uuidBytesToString(row.ID.Bytes),
			row.Username,
			row.Action,
			row.EntityType,
			entityID,
			row.Description,
			createdAt,
		})
	}
	writer.Flush()
}

// DeleteOld — DELETE /api/activity-log
func (h *ActivityLogHandler) DeleteOld(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		BeforeDate string `json:"before_date"`
	}
	if err := parseBody(r, &body); err != nil || body.BeforeDate == "" {
		respondError(w, http.StatusBadRequest, "field 'before_date' diperlukan")
		return
	}
	t, err := time.Parse("2006-01-02", body.BeforeDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal 'before_date' tidak valid")
		return
	}

	before := pgtype.Timestamptz{Time: t, Valid: true}
	deleted, err := h.queries.DeleteOldActivityLog(ctx, before)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus log aktivitas")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func uuidBytesToString(b [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
