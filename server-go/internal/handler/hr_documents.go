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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// HRDocumentsHandler covers two related things:
//   - Stateless generation of the four Indonesian HR documents (PKWT, PKWTT,
//     Surat Peringatan, Paklaring) as downloadable DOCX or PDF. Nothing is
//     persisted — the request carries all field values.
//   - The employee_documents store: signed scans HR uploads (during onboarding
//     or later) and keeps on file per employee.
type HRDocumentsHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewHRDocumentsHandler(pool *pgxpool.Pool, queries *db.Queries) *HRDocumentsHandler {
	return &HRDocumentsHandler{pool: pool, queries: queries}
}

func (h *HRDocumentsHandler) SetUploadsDir(dir string) { h.uploadsDir = dir }

func (h *HRDocumentsHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// Generate renders a document to DOCX or PDF and streams it back as an
// attachment. Format is taken from ?format=docx|pdf (default docx).
func (h *HRDocumentsHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var in service.HRDocInput
	if err := parseBody(r, &in); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	in.Type = strings.TrimSpace(in.Type)

	doc, err := service.BuildHRDocument(in)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "docx"
	}

	var (
		payload  []byte
		mime     string
		ext      string
		buildErr error
	)
	switch format {
	case "pdf":
		payload, buildErr = service.RenderHRDocPDF(doc)
		mime, ext = "application/pdf", "pdf"
	case "docx":
		payload, buildErr = service.RenderHRDocDOCX(doc)
		mime, ext = "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"
	default:
		respondError(w, http.StatusBadRequest, "format harus 'docx' atau 'pdf'")
		return
	}
	if buildErr != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat dokumen")
		return
	}

	filename := fmt.Sprintf("%s.%s", doc.FilenameBase, ext)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write(payload)
}

// ---- employee_documents store --------------------------------------------

type employeeDocumentDTO struct {
	ID           string `json:"id"`
	DocType      string `json:"doc_type"`
	Title        string `json:"title"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	IsSigned     bool   `json:"is_signed"`
	Notes        string `json:"notes"`
	UploadedBy   string `json:"uploaded_by"`
	CreatedAt    string `json:"created_at"`
}

// ListEmployeeDocuments returns every document filed for an employee, newest
// first, with the uploader's username resolved.
func (h *HRDocumentsHandler) ListEmployeeDocuments(w http.ResponseWriter, r *http.Request) {
	empID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
		return
	}
	ctx := r.Context()
	rows, err := h.pool.Query(ctx, `
		SELECT d.id, d.doc_type, d.title,
		       COALESCE(d.original_name, ''), COALESCE(d.mime_type, ''),
		       COALESCE(d.size_bytes, 0), d.is_signed, COALESCE(d.notes, ''),
		       COALESCE(u.username, ''), d.created_at
		FROM employee_documents d
		LEFT JOIN users u ON u.id = d.uploaded_by
		WHERE d.employee_id = $1
		ORDER BY d.created_at DESC`,
		pgtype.UUID{Bytes: empID, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil dokumen")
		return
	}
	defer rows.Close()

	out := []employeeDocumentDTO{}
	for rows.Next() {
		var (
			id        pgtype.UUID
			d         employeeDocumentDTO
			createdAt time.Time
		)
		if err := rows.Scan(&id, &d.DocType, &d.Title, &d.OriginalName, &d.MimeType,
			&d.SizeBytes, &d.IsSigned, &d.Notes, &d.UploadedBy, &createdAt); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca dokumen")
			return
		}
		d.ID = uuid.UUID(id.Bytes).String()
		d.CreatedAt = createdAt.Format(time.RFC3339)
		out = append(out, d)
	}
	respondJSON(w, http.StatusOK, out)
}

// UploadEmployeeDocument stores an uploaded signed document for an employee.
func (h *HRDocumentsHandler) UploadEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	empID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
		return
	}

	if err := r.ParseMultipartForm(25 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "field 'file' tidak ditemukan")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".pdf": true, ".jpg": true, ".jpeg": true, ".png": true, ".docx": true, ".doc": true}
	if !allowed[ext] {
		respondError(w, http.StatusBadRequest, "format file tidak didukung (pdf, jpg, png, doc, docx)")
		return
	}

	docType := strings.TrimSpace(r.FormValue("doc_type"))
	if docType == "" {
		docType = "other"
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = header.Filename
	}
	notes := strings.TrimSpace(r.FormValue("notes"))
	isSigned := true
	if v := strings.TrimSpace(r.FormValue("is_signed")); v == "false" || v == "0" {
		isSigned = false
	}

	uploadsDir := h.resolveUploadsDir()
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat direktori upload")
		return
	}
	storedName := fmt.Sprintf("empdoc-%s-%d%s", empID.String(), time.Now().UnixNano(), ext)
	dst, err := os.Create(filepath.Join(uploadsDir, storedName))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan file")
		return
	}
	written, copyErr := io.Copy(dst, file)
	dst.Close()
	if copyErr != nil {
		_ = os.Remove(filepath.Join(uploadsDir, storedName))
		respondError(w, http.StatusInternalServerError, "gagal menulis file")
		return
	}

	ctx := r.Context()
	var uploadedBy pgtype.UUID
	if uid := middleware.UserIDFromCtx(ctx); uid != uuid.Nil {
		uploadedBy = pgtype.UUID{Bytes: uid, Valid: true}
	}

	var (
		newID     pgtype.UUID
		createdAt time.Time
	)
	err = h.pool.QueryRow(ctx, `
		INSERT INTO employee_documents
		  (employee_id, doc_type, title, file_path, original_name, mime_type, size_bytes, is_signed, notes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		pgtype.UUID{Bytes: empID, Valid: true}, docType, title, storedName,
		header.Filename, header.Header.Get("Content-Type"), written, isSigned,
		textOrNull(notes), uploadedBy).Scan(&newID, &createdAt)
	if err != nil {
		_ = os.Remove(filepath.Join(uploadsDir, storedName))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data dokumen")
		return
	}

	logMutation(r, h.queries, "CREATE", "hr_employee_document", empID,
		fmt.Sprintf("Upload dokumen %q (%s) untuk karyawan", title, docType))

	respondJSON(w, http.StatusCreated, employeeDocumentDTO{
		ID:           uuid.UUID(newID.Bytes).String(),
		DocType:      docType,
		Title:        title,
		OriginalName: header.Filename,
		MimeType:     header.Header.Get("Content-Type"),
		SizeBytes:    written,
		IsSigned:     isSigned,
		Notes:        notes,
		CreatedAt:    createdAt.Format(time.RFC3339),
	})
}

// DownloadEmployeeDocument streams a stored document file inline, behind auth,
// rather than exposing the uploads dir publicly (HR documents are sensitive).
func (h *HRDocumentsHandler) DownloadEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	docID, err := parseUUID(chi.URLParam(r, "docId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID dokumen tidak valid")
		return
	}
	ctx := r.Context()
	var filePath, originalName, mimeType string
	err = h.pool.QueryRow(ctx, `
		SELECT file_path, COALESCE(original_name, file_path), COALESCE(mime_type, '')
		FROM employee_documents WHERE id = $1`,
		pgtype.UUID{Bytes: docID, Valid: true}).Scan(&filePath, &originalName, &mimeType)
	if err != nil {
		respondError(w, http.StatusNotFound, "dokumen tidak ditemukan")
		return
	}
	full := filepath.Join(h.resolveUploadsDir(), filePath)
	f, err := os.Open(full)
	if err != nil {
		respondError(w, http.StatusNotFound, "file dokumen tidak ditemukan di server")
		return
	}
	defer f.Close()
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, originalName))
	_, _ = io.Copy(w, f)
}

// DeleteEmployeeDocument removes a document row and unlinks its file.
func (h *HRDocumentsHandler) DeleteEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	empID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID karyawan tidak valid")
		return
	}
	docID, err := parseUUID(chi.URLParam(r, "docId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID dokumen tidak valid")
		return
	}
	ctx := r.Context()
	var (
		filePath string
		title    string
	)
	err = h.pool.QueryRow(ctx, `
		DELETE FROM employee_documents
		WHERE id = $1 AND employee_id = $2
		RETURNING file_path, title`,
		pgtype.UUID{Bytes: docID, Valid: true},
		pgtype.UUID{Bytes: empID, Valid: true}).Scan(&filePath, &title)
	if err != nil {
		respondError(w, http.StatusNotFound, "dokumen tidak ditemukan")
		return
	}
	_ = os.Remove(filepath.Join(h.resolveUploadsDir(), filePath))

	logMutation(r, h.queries, "DELETE", "hr_employee_document", empID,
		fmt.Sprintf("Menghapus dokumen %q karyawan", title))

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
