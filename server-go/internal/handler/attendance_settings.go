package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ── Work schedules ───────────────────────────────────────────────────────────

// ListWorkSchedules — GET /api/hr/attendance/work-schedules
func (h *AttendanceHandler) ListWorkSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := h.queries.ListWorkSchedules(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jadwal kerja")
		return
	}
	if list == nil {
		list = []*db.WorkSchedule{}
	}
	respondJSON(w, http.StatusOK, list)
}

// UpsertWorkSchedule — POST /api/hr/attendance/work-schedules
// Body: { branch_id, work_start "08:00", work_end "17:00", grace_minutes,
//         early_leave_minutes, work_days [1,2,3,4,5,6] }
func (h *AttendanceHandler) UpsertWorkSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID          string `json:"branch_id"`
		WorkStart         string `json:"work_start"`
		WorkEnd           string `json:"work_end"`
		GraceMinutes      int32  `json:"grace_minutes"`
		EarlyLeaveMinutes int32  `json:"early_leave_minutes"`
		WorkDays          []int32 `json:"work_days"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	branchID, err := uuidOrNull(body.BranchID)
	if err != nil || !branchID.Valid {
		respondError(w, http.StatusBadRequest, "cabang wajib dipilih")
		return
	}
	start, err := parseClockTime(body.WorkStart)
	if err != nil {
		respondError(w, http.StatusBadRequest, "jam mulai tidak valid (HH:MM)")
		return
	}
	end, err := parseClockTime(body.WorkEnd)
	if err != nil {
		respondError(w, http.StatusBadRequest, "jam selesai tidak valid (HH:MM)")
		return
	}
	if len(body.WorkDays) == 0 {
		body.WorkDays = []int32{1, 2, 3, 4, 5, 6}
	}
	for _, d := range body.WorkDays {
		if d < 1 || d > 7 {
			respondError(w, http.StatusBadRequest, "hari kerja harus berupa angka ISO 1-7")
			return
		}
	}

	ctx := r.Context()
	sched, err := h.queries.UpsertWorkSchedule(ctx, &db.UpsertWorkScheduleParams{
		BranchID:          branchID,
		WorkStart:         start,
		WorkEnd:           end,
		GraceMinutes:      body.GraceMinutes,
		EarlyLeaveMinutes: body.EarlyLeaveMinutes,
		WorkDays:          body.WorkDays,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan jadwal kerja")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "work_schedule",
		EntityID:    sched.ID.Bytes,
		Description: "Memperbarui jadwal kerja cabang",
	})

	respondJSON(w, http.StatusOK, sched)
}

// parseClockTime parses "HH:MM" into a pgtype.Time (microseconds since midnight).
func parseClockTime(s string) (pgtype.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse("15:04", s)
	if err != nil {
		// Also accept "HH:MM:SS".
		t, err = time.Parse("15:04:05", s)
		if err != nil {
			return pgtype.Time{}, err
		}
	}
	micros := int64(t.Hour())*3600_000_000 + int64(t.Minute())*60_000_000 + int64(t.Second())*1_000_000
	return pgtype.Time{Microseconds: micros, Valid: true}, nil
}

// ── Public holidays ──────────────────────────────────────────────────────────

// ListPublicHolidays — GET /api/hr/attendance/holidays
func (h *AttendanceHandler) ListPublicHolidays(w http.ResponseWriter, r *http.Request) {
	list, err := h.queries.ListPublicHolidays(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil hari libur")
		return
	}
	if list == nil {
		list = []*db.PublicHoliday{}
	}
	respondJSON(w, http.StatusOK, list)
}

// CreatePublicHoliday — POST /api/hr/attendance/holidays
func (h *AttendanceHandler) CreatePublicHoliday(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date string `json:"date"`
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	d, err := dateOrNull(body.Date)
	if err != nil || !d.Valid {
		respondError(w, http.StatusBadRequest, "tanggal wajib diisi (YYYY-MM-DD)")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama hari libur wajib diisi")
		return
	}

	ctx := r.Context()
	hol, err := h.queries.CreatePublicHoliday(ctx, &db.CreatePublicHolidayParams{Date: d, Name: body.Name})
	if err != nil {
		respondError(w, http.StatusConflict, "tanggal hari libur sudah ada atau tidak valid")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "public_holiday",
		EntityID:    hol.ID.Bytes,
		Description: fmt.Sprintf("Menambah hari libur %s (%s)", hol.Name, body.Date),
	})

	respondJSON(w, http.StatusCreated, hol)
}

// DeletePublicHoliday — DELETE /api/hr/attendance/holidays/:id
func (h *AttendanceHandler) DeletePublicHoliday(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	if err := h.queries.DeletePublicHoliday(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus hari libur")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:     middleware.UserIDFromCtx(ctx),
		Username:   middleware.UsernameFromCtx(ctx),
		Action:     "DELETE",
		EntityType: "public_holiday",
		EntityID:   id,
		Description: "Menghapus hari libur",
	})
	respondJSON(w, http.StatusOK, map[string]string{"message": "hari libur dihapus"})
}

// ── Attendance devices ───────────────────────────────────────────────────────

// ListDevices — GET /api/hr/attendance/devices
// Never returns the raw key (only stored as a hash).
func (h *AttendanceHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	list, err := h.queries.ListAttendanceDevices(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil perangkat")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]any{
			"id":          d.ID,
			"name":        d.Name,
			"branch_id":   d.BranchID,
			"branch_name": d.BranchName,
			"is_active":   d.IsActive,
			"created_at":  d.CreatedAt,
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// CreateDevice — POST /api/hr/attendance/devices
// Generates a random API key, stores ONLY its SHA-256 hash, returns the raw key
// exactly ONCE in the response.
func (h *AttendanceHandler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		BranchID string `json:"branch_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama perangkat wajib diisi")
		return
	}
	branchID, err := uuidOrNull(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cabang tidak valid")
		return
	}

	rawKey, err := generateDeviceKey()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat kunci perangkat")
		return
	}
	hash := middleware.HashDeviceKey(rawKey)

	ctx := r.Context()
	dev, err := h.queries.CreateAttendanceDevice(ctx, &db.CreateAttendanceDeviceParams{
		Name:       body.Name,
		BranchID:   branchID,
		ApiKeyHash: hash,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perangkat")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "attendance_device",
		EntityID:    dev.ID.Bytes,
		Description: fmt.Sprintf("Mendaftarkan perangkat absensi %s", dev.Name),
	})

	// api_key is returned ONCE and never persisted in plaintext.
	respondJSON(w, http.StatusCreated, map[string]any{
		"id":        dev.ID,
		"name":      dev.Name,
		"branch_id": dev.BranchID,
		"is_active": dev.IsActive,
		"api_key":   rawKey,
		"warning":   "Simpan kunci ini sekarang. Kunci tidak akan ditampilkan lagi.",
	})
}

// SetDeviceActive — PUT /api/hr/attendance/devices/:id
func (h *AttendanceHandler) SetDeviceActive(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		IsActive bool `json:"is_active"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	ctx := r.Context()
	dev, err := h.queries.SetAttendanceDeviceActive(ctx, &db.SetAttendanceDeviceActiveParams{
		IsActive: body.IsActive,
		ID:       pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "perangkat tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui perangkat")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "attendance_device",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah status perangkat %s", dev.Name),
	})
	respondJSON(w, http.StatusOK, map[string]any{
		"id": dev.ID, "name": dev.Name, "branch_id": dev.BranchID, "is_active": dev.IsActive,
	})
}

// DeleteDevice — DELETE /api/hr/attendance/devices/:id
func (h *AttendanceHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	if err := h.queries.DeleteAttendanceDevice(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusConflict, "perangkat masih dipakai data kehadiran; nonaktifkan saja")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "attendance_device",
		EntityID:    id,
		Description: "Menghapus perangkat absensi",
	})
	respondJSON(w, http.StatusOK, map[string]string{"message": "perangkat dihapus"})
}

// generateDeviceKey returns a random 32-byte hex key, prefixed for readability.
func generateDeviceKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "dev_" + hex.EncodeToString(buf), nil
}
