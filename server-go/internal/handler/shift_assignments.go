package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

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

// resolveShiftEntry checks a cell's rules; callers check the date rule.
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
		return nil, shiftRule(http.StatusConflict, "%s sedang %s pada %s — batalkan liburnya dulu untuk menjadwalkan shift",
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
	if err := recomputeTodayAttendance(ctx, q, e.employeeID, e.date); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE shift_change_requests SET status = 'superseded', decided_by = $3, decided_at = now()
		WHERE employee_id = $1 AND date = $2 AND status = 'pending'`, e.employeeID, day, userID)
	return err
}

// recomputeTodayAttendance re-judges today, since lateness is measured against the shift.
func recomputeTodayAttendance(ctx context.Context, q db.DBTX, employeeID pgtype.UUID, date time.Time) error {
	if date.Format("2006-01-02") != service.TodayWIB().Format("2006-01-02") {
		return nil
	}
	return service.RecomputeDay(ctx, db.New(q), employeeID, date)
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

// SaveShiftAssignments — PUT /api/hr/shift-assignments (atomic: one bad cell rejects the batch)
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
