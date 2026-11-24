package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// AttendanceDeviceHandler serves the device-key-authenticated endpoints the
// Android face app talks to. These routes live OUTSIDE the JWT group.
type AttendanceDeviceHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewAttendanceDeviceHandler(pool *pgxpool.Pool, queries *db.Queries) *AttendanceDeviceHandler {
	return &AttendanceDeviceHandler{pool: pool, queries: queries}
}

func (h *AttendanceDeviceHandler) SetUploadsDir(dir string) { h.uploadsDir = dir }

func (h *AttendanceDeviceHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return config.DefaultUploadsDir
}

// deviceEmployeeDTO matches the Android EmployeeDto: embedding base64, enrolled_at epoch ms.
type deviceEmployeeDTO struct {
	EmployeeCode         string  `json:"employee_code"`
	FullName             string  `json:"full_name"`
	PhotoPath            *string `json:"photo_path"`
	FaceEmbedding        *string `json:"face_embedding,omitempty"`
	FaceEmbeddingVersion *string `json:"face_embedding_version,omitempty"`
	FaceEnrolledAt       *int64  `json:"face_enrolled_at,omitempty"`
	// Not "photo_url": the app would derive an enrollment from it.
	FacePhotoURL *string `json:"face_photo_url,omitempty"`
}

// uploadsURL turns a stored upload file name into its served path.
func uploadsURL(name string) *string {
	if name == "" {
		return nil
	}
	u := "/uploads/" + name
	return &u
}

// latestFacePhotosByCode maps employee_code → newest face photo file name for a
// branch's active roster.
func (h *AttendanceDeviceHandler) latestFacePhotosByCode(ctx context.Context, branchID pgtype.UUID) (map[string]string, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT e.employee_code,
		       (SELECT fp.photo_path FROM employee_face_photos fp
		        WHERE fp.employee_id = e.id
		        ORDER BY fp.captured_at DESC, fp.created_at DESC LIMIT 1)
		FROM employees e
		WHERE e.branch_id = $1 AND e.status = 'active'`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code string
		var path pgtype.Text
		if err := rows.Scan(&code, &path); err != nil {
			return nil, err
		}
		if path.Valid {
			out[code] = path.String
		}
	}
	return out, rows.Err()
}

// Employees — GET /api/hr/attendance/device/employees (roster + stored embeddings)
func (h *AttendanceDeviceHandler) Employees(w http.ResponseWriter, r *http.Request) {
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

	roster, err := h.queries.ListDeviceRosterByBranch(ctx, dev.BranchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar karyawan")
		return
	}

	facePhotos, err := h.latestFacePhotosByCode(ctx, dev.BranchID)
	if err != nil {
		// Photos are cosmetic; the roster (and its embeddings) must still sync.
		log.Printf("attendance device employees: face photos failed: %v", err)
		facePhotos = map[string]string{}
	}

	out := make([]deviceEmployeeDTO, 0, len(roster))
	for _, row := range roster {
		dto := deviceEmployeeDTO{
			EmployeeCode: row.EmployeeCode,
			FullName:     row.FullName,
		}
		if row.PhotoPath.Valid {
			p := row.PhotoPath.String
			dto.PhotoPath = &p
		}
		if fp, ok := facePhotos[row.EmployeeCode]; ok {
			dto.FacePhotoURL = uploadsURL(fp)
		} else if row.PhotoPath.Valid {
			dto.FacePhotoURL = uploadsURL(row.PhotoPath.String)
		}
		if len(row.FaceEmbedding) > 0 {
			enc := base64.StdEncoding.EncodeToString(row.FaceEmbedding)
			dto.FaceEmbedding = &enc
		}
		if row.FaceEmbeddingVersion.Valid {
			v := row.FaceEmbeddingVersion.String
			dto.FaceEmbeddingVersion = &v
		}
		if row.FaceEnrolledAt.Valid {
			ms := row.FaceEnrolledAt.Time.UnixMilli()
			dto.FaceEnrolledAt = &ms
		}
		out = append(out, dto)
	}
	respondJSON(w, http.StatusOK, out)
}

// EnrollDeviceRequest is the JSON body the Android app posts to self-register a
// device using the logged-in user's JWT.
type EnrollDeviceRequest struct {
	DeviceName string `json:"device_name"`
	BranchID   string `json:"branch_id"` // optional; admin may assign later
}

// Enroll — POST /api/hr/attendance/device/enroll
// JWT route (the device has no key yet); returns the raw key once, stores only its hash.
func (h *AttendanceDeviceHandler) Enroll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req EnrollDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca permintaan (JSON tidak valid)")
		return
	}
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	if req.DeviceName == "" {
		respondError(w, http.StatusBadRequest, "nama perangkat wajib diisi")
		return
	}
	branchID, err := uuidOrNull(strings.TrimSpace(req.BranchID))
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

	dev, err := h.queries.CreateAttendanceDevice(ctx, &db.CreateAttendanceDeviceParams{
		Name:       req.DeviceName,
		BranchID:   branchID,
		ApiKeyHash: hash,
	})
	if err != nil {
		log.Printf("attendance device enroll: CreateAttendanceDevice(%q) failed: %v", req.DeviceName, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perangkat")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "attendance_device",
		EntityID:    dev.ID.Bytes,
		Description: fmt.Sprintf("Mendaftarkan perangkat absensi %s (self-enroll)", dev.Name),
	})

	// device_key is returned ONCE and never persisted in plaintext.
	respondJSON(w, http.StatusCreated, map[string]any{
		"device_key":  rawKey,
		"device_name": dev.Name,
	})
}

// maxFacePhotoBytes caps the decoded enrollment photo. The app sends an aligned
// face crop (a few KB), so this is generous.
const maxFacePhotoBytes = 5 << 20

func tsString(t *time.Time) any {
	if t == nil {
		return nil
	}
	// WIB so a device displaying the hour shows local time, not UTC.
	return service.InWIB(*t).Format(time.RFC3339)
}
