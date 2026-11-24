package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type shiftDefinition struct {
	ID           string  `json:"id"`
	BranchID     string  `json:"branch_id"`
	BranchName   string  `json:"branch_name"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Start1       string  `json:"start1"`
	End1         string  `json:"end1"`
	Start2       *string `json:"start2"`
	End2         *string `json:"end2"`
	BreakMinutes int     `json:"break_minutes"`
	WorkMinutes  int     `json:"work_minutes"`
	DeliveryOnly bool    `json:"delivery_only"`
	Color        string  `json:"color"`
	SortOrder    int     `json:"sort_order"`
	IsActive     bool    `json:"is_active"`
}

func scanShiftDefinition(row pgx.Row) (*shiftDefinition, error) {
	var s shiftDefinition
	var m1, m2 int
	var m3, m4 *int
	if err := row.Scan(&s.ID, &s.BranchID, &s.BranchName, &s.Code, &s.Name,
		&s.Start1, &s.End1, &s.Start2, &s.End2,
		&s.BreakMinutes, &s.DeliveryOnly, &s.Color, &s.SortOrder, &s.IsActive,
		&m1, &m2, &m3, &m4); err != nil {
		return nil, err
	}
	s.WorkMinutes = service.ShiftWorkMinutes(m1, m2, m3, m4, s.BreakMinutes)
	return &s, nil
}

// loadShiftDefinitions returns shifts ordered by branch, then sort order. An
// empty branchID returns every branch's.
func loadShiftDefinitions(ctx context.Context, q db.DBTX, branchID string, includeInactive bool) ([]*shiftDefinition, error) {
	sql := shiftDefinitionSelect + ` WHERE ($1 = '' OR sd.branch_id::text = $1) AND ($2 OR sd.is_active)
ORDER BY b.name, sd.sort_order, sd.start1, sd.code`
	rows, err := q.Query(ctx, sql, branchID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*shiftDefinition{}
	for rows.Next() {
		s, err := scanShiftDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListShifts — GET /api/hr/shifts?branch_id=&include_inactive=1
func (h *ShiftHandler) ListShifts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := loadShiftDefinitions(r.Context(), h.pool,
		strings.TrimSpace(q.Get("branch_id")), q.Get("include_inactive") == "1")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar shift")
		return
	}
	respondJSON(w, http.StatusOK, list)
}

type shiftBody struct {
	BranchID     string `json:"branch_id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Start1       string `json:"start1"`
	End1         string `json:"end1"`
	Start2       string `json:"start2"`
	End2         string `json:"end2"`
	BreakMinutes int    `json:"break_minutes"`
	DeliveryOnly bool   `json:"delivery_only"`
	Color        string `json:"color"`
	SortOrder    int    `json:"sort_order"`
	IsActive     *bool  `json:"is_active"`
}

type parsedShift struct {
	code, name, color      string
	start1, end1           pgtype.Time
	start2, end2           pgtype.Time // invalid = single segment
	breakMinutes, sortOrd  int
	deliveryOnly, isActive bool
}

// validate checks the body and returns the parsed values. Overnight shifts are
// not supported: attendance records one calendar day per row.
func (b *shiftBody) validate() (*parsedShift, error) {
	p := &parsedShift{
		code:         strings.ToUpper(strings.TrimSpace(b.Code)),
		name:         strings.TrimSpace(b.Name),
		color:        strings.TrimSpace(b.Color),
		breakMinutes: b.BreakMinutes,
		sortOrd:      b.SortOrder,
		deliveryOnly: b.DeliveryOnly,
		isActive:     b.IsActive == nil || *b.IsActive,
	}
	if p.code == "" || p.name == "" {
		return nil, shiftRule(http.StatusBadRequest, "kode dan nama shift wajib diisi")
	}
	if len(p.code) > 8 {
		return nil, shiftRule(http.StatusBadRequest, "kode shift maksimal 8 karakter")
	}
	if p.color == "" {
		p.color = "#dbeafe"
	}
	var err error
	if p.start1, err = parseClockTime(b.Start1); err != nil {
		return nil, shiftRule(http.StatusBadRequest, "jam mulai tidak valid (HH:MM)")
	}
	if p.end1, err = parseClockTime(b.End1); err != nil {
		return nil, shiftRule(http.StatusBadRequest, "jam selesai tidak valid (HH:MM)")
	}
	if p.end1.Microseconds <= p.start1.Microseconds {
		return nil, shiftRule(http.StatusBadRequest, "jam selesai harus setelah jam mulai (shift melewati tengah malam tidak didukung)")
	}
	s2, e2 := strings.TrimSpace(b.Start2), strings.TrimSpace(b.End2)
	if (s2 == "") != (e2 == "") {
		return nil, shiftRule(http.StatusBadRequest, "jam mulai dan selesai sesi kedua harus diisi keduanya atau dikosongkan")
	}
	if s2 != "" {
		if p.start2, err = parseClockTime(s2); err != nil {
			return nil, shiftRule(http.StatusBadRequest, "jam mulai sesi kedua tidak valid (HH:MM)")
		}
		if p.end2, err = parseClockTime(e2); err != nil {
			return nil, shiftRule(http.StatusBadRequest, "jam selesai sesi kedua tidak valid (HH:MM)")
		}
		if p.start2.Microseconds < p.end1.Microseconds || p.end2.Microseconds <= p.start2.Microseconds {
			return nil, shiftRule(http.StatusBadRequest, "sesi kedua harus dimulai setelah sesi pertama selesai")
		}
	}
	if p.breakMinutes < 0 {
		return nil, shiftRule(http.StatusBadRequest, "istirahat tidak boleh negatif")
	}
	return p, nil
}

// CreateShift — POST /api/hr/shifts
func (h *ShiftHandler) CreateShift(w http.ResponseWriter, r *http.Request) {
	var body shiftBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang wajib dipilih")
		return
	}
	p, err := body.validate()
	if err != nil {
		respondShiftErr(w, err, "data shift tidak valid")
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	err = h.pool.QueryRow(ctx, `
		INSERT INTO shift_definitions (branch_id, code, name, start1, end1, start2, end2,
		                               break_minutes, delivery_only, color, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id`,
		pgUUID(branchID), p.code, p.name, p.start1, p.end1, p.start2, p.end2,
		p.breakMinutes, p.deliveryOnly, p.color, p.sortOrd, p.isActive).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "shift_definitions_branch_id_code_key") {
			respondError(w, http.StatusConflict, "kode shift sudah dipakai di cabang ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan shift")
		return
	}
	s, err := scanShiftDefinition(h.pool.QueryRow(ctx, shiftDefinitionSelect+` WHERE sd.id = $1`, id))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil shift")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "shift",
		EntityID:    id,
		Description: fmt.Sprintf("Menambah shift %s (%s) di %s", s.Name, s.Code, s.BranchName),
	})
	respondJSON(w, http.StatusCreated, s)
}

// UpdateShift — PUT /api/hr/shifts/{id}
// Changing a shift's hours also changes how attendance on already-rostered
// future days is judged; past attendance keeps the flags it was computed with.
// The branch is fixed: moving a shift would strand the assignments that use it.
func (h *ShiftHandler) UpdateShift(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body shiftBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	p, err := body.validate()
	if err != nil {
		respondShiftErr(w, err, "data shift tidak valid")
		return
	}
	ctx := r.Context()
	tag, err := h.pool.Exec(ctx, `
		UPDATE shift_definitions
		SET code = $2, name = $3, start1 = $4, end1 = $5, start2 = $6, end2 = $7,
		    break_minutes = $8, delivery_only = $9, color = $10, sort_order = $11, is_active = $12
		WHERE id = $1`,
		pgUUID(id), p.code, p.name, p.start1, p.end1, p.start2, p.end2,
		p.breakMinutes, p.deliveryOnly, p.color, p.sortOrd, p.isActive)
	if err != nil {
		if strings.Contains(err.Error(), "shift_definitions_branch_id_code_key") {
			respondError(w, http.StatusConflict, "kode shift sudah dipakai di cabang ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan shift")
		return
	}
	if tag.RowsAffected() == 0 {
		respondError(w, http.StatusNotFound, "shift tidak ditemukan")
		return
	}
	s, err := scanShiftDefinition(h.pool.QueryRow(ctx, shiftDefinitionSelect+` WHERE sd.id = $1`, id))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil shift")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "shift",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah shift %s (%s) di %s", s.Name, s.Code, s.BranchName),
	})
	respondJSON(w, http.StatusOK, s)
}

// DeleteShift — DELETE /api/hr/shifts/{id}
// A shift that has ever been rostered is deactivated instead of deleted: the
// roster of past days must keep saying which shift people worked.
func (h *ShiftHandler) DeleteShift(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	s, err := scanShiftDefinition(h.pool.QueryRow(ctx, shiftDefinitionSelect+` WHERE sd.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "shift tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil shift")
		return
	}
	var refs int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM shift_assignments WHERE shift_id = $1`, id).Scan(&refs); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa penggunaan shift")
		return
	}
	action, desc := "DELETE", fmt.Sprintf("Menghapus shift %s (%s) di %s", s.Name, s.Code, s.BranchName)
	if refs > 0 {
		_, err = h.pool.Exec(ctx, `UPDATE shift_definitions SET is_active = false WHERE id = $1`, id)
		action, desc = "UPDATE", fmt.Sprintf("Menonaktifkan shift %s (%s) di %s (masih dipakai jadwal)", s.Name, s.Code, s.BranchName)
	} else {
		_, err = h.pool.Exec(ctx, `DELETE FROM shift_definitions WHERE id = $1`, id)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus shift")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      action,
		EntityType:  "shift",
		EntityID:    id,
		Description: desc,
	})
	respondJSON(w, http.StatusOK, map[string]any{"deactivated": refs > 0})
}
