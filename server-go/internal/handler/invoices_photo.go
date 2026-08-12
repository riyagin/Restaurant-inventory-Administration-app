package handler

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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

// SetUploadsDir injects the uploads directory into the handler.
func (h *InvoicesHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

// receiptFileExt decides what to save a receipt upload as.
//
// Judging by filename extension alone rejected perfectly good uploads: a photo
// taken in the app and posted as a blob arrives with no filename at all, and
// Android share sheets routinely hand over "image.webp" or a HEIC from the
// camera roll. So the part's declared content type is consulted first — it is
// what the browser actually knows about the bytes — and the filename is only a
// fallback for the browsers that send a generic octet-stream.
//
// The error names the format that was rejected. "format file tidak didukung"
// with no subject leaves the operator guessing which of the three files they
// just tried was the problem.
func receiptFileExt(header *multipart.FileHeader) (string, error) {
	byContentType := map[string]string{
		"image/jpeg":      ".jpg",
		"image/jpg":       ".jpg",
		"image/png":       ".png",
		"image/webp":      ".webp",
		"image/heic":      ".heic",
		"image/heif":      ".heic",
		"application/pdf": ".pdf",
	}
	allowedExt := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true,
		".webp": true, ".heic": true, ".heif": true, ".pdf": true,
	}

	contentType := strings.ToLower(strings.TrimSpace(
		strings.SplitN(header.Header.Get("Content-Type"), ";", 2)[0]))
	if ext, ok := byContentType[contentType]; ok {
		return ext, nil
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if allowedExt[ext] {
		if ext == ".jpeg" {
			return ".jpg", nil
		}
		if ext == ".heif" {
			return ".heic", nil
		}
		return ext, nil
	}

	switch {
	case ext != "":
		return "", fmt.Errorf("format file %s tidak didukung — gunakan JPG, PNG, WEBP, HEIC atau PDF", ext)
	case contentType != "":
		return "", fmt.Errorf("jenis file %q tidak didukung — gunakan JPG, PNG, WEBP, HEIC atau PDF", contentType)
	default:
		return "", errors.New("jenis file tidak dikenali — gunakan JPG, PNG, WEBP, HEIC atau PDF")
	}
}

// UploadPhoto — POST /api/invoices/:id/photo
func (h *InvoicesHandler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
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

	ext, err := receiptFileExt(header)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	invoice, err := h.queries.GetInvoiceByID(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}

	filename := fmt.Sprintf("invoice-%s-%d%s", id.String(), time.Now().Unix(), ext)
	uploadsDir := h.uploadsDir
	if uploadsDir == "" {
		uploadsDir = filepath.Join("..", "server", "uploads")
	}

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

	// Delete old file if one exists
	if invoice.PhotoPath.Valid && invoice.PhotoPath.String != "" {
		_ = os.Remove(filepath.Join(uploadsDir, invoice.PhotoPath.String))
	}

	if err := h.queries.UpdateInvoicePhotoPath(ctx, &db.UpdateInvoicePhotoPathParams{
		PhotoPath: pgtype.Text{String: filename, Valid: true},
		ID:        invoiceUUID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan path foto")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Upload foto faktur %s", invoice.InvoiceNumber),
	})

	respondJSON(w, http.StatusOK, map[string]string{"photo_path": filename})
}

// DeletePhoto — DELETE /api/invoices/:id/photo
func (h *InvoicesHandler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	invoice, err := h.queries.GetInvoiceByID(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}

	if !invoice.PhotoPath.Valid || invoice.PhotoPath.String == "" {
		respondError(w, http.StatusBadRequest, "faktur tidak memiliki foto")
		return
	}

	uploadsDir := h.uploadsDir
	if uploadsDir == "" {
		uploadsDir = filepath.Join("..", "server", "uploads")
	}

	_ = os.Remove(filepath.Join(uploadsDir, invoice.PhotoPath.String))

	if err := h.queries.UpdateInvoicePhotoPath(ctx, &db.UpdateInvoicePhotoPathParams{
		PhotoPath: pgtype.Text{},
		ID:        invoiceUUID,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus path foto")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Hapus foto faktur %s", invoice.InvoiceNumber),
	})

	w.WriteHeader(http.StatusOK)
}
