package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Shift system — per-branch shift definitions and the daily roster.
//
// The roster is edited from manpower planning under one date rule, enforced
// here rather than in the UI:
//
//   - a day before today is read-only for everyone — the roster that was in
//     force is history, and attendance was already judged against it;
//   - today's roster changes only with a manager's approval: an edit by anyone
//     else becomes a shift_change_requests row and the approved roster stays in
//     force until it is decided (a manager's own edit applies directly);
//   - future days apply directly.
type ShiftHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewShiftHandler(pool *pgxpool.Pool, queries *db.Queries) *ShiftHandler {
	return &ShiftHandler{pool: pool, queries: queries}
}

// errShiftRule is a validation failure worth showing to the user verbatim.
type errShiftRule struct {
	status int
	msg    string
}

func (e *errShiftRule) Error() string { return e.msg }

func shiftRule(status int, format string, args ...any) error {
	return &errShiftRule{status: status, msg: fmt.Sprintf(format, args...)}
}

func respondShiftErr(w http.ResponseWriter, err error, fallback string) {
	var rule *errShiftRule
	if errors.As(err, &rule) {
		respondError(w, rule.status, rule.msg)
		return
	}
	respondError(w, http.StatusInternalServerError, fallback)
}

func fmtIndoDate(t time.Time) string { return t.Format("02/01/2006") }

// ── Shift definitions ────────────────────────────────────────────────────────

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

const shiftDefinitionSelect = `
SELECT sd.id::text, sd.branch_id::text, b.name, sd.code, sd.name,
       to_char(sd.start1, 'HH24:MI'), to_char(sd.end1, 'HH24:MI'),
       to_char(sd.start2, 'HH24:MI'), to_char(sd.end2, 'HH24:MI'),
       sd.break_minutes, sd.delivery_only, sd.color, sd.sort_order, sd.is_active,
       (EXTRACT(EPOCH FROM sd.start1) / 60)::int, (EXTRACT(EPOCH FROM sd.end1) / 60)::int,
       (EXTRACT(EPOCH FROM sd.start2) / 60)::int, (EXTRACT(EPOCH FROM sd.end2) / 60)::int
FROM shift_definitions sd
JOIN branches b ON b.id = sd.branch_id`

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

// ── Roster ───────────────────────────────────────────────────────────────────

type shiftAssignmentEntry struct {
	EmployeeID string  `json:"employee_id"`
	Date       string  `json:"date"`        // YYYY-MM-DD
	ShiftID    *string `json:"shift_id"`    // null/"" = remove the shift
	DivisionID *string `json:"division_id"` // null/"" = the employee's home division
}

type shiftAssignmentsBody struct {
	Entries []shiftAssignmentEntry `json:"entries"`
	Reason  string                 `json:"reason"`
}

type shiftAssignmentResult struct {
	EmployeeID string `json:"employee_id"`
	Date       string `json:"date"`
	// applied | pending (awaiting a manager) | unchanged
	Status string `json:"status"`
}

// resolvedEntry is an entry after validation, ready to write.
type resolvedEntry struct {
	employeeID   pgtype.UUID
	employeeName string
	date         time.Time
	shiftID      pgtype.UUID // invalid = remove
	shiftLabel   string
	divisionID   pgtype.UUID // invalid = home division
}

func optUUID(s *string) (pgtype.UUID, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return pgtype.UUID{}, nil
	}
	id, err := parseUUID(strings.TrimSpace(*s))
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgUUID(id), nil
}

// resolveShiftEntry applies every rule a roster cell must satisfy, against the
// state inside the caller's transaction. The date rule is checked by callers,
// because approving a request and saving a cell treat "today" differently.
func resolveShiftEntry(ctx context.Context, q db.DBTX, e shiftAssignmentEntry) (*resolvedEntry, error) {
	empID, err := parseUUID(e.EmployeeID)
	if err != nil {
		return nil, shiftRule(http.StatusBadRequest, "karyawan tidak valid")
	}
	date, err := time.Parse("2006-01-02", strings.TrimSpace(e.Date))
	if err != nil {
		return nil, shiftRule(http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
	}
	shiftID, err := optUUID(e.ShiftID)
	if err != nil {
		return nil, shiftRule(http.StatusBadRequest, "shift tidak valid")
	}
	divisionID, err := optUUID(e.DivisionID)
	if err != nil {
		return nil, shiftRule(http.StatusBadRequest, "divisi tidak valid")
	}
	out := &resolvedEntry{employeeID: pgUUID(empID), date: date, shiftID: shiftID, divisionID: divisionID}

	var branchID pgtype.UUID
	var status string
	var deliveryStaff bool
	err = q.QueryRow(ctx, `SELECT full_name, branch_id, status, is_delivery_staff FROM employees WHERE id = $1`,
		out.employeeID).Scan(&out.employeeName, &branchID, &status, &deliveryStaff)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shiftRule(http.StatusNotFound, "karyawan tidak ditemukan")
		}
		return nil, err
	}

	if divisionID.Valid {
		var divBranch pgtype.UUID
		if err := q.QueryRow(ctx, `SELECT branch_id FROM divisions WHERE id = $1`, divisionID).Scan(&divBranch); err != nil {
			return nil, shiftRule(http.StatusBadRequest, "divisi tidak ditemukan")
		}
		if divBranch != branchID {
			return nil, shiftRule(http.StatusBadRequest, "divisi bukan milik cabang %s", out.employeeName)
		}
	}

	if !shiftID.Valid {
		return out, nil // removing a shift needs no further checks
	}
	if status != "active" {
		return nil, shiftRule(http.StatusConflict, "%s tidak aktif dan tidak dapat dijadwalkan", out.employeeName)
	}
	var shiftBranch pgtype.UUID
	var name, code string
	var active, deliveryOnly bool
	err = q.QueryRow(ctx, `SELECT branch_id, name, code, is_active, delivery_only FROM shift_definitions WHERE id = $1`,
		shiftID).Scan(&shiftBranch, &name, &code, &active, &deliveryOnly)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shiftRule(http.StatusNotFound, "shift tidak ditemukan")
		}
		return nil, err
	}
	out.shiftLabel = fmt.Sprintf("%s (%s)", name, code)
	if shiftBranch != branchID {
		return nil, shiftRule(http.StatusBadRequest, "shift %s bukan milik cabang %s", out.shiftLabel, out.employeeName)
	}
	if !active {
		return nil, shiftRule(http.StatusConflict, "shift %s sudah tidak aktif", out.shiftLabel)
	}
	if deliveryOnly && !deliveryStaff {
		return nil, shiftRule(http.StatusConflict,
			"%s khusus staf delivery order — tandai %s sebagai staf DO terlebih dahulu", out.shiftLabel, out.employeeName)
	}
	// An approved day off wins: rostering over it would put someone on the
	// coverage count who is not coming in.
	var leaveName string
	err = q.QueryRow(ctx, `
		SELECT lt.name FROM leave_requests lr JOIN leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.employee_id = $1 AND lr.status = 'approved' AND $2::date BETWEEN lr.start_date AND lr.end_date
		LIMIT 1`, out.employeeID, pgtype.Date{Time: date, Valid: true}).Scan(&leaveName)
	if err == nil {
		return nil, shiftRule(http.StatusConflict, "%s sedang %s pada %s — batalkan cutinya dulu untuk menjadwalkan shift",
			out.employeeName, leaveName, fmtIndoDate(date))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// applyShiftEntry writes an entry into the effective roster and retires any
// open request for that cell, since what it asked for is no longer the question.
func applyShiftEntry(ctx context.Context, q db.DBTX, e *resolvedEntry, userID pgtype.UUID) error {
	day := pgtype.Date{Time: e.date, Valid: true}
	var err error
	if e.shiftID.Valid {
		_, err = q.Exec(ctx, `
			INSERT INTO shift_assignments (employee_id, date, shift_id, division_id, updated_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (employee_id, date) DO UPDATE
			SET shift_id = EXCLUDED.shift_id, division_id = EXCLUDED.division_id,
			    updated_by = EXCLUDED.updated_by, updated_at = now()`,
			e.employeeID, day, e.shiftID, e.divisionID, userID)
	} else {
		_, err = q.Exec(ctx, `DELETE FROM shift_assignments WHERE employee_id = $1 AND date = $2`, e.employeeID, day)
	}
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE shift_change_requests SET status = 'superseded', decided_by = $3, decided_at = now()
		WHERE employee_id = $1 AND date = $2 AND status = 'pending'`, e.employeeID, day, userID)
	return err
}

// currentAssignment returns the cell's effective shift and division override.
func currentAssignment(ctx context.Context, q db.DBTX, employeeID pgtype.UUID, date time.Time) (pgtype.UUID, pgtype.UUID, error) {
	var shiftID, divisionID pgtype.UUID
	err := q.QueryRow(ctx, `SELECT shift_id, division_id FROM shift_assignments WHERE employee_id = $1 AND date = $2`,
		employeeID, pgtype.Date{Time: date, Valid: true}).Scan(&shiftID, &divisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, pgtype.UUID{}, nil
	}
	return shiftID, divisionID, err
}

// SaveShiftAssignments — PUT /api/hr/shift-assignments
// Body: {entries: [{employee_id, date, shift_id|null, division_id|null}], reason}
// The batch is atomic: one rule failure rejects all of it, naming the cell.
func (h *ShiftHandler) SaveShiftAssignments(w http.ResponseWriter, r *http.Request) {
	var body shiftAssignmentsBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if len(body.Entries) == 0 {
		respondError(w, http.StatusBadRequest, "tidak ada jadwal yang disimpan")
		return
	}
	if len(body.Entries) > 500 {
		respondError(w, http.StatusBadRequest, "maksimal 500 jadwal per simpan")
		return
	}
	ctx := r.Context()
	userID := pgUUID(middleware.UserIDFromCtx(ctx))
	canApprove := middleware.HasRole(ctx, middleware.RoleManager)
	now := today()
	reason := strings.TrimSpace(body.Reason)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	results := make([]shiftAssignmentResult, 0, len(body.Entries))
	var applied, pending int
	var logLines []string
	for _, entry := range body.Entries {
		e, err := resolveShiftEntry(ctx, tx, entry)
		if err != nil {
			respondShiftErr(w, err, "gagal memeriksa jadwal")
			return
		}
		if e.date.Before(now) {
			respondError(w, http.StatusConflict, fmt.Sprintf(
				"jadwal %s sudah lewat dan tidak dapat diubah", fmtIndoDate(e.date)))
			return
		}
		label := e.shiftLabel
		if label == "" {
			label = "tanpa shift"
		}
		res := shiftAssignmentResult{EmployeeID: entry.EmployeeID, Date: e.date.Format("2006-01-02"), Status: "applied"}

		if e.date.Equal(now) && !canApprove {
			curShift, curDiv, err := currentAssignment(ctx, tx, e.employeeID, e.date)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membaca jadwal")
				return
			}
			day := pgtype.Date{Time: e.date, Valid: true}
			if curShift == e.shiftID && curDiv == e.divisionID {
				// Asking for what is already in force: drop any open request instead.
				if _, err := tx.Exec(ctx, `
					UPDATE shift_change_requests SET status = 'superseded', decided_by = $3, decided_at = now()
					WHERE employee_id = $1 AND date = $2 AND status = 'pending'`, e.employeeID, day, userID); err != nil {
					respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan")
					return
				}
				res.Status = "unchanged"
				results = append(results, res)
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO shift_change_requests (employee_id, date, shift_id, division_id, prev_shift_id, reason, requested_by)
				VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)
				ON CONFLICT (employee_id, date) WHERE status = 'pending' DO UPDATE
				SET shift_id = EXCLUDED.shift_id, division_id = EXCLUDED.division_id,
				    prev_shift_id = EXCLUDED.prev_shift_id, reason = EXCLUDED.reason,
				    requested_by = EXCLUDED.requested_by, requested_at = now()`,
				e.employeeID, day, e.shiftID, e.divisionID, curShift, reason, userID); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan perubahan shift")
				return
			}
			res.Status = "pending"
			pending++
			logLines = append(logLines, fmt.Sprintf("%s %s → %s (menunggu persetujuan)", e.employeeName, fmtIndoDate(e.date), label))
		} else {
			if err := applyShiftEntry(ctx, tx, e, userID); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan jadwal")
				return
			}
			applied++
			logLines = append(logLines, fmt.Sprintf("%s %s → %s", e.employeeName, fmtIndoDate(e.date), label))
		}
		results = append(results, res)
	}

	if len(logLines) > 0 {
		desc := "Mengubah jadwal shift: " + strings.Join(logLines, "; ")
		if len(logLines) > 5 {
			desc = fmt.Sprintf("Mengubah jadwal shift %d hari-karyawan (%s; …)", len(logLines), strings.Join(logLines[:5], "; "))
		}
		_ = service.LogActivity(ctx, h.queries.WithTx(tx), service.LogParams{
			UserID:      middleware.UserIDFromCtx(ctx),
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "shift_assignment",
			Description: desc,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan jadwal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"applied": applied, "pending": pending, "results": results})
}

// ── Change requests (today's roster) ─────────────────────────────────────────

type shiftChangeRequestRow struct {
	ID                  string  `json:"id"`
	EmployeeID          string  `json:"employee_id"`
	EmployeeName        string  `json:"employee_name"`
	EmployeeCode        string  `json:"employee_code"`
	BranchID            *string `json:"branch_id"`
	BranchName          *string `json:"branch_name"`
	Date                string  `json:"date"`
	ShiftID             *string `json:"shift_id"`
	ShiftName           *string `json:"shift_name"`
	ShiftCode           *string `json:"shift_code"`
	PrevShiftName       *string `json:"prev_shift_name"`
	PrevShiftCode       *string `json:"prev_shift_code"`
	DivisionName        *string `json:"division_name"`
	Reason              *string `json:"reason"`
	Status              string  `json:"status"`
	DecisionNote        *string `json:"decision_note"`
	RequestedByUsername *string `json:"requested_by_username"`
	RequestedAt         string  `json:"requested_at"`
	// Expired: the day has passed, so the request can only be rejected.
	Expired bool `json:"expired"`
}

// ListShiftChangeRequests — GET /api/hr/shift-change-requests?status=pending&branch_id=
func (h *ShiftHandler) ListShiftChangeRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	branchID := strings.TrimSpace(q.Get("branch_id"))
	rows, err := h.pool.Query(r.Context(), `
		SELECT cr.id::text, cr.employee_id::text, e.full_name, e.employee_code,
		       e.branch_id::text, b.name, to_char(cr.date, 'YYYY-MM-DD'),
		       cr.shift_id::text, sd.name, sd.code, ps.name, ps.code,
		       COALESCE(d.name, hd.name), cr.reason, cr.status, cr.decision_note,
		       u.username, to_char(cr.requested_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'),
		       cr.date < $3::date
		FROM shift_change_requests cr
		JOIN employees e ON e.id = cr.employee_id
		LEFT JOIN branches b ON b.id = e.branch_id
		LEFT JOIN shift_definitions sd ON sd.id = cr.shift_id
		LEFT JOIN shift_definitions ps ON ps.id = cr.prev_shift_id
		LEFT JOIN divisions d ON d.id = cr.division_id
		LEFT JOIN divisions hd ON hd.id = e.division_id
		LEFT JOIN users u ON u.id = cr.requested_by
		WHERE ($1 = '' OR cr.status = $1)
		  AND ($2 = '' OR e.branch_id::text = $2)
		ORDER BY cr.date, cr.requested_at
		LIMIT 500`, status, branchID, pgtype.Date{Time: today(), Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan perubahan shift")
		return
	}
	defer rows.Close()
	out := []shiftChangeRequestRow{}
	for rows.Next() {
		var c shiftChangeRequestRow
		if err := rows.Scan(&c.ID, &c.EmployeeID, &c.EmployeeName, &c.EmployeeCode,
			&c.BranchID, &c.BranchName, &c.Date,
			&c.ShiftID, &c.ShiftName, &c.ShiftCode, &c.PrevShiftName, &c.PrevShiftCode,
			&c.DivisionName, &c.Reason, &c.Status, &c.DecisionNote,
			&c.RequestedByUsername, &c.RequestedAt, &c.Expired); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca pengajuan perubahan shift")
			return
		}
		out = append(out, c)
	}
	respondJSON(w, http.StatusOK, out)
}

// decideShiftChange locks a pending request and hands it to fn inside a
// transaction; fn returns the new status and the log sentence.
func (h *ShiftHandler) decideShiftChange(w http.ResponseWriter, r *http.Request,
	fn func(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error)) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body decisionBody
	_ = parseBody(r, &body)
	ctx := r.Context()
	userID := pgUUID(middleware.UserIDFromCtx(ctx))

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	var entry shiftAssignmentEntry
	var status string
	err = tx.QueryRow(ctx, `
		SELECT employee_id::text, to_char(date, 'YYYY-MM-DD'), shift_id::text, division_id::text, status
		FROM shift_change_requests WHERE id = $1 FOR UPDATE`, pgUUID(id)).
		Scan(&entry.EmployeeID, &entry.Date, &entry.ShiftID, &entry.DivisionID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengajuan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan")
		return
	}
	if status != "pending" {
		respondError(w, http.StatusConflict, "hanya pengajuan berstatus menunggu yang dapat diputuskan")
		return
	}
	newStatus, desc, err := fn(ctx, tx, entry)
	if err != nil {
		respondShiftErr(w, err, "gagal memproses pengajuan")
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shift_change_requests SET status = $2, decision_note = NULLIF($3, ''), decided_by = $4, decided_at = now()
		WHERE id = $1`, pgUUID(id), newStatus, strings.TrimSpace(body.Note), userID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan keputusan")
		return
	}
	_ = service.LogActivity(ctx, h.queries.WithTx(tx), service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "shift_change_request",
		EntityID:    id,
		Description: desc,
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan keputusan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

// ApproveShiftChange — POST /api/hr/shift-change-requests/{id}/approve (manager)
// Re-validates against today's state: a leave approved or a shift deactivated
// after the request was made still wins.
func (h *ShiftHandler) ApproveShiftChange(w http.ResponseWriter, r *http.Request) {
	h.decideShiftChange(w, r, func(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error) {
		e, err := resolveShiftEntry(ctx, tx, entry)
		if err != nil {
			return "", "", err
		}
		if e.date.Before(today()) {
			return "", "", shiftRule(http.StatusConflict,
				"tanggal %s sudah lewat — pengajuan ini hanya dapat ditolak", fmtIndoDate(e.date))
		}
		userID := pgUUID(middleware.UserIDFromCtx(ctx))
		// applyShiftEntry would supersede this very request; write the roster
		// directly and let the caller mark it approved.
		day := pgtype.Date{Time: e.date, Valid: true}
		if e.shiftID.Valid {
			_, err = tx.Exec(ctx, `
				INSERT INTO shift_assignments (employee_id, date, shift_id, division_id, updated_by)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (employee_id, date) DO UPDATE
				SET shift_id = EXCLUDED.shift_id, division_id = EXCLUDED.division_id,
				    updated_by = EXCLUDED.updated_by, updated_at = now()`,
				e.employeeID, day, e.shiftID, e.divisionID, userID)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM shift_assignments WHERE employee_id = $1 AND date = $2`, e.employeeID, day)
		}
		if err != nil {
			return "", "", err
		}
		label := e.shiftLabel
		if label == "" {
			label = "tanpa shift"
		}
		return "approved", fmt.Sprintf("Menyetujui perubahan shift %s %s → %s", e.employeeName, fmtIndoDate(e.date), label), nil
	})
}

// RejectShiftChange — POST /api/hr/shift-change-requests/{id}/reject (manager)
func (h *ShiftHandler) RejectShiftChange(w http.ResponseWriter, r *http.Request) {
	h.decideShiftChange(w, r, func(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error) {
		var name string
		_ = tx.QueryRow(ctx, `SELECT full_name FROM employees WHERE id::text = $1`, entry.EmployeeID).Scan(&name)
		return "rejected", fmt.Sprintf("Menolak perubahan shift %s tanggal %s", name, entry.Date), nil
	})
}

// ── Employee shift profile ───────────────────────────────────────────────────

type shiftProfileBody struct {
	DivisionID      *string `json:"division_id"`
	IsDeliveryStaff bool    `json:"is_delivery_staff"`
}

// UpdateEmployeeShiftProfile — PUT /api/hr/employees/{id}/shift-profile
// Sets the home division and the delivery-order flag. Un-flagging someone who
// is still rostered on a delivery-only shift from today on is refused: the
// roster would then hold an assignment its own rule forbids.
func (h *ShiftHandler) UpdateEmployeeShiftProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body shiftProfileBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	divisionID, err := optUUID(body.DivisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "divisi tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgUUID(id)

	var name string
	var branchID pgtype.UUID
	if err := h.pool.QueryRow(ctx, `SELECT full_name, branch_id FROM employees WHERE id = $1`, empID).Scan(&name, &branchID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil karyawan")
		return
	}
	var divName string
	if divisionID.Valid {
		var divBranch pgtype.UUID
		if err := h.pool.QueryRow(ctx, `SELECT branch_id, name FROM divisions WHERE id = $1`, divisionID).Scan(&divBranch, &divName); err != nil {
			respondError(w, http.StatusBadRequest, "divisi tidak ditemukan")
			return
		}
		if divBranch != branchID {
			respondError(w, http.StatusBadRequest, "divisi bukan milik cabang karyawan ini")
			return
		}
	}
	if !body.IsDeliveryStaff {
		var n int
		if err := h.pool.QueryRow(ctx, `
			SELECT count(*) FROM shift_assignments sa JOIN shift_definitions sd ON sd.id = sa.shift_id
			WHERE sa.employee_id = $1 AND sa.date >= $2 AND sd.delivery_only`,
			empID, pgtype.Date{Time: today(), Valid: true}).Scan(&n); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa jadwal")
			return
		}
		if n > 0 {
			respondError(w, http.StatusConflict, fmt.Sprintf(
				"%s masih dijadwalkan %d hari di shift khusus delivery order — ubah jadwalnya dulu", name, n))
			return
		}
	}
	if _, err := h.pool.Exec(ctx, `UPDATE employees SET division_id = $2, is_delivery_staff = $3, updated_at = now() WHERE id = $1`,
		empID, divisionID, body.IsDeliveryStaff); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan profil shift")
		return
	}
	if divName == "" {
		divName = "tanpa divisi"
	}
	do := "bukan staf DO"
	if body.IsDeliveryStaff {
		do = "staf DO"
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah profil shift %s: divisi %s, %s", name, divName, do),
	})
	respondJSON(w, http.StatusOK, map[string]any{"division_id": body.DivisionID, "is_delivery_staff": body.IsDeliveryStaff})
}
