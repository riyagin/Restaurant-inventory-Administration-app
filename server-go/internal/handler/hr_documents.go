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

// parseDocDate reads a "YYYY-MM-DD" document date, falling back to today so a
// blank or malformed date still numbers the letter in the current month.
func parseDocDate(s string) time.Time {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
		return t
	}
	return time.Now()
}

// PeekDocumentNumber — GET /api/hr/documents/next-number?type=&date=
// Renders what the next number will look like without advancing the counter, so
// the generator form can show it before anything is produced.
func (h *HRDocumentsHandler) PeekDocumentNumber(w http.ResponseWriter, r *http.Request) {
	s, err := h.queries.GetHRSettings(r.Context())
	if err != nil || s == nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan HR")
		return
	}
	q := r.URL.Query()
	respondJSON(w, http.StatusOK, map[string]any{
		"number":  service.FormatDocNumber(s.DocNumberFormat, s.DocNumberCounter, q.Get("type"), parseDocDate(q.Get("date"))),
		"counter": s.DocNumberCounter,
	})
}

// ReserveDocumentNumber — POST /api/hr/documents/next-number?type=&date=
// Claims the current counter value for a letter that was actually produced and
// advances it, so the next letter gets a different number.
func (h *HRDocumentsHandler) ReserveDocumentNumber(w http.ResponseWriter, r *http.Request) {
	row, err := h.queries.ConsumeHRDocumentNumber(r.Context())
	if err != nil || row == nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil nomor dokumen")
		return
	}
	q := r.URL.Query()
	respondJSON(w, http.StatusOK, map[string]any{
		"number":  service.FormatDocNumber(row.DocNumberFormat, row.Reserved, q.Get("type"), parseDocDate(q.Get("date"))),
		"counter": row.Reserved,
	})
}

// applySettingsDefaults fills the company-level half of a document request from
// HR settings. Those fields (letterhead, signatory, standard working hours and
// payment wording) are configured once and are not asked for on the generator
// form, so anything the request leaves blank comes from here. A value the client
// did send still wins — one-off overrides stay possible.
func (h *HRDocumentsHandler) applySettingsDefaults(r *http.Request, in *service.HRDocInput) {
	s, err := h.queries.GetHRSettings(r.Context())
	if err != nil || s == nil {
		return
	}
	fill := func(dst *string, v string) {
		if strings.TrimSpace(*dst) == "" {
			*dst = v
		}
	}
	fill(&in.CompanyName, s.CompanyName)
	fill(&in.CompanyAddress, s.Address)
	fill(&in.CompanyPhone, s.CompanyPhone)
	fill(&in.CompanyEmail, s.CompanyEmail)
	fill(&in.City, s.CompanyCity)
	fill(&in.SignatoryName, s.SignatoryName)
	fill(&in.SignatoryPosition, s.SignatoryPosition)
	fill(&in.SignatoryNID, s.SignatoryNationalID)
	fill(&in.WorkingHours, s.DocWorkingHours)
	fill(&in.PaymentInfo, s.DocPaymentInfo)
	if in.ProbationMonths == 0 && in.Type == service.DocTypePKWTT {
		in.ProbationMonths = int(s.DocProbationMonths)
	}
}

// nextDocumentNumber consumes the running counter and renders it through the
// configured format. Called only when the request left the number blank, so a
// manually-entered number never burns a counter value.
func (h *HRDocumentsHandler) nextDocumentNumber(r *http.Request, docType string, date time.Time) string {
	row, err := h.queries.ConsumeHRDocumentNumber(r.Context())
	if err != nil || row == nil {
		return ""
	}
	return service.FormatDocNumber(row.DocNumberFormat, row.Reserved, docType, date)
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
	h.applySettingsDefaults(r, &in)
	if strings.TrimSpace(in.DocumentNumber) == "" {
		in.DocumentNumber = h.nextDocumentNumber(r, in.Type, parseDocDate(in.DocumentDate))
	}

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

// ── contract condition templates ───────────────────────────────────────────
//
// A named preset of the *ketentuan* half of a PKWT/PKWTT, so hiring three
// cashiers doesn't mean typing the same terms three times (and getting them
// subtly different). Applying one fills the generator form; every field stays
// editable afterwards. Company-level values are deliberately absent — they come
// from hr_settings, and duplicating them per template is how they would drift.

type contractTemplateDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DocType        string `json:"doc_type"`
	PositionName   string `json:"position_name"`
	DivisionName   string `json:"division_name"`
	PlaceOfWork    string `json:"place_of_work"`
	Salary         int64  `json:"salary"`
	SalaryPeriod   string `json:"salary_period"`
	JobDescription string `json:"job_description"`
	ContractMonths int    `json:"contract_months"`
	Notes          string `json:"notes"`
}

func (d *contractTemplateDTO) normalize() string {
	d.Name = strings.TrimSpace(d.Name)
	d.DocType = strings.TrimSpace(d.DocType)
	d.SalaryPeriod = strings.TrimSpace(d.SalaryPeriod)
	if d.Name == "" {
		return "nama template wajib diisi"
	}
	switch d.DocType {
	case "", service.DocTypePKWT, service.DocTypePKWTT:
	default:
		return "jenis dokumen template harus pkwt, pkwtt, atau kosong"
	}
	if d.SalaryPeriod == "" {
		d.SalaryPeriod = "bulan"
	}
	if d.SalaryPeriod != "bulan" && d.SalaryPeriod != "hari" {
		return "periode upah harus bulan atau hari"
	}
	if d.Salary < 0 {
		return "upah tidak boleh negatif"
	}
	if d.ContractMonths < 0 {
		return "jangka waktu kontrak tidak boleh negatif"
	}
	return ""
}

const contractTemplateCols = `id, name, doc_type, position_name, division_name, place_of_work,
	salary, salary_period, job_description, contract_months, notes`

func scanContractTemplate(row interface {
	Scan(dest ...any) error
}) (contractTemplateDTO, error) {
	var (
		t  contractTemplateDTO
		id pgtype.UUID
	)
	err := row.Scan(&id, &t.Name, &t.DocType, &t.PositionName, &t.DivisionName, &t.PlaceOfWork,
		&t.Salary, &t.SalaryPeriod, &t.JobDescription, &t.ContractMonths, &t.Notes)
	t.ID = uuid.UUID(id.Bytes).String()
	return t, err
}

// ListContractTemplates — GET /api/hr/contract-templates?type=pkwt
func (h *HRDocumentsHandler) ListContractTemplates(w http.ResponseWriter, r *http.Request) {
	docType := strings.TrimSpace(r.URL.Query().Get("type"))
	rows, err := h.pool.Query(r.Context(),
		`SELECT `+contractTemplateCols+`
		 FROM hr_contract_templates
		 WHERE $1::text = '' OR doc_type = '' OR doc_type = $1
		 ORDER BY name`, docType)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil template kontrak")
		return
	}
	defer rows.Close()

	out := []contractTemplateDTO{}
	for rows.Next() {
		t, err := scanContractTemplate(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca template kontrak")
			return
		}
		out = append(out, t)
	}
	respondJSON(w, http.StatusOK, out)
}

// CreateContractTemplate — POST /api/hr/contract-templates
func (h *HRDocumentsHandler) CreateContractTemplate(w http.ResponseWriter, r *http.Request) {
	var body contractTemplateDTO
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}

	var id pgtype.UUID
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO hr_contract_templates
		  (name, doc_type, position_name, division_name, place_of_work,
		   salary, salary_period, job_description, contract_months, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		body.Name, body.DocType, body.PositionName, body.DivisionName, body.PlaceOfWork,
		body.Salary, body.SalaryPeriod, body.JobDescription, body.ContractMonths, body.Notes).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan template kontrak")
		return
	}

	logMutation(r, h.queries, "CREATE", "hr_contract_template", uuid.UUID(id.Bytes),
		"Menambah template kontrak "+body.Name)
	body.ID = uuid.UUID(id.Bytes).String()
	respondJSON(w, http.StatusCreated, body)
}

// UpdateContractTemplate — PUT /api/hr/contract-templates/{id}
func (h *HRDocumentsHandler) UpdateContractTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}
	var body contractTemplateDTO
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if msg := body.normalize(); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE hr_contract_templates
		SET name = $2, doc_type = $3, position_name = $4, division_name = $5, place_of_work = $6,
		    salary = $7, salary_period = $8, job_description = $9, contract_months = $10,
		    notes = $11, updated_at = now()
		WHERE id = $1`,
		pgtype.UUID{Bytes: id, Valid: true}, body.Name, body.DocType, body.PositionName,
		body.DivisionName, body.PlaceOfWork, body.Salary, body.SalaryPeriod,
		body.JobDescription, body.ContractMonths, body.Notes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui template kontrak")
		return
	}
	if tag.RowsAffected() == 0 {
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}

	logMutation(r, h.queries, "UPDATE", "hr_contract_template", id, "Memperbarui template kontrak "+body.Name)
	body.ID = id.String()
	respondJSON(w, http.StatusOK, body)
}

// DeleteContractTemplate — DELETE /api/hr/contract-templates/{id}
func (h *HRDocumentsHandler) DeleteContractTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID template tidak valid")
		return
	}
	var name string
	err = h.pool.QueryRow(r.Context(),
		`DELETE FROM hr_contract_templates WHERE id = $1 RETURNING name`,
		pgtype.UUID{Bytes: id, Valid: true}).Scan(&name)
	if err != nil {
		respondError(w, http.StatusNotFound, "template tidak ditemukan")
		return
	}
	logMutation(r, h.queries, "DELETE", "hr_contract_template", id, "Menghapus template kontrak "+name)
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
