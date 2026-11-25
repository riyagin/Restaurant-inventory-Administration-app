package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

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

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	newStatus, err := h.decideShiftChangeInTx(ctx, tx, id, body.Note, "", fn)
	if err != nil {
		switch {
		case errors.Is(err, errShiftRequestNotFound):
			respondError(w, http.StatusNotFound, "pengajuan tidak ditemukan")
		case errors.Is(err, errShiftRequestNotPending):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondShiftErr(w, err, "gagal memproses pengajuan")
		}
		return
	}
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan keputusan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

var (
	errShiftRequestNotFound   = errors.New("pengajuan tidak ditemukan")
	errShiftRequestNotPending = errors.New("hanya pengajuan berstatus menunggu yang dapat diputuskan")
)

// decideShiftChangeInTx locks a pending request, hands it to fn and records the
// decision on tx; the caller owns the transaction. logSuffix is appended to the
// activity-log sentence.
func (h *ShiftHandler) decideShiftChangeInTx(ctx context.Context, tx pgx.Tx, id [16]byte, note, logSuffix string,
	fn func(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error)) (string, error) {
	var entry shiftAssignmentEntry
	var status string
	err := tx.QueryRow(ctx, `
		SELECT employee_id::text, to_char(date, 'YYYY-MM-DD'), shift_id::text, division_id::text, status
		FROM shift_change_requests WHERE id = $1 FOR UPDATE`, pgUUID(id)).
		Scan(&entry.EmployeeID, &entry.Date, &entry.ShiftID, &entry.DivisionID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errShiftRequestNotFound
		}
		return "", err
	}
	if status != "pending" {
		return "", errShiftRequestNotPending
	}
	newStatus, desc, err := fn(ctx, tx, entry)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shift_change_requests SET status = $2, decision_note = NULLIF($3, ''), decided_by = $4, decided_at = now()
		WHERE id = $1`, pgUUID(id), newStatus, strings.TrimSpace(note), pgUUID(middleware.UserIDFromCtx(ctx))); err != nil {
		return "", err
	}
	_ = service.LogActivity(ctx, h.queries.WithTx(tx), service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "shift_change_request",
		EntityID:    id,
		Description: desc + logSuffix,
	})
	return newStatus, nil
}

// ApproveShiftChange — POST /api/hr/shift-change-requests/{id}/approve (manager; re-validated)
func (h *ShiftHandler) ApproveShiftChange(w http.ResponseWriter, r *http.Request) {
	h.decideShiftChange(w, r, h.approveShiftChangeFn)
}

// approveShiftChangeFn writes the approved roster cell; used by the single and
// the bulk approval.
func (h *ShiftHandler) approveShiftChangeFn(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error) {
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
	if err := recomputeTodayAttendance(ctx, tx, e.employeeID, e.date); err != nil {
		return "", "", err
	}
	label := e.shiftLabel
	if label == "" {
		label = "tanpa shift"
	}
	return "approved", fmt.Sprintf("Menyetujui perubahan shift %s %s → %s", e.employeeName, fmtIndoDate(e.date), label), nil
}

// RejectShiftChange — POST /api/hr/shift-change-requests/{id}/reject (manager)
func (h *ShiftHandler) RejectShiftChange(w http.ResponseWriter, r *http.Request) {
	h.decideShiftChange(w, r, func(ctx context.Context, tx pgx.Tx, entry shiftAssignmentEntry) (string, string, error) {
		var name string
		_ = tx.QueryRow(ctx, `SELECT full_name FROM employees WHERE id::text = $1`, entry.EmployeeID).Scan(&name)
		return "rejected", fmt.Sprintf("Menolak perubahan shift %s tanggal %s", name, entry.Date), nil
	})
}
