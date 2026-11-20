package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// DeleteFaceData — DELETE /api/hr/employees/:id/face (refused for active employees)
func (h *HREmployeesHandler) DeleteFaceData(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.Status == "active" {
		respondError(w, http.StatusConflict, "data wajah karyawan aktif tidak dapat dihapus; tandai resign terlebih dahulu")
		return
	}

	if _, err := h.pool.Exec(ctx, `
		UPDATE employees
		SET face_embedding = NULL,
		    face_embedding_version = NULL,
		    face_enrolled_at = NULL,
		    updated_at = now()
		WHERE id = $1`, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus data wajah")
		return
	}

	removed, err := service.DeleteAllFacePhotos(ctx, h.pool, h.resolveUploadsDir(), pgID)
	if err != nil {
		log.Printf("hr employee delete face: delete photos (emp=%s) failed: %v", emp.EmployeeCode, err)
		respondError(w, http.StatusInternalServerError, "data wajah terhapus, tetapi gagal menghapus foto")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "employee_face",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus data wajah & %d foto absensi karyawan %s (%s)", removed, emp.FullName, emp.EmployeeCode),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":        "data wajah berhasil dihapus",
		"photos_removed": removed,
	})
}

// TransitionToPermanent — POST /api/hr/employees/:id/transition-permanent
func (h *HREmployeesHandler) TransitionToPermanent(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body transitionPermanentBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	effDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.EffectiveDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal efektif tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.EmploymentType != service.EmploymentTypeContract && emp.EmploymentType != service.EmploymentTypeProbation {
		respondError(w, http.StatusConflict, "karyawan sudah berstatus tetap")
		return
	}
	if effDate.Before(emp.JoinDate.Time) {
		respondError(w, http.StatusBadRequest, "tanggal status tetap tidak boleh sebelum tanggal bergabung")
		return
	}

	// Probation already counts towards THR tenure; only a contract restarts it.
	permanentSince := pgtype.Date{Time: effDate, Valid: true}
	if emp.EmploymentType == service.EmploymentTypeProbation {
		permanentSince = pgtype.Date{}
	}
	if err := h.queries.TransitionEmployeeToPermanent(ctx, &db.TransitionEmployeeToPermanentParams{
		ID:             pgID,
		PermanentSince: permanentSince,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengubah status karyawan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Mengubah status %s menjadi karyawan tetap (berlaku %s)", emp.FullName, effDate.Format("2006-01-02")),
	})

	updated, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// Resign — POST /api/hr/employees/:id/resign
func (h *HREmployeesHandler) Resign(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body resignBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	resignDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.ResignDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal resign tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if emp.Status == "resigned" {
		respondError(w, http.StatusConflict, "karyawan sudah berstatus resign")
		return
	}
	if resignDate.Before(emp.JoinDate.Time) {
		respondError(w, http.StatusBadRequest, "tanggal resign tidak boleh sebelum tanggal bergabung")
		return
	}

	if err := h.queries.ResignEmployee(ctx, &db.ResignEmployeeParams{
		ID:         pgID,
		ResignDate: pgtype.Date{Time: resignDate, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengubah status karyawan")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "employee",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai %s sebagai resign (berlaku %s)", emp.FullName, resignDate.Format("2006-01-02")),
	})

	updated, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// Delete — DELETE /api/hr/employees/:id
func (h *HREmployeesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	// Photo rows cascade; collect the file names first to remove the files.
	facePhotos, err := service.ListFacePhotoPaths(ctx, h.pool, pgID)
	if err != nil {
		log.Printf("hr employee delete: list face photos failed: %v", err)
	}

	if err := h.queries.DeleteEmployee(ctx, pgID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			respondError(w, http.StatusConflict, "karyawan masih dipakai data HR lain; ubah status menjadi nonaktif")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menghapus karyawan")
		return
	}

	if emp.PhotoPath.Valid && emp.PhotoPath.String != "" {
		_ = os.Remove(filepath.Join(h.resolveUploadsDir(), emp.PhotoPath.String))
	}
	service.RemoveFacePhotoFiles(h.resolveUploadsDir(), facePhotos)

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus karyawan %s (%s)", emp.FullName, emp.EmployeeCode),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "karyawan berhasil dihapus"})
}
