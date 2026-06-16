package handler

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// PayslipHandler renders PDF payslips on demand from immutable payroll snapshots
// and manages the singleton hr_settings used for the company header. Payslip
// endpoints reject periods that are still 'open' with a 409 (data is not final yet).
type PayslipHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewPayslipHandler(pool *pgxpool.Pool, queries *db.Queries) *PayslipHandler {
	return &PayslipHandler{pool: pool, queries: queries}
}

// SetUploadsDir injects the uploads directory (server/uploads) used for the logo.
func (h *PayslipHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

func (h *PayslipHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// monthLabelID renders a month as "Mei 2026" (Indonesian month names).
var idMonths = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func monthLabelID(t time.Time) string {
	return fmt.Sprintf("%s %d", idMonths[int(t.Month())], t.Year())
}

func dateLabelID(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	short := [...]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
	return fmt.Sprintf("%02d %s %d", t.Day(), short[int(t.Month())], t.Year())
}

func numericFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// trimDays formats a NUMERIC(5,2) day count without trailing zeros (2.00 -> "2",
// 1.50 -> "1.5").
func trimDays(n pgtype.Numeric) string {
	f := numericFloat(n)
	s := strconv.FormatFloat(f, 'f', -1, 64)
	return s
}

// buildPayslipData assembles the DB-free PayslipData for a single line.
func (h *PayslipHandler) buildPayslipData(r *http.Request, lineID pgtype.UUID) (service.PayslipData, *db.GetPayrollLineForPayslipRow, error) {
	ctx := r.Context()
	var empty service.PayslipData

	line, err := h.queries.GetPayrollLineForPayslip(ctx, lineID)
	if err != nil {
		return empty, nil, err
	}

	components, _ := h.queries.ListPayrollLineComponents(ctx, lineID)

	settings, err := h.queries.GetHRSettings(ctx)
	if err != nil {
		return empty, line, err
	}

	logoPath := ""
	if settings.LogoPath.Valid && strings.TrimSpace(settings.LogoPath.String) != "" {
		p := filepath.Join(h.resolveUploadsDir(), settings.LogoPath.String)
		if _, statErr := os.Stat(p); statErr == nil {
			logoPath = p
		}
	}

	// ── Earnings (PENDAPATAN) ──────────────────────────────────────────────────
	earnings := []service.PayslipLineItem{
		{Label: "Gaji Pokok", Amount: line.BaseSalary},
	}
	for _, c := range components {
		switch c.Type {
		case "allowance":
			earnings = append(earnings, service.PayslipLineItem{Label: c.Name, Amount: c.Amount})
		case "bonus":
			if c.Amount != 0 {
				earnings = append(earnings, service.PayslipLineItem{Label: c.Name, Amount: c.Amount})
			}
		}
	}
	if line.OvertimeAmount != 0 {
		earnings = append(earnings, service.PayslipLineItem{
			Label:  fmt.Sprintf("Lembur (%s hari)", trimDays(line.OvertimeDays)),
			Amount: line.OvertimeAmount,
		})
	}
	if line.OvertimeHourlyAmount != 0 {
		earnings = append(earnings, service.PayslipLineItem{
			Label:  fmt.Sprintf("Lembur (%s jam)", trimDays(line.OvertimeHours)),
			Amount: line.OvertimeHourlyAmount,
		})
	}
	if line.PublicHolidayAmount != 0 {
		earnings = append(earnings, service.PayslipLineItem{
			Label:  fmt.Sprintf("Hari Libur (%s hari)", trimDays(line.PublicHolidayDays)),
			Amount: line.PublicHolidayAmount,
		})
	}

	// ── Deductions (POTONGAN) ──────────────────────────────────────────────────
	var deductions []service.PayslipLineItem
	for _, c := range components {
		if c.Type == "deduction" && c.Amount != 0 {
			deductions = append(deductions, service.PayslipLineItem{Label: c.Name, Amount: c.Amount})
		}
	}
	if line.KasbonDeduction != 0 {
		label := "Kasbon"
		if nums, kerr := h.queries.ListLineKasbonNumbers(ctx, lineID); kerr == nil && len(nums) > 0 {
			label = "Kasbon (" + strings.Join(nums, ", ") + ")"
		}
		deductions = append(deductions, service.PayslipLineItem{Label: label, Amount: line.KasbonDeduction})
	}
	if line.UnpaidLeaveDeduction != 0 {
		deductions = append(deductions, service.PayslipLineItem{
			Label:  fmt.Sprintf("Cuti Tanpa Gaji (%d hari)", line.UnpaidLeaveDays),
			Amount: line.UnpaidLeaveDeduction,
		})
	}

	totalDeduction := line.GrossPay - line.NetPay

	note := ""
	if line.ReviewNote.Valid {
		note = line.ReviewNote.String
	}

	data := service.PayslipData{
		CompanyName:    settings.CompanyName,
		Address:        settings.Address,
		LogoPath:       logoPath,
		PayslipFooter:  settings.PayslipFooter,
		EmployeeName:   line.EmployeeName,
		EmployeeCode:   line.EmployeeCode,
		Position:       textValue(line.PositionName),
		Branch:         textValue(line.BranchName),
		JoinDate:       dateLabelID(line.JoinDate.Time),
		PeriodLabel:    monthLabelID(line.PeriodMonth.Time),
		Earnings:       earnings,
		Deductions:     deductions,
		TotalEarnings:  line.GrossPay,
		TotalDeduction: totalDeduction,
		NetPay:         line.NetPay,
		Note:           note,
	}
	return data, line, nil
}

func textValue(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// DownloadPayslip — GET /api/hr/payroll/lines/:id/payslip
func (h *PayslipHandler) DownloadPayslip(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	data, line, err := h.buildPayslipData(r, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan data slip gaji")
		return
	}
	if line.PeriodStatus == "open" {
		respondError(w, http.StatusConflict, "slip gaji hanya tersedia untuk periode yang sudah ditutup")
		return
	}

	pdfBytes, err := service.BuildPayslipPDF(data)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat PDF slip gaji")
		return
	}

	filename := fmt.Sprintf("slip-gaji-%s-%s.pdf", line.EmployeeCode, line.PeriodMonth.Time.Format("2006-01"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write(pdfBytes)
}

// DownloadPeriodPayslips — GET /api/hr/payroll/periods/:id/payslips
// Streams a ZIP containing one PDF per payroll line in the period. Rejects open
// periods with a 409.
func (h *PayslipHandler) DownloadPeriodPayslips(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	period, err := h.queries.GetPayrollPeriodByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "periode penggajian tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil periode penggajian")
		return
	}
	if period.Status == "open" {
		respondError(w, http.StatusConflict, "slip gaji hanya tersedia untuk periode yang sudah ditutup")
		return
	}

	lines, err := h.queries.ListPayrollLinesForPeriod(ctx, &db.ListPayrollLinesForPeriodParams{
		PayrollPeriodID: pgID,
		Q:               "",
		Sort:            "name",
		Order:           "asc",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris penggajian")
		return
	}
	if len(lines) == 0 {
		respondError(w, http.StatusNotFound, "tidak ada baris penggajian pada periode ini")
		return
	}

	// Build the ZIP into a buffer (payslips are small; A4 PDFs ~ a few KB each).
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	monthStr := period.PeriodMonth.Time.Format("2006-01")
	for _, l := range lines {
		data, lineRow, derr := h.buildPayslipData(r, l.ID)
		if derr != nil {
			continue
		}
		pdfBytes, perr := service.BuildPayslipPDF(data)
		if perr != nil {
			continue
		}
		entryName := fmt.Sprintf("slip-gaji-%s-%s.pdf", lineRow.EmployeeCode, monthStr)
		fwz, werr := zw.Create(entryName)
		if werr != nil {
			continue
		}
		_, _ = io.Copy(fwz, bytes.NewReader(pdfBytes))
	}
	if err := zw.Close(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat arsip ZIP")
		return
	}

	zipName := fmt.Sprintf("slip-gaji-periode-%s.zip", monthStr)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	_, _ = w.Write(buf.Bytes())
}

// ── HR Settings ───────────────────────────────────────────────────────────────

// GetSettings — GET /api/hr/settings
func (h *PayslipHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.queries.GetHRSettings(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan HR")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

type hrSettingsBody struct {
	CompanyName   string `json:"company_name"`
	Address       string `json:"address"`
	PayslipFooter string `json:"payslip_footer"`
}

// UpdateSettings — PUT /api/hr/settings
func (h *PayslipHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body hrSettingsBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	settings, err := h.queries.UpdateHRSettings(r.Context(), &db.UpdateHRSettingsParams{
		CompanyName:   strings.TrimSpace(body.CompanyName),
		Address:       strings.TrimSpace(body.Address),
		PayslipFooter: strings.TrimSpace(body.PayslipFooter),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengaturan HR")
		return
	}

	_ = service.LogActivity(r.Context(), h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(r.Context()),
		Username:    middleware.UsernameFromCtx(r.Context()),
		Action:      "UPDATE",
		EntityType:  "hr_settings",
		EntityID:    uuid.Nil,
		Description: "Memperbarui pengaturan slip gaji",
	})

	respondJSON(w, http.StatusOK, settings)
}

// UploadLogo — POST /api/hr/settings/logo
func (h *PayslipHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "gagal membaca form upload")
		return
	}
	file, header, err := r.FormFile("logo")
	if err != nil {
		respondError(w, http.StatusBadRequest, "field 'logo' tidak ditemukan")
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
	current, err := h.queries.GetHRSettings(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan HR")
		return
	}

	filename := fmt.Sprintf("hr-logo-%d%s", time.Now().Unix(), ext)
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

	if current.LogoPath.Valid && current.LogoPath.String != "" {
		_ = os.Remove(filepath.Join(uploadsDir, current.LogoPath.String))
	}

	settings, err := h.queries.UpdateHRSettingsLogo(ctx, pgtype.Text{String: filename, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan path logo")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_settings",
		EntityID:    uuid.Nil,
		Description: "Mengunggah logo perusahaan untuk slip gaji",
	})

	respondJSON(w, http.StatusOK, settings)
}
