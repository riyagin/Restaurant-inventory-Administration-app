package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

// buildThrPayslipData assembles the DB-free PayslipData for a single THR line.
func (h *ThrHandler) buildThrPayslipData(ctx context.Context, lineID pgtype.UUID) (service.PayslipData, *db.GetThrLineForPayslipRow, error) {
	var empty service.PayslipData

	line, err := h.queries.GetThrLineForPayslip(ctx, lineID)
	if err != nil {
		return empty, nil, err
	}
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

	note := fmt.Sprintf("Masa kerja: %d bulan (%s × gaji pokok %s).",
		line.MonthsWorked, ratioLabel(line.MonthsWorked), formatRupiahShort(line.BaseSalary))
	if line.ReviewNote.Valid && strings.TrimSpace(line.ReviewNote.String) != "" {
		note += " " + line.ReviewNote.String
	}

	data := service.PayslipData{
		CompanyName:    settings.CompanyName,
		Address:        settings.Address,
		LogoPath:       logoPath,
		PayslipFooter:  settings.PayslipFooter,
		Title:          "SLIP THR",
		EmployeeName:   line.EmployeeName,
		EmployeeCode:   line.EmployeeCode,
		Position:       textValue(line.PositionName),
		Branch:         textValue(line.BranchName),
		JoinDate:       dateLabelID(line.JoinDate.Time),
		PeriodLabel:    line.RunName,
		Earnings:       []service.PayslipLineItem{{Label: "THR (Tunjangan Hari Raya)", Amount: line.ThrAmount}},
		Deductions:     nil,
		TotalEarnings:  line.ThrAmount,
		TotalDeduction: 0,
		NetPay:         line.ThrAmount,
		Note:           note,
	}
	return data, line, nil
}

// ratioLabel renders the THR proportion as "n/12" (or "1 bulan penuh" at ≥12 months).
func ratioLabel(months int32) string {
	if months >= 12 {
		return "1 bulan penuh"
	}
	return fmt.Sprintf("%d/12", months)
}

// formatRupiahShort renders a whole-rupiah amount id-ID style (e.g. "Rp 1.500.000").
func formatRupiahShort(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp " + b.String()
	if neg {
		out = "-" + out
	}
	return out
}

// DownloadPayslip — GET /api/hr/thr/lines/:id/payslip
func (h *ThrHandler) DownloadPayslip(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	data, line, err := h.buildThrPayslipData(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "baris THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan data slip THR")
		return
	}
	if line.RunStatus == "open" {
		respondError(w, http.StatusConflict, "slip THR hanya tersedia untuk run yang sudah ditutup")
		return
	}

	pdfBytes, err := service.BuildPayslipPDF(data)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat PDF slip THR")
		return
	}

	filename := fmt.Sprintf("slip-thr-%s-%s.pdf", line.EmployeeCode, line.PaymentDate.Time.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write(pdfBytes)
}

// DownloadRunPayslips — GET /api/hr/thr/runs/:id/payslips
// One PDF holding every THR slip in the run, two per A4-landscape page so each
// sheet cuts into two A5-portrait slips.
func (h *ThrHandler) DownloadRunPayslips(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	run, err := h.queries.GetThrRunByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "run THR tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil run THR")
		return
	}
	if run.Status == "open" {
		respondError(w, http.StatusConflict, "slip THR hanya tersedia untuk run yang sudah ditutup")
		return
	}

	lines, err := h.queries.ListThrLinesForRun(ctx, &db.ListThrLinesForRunParams{
		ThrRunID: pgID,
		Q:        "",
		Sort:     "name",
		Order:    "asc",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil baris THR")
		return
	}
	if len(lines) == 0 {
		respondError(w, http.StatusNotFound, "tidak ada baris THR pada run ini")
		return
	}

	dateStr := run.PaymentDate.Time.Format("2006-01-02")
	slips := make([]service.PayslipData, 0, len(lines))
	for _, l := range lines {
		data, _, derr := h.buildThrPayslipData(ctx, l.ID)
		if derr != nil {
			continue
		}
		slips = append(slips, data)
	}
	if len(slips) == 0 {
		respondError(w, http.StatusInternalServerError, "gagal menyiapkan data slip THR")
		return
	}

	pdfBytes, err := service.BuildPayslipSheetPDF(slips)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat PDF slip THR")
		return
	}

	filename := fmt.Sprintf("slip-thr-%s.pdf", dateStr)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write(pdfBytes)
}
