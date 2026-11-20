package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// UploadPhoto — POST /api/hr/employees/:id/photo
func (h *HREmployeesHandler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form upload")
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		respondError(w, http.StatusBadRequest, "field 'photo' tidak ditemukan")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png":
	default:
		respondError(w, http.StatusBadRequest, "format file tidak didukung (jpg, jpeg, png)")
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

	filename := fmt.Sprintf("employee-%s-%d%s", id.String(), time.Now().Unix(), ext)
	uploadsDir := h.resolveUploadsDir()

	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat direktori upload")
		return
	}

	dst, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menulis file")
		return
	}

	if emp.PhotoPath.Valid && emp.PhotoPath.String != "" {
		_ = os.Remove(filepath.Join(uploadsDir, emp.PhotoPath.String))
	}

	if err := h.queries.UpdateEmployeePhotoPath(ctx, &db.UpdateEmployeePhotoPathParams{
		PhotoPath: pgtype.Text{String: filename, Valid: true},
		ID:        pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan path foto")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Upload foto karyawan %s", emp.FullName),
	})

	respondJSON(w, http.StatusOK, map[string]string{"photo_path": filename})
}

// DeletePhoto — DELETE /api/hr/employees/:id/photo
func (h *HREmployeesHandler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
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

	if !emp.PhotoPath.Valid || emp.PhotoPath.String == "" {
		respondError(w, http.StatusBadRequest, "karyawan tidak memiliki foto")
		return
	}

	_ = os.Remove(filepath.Join(h.resolveUploadsDir(), emp.PhotoPath.String))

	if err := h.queries.UpdateEmployeePhotoPath(ctx, &db.UpdateEmployeePhotoPathParams{
		PhotoPath: pgtype.Text{},
		ID:        pgID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus path foto")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_employee",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus foto karyawan %s", emp.FullName),
	})

	respondJSON(w, http.StatusOK, map[string]string{"message": "foto berhasil dihapus"})
}
