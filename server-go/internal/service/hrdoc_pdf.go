package service

import (
	"bytes"
	"strings"

	"github.com/go-pdf/fpdf"
)

// pdfPunct maps the few typographic characters used in the templates to
// Latin-1/ASCII equivalents, because fpdf's core fonts are cp1252 and the
// shared tr() helper turns anything >= U+0100 into '?'. DOCX keeps the original
// (nicer) glyphs; only the PDF path is normalized.
var pdfPunct = strings.NewReplacer(
	"—", "-", // em dash
	"–", "-", // en dash
	"•", "-", // bullet
	"…", "...", // ellipsis
	"“", "\"", "”", "\"", "‘", "'", "’", "'",
)

// pdfText normalizes then Latin-1-encodes a string for fpdf output.
func pdfText(s string) string { return tr(pdfPunct.Replace(s)) }

// RenderHRDocPDF renders an HRDocument to a formal A4 PDF using the same fpdf
// library as the payslip (pure Go, VPS-friendly). Times is used throughout for a
// conventional legal-document look.
func RenderHRDocPDF(doc *HRDocument) ([]byte, error) {
	const (
		marginL   = 22.0
		marginR   = 18.0
		marginTop = 18.0
		pageW     = 210.0
		contentW  = pageW - marginL - marginR
		lh        = 5.2 // line height for body text
	)

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginL, marginTop, marginR)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AddPage()

	// Paragraph helper that keeps wrapped lines aligned to a given x by moving
	// the left margin for the duration of the MultiCell.
	writePara := func(text string, size float64, style, align string, x, w float64) {
		pdf.SetFont("Times", style, size)
		pdf.SetLeftMargin(x)
		pdf.SetX(x)
		pdf.MultiCell(w, lh, pdfText(text), "", align, false)
		pdf.SetLeftMargin(marginL)
	}

	for _, b := range doc.Blocks {
		switch b.Kind {
		case kindLetterhead:
			pdf.SetFont("Times", "B", 15)
			pdf.SetX(marginL)
			pdf.MultiCell(contentW, 7, pdfText(b.Text), "", "C", false)
			pdf.SetFont("Times", "", 9.5)
			for _, ln := range b.Lines {
				pdf.SetX(marginL)
				pdf.MultiCell(contentW, 4.6, pdfText(ln), "", "C", false)
			}
			pdf.Ln(1.5)
			y := pdf.GetY()
			pdf.SetLineWidth(0.6)
			pdf.Line(marginL, y, pageW-marginR, y)
			pdf.SetLineWidth(0.2)
			pdf.Line(marginL, y+0.9, pageW-marginR, y+0.9)
			pdf.Ln(2)

		case kindTitle:
			pdf.Ln(1)
			pdf.SetFont("Times", "BU", 13)
			pdf.SetX(marginL)
			pdf.MultiCell(contentW, 6, pdfText(b.Text), "", "C", false)

		case kindSubtitle:
			pdf.SetFont("Times", "", 10.5)
			pdf.SetX(marginL)
			pdf.MultiCell(contentW, 4.8, pdfText(b.Text), "", "C", false)

		case kindHeading:
			pdf.Ln(2)
			pdf.SetFont("Times", "B", 11)
			pdf.SetX(marginL)
			pdf.MultiCell(contentW, 5, pdfText(b.Text), "", "L", false)
			pdf.Ln(0.5)

		case kindParagraph:
			pdf.Ln(1)
			writePara(b.Text, 11, "", "J", marginL, contentW)

		case kindClause:
			pdf.Ln(0.5)
			markerW := 8.0
			pdf.SetFont("Times", "", 11)
			startY := pdf.GetY()
			pdf.SetXY(marginL, startY)
			pdf.CellFormat(markerW, lh, pdfText(b.Marker), "", 0, "L", false, 0, "")
			writePara(b.Text, 11, "", "J", marginL+markerW, contentW-markerW)

		case kindIdentity:
			pdf.Ln(0.5)
			labelW := 46.0
			for _, row := range b.Items {
				pdf.SetFont("Times", "", 11)
				y := pdf.GetY()
				pdf.SetXY(marginL, y)
				pdf.CellFormat(labelW, lh, pdfText(row.Label), "", 0, "L", false, 0, "")
				pdf.CellFormat(4, lh, ":", "", 0, "L", false, 0, "")
				writePara(row.Value, 11, "", "L", marginL+labelW+4, contentW-labelW-4)
			}

		case kindSpacer:
			h := b.Spacing
			if h <= 0 {
				h = 3
			}
			pdf.Ln(h)

		case kindSignature:
			renderSignaturePDF(pdf, b.Cols, marginL, contentW, lh)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderSignaturePDF(pdf *fpdf.Fpdf, cols []signCol, marginL, contentW, lh float64) {
	if len(cols) == 0 {
		return
	}
	pdf.Ln(2)
	colW := contentW / float64(len(cols))
	startY := pdf.GetY()
	maxY := startY

	for i, c := range cols {
		x := marginL + float64(i)*colW
		pdf.SetXY(x, startY)
		pdf.SetFont("Times", "", 11)

		for _, ln := range c.TopLines {
			pdf.SetX(x)
			pdf.MultiCell(colW-4, lh, pdfText(ln), "", "C", false)
		}
		if c.Role != "" {
			pdf.SetFont("Times", "B", 11)
			pdf.SetX(x)
			pdf.MultiCell(colW-4, lh, pdfText(c.Role), "", "C", false)
			pdf.SetFont("Times", "", 11)
		}
		pdf.Ln(18) // signature gap
		if strings.TrimSpace(c.Name) != "" {
			pdf.SetFont("Times", "BU", 11)
			pdf.SetX(x)
			pdf.MultiCell(colW-4, lh, pdfText(c.Name), "", "C", false)
			pdf.SetFont("Times", "", 11)
		}
		for _, ln := range c.SubLines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			pdf.SetX(x)
			pdf.MultiCell(colW-4, lh, pdfText(ln), "", "C", false)
		}
		if pdf.GetY() > maxY {
			maxY = pdf.GetY()
		}
	}
	pdf.SetXY(marginL, maxY)
}
