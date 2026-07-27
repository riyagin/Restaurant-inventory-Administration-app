package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Face enrollment overview — the data behind the web "Wajah & Perangkat"
// dashboard. Read-only aggregates over employees.face_embedding plus the
// attendance-device roster; the per-employee list of who is missing an
// enrollment is served by GET /api/hr/employees?face=not (see hr_employees.go).

type faceBranchCoverage struct {
	BranchID   pgtype.UUID `json:"branch_id"`
	BranchName string      `json:"branch_name"`
	Total      int64       `json:"total"`
	Enrolled   int64       `json:"enrolled"`
	Missing    int64       `json:"missing"`
	// DeviceCount is how many attendance devices are bound to this branch. A
	// branch with employees but zero devices can never enrol anyone, which is
	// worth surfacing separately from "nobody has enrolled yet".
	DeviceCount int64 `json:"device_count"`
}

type faceVersionCount struct {
	Version string `json:"version"`
	Count   int64  `json:"count"`
}

type faceRecentEnrollment struct {
	ID         pgtype.UUID        `json:"id"`
	Code       string             `json:"employee_code"`
	FullName   string             `json:"full_name"`
	BranchName string             `json:"branch_name"`
	Version    pgtype.Text        `json:"face_embedding_version"`
	EnrolledAt pgtype.Timestamptz `json:"face_enrolled_at"`
}

type faceDeviceRow struct {
	ID         pgtype.UUID        `json:"id"`
	Name       string             `json:"name"`
	BranchID   pgtype.UUID        `json:"branch_id"`
	BranchName pgtype.Text        `json:"branch_name"`
	IsActive   bool               `json:"is_active"`
	CreatedAt  pgtype.Timestamptz `json:"created_at"`
	LastSeenAt pgtype.Timestamptz `json:"last_seen_at"`
	// CheckInsToday counts attendance records this device produced today, so an
	// operator can tell "online but idle" apart from "actively taking check-ins".
	CheckInsToday int64 `json:"check_ins_today"`
}

type faceSourceCount struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// FaceOverview — GET /api/hr/attendance/face/overview
//
// Aggregates face-enrollment coverage, the registered device fleet, and how
// check-ins are actually arriving (face vs fingerprint vs manual) over a
// trailing window. Query param: days (default 30, max 365) for the source mix.
func (h *AttendanceHandler) FaceOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}

	// ── totals (active employees only — resigned staff must not drag coverage down)
	var total, enrolled int64
	err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE face_embedding IS NOT NULL)
		FROM employees
		WHERE status = 'active'`).Scan(&total, &enrolled)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung cakupan data wajah")
		return
	}

	// ── per-branch coverage, including branches with no employees yet
	branchRows, err := h.pool.Query(ctx, `
		SELECT b.id, b.name,
		       COUNT(e.id) FILTER (WHERE e.status = 'active'),
		       COUNT(e.id) FILTER (WHERE e.status = 'active' AND e.face_embedding IS NOT NULL),
		       (SELECT COUNT(*) FROM attendance_devices d WHERE d.branch_id = b.id)
		FROM branches b
		LEFT JOIN employees e ON e.branch_id = b.id
		GROUP BY b.id, b.name
		ORDER BY b.name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil cakupan per cabang")
		return
	}
	defer branchRows.Close()

	branches := []faceBranchCoverage{}
	for branchRows.Next() {
		var c faceBranchCoverage
		if err := branchRows.Scan(&c.BranchID, &c.BranchName, &c.Total, &c.Enrolled, &c.DeviceCount); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca cakupan per cabang")
			return
		}
		c.Missing = c.Total - c.Enrolled
		branches = append(branches, c)
	}
	if err := branchRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca cakupan per cabang")
		return
	}

	// ── embedding model versions in use. More than one row here means the fleet
	// is split across model spaces and the minority group cannot be matched by
	// devices running the majority model — it needs re-enrollment.
	versionRows, err := h.pool.Query(ctx, `
		SELECT COALESCE(face_embedding_version, '(tidak diketahui)'), COUNT(*)
		FROM employees
		WHERE status = 'active' AND face_embedding IS NOT NULL
		GROUP BY 1
		ORDER BY 2 DESC`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil versi model")
		return
	}
	defer versionRows.Close()

	versions := []faceVersionCount{}
	for versionRows.Next() {
		var v faceVersionCount
		if err := versionRows.Scan(&v.Version, &v.Count); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca versi model")
			return
		}
		versions = append(versions, v)
	}
	if err := versionRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca versi model")
		return
	}

	// ── most recent enrollments
	recentRows, err := h.pool.Query(ctx, `
		SELECT e.id, e.employee_code, e.full_name, b.name,
		       e.face_embedding_version, e.face_enrolled_at
		FROM employees e
		JOIN branches b ON b.id = e.branch_id
		WHERE e.face_embedding IS NOT NULL AND e.face_enrolled_at IS NOT NULL
		ORDER BY e.face_enrolled_at DESC
		LIMIT 10`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pendaftaran terbaru")
		return
	}
	defer recentRows.Close()

	recent := []faceRecentEnrollment{}
	for recentRows.Next() {
		var e faceRecentEnrollment
		if err := recentRows.Scan(&e.ID, &e.Code, &e.FullName, &e.BranchName, &e.Version, &e.EnrolledAt); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca pendaftaran terbaru")
			return
		}
		recent = append(recent, e)
	}
	if err := recentRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca pendaftaran terbaru")
		return
	}

	// ── device fleet + today's check-in volume per device
	deviceRows, err := h.pool.Query(ctx, `
		SELECT d.id, d.name, d.branch_id, b.name, d.is_active, d.created_at, d.last_seen_at,
		       (SELECT COUNT(*) FROM attendance_records ar
		         WHERE ar.device_id = d.id AND ar.date = CURRENT_DATE)
		FROM attendance_devices d
		LEFT JOIN branches b ON b.id = d.branch_id
		ORDER BY d.is_active DESC, d.last_seen_at DESC NULLS LAST, d.name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar perangkat")
		return
	}
	defer deviceRows.Close()

	devices := []faceDeviceRow{}
	for deviceRows.Next() {
		var d faceDeviceRow
		if err := deviceRows.Scan(&d.ID, &d.Name, &d.BranchID, &d.BranchName, &d.IsActive,
			&d.CreatedAt, &d.LastSeenAt, &d.CheckInsToday); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca daftar perangkat")
			return
		}
		devices = append(devices, d)
	}
	if err := deviceRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca daftar perangkat")
		return
	}

	// ── how check-ins arrived over the trailing window
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	sourceRows, err := h.pool.Query(ctx, `
		SELECT check_in_source, COUNT(*)
		FROM attendance_records
		WHERE check_in_source IS NOT NULL AND date >= $1::date
		GROUP BY check_in_source
		ORDER BY 2 DESC`, since)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan sumber absensi")
		return
	}
	defer sourceRows.Close()

	sources := []faceSourceCount{}
	for sourceRows.Next() {
		var s faceSourceCount
		if err := sourceRows.Scan(&s.Source, &s.Count); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca ringkasan sumber absensi")
			return
		}
		sources = append(sources, s)
	}
	if err := sourceRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca ringkasan sumber absensi")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"total_active":       total,
		"enrolled":           enrolled,
		"missing":            total - enrolled,
		"branches":           branches,
		"versions":           versions,
		"recent_enrollments": recent,
		"devices":            devices,
		"source_days":        days,
		"sources":            sources,
	})
}
