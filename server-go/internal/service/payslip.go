package service

import (
	"bytes"
	"strconv"
	"strings"

	// github.com/go-pdf/fpdf is chosen as the PDF library because it is pure Go
	// (no cgo, no external binaries like wkhtmltopdf), has a single dependency, and
	// runs unchanged under PM2 on the Ubuntu VPS. The maroto alternative is heavier
	// and pulls gofpdf transitively; fpdf gives us the low-level A4 control we need
	// for the two-column payslip layout while staying VPS-friendly.
	"github.com/go-pdf/fpdf"
)

// PayslipLineItem is a single labelled money row in either the PENDAPATAN
// (earnings) or POTONGAN (deductions) column.
type PayslipLineItem struct {
	Label  string
	Amount int64
}

// PayslipData is the fully-resolved, DB-free input to BuildPayslipPDF. The handler
// assembles it from payroll_lines + components + settings so this function stays
// pure and unit-testable (valid-PDF magic-byte test).
type PayslipData struct {
	// Company header (from hr_settings).
	CompanyName   string
	Address       string
	LogoPath      string // absolute path on disk; ignored if empty / unreadable
	PayslipFooter string

	// Employee + period identity.
	EmployeeName string
	EmployeeCode string
	Position     string
	Branch       string
	JoinDate     string // pre-formatted, e.g. "01 Jan 2024"
	PeriodLabel  string // pre-formatted month, e.g. "Mei 2026"

	// Earnings (PENDAPATAN) and deductions (POTONGAN) rows, already ordered.
	Earnings   []PayslipLineItem
	Deductions []PayslipLineItem

	// Totals.
	TotalEarnings  int64
	TotalDeduction int64
	NetPay         int64

	// Catatan (review note), optional.
	Note string
}

// formatRupiah renders a whole-rupiah amount in id-ID style, e.g. 1500000 ->
// "Rp 1.500.000". Negative values keep the sign before the currency prefix.
func formatRupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	// Insert thousands separators (".").
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

// BuildPayslipPDF renders a single A4 payslip and returns the PDF bytes. It is
// robust to a missing/unreadable logo (simply skips the image). No DB access.
func BuildPayslipPDF(d PayslipData) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	const pageW = 210.0
	const left = 15.0
	const right = 15.0
	contentW := pageW - left - right // 180

	// ── Header: logo + company (left), SLIP GAJI + period (right) ──────────────
	headerTop := pdf.GetY()
	logoW := 0.0
	if d.LogoPath != "" {
		// Register the image first; on any error (missing/corrupt file) clear the
		// error state and render without a logo so a bad logo never aborts the slip.
		info := pdf.RegisterImageOptions(d.LogoPath, fpdf.ImageOptions{})
		if pdf.Ok() && info != nil {
			pdf.ImageOptions(d.LogoPath, left, headerTop, 22, 0, false, fpdf.ImageOptions{}, 0, "")
			logoW = 26
		}
		if !pdf.Ok() {
			pdf.ClearError()
			logoW = 0
		}
	}

	pdf.SetXY(left+logoW, headerTop)
	pdf.SetFont("Arial", "B", 14)
	company := d.CompanyName
	if strings.TrimSpace(company) == "" {
		company = "Perusahaan"
	}
	pdf.CellFormat(contentW/2-logoW, 7, tr(company), "", 2, "L", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	if strings.TrimSpace(d.Address) != "" {
		pdf.SetX(left + logoW)
		pdf.MultiCell(contentW/2-logoW, 5, tr(d.Address), "", "L", false)
	}

	// Right block.
	pdf.SetXY(left+contentW/2, headerTop)
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(contentW/2, 8, "SLIP GAJI", "", 2, "R", false, 0, "")
	pdf.SetFont("Arial", "", 10)
	pdf.SetX(left + contentW/2)
	pdf.CellFormat(contentW/2, 6, tr("Periode: "+d.PeriodLabel), "", 1, "R", false, 0, "")

	pdf.SetY(headerTop + 24)
	pdf.SetLineWidth(0.3)
	pdf.Line(left, pdf.GetY(), pageW-right, pdf.GetY())
	pdf.Ln(4)

	// ── Employee info (two columns) ────────────────────────────────────────────
	pdf.SetFont("Arial", "", 10)
	colW := contentW / 2
	infoRow := func(lk, lv, rk, rv string) {
		y := pdf.GetY()
		pdf.SetXY(left, y)
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(35, 6, tr(lk), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.CellFormat(colW-35, 6, tr(": "+lv), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(40, 6, tr(rk), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.CellFormat(colW-40, 6, tr(": "+rv), "", 1, "L", false, 0, "")
	}
	infoRow("Karyawan", d.EmployeeName+" ("+d.EmployeeCode+")", "Jabatan", orDash(d.Position))
	infoRow("Cabang", orDash(d.Branch), "Tanggal Bergabung", orDash(d.JoinDate))
	pdf.Ln(3)

	// ── Two-column earnings / deductions ───────────────────────────────────────
	tableTop := pdf.GetY()
	pdf.SetFont("Arial", "B", 11)
	pdf.SetXY(left, tableTop)
	pdf.CellFormat(colW-2, 7, "PENDAPATAN", "B", 0, "L", false, 0, "")
	pdf.SetX(left + colW + 2)
	pdf.CellFormat(colW-2, 7, "POTONGAN", "B", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 9)
	maxRows := len(d.Earnings)
	if len(d.Deductions) > maxRows {
		maxRows = len(d.Deductions)
	}
	rowY := pdf.GetY() + 1
	for i := 0; i < maxRows; i++ {
		pdf.SetXY(left, rowY)
		if i < len(d.Earnings) {
			pdf.CellFormat(colW-32, 6, tr(d.Earnings[i].Label), "", 0, "L", false, 0, "")
			pdf.CellFormat(30, 6, formatRupiah(d.Earnings[i].Amount), "", 0, "R", false, 0, "")
		} else {
			pdf.CellFormat(colW-2, 6, "", "", 0, "L", false, 0, "")
		}
		pdf.SetX(left + colW + 2)
		if i < len(d.Deductions) {
			pdf.CellFormat(colW-34, 6, tr(d.Deductions[i].Label), "", 0, "L", false, 0, "")
			pdf.CellFormat(30, 6, formatRupiah(d.Deductions[i].Amount), "", 1, "R", false, 0, "")
		} else {
			pdf.CellFormat(colW-2, 6, "", "", 1, "L", false, 0, "")
		}
		rowY = pdf.GetY()
	}

	// Totals row.
	pdf.SetY(rowY + 1)
	pdf.SetFont("Arial", "B", 10)
	pdf.SetX(left)
	pdf.CellFormat(colW-32, 7, "Total Pendapatan", "T", 0, "L", false, 0, "")
	pdf.CellFormat(30, 7, formatRupiah(d.TotalEarnings), "T", 0, "R", false, 0, "")
	pdf.SetX(left + colW + 2)
	pdf.CellFormat(colW-34, 7, "Total Potongan", "T", 0, "L", false, 0, "")
	pdf.CellFormat(30, 7, formatRupiah(d.TotalDeduction), "T", 1, "R", false, 0, "")
	pdf.Ln(4)

	// ── Net pay box ────────────────────────────────────────────────────────────
	pdf.SetFont("Arial", "B", 12)
	pdf.SetFillColor(240, 243, 247)
	pdf.CellFormat(contentW, 9, tr("GAJI BERSIH (Take Home Pay): "+formatRupiah(d.NetPay)), "1", 1, "C", true, 0, "")
	pdf.SetFont("Arial", "I", 9)
	terbilang := Terbilang(d.NetPay) + " rupiah"
	pdf.MultiCell(contentW, 5, tr("Terbilang: "+capitalizeFirst(terbilang)), "", "C", false)
	pdf.Ln(4)

	// ── Catatan + footer ───────────────────────────────────────────────────────
	if strings.TrimSpace(d.Note) != "" {
		pdf.SetFont("Arial", "", 9)
		pdf.MultiCell(contentW, 5, tr("Catatan: "+d.Note), "", "L", false)
		pdf.Ln(2)
	}
	if strings.TrimSpace(d.PayslipFooter) != "" {
		pdf.SetFont("Arial", "I", 8)
		pdf.SetTextColor(110, 110, 110)
		pdf.MultiCell(contentW, 4, tr(d.PayslipFooter), "", "C", false)
		pdf.SetTextColor(0, 0, 0)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// tr converts a UTF-8 string to the cp1252 encoding fpdf's core fonts expect.
// Indonesian text is Latin-1 compatible so this is effectively a passthrough but
// keeps accented characters (if any creep in) from breaking the PDF.
func tr(s string) string {
	r := make([]rune, 0, len(s))
	for _, c := range s {
		if c < 256 {
			r = append(r, c)
		} else {
			r = append(r, '?')
		}
	}
	return string(r)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	return string(r)
}
