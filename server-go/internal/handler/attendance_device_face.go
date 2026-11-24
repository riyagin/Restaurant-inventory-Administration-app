package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// FaceEnrollRequest is the JSON body a device posts to store a face enrollment.
type FaceEnrollRequest struct {
	EmployeeCode     string `json:"employee_code"`
	Embedding        string `json:"embedding"`         // base64-encoded packed float32 vector
	EmbeddingVersion string `json:"embedding_version"` // model + preprocessing space id
	// Photo is the enrollment reference shot (base64 JPEG). Optional: older app
	// builds don't send it, and the embedding is still stored without it.
	Photo string `json:"photo,omitempty"`
}

// EnrollFace — POST /api/hr/attendance/device/face (own-branch employees only)
func (h *AttendanceDeviceHandler) EnrollFace(w http.ResponseWriter, r *http.Request) {
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

	var req FaceEnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca permintaan (JSON tidak valid)")
		return
	}
	req.EmployeeCode = strings.TrimSpace(req.EmployeeCode)
	req.EmbeddingVersion = strings.TrimSpace(req.EmbeddingVersion)
	if req.EmployeeCode == "" {
		respondError(w, http.StatusBadRequest, "employee_code wajib diisi")
		return
	}
	if req.EmbeddingVersion == "" {
		respondError(w, http.StatusBadRequest, "embedding_version wajib diisi")
		return
	}
	embedding, err := base64.StdEncoding.DecodeString(req.Embedding)
	if err != nil || len(embedding) == 0 {
		respondError(w, http.StatusBadRequest, "embedding tidak valid (harus base64 dan tidak kosong)")
		return
	}
	var photo []byte
	if req.Photo != "" {
		photo, err = base64.StdEncoding.DecodeString(req.Photo)
		if err != nil || len(photo) == 0 {
			respondError(w, http.StatusBadRequest, "foto tidak valid (harus base64 JPEG)")
			return
		}
		if len(photo) > maxFacePhotoBytes {
			respondError(w, http.StatusRequestEntityTooLarge, "foto terlalu besar")
			return
		}
	}

	emp, err := h.queries.GetEmployeeByCode(ctx, req.EmployeeCode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan dengan kode tersebut tidak ditemukan")
			return
		}
		log.Printf("attendance device enroll: GetEmployeeByCode(%q) failed: %v", req.EmployeeCode, err)
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	// A device may only enroll faces for its own branch.
	if !emp.BranchID.Valid || emp.BranchID.Bytes != dev.BranchID.Bytes {
		respondError(w, http.StatusForbidden, "karyawan bukan bagian dari cabang perangkat ini")
		return
	}

	err = h.queries.UpsertEmployeeFaceEmbedding(ctx, &db.UpsertEmployeeFaceEmbeddingParams{
		ID:                   emp.ID,
		FaceEmbedding:        embedding,
		FaceEmbeddingVersion: pgtype.Text{String: req.EmbeddingVersion, Valid: true},
		FaceEnrolledAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		log.Printf("attendance device enroll: UpsertEmployeeFaceEmbedding(emp=%s) failed: %v", req.EmployeeCode, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data wajah")
		return
	}

	// A failed photo save is logged, not fatal: the embedding is what matters.
	if len(photo) > 0 {
		if _, perr := service.SaveFacePhoto(ctx, h.pool, h.resolveUploadsDir(), emp.ID,
			bytes.NewReader(photo), service.FacePhotoSourceEnroll, time.Now()); perr != nil {
			log.Printf("attendance device enroll: save face photo (emp=%s) failed: %v", req.EmployeeCode, perr)
		}
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		Username:   "perangkat: " + dev.Name,
		Action:     "UPDATE",
		EntityType: "employee_face",
		EntityID:   emp.ID.Bytes,
		Description: fmt.Sprintf("Menyimpan data wajah karyawan %s (%s) dari perangkat %s",
			emp.FullName, req.EmployeeCode, dev.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"message":       "wajah tersimpan",
		"employee_code": req.EmployeeCode,
	})
}

// FaceSyncEntry is one locally-held enrollment as reported by a device.
type FaceSyncEntry struct {
	EmployeeCode     string `json:"employee_code"`
	EmbeddingVersion string `json:"embedding_version"`
	EnrolledAt       int64  `json:"enrolled_at"` // epoch ms; 0 when unknown
}

// Enrollments in another embedding space than ModelVersion go to to_reenroll.
type FaceSyncRequest struct {
	ModelVersion string          `json:"model_version"`
	Entries      []FaceSyncEntry `json:"entries"`
}

// faceSyncDownload carries an enrollment the device is missing or has stale.
type faceSyncDownload struct {
	EmployeeCode         string  `json:"employee_code"`
	FullName             string  `json:"full_name"`
	FaceEmbedding        string  `json:"face_embedding"`
	FaceEmbeddingVersion string  `json:"face_embedding_version"`
	FaceEnrolledAt       *int64  `json:"face_enrolled_at,omitempty"`
	PhotoPath            *string `json:"photo_path,omitempty"`
}

// faceSyncMismatch names an employee the device holds differently from the server.
type faceSyncMismatch struct {
	EmployeeCode  string `json:"employee_code"`
	FullName      string `json:"full_name,omitempty"`
	DeviceVersion string `json:"device_version,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	Reason        string `json:"reason"`
}

// FaceSync — POST /api/hr/attendance/device/face/sync
// Buckets: to_download (with embeddings), to_upload, to_delete (left the roster),
// to_reenroll (different embedding space), in_sync.
func (h *AttendanceDeviceHandler) FaceSync(w http.ResponseWriter, r *http.Request) {
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

	var req FaceSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca permintaan (JSON tidak valid)")
		return
	}
	req.ModelVersion = strings.TrimSpace(req.ModelVersion)

	// Index the device's manifest by employee code.
	local := make(map[string]FaceSyncEntry, len(req.Entries))
	for _, e := range req.Entries {
		code := strings.TrimSpace(e.EmployeeCode)
		if code == "" {
			continue
		}
		e.EmployeeCode = code
		e.EmbeddingVersion = strings.TrimSpace(e.EmbeddingVersion)
		local[code] = e
	}

	roster, err := h.queries.ListDeviceRosterByBranch(ctx, dev.BranchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar karyawan")
		return
	}

	toDownload := []faceSyncDownload{}
	toUpload := []faceSyncMismatch{}
	toDelete := []faceSyncMismatch{}
	toReenroll := []faceSyncMismatch{}
	inSync := []string{}
	serverEnrolled := 0

	for _, row := range roster {
		entry, onDevice := local[row.EmployeeCode]
		hasServer := len(row.FaceEmbedding) > 0
		if hasServer {
			serverEnrolled++
		}

		serverVersion := ""
		if row.FaceEmbeddingVersion.Valid {
			serverVersion = row.FaceEmbeddingVersion.String
		}
		var serverEnrolledAt int64
		if row.FaceEnrolledAt.Valid {
			serverEnrolledAt = row.FaceEnrolledAt.Time.UnixMilli()
		}

		switch {
		case !hasServer && !onDevice:
			// Neither side has it — surfaces on the web dashboard as "belum terdaftar".

		case !hasServer && onDevice:
			toUpload = append(toUpload, faceSyncMismatch{
				EmployeeCode:  row.EmployeeCode,
				FullName:      row.FullName,
				DeviceVersion: entry.EmbeddingVersion,
				Reason:        "server belum memiliki data wajah ini",
			})

		case hasServer && !onDevice:
			// The device can only use an embedding from the space its model runs in.
			if req.ModelVersion != "" && serverVersion != req.ModelVersion {
				toReenroll = append(toReenroll, faceSyncMismatch{
					EmployeeCode:  row.EmployeeCode,
					FullName:      row.FullName,
					ServerVersion: serverVersion,
					Reason:        "versi model server berbeda dengan perangkat",
				})
				break
			}
			toDownload = append(toDownload, buildFaceDownload(row, serverVersion, serverEnrolledAt))

		default: // both sides have an enrollment
			switch {
			case entry.EmbeddingVersion != "" && serverVersion != "" && entry.EmbeddingVersion != serverVersion:
				toReenroll = append(toReenroll, faceSyncMismatch{
					EmployeeCode:  row.EmployeeCode,
					FullName:      row.FullName,
					DeviceVersion: entry.EmbeddingVersion,
					ServerVersion: serverVersion,
					Reason:        "versi embedding berbeda antara perangkat dan server",
				})
			case serverEnrolledAt > entry.EnrolledAt:
				// Server copy is newer — someone re-enrolled on another kiosk.
				toDownload = append(toDownload, buildFaceDownload(row, serverVersion, serverEnrolledAt))
			default:
				inSync = append(inSync, row.EmployeeCode)
			}
		}

		delete(local, row.EmployeeCode)
	}

	// Anything left in the device manifest is not in this branch's active roster.
	for code, entry := range local {
		toDelete = append(toDelete, faceSyncMismatch{
			EmployeeCode:  code,
			DeviceVersion: entry.EmbeddingVersion,
			Reason:        "karyawan tidak ada di daftar aktif cabang ini",
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"branch_device":   dev.Name,
		"model_version":   req.ModelVersion,
		"server_total":    len(roster),
		"server_enrolled": serverEnrolled,
		"device_total":    len(req.Entries),
		"to_download":     toDownload,
		"to_upload":       toUpload,
		"to_delete":       toDelete,
		"to_reenroll":     toReenroll,
		"in_sync":         inSync,
		"synced_at":       time.Now().UnixMilli(),
	})
}

func buildFaceDownload(row *db.ListDeviceRosterByBranchRow, version string, enrolledAt int64) faceSyncDownload {
	d := faceSyncDownload{
		EmployeeCode:         row.EmployeeCode,
		FullName:             row.FullName,
		FaceEmbedding:        base64.StdEncoding.EncodeToString(row.FaceEmbedding),
		FaceEmbeddingVersion: version,
	}
	if enrolledAt > 0 {
		d.FaceEnrolledAt = &enrolledAt
	}
	if row.PhotoPath.Valid {
		p := row.PhotoPath.String
		d.PhotoPath = &p
	}
	return d
}
