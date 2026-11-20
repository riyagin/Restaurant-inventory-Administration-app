package service

import (
	"bytes"
	"fmt"
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
	// they are listed in PENDAPATAN marked "*", and the net box shows the
	// grand total (with them) and the transfer (without them).
	DailyPaid []PayslipLineItem

	// Totals.
	TotalEarnings  int64
	TotalDeduction int64
	NetPay         int64

	// Catatan (review note), optional.
	Note string

	// Kasbons repaid on this slip, with what is still owed after it.
	Kasbons []PayslipKasbon

	// Performance is the month's point record (Kebijakan HRD III: everyone
	// starts at 100, violations deduct). Nil = not shown (e.g. the THR slip).
	Performance *PayslipPerformance
}

// PayslipPerformance is the KINERJA block: the score frozen on the payroll line,
// its grade, and the month's violations grouped by offense. Employees are
// entitled to an explanation of their monthly points (Ketentuan Tambahan 5).
type PayslipPerformance struct {
	Score int // 0–100; -1 = not evaluated
	Grade string
	// Rows: one per violation and per day that cost pay, in date order
	// (BuildPayslipOffenses).
	Rows []PayslipOffense
	// WindowNote explains when the score month and the pay window differ
	// (cut-off 26–25), e.g. "Poin: 1–31 Okt · potongan gaji: 26 Sep – 25 Okt".
	WindowNote string
	// GradeLinked: the slip carries components paid by grade, so the block
	// says which grade they were paid at.
	GradeLinked bool
}

// PayslipKasbon is one cash advance repaid (partly) on the slip.
type PayslipKasbon struct {
	Number    string
	Amount    int64 // the advance as disbursed
	Deducted  int64 // repaid on this slip
	Remaining int64 // still owed after this slip
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

	// Daily payouts (uang makan) are earnings too: they sit in PENDAPATAN,
	// marked "*", and count in Total Pendapatan. Only the transfer leaves them
	// out — the net box below shows both figures.
	earnRows := append([]PayslipLineItem(nil), d.Earnings...)
	var dailyTotal int64
	for _, it := range d.DailyPaid {
		dailyTotal += it.Amount
		earnRows = append(earnRows, PayslipLineItem{Label: it.Label + " *", Amount: it.Amount})
	}

	pdf.SetFont("Arial", "", 7)
	maxRows := len(earnRows)
	if len(d.Deductions) > maxRows {
		maxRows = len(d.Deductions)
	}
	rowY := pdf.GetY() + 0.8
	for i := 0; i < maxRows; i++ {
		pdf.SetXY(left, rowY)
		if i < len(earnRows) {
			pdf.CellFormat(colW-2-amtW, 4.2, tr(fitText(pdf, earnRows[i].Label, colW-3-amtW)), "", 0, "L", false, 0, "")
			pdf.CellFormat(amtW, 4.2, formatRupiah(earnRows[i].Amount), "", 0, "R", false, 0, "")
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
	pdf.CellFormat(amtW, 5.5, formatRupiah(d.TotalEarnings+dailyTotal), "T", 0, "R", false, 0, "")
	pdf.SetX(left + colW + 2)
	pdf.CellFormat(colW-2-amtW, 5.5, "Total Potongan", "T", 0, "L", false, 0, "")
	pdf.CellFormat(amtW, 5.5, formatRupiah(d.TotalDeduction), "T", 1, "R", false, 0, "")
	pdf.Ln(3)

	// ── Net pay box ────────────────────────────────────────────────────────────
	// Without daily payouts the box is the transfer alone. With them it shows
	// the grand total (everything the month paid), what was already handed out
	// daily, and the transfer — the amount the bank sends.
	pdf.SetFillColor(240, 243, 247)
	if dailyTotal > 0 {
		grand := d.NetPay + dailyTotal
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 8.5)
		pdf.CellFormat(contentW-amtW-6, 6, tr("TOTAL GAJI (termasuk dibayar harian)"), "LT", 0, "L", true, 0, "")
		pdf.CellFormat(amtW+6, 6, formatRupiah(grand), "TR", 1, "R", true, 0, "")
		pdf.SetX(left)
		pdf.SetFont("Arial", "", 7.5)
		pdf.CellFormat(contentW-amtW-6, 4.5, tr("Sudah dibayar tunai harian *"), "L", 0, "L", true, 0, "")
		pdf.CellFormat(amtW+6, 4.5, "- "+formatRupiah(dailyTotal), "R", 1, "R", true, 0, "")
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 9.5)
		pdf.CellFormat(contentW-amtW-6, 7, tr("GAJI BERSIH DITRANSFER"), "LTB", 0, "L", true, 0, "")
		pdf.CellFormat(amtW+6, 7, formatRupiah(d.NetPay), "TRB", 1, "R", true, 0, "")
	} else {
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 9.5)
		pdf.CellFormat(contentW, 7.5, tr("GAJI BERSIH: "+formatRupiah(d.NetPay)), "1", 1, "C", true, 0, "")
	}
	pdf.SetX(left)
	pdf.SetFont("Arial", "I", 7)
	terbilang := Terbilang(d.NetPay) + " rupiah"
	label := "Terbilang: "
	if dailyTotal > 0 {
		label = "Terbilang (ditransfer): "
	}
	pdf.MultiCell(contentW, 3.6, tr(label+capitalizeFirst(terbilang)), "", "C", false)
	if dailyTotal > 0 {
		pdf.SetX(left)
		pdf.SetFont("Arial", "I", 6.5)
		pdf.SetTextColor(110, 110, 110)
		pdf.MultiCell(contentW, 3.2, tr("* Dibayar tunai setiap hari kerja selama periode, tidak ikut ditransfer."), "", "C", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(2.5)

	// ── Kasbon (what is repaid and what is still owed) ────────────────────────
	if len(d.Kasbons) > 0 {
		var remaining int64
		for _, k := range d.Kasbons {
			remaining += k.Remaining
		}
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 7)
		pdf.CellFormat(contentW*0.5, 4.5, tr("KASBON"), "B", 0, "L", false, 0, "")
		pdf.CellFormat(contentW*0.5, 4.5, tr("Sisa setelah gaji ini: "+formatRupiah(remaining)), "B", 1, "R", false, 0, "")
		pdf.SetFont("Arial", "", 7)
		for _, k := range d.Kasbons {
			pdf.SetX(left)
			pdf.CellFormat(contentW-amtW-14, 3.9, tr(fmt.Sprintf("%s · pinjaman %s · dipotong bulan ini %s",
				k.Number, formatRupiah(k.Amount), formatRupiah(k.Deducted))), "", 0, "L", false, 0, "")
			pdf.CellFormat(amtW+14, 3.9, tr("sisa "+formatRupiah(k.Remaining)), "", 1, "R", false, 0, "")
		}
		pdf.Ln(2)
	}

	// ── Kinerja: every offense by date, with points and pay ────────────────────
	if p := d.Performance; p != nil {
		totalPts := 0
		var totalPay int64
		for _, r := range p.Rows {
			totalPts += r.Points
			totalPay += r.Amount
		}
		pdf.SetX(left)
		pdf.SetFont("Arial", "B", 7)
		pdf.CellFormat(contentW*0.5, 4.5, tr("KINERJA & PELANGGARAN"), "B", 0, "L", false, 0, "")
		head := "Poin: belum dievaluasi"
		if p.Score >= 0 {
			head = fmt.Sprintf("Poin: %d / 100", p.Score)
			if strings.TrimSpace(p.Grade) != "" {
				head += "  ·  Grade " + p.Grade
			}
		}
		pdf.CellFormat(contentW*0.5, 4.5, tr(head), "B", 1, "R", false, 0, "")

		const dateW, ptsW, payW, rowH = 15.0, 13.0, 22.0, 3.5
		labelW := contentW - dateW - ptsW - payW
		if len(p.Rows) == 0 {
			pdf.SetX(left)
			pdf.SetFont("Arial", "I", 7)
			pdf.CellFormat(contentW, 4, tr("Tidak ada pelanggaran — poin penuh."), "", 1, "L", false, 0, "")
		} else {
			pdf.SetX(left)
			pdf.SetFont("Arial", "B", 6.3)
			pdf.SetTextColor(90, 90, 90)
			pdf.CellFormat(dateW, rowH, "Tanggal", "", 0, "L", false, 0, "")
			pdf.CellFormat(labelW, rowH, tr("Pelanggaran & waktu"), "", 0, "L", false, 0, "")
			pdf.CellFormat(ptsW, rowH, "Poin", "", 0, "R", false, 0, "")
			pdf.CellFormat(payW, rowH, "Potongan gaji", "", 1, "R", false, 0, "")
			pdf.SetTextColor(0, 0, 0)

			// Keep the slip on its A5 half: room is what is left above the footer
			// band after the totals row, the notes and a two-line Catatan.
			room := (sheetH - panelBotY - 6) - pdf.GetY() - 4 - 7 - 8
			maxRows := int(room / rowH)
			if maxRows < 1 {
				maxRows = 1
			}
			shown := p.Rows
			var hidden []PayslipOffense
			if len(shown) > maxRows {
				shown, hidden = p.Rows[:maxRows-1], p.Rows[maxRows-1:]
			}
			pdf.SetFont("Arial", "", 6.5)
			for _, r := range shown {
				pdf.SetX(left)
				pdf.CellFormat(dateW, rowH, tr(offenseDateLabel(r.Date)), "", 0, "L", false, 0, "")
				pdf.CellFormat(labelW, rowH, tr(fitText(pdf, r.Label, labelW-1)), "", 0, "L", false, 0, "")
				pts, pay := "-", "-"
				if r.Points > 0 {
					pts = fmt.Sprintf("-%d", r.Points)
				}
				if r.Amount > 0 {
					pay = formatRupiah(r.Amount)
				}
				pdf.CellFormat(ptsW, rowH, pts, "", 0, "R", false, 0, "")
				pdf.CellFormat(payW, rowH, tr(pay), "", 1, "R", false, 0, "")
			}
			if len(hidden) > 0 {
				hp, hpay := 0, int64(0)
				for _, r := range hidden {
					hp += r.Points
					hpay += r.Amount
				}
				pdf.SetX(left)
				pdf.SetFont("Arial", "I", 6.5)
				pdf.CellFormat(dateW, rowH, "", "", 0, "L", false, 0, "")
				pdf.CellFormat(labelW, rowH, tr(fmt.Sprintf("… %d baris lainnya (lihat Evaluasi Karyawan)", len(hidden))), "", 0, "L", false, 0, "")
				pdf.CellFormat(ptsW, rowH, fmt.Sprintf("-%d", hp), "", 0, "R", false, 0, "")
				pdf.CellFormat(payW, rowH, tr(formatRupiah(hpay)), "", 1, "R", false, 0, "")
			}
			pdf.SetX(left)
			pdf.SetFont("Arial", "B", 6.8)
			pdf.CellFormat(dateW+labelW, 4, tr("Total"), "T", 0, "L", false, 0, "")
			pdf.CellFormat(ptsW, 4, fmt.Sprintf("-%d", totalPts), "T", 0, "R", false, 0, "")
			pdf.CellFormat(payW, 4, tr(formatRupiah(totalPay)), "T", 1, "R", false, 0, "")
		}

		var notes []string
		if totalPts > 100 {
			notes = append(notes, "Poin tidak dapat kurang dari 0.")
		}
		if p.WindowNote != "" {
			notes = append(notes, p.WindowNote+".")
		}
		if p.GradeLinked && strings.TrimSpace(p.Grade) != "" {
			notes = append(notes, "Komponen yang mengikuti grade dibayar sesuai Grade "+p.Grade+".")
		}
		if len(notes) > 0 {
			pdf.SetX(left)
			pdf.SetFont("Arial", "I", 6.3)
			pdf.SetTextColor(110, 110, 110)
			pdf.MultiCell(contentW, 3.1, tr(strings.Join(notes, " ")), "", "L", false)
			pdf.SetTextColor(0, 0, 0)
		}
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

// cp1252 encodes UTF-8 text into the cp1252 bytes fpdf's core fonts expect.
var cp1252 = fpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")

// tr converts a UTF-8 string to the cp1252 encoding fpdf's core fonts expect,
// so Indonesian text and the usual typography (é, ·, ×, —, “ ”) print as
// themselves. It used to rebuild the string from runes, which re-encoded every
// character from U+0080 up as UTF-8 and printed two wrong glyphs (· → "Â·").
func tr(s string) string { return cp1252(s) }

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

// fitText shortens s with an ellipsis until it fits w mm in the current font.
func fitText(pdf *fpdf.Fpdf, s string, w float64) string {
	if pdf.GetStringWidth(tr(s)) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if pdf.GetStringWidth(tr(string(r)+"…")) <= w {
			return string(r) + "…"
		}
	}
	return s
}
