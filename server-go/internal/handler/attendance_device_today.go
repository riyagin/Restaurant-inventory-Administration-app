package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// deviceTodayEntry is one employee's day as the SERVER recorded it.
type deviceTodayEntry struct {
	EmployeeCode string  `json:"employee_code"`
	FullName     string  `json:"full_name"`
	FacePhotoURL *string `json:"face_photo_url,omitempty"`
	IsVisitor    bool    `json:"is_visitor"` // from another branch, punched on this device today
	Status       *string `json:"status"`     // nil = no record yet today
	CheckIn      any     `json:"check_in"`
	CheckOut     any     `json:"check_out"`
	CheckInSrc   *string `json:"check_in_source"`
	CheckOutSrc  *string `json:"check_out_source"`
	// false when the slot's time came from a device clock the server could not
	// check (see service/device_punch.go). A manual correction counts as verified.
	CheckInVerified  bool `json:"check_in_verified"`
	CheckOutVerified bool `json:"check_out_verified"`
	IsLate           bool `json:"is_late"`
	LateMinutes      int  `json:"late_minutes"`
	IsEarlyLeave     bool `json:"is_early_leave"`
}

// Today — GET /api/hr/attendance/device/today
//
// What the kiosk's "Kehadiran Hari Ini" screen shows: for every active
// employee of the device's branch (plus visitors who punched on this device
// today), today's check-in / check-out exactly as stored on the server. The
// kiosk never computes these itself, so an employee reading the screen sees the
// time that payroll will use.
func (h *AttendanceDeviceHandler) Today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dev, ok := middleware.DeviceFromCtx(ctx)
	if !ok || dev == nil {
		respondError(w, http.StatusUnauthorized, "perangkat tidak dikenal")
		return
	}
	if !dev.BranchID.Valid {
		respondError(w, http.StatusBadRequest, "perangkat belum terhubung ke cabang")
		return
	}

	now := time.Now()
	today := service.TodayWIB()
	pgToday := pgtype.Date{Time: today, Valid: true}

	rows, err := h.pool.Query(ctx, `
		WITH roster AS (
		  SELECT e.id, e.employee_code, e.full_name, e.photo_path, false AS is_visitor
		  FROM employees e
		  WHERE e.branch_id = $1 AND e.status = 'active'
		  UNION
		  SELECT DISTINCT e.id, e.employee_code, e.full_name, e.photo_path, true
		  FROM attendance_punches p
		  JOIN employees e ON e.id = p.employee_id
		  WHERE p.device_id = $2 AND p.work_date = $3
		    AND e.branch_id IS DISTINCT FROM $1
		)
		SELECT r.employee_code, r.full_name, r.photo_path, r.is_visitor,
		       (SELECT fp.photo_path FROM employee_face_photos fp
		        WHERE fp.employee_id = r.id
		        ORDER BY fp.captured_at DESC, fp.created_at DESC LIMIT 1),
		       ar.status, ar.check_in, ar.check_out, ar.check_in_source, ar.check_out_source,
		       COALESCE(ar.is_late, false), COALESCE(ar.late_minutes, 0), COALESCE(ar.is_early_leave, false),
		       (SELECT p.time_source FROM attendance_punches p
		        WHERE p.employee_id = r.id AND p.punched_at = ar.check_in LIMIT 1),
		       (SELECT p.time_source FROM attendance_punches p
		        WHERE p.employee_id = r.id AND p.punched_at = ar.check_out LIMIT 1)
		FROM roster r
		LEFT JOIN attendance_records ar ON ar.employee_id = r.id AND ar.date = $3
		ORDER BY r.is_visitor, r.full_name`, dev.BranchID, dev.DeviceID, pgToday)
	if err != nil {
		log.Printf("attendance device today: query failed: %v", err)
		respondError(w, http.StatusInternalServerError, "gagal mengambil kehadiran hari ini")
		return
	}
	defer rows.Close()

	out := []deviceTodayEntry{}
	for rows.Next() {
		var (
			e                     deviceTodayEntry
			photo, facePhoto      pgtype.Text
			status, inSrc, outSrc pgtype.Text
			checkIn, checkOut     pgtype.Timestamptz
			inTS, outTS           pgtype.Text
			lateMin               int32
		)
		if err := rows.Scan(&e.EmployeeCode, &e.FullName, &photo, &e.IsVisitor, &facePhoto,
			&status, &checkIn, &checkOut, &inSrc, &outSrc,
			&e.IsLate, &lateMin, &e.IsEarlyLeave, &inTS, &outTS); err != nil {
			log.Printf("attendance device today: scan failed: %v", err)
			respondError(w, http.StatusInternalServerError, "gagal mengambil kehadiran hari ini")
			return
		}
		e.LateMinutes = int(lateMin)
		if facePhoto.Valid {
			e.FacePhotoURL = uploadsURL(facePhoto.String)
		} else if photo.Valid {
			e.FacePhotoURL = uploadsURL(photo.String)
		}
		e.Status = textPtr(status)
		e.CheckInSrc, e.CheckOutSrc = textPtr(inSrc), textPtr(outSrc)
		e.CheckIn, e.CheckOut = tsString(tsPtr(checkIn)), tsString(tsPtr(checkOut))
		e.CheckInVerified = slotVerified(checkIn, inSrc, inTS)
		e.CheckOutVerified = slotVerified(checkOut, outSrc, outTS)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kehadiran hari ini")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"date":        today.Format("2006-01-02"),
		"server_time": service.InWIB(now).Format(time.RFC3339),
		"employees":   out,
	})
}

// slotVerified: see service.SlotVerified (shared with the history view).
func slotVerified(at pgtype.Timestamptz, src, timeSource pgtype.Text) bool {
	return service.SlotVerified(at, src, timeSource)
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// History — GET /api/hr/attendance/device/employees/{code}/history
//
// The kiosk's per-employee view: the last service.HistoryWindowDays days as the
// server recorded them (lateness, early leave, missing check-out, leave, Alpha
// and whether the allowance covered it). The same builder serves the HR
// employee page, so the two can never disagree.
//
// Scoped like the "today" list: an employee of the device's branch, or a
// visitor who punched on this device inside the window.
func (h *AttendanceDeviceHandler) History(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dev, ok := middleware.DeviceFromCtx(ctx)
	if !ok || dev == nil {
		respondError(w, http.StatusUnauthorized, "perangkat tidak dikenal")
		return
	}
	if !dev.BranchID.Valid {
		respondError(w, http.StatusBadRequest, "perangkat belum terhubung ke cabang")
		return
	}
	emp, err := h.queries.GetEmployeeByCode(ctx, strings.TrimSpace(chi.URLParam(r, "code")))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	today := service.TodayWIB()
	if !emp.BranchID.Valid || emp.BranchID.Bytes != dev.BranchID.Bytes {
		var visited bool
		if err := h.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM attendance_punches
			               WHERE employee_id = $1 AND device_id = $2 AND work_date >= $3)`,
			emp.ID, dev.DeviceID,
			pgtype.Date{Time: today.AddDate(0, 0, -(service.HistoryWindowDays - 1)), Valid: true},
		).Scan(&visited); err != nil || !visited {
			respondError(w, http.StatusForbidden, "karyawan bukan bagian dari cabang perangkat ini")
			return
		}
	}

	hist, err := service.GetAttendanceHistory(ctx, h.pool, h.queries, emp.ID, today)
	if err != nil {
		log.Printf("attendance device history (emp=%s): %v", emp.EmployeeCode, err)
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat kehadiran")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"employee_code": emp.EmployeeCode,
		"full_name":     emp.FullName,
		"history":       hist,
	})
}
