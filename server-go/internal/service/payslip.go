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

	// Title is the slip heading (right block). Defaults to "SLIP GAJI" when empty;
	// the THR payslip passes "SLIP THR".
	Title string

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

	// DailyPaid lists wage components disbursed manually day by day during the period
	// (type 'daily_allowance', e.g. uang makan handed out in cash). They are shown for
	// the employee's information only and are NOT part of TotalEarnings / NetPay, so
	// they render in their own block below the net-pay box.
	DailyPaid []PayslipLineItem

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

// Sheet geometry. Payslips are printed two-up on A4 *landscape* (297 x 210) and
// split down the middle, which yields two A5 *portrait* halves (148.5 x 210) —
// each one a self-contained slip. Cutting an A4 portrait sheet horizontally would
// instead give landscape A5, which is why the sheet is landscape here.
const (
	sheetW    = 297.0
	sheetH    = 210.0
	panelW    = sheetW / 2 // 148.5 — one A5 portrait half
	panelPadX = 9.0
	panelPadY = 10.0
	panelBotY = 8.0
)

// BuildPayslipPDF renders a single payslip: one A4-landscape sheet with the slip
// on the left half and the right half left blank, so it prints and cuts exactly
// like a page from the bulk sheet. Robust to a missing/unreadable logo.
func BuildPayslipPDF(d PayslipData) ([]byte, error) {
	return BuildPayslipSheetPDF([]PayslipData{d})
}

// BuildPayslipSheetPDF renders many payslips two-up: each A4-landscape page holds
// two A5-portrait slips side by side, separated by a dashed cut guide. Cutting
// along the guide produces two portrait A5 documents.
func BuildPayslipSheetPDF(items []PayslipData) ([]byte, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(panelPadX, panelPadY, panelPadX)
	// Every panel is positioned absolutely and sized to fit an A5 half, so the
	// automatic page break must stay off — otherwise a long slip would spill onto
	// a new page and desynchronise the two-up pairing.
	pdf.SetAutoPageBreak(false, 0)

	if len(items) == 0 {
		pdf.AddPage()
	}
	for i, d := range items {
		if i%2 == 0 {
			pdf.AddPage()
			drawCutGuide(pdf)
		}
		originX := 0.0
		if i%2 == 1 {
			originX = panelW
		}
		drawPayslipPanel(pdf, d, originX)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawCutGuide draws the dashed vertical line the sheet is cut along.
func drawCutGuide(pdf *fpdf.Fpdf) {
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(150, 150, 150)
	pdf.SetDashPattern([]float64{2, 2}, 0)
	pdf.Line(panelW, 4, panelW, sheetH-4)
	pdf.SetDashPattern([]float64{}, 0)
	pdf.SetDrawColor(0, 0, 0)
}

// drawPayslipPanel renders one complete slip inside the A5 half starting at
// originX. All coordinates are absolute so the two panels never interfere. It
// returns the Y the body ended at (footer excluded, since that is bottom-pinned)
// so tests can assert a dense slip still fits inside the half.
func drawPayslipPanel(pdf *fpdf.Fpdf, d PayslipData, originX float64) float64 {
	left := originX + panelPadX
	contentW := panelW - 2*panelPadX // 130.5
	colW := contentW / 2

	// ── Header: logo + company (left), SLIP GAJI + period (right) ──────────────
	headerTop := panelPadY
	logoW := 0.0
	if d.LogoPath != "" {
		// Register the image first; on any error (missing/corrupt file) clear the
		// error state and render without a logo so a bad logo never aborts the slip.
		info := pdf.RegisterImageOptions(d.LogoPath, fpdf.ImageOptions{})
		if pdf.Ok() && info != nil {
			pdf.ImageOptions(d.LogoPath, left, headerTop, 15, 0, false, fpdf.ImageOptions{}, 0, "")
			logoW = 18
		}
		if !pdf.Ok() {
			pdf.ClearError()
			logoW = 0
		}
	}

	pdf.SetXY(left+logoW, headerTop)
	pdf.SetFont("Arial", "B", 11)
	company := d.CompanyName
	if strings.TrimSpace(company) == "" {
		company = "Perusahaan"
	}
	pdf.CellFormat(contentW*0.58-logoW, 5.5, tr(company), "", 2, "L", false, 0, "")
	pdf.SetFont("Arial", "", 7)
	if strings.TrimSpace(d.Address) != "" {
		pdf.SetX(left + logoW)
		pdf.MultiCell(contentW*0.58-logoW, 3.4, tr(d.Address), "", "L", false)
	}

	// Right block.
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = "SLIP GAJI"
	}
	pdf.SetXY(left+contentW*0.58, headerTop)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(contentW*0.42, 6, tr(title), "", 2, "R", false, 0, "")
	pdf.SetFont("Arial", "", 8)
	pdf.SetX(left + contentW*0.58)
	pdf.CellFormat(contentW*0.42, 4.5, tr("Periode: "+d.PeriodLabel), "", 1, "R", false, 0, "")

	pdf.SetY(headerTop + 17)
	pdf.SetLineWidth(0.3)
	pdf.Line(left, pdf.GetY(), left+contentW, pdf.GetY())
	pdf.Ln(2.5)

	// ── Employee info (two columns) ────────────────────────────────────────────
	infoRow := func(lk, lv, rk, rv string) {
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 7.5)
		pdf.CellFormat(22, 4.5, tr(lk), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 7.5)
		pdf.CellFormat(colW-22, 4.5, tr(": "+lv), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "B", 7.5)
		pdf.CellFormat(24, 4.5, tr(rk), "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 7.5)
		pdf.CellFormat(colW-24, 4.5, tr(": "+rv), "", 1, "L", false, 0, "")
	}
	infoRow("Karyawan", d.EmployeeName+" ("+d.EmployeeCode+")", "Jabatan", orDash(d.Position))
	infoRow("Cabang", orDash(d.Branch), "Bergabung", orDash(d.JoinDate))
	pdf.Ln(2)

	// ── Two-column earnings / deductions ───────────────────────────────────────
	const amtW = 26.0
	pdf.SetX(left)
	pdf.SetFont("Arial", "B", 8.5)
	pdf.CellFormat(colW-2, 5.5, "PENDAPATAN", "B", 0, "L", false, 0, "")
	pdf.SetX(left + colW + 2)
	pdf.CellFormat(colW-2, 5.5, "POTONGAN", "B", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 7)
	maxRows := len(d.Earnings)
	if len(d.Deductions) > maxRows {
		maxRows = len(d.Deductions)
	}
	rowY := pdf.GetY() + 0.8
	for i := 0; i < maxRows; i++ {
		pdf.SetXY(left, rowY)
		if i < len(d.Earnings) {
			pdf.CellFormat(colW-2-amtW, 4.2, tr(d.Earnings[i].Label), "", 0, "L", false, 0, "")
			pdf.CellFormat(amtW, 4.2, formatRupiah(d.Earnings[i].Amount), "", 0, "R", false, 0, "")
		} else {
			pdf.CellFormat(colW-2, 4.2, "", "", 0, "L", false, 0, "")
		}
		pdf.SetX(left + colW + 2)
		if i < len(d.Deductions) {
			pdf.CellFormat(colW-2-amtW, 4.2, tr(d.Deductions[i].Label), "", 0, "L", false, 0, "")
			pdf.CellFormat(amtW, 4.2, formatRupiah(d.Deductions[i].Amount), "", 1, "R", false, 0, "")
		} else {
			pdf.CellFormat(colW-2, 4.2, "", "", 1, "L", false, 0, "")
		}
		rowY = pdf.GetY()
	}

	// Totals row.
	pdf.SetY(rowY + 0.8)
	pdf.SetFont("Arial", "B", 7.5)
	pdf.SetX(left)
	pdf.CellFormat(colW-2-amtW, 5.5, "Total Pendapatan", "T", 0, "L", false, 0, "")
	pdf.CellFormat(amtW, 5.5, formatRupiah(d.TotalEarnings), "T", 0, "R", false, 0, "")
	pdf.SetX(left + colW + 2)
	pdf.CellFormat(colW-2-amtW, 5.5, "Total Potongan", "T", 0, "L", false, 0, "")
	pdf.CellFormat(amtW, 5.5, formatRupiah(d.TotalDeduction), "T", 1, "R", false, 0, "")
	pdf.Ln(3)

	// ── Net pay box ────────────────────────────────────────────────────────────
	pdf.SetX(left)
	pdf.SetFont("Arial", "B", 9.5)
	pdf.SetFillColor(240, 243, 247)
	pdf.CellFormat(contentW, 7.5, tr("GAJI BERSIH: "+formatRupiah(d.NetPay)), "1", 1, "C", true, 0, "")
	pdf.SetX(left)
	pdf.SetFont("Arial", "I", 7)
	terbilang := Terbilang(d.NetPay) + " rupiah"
	pdf.MultiCell(contentW, 3.6, tr("Terbilang: "+capitalizeFirst(terbilang)), "", "C", false)
	pdf.Ln(2.5)

	// ── Dibayar harian (informational, outside the take-home total) ────────────
	if len(d.DailyPaid) > 0 {
		var dailyTotal int64
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 7)
		pdf.CellFormat(contentW, 4.5, tr("DIBAYAR HARIAN (di luar transfer gaji)"), "B", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 7)
		for _, it := range d.DailyPaid {
			dailyTotal += it.Amount
			pdf.SetX(left)
			pdf.CellFormat(contentW-amtW, 4, tr(it.Label), "", 0, "L", false, 0, "")
			pdf.CellFormat(amtW, 4, formatRupiah(it.Amount), "", 1, "R", false, 0, "")
		}
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 7)
		pdf.CellFormat(contentW-amtW, 4, tr("Total Dibayar Harian"), "T", 0, "L", false, 0, "")
		pdf.CellFormat(amtW, 4, formatRupiah(dailyTotal), "T", 1, "R", false, 0, "")
		pdf.SetX(left)
		pdf.SetFont("Arial", "I", 6.5)
		pdf.SetTextColor(110, 110, 110)
		pdf.MultiCell(contentW, 3.2, tr("Sudah diterima tunai secara harian, tidak termasuk dalam gaji bersih di atas."), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(2)
	}

	// ── Catatan ────────────────────────────────────────────────────────────────
	if strings.TrimSpace(d.Note) != "" {
		pdf.SetX(left)
		pdf.SetFont("Arial", "", 7)
		pdf.MultiCell(contentW, 3.6, tr("Catatan: "+d.Note), "", "L", false)
	}
	bodyEnd := pdf.GetY()

	// ── Footer, pinned to the bottom of the A5 half ────────────────────────────
	if strings.TrimSpace(d.PayslipFooter) != "" {
		pdf.SetXY(left, sheetH-panelBotY-6)
		pdf.SetFont("Arial", "I", 6.5)
		pdf.SetTextColor(110, 110, 110)
		pdf.MultiCell(contentW, 3, tr(d.PayslipFooter), "", "C", false)
		pdf.SetTextColor(0, 0, 0)
	}
	return bodyEnd
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
