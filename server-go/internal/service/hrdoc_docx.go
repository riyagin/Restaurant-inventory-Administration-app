package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// RenderHRDocDOCX renders an HRDocument to a Word .docx file by writing the
// OOXML package (a ZIP of XML parts) directly. This keeps the backend pure Go
// with no extra dependency — consistent with the fpdf choice for PDFs — and
// produces an editable master HR can open in Word/LibreOffice.
func RenderHRDocDOCX(doc *HRDocument) ([]byte, error) {
	var body strings.Builder
	for _, b := range doc.Blocks {
		body.WriteString(renderBlockDOCX(b))
	}

	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>` + body.String() + sectPrXML + `</w:body></w:document>`

	files := map[string]string{
		"[Content_Types].xml":          contentTypesXML,
		"_rels/.rels":                  relsXML,
		"word/_rels/document.xml.rels": docRelsXML,
		"word/styles.xml":              stylesXML,
		"word/document.xml":            documentXML,
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Deterministic order helps reproducibility; [Content_Types].xml first is
	// conventional though not required.
	order := []string{"[Content_Types].xml", "_rels/.rels", "word/_rels/document.xml.rels", "word/styles.xml", "word/document.xml"}
	for _, name := range order {
		f, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderBlockDOCX(b Block) string {
	switch b.Kind {
	case kindLetterhead:
		var sb strings.Builder
		sb.WriteString(paraXML(b.Text, paraOpts{align: "center", bold: true, size: 30, after: 40}))
		for _, ln := range b.Lines {
			sb.WriteString(paraXML(ln, paraOpts{align: "center", size: 19, after: 20}))
		}
		// Horizontal rule via bottom border on an empty paragraph.
		sb.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="12" w:space="1" w:color="000000"/></w:pBdr><w:spacing w:after="120"/></w:pPr></w:p>`)
		return sb.String()
	case kindTitle:
		return paraXML(b.Text, paraOpts{align: "center", bold: true, underline: true, size: 26, before: 60, after: 40})
	case kindSubtitle:
		return paraXML(b.Text, paraOpts{align: "center", size: 21, after: 20})
	case kindHeading:
		return paraXML(b.Text, paraOpts{align: "left", bold: true, size: 22, before: 120, after: 40})
	case kindParagraph:
		return paraXML(b.Text, paraOpts{align: "both", size: 22, before: 40, after: 40})
	case kindClause:
		return clauseXML(b.Marker, b.Text)
	case kindIdentity:
		return identityTableXML(b.Items)
	case kindSpacer:
		tw := int(b.Spacing * 56.0)
		if tw <= 0 {
			tw = 160
		}
		return fmt.Sprintf(`<w:p><w:pPr><w:spacing w:before="%d" w:after="0" w:line="120" w:lineRule="exact"/></w:pPr></w:p>`, tw)
	case kindSignature:
		return signatureTableXML(b.Cols)
	}
	return ""
}

type paraOpts struct {
	align     string // "left","center","right","both"
	bold      bool
	underline bool
	size      int // half-points
	before    int // twips
	after     int // twips
}

func paraXML(text string, o paraOpts) string {
	if o.size == 0 {
		o.size = 22
	}
	if o.align == "" {
		o.align = "left"
	}
	var pPr strings.Builder
	pPr.WriteString("<w:pPr>")
	fmt.Fprintf(&pPr, `<w:spacing w:before="%d" w:after="%d"/>`, o.before, o.after)
	fmt.Fprintf(&pPr, `<w:jc w:val="%s"/>`, o.align)
	pPr.WriteString("</w:pPr>")
	return "<w:p>" + pPr.String() + runXML(text, runOpts{bold: o.bold, underline: o.underline, size: o.size}) + "</w:p>"
}

type runOpts struct {
	bold      bool
	underline bool
	size      int
}

func runXML(text string, o runOpts) string {
	if o.size == 0 {
		o.size = 22
	}
	var rPr strings.Builder
	rPr.WriteString("<w:rPr>")
	if o.bold {
		rPr.WriteString("<w:b/>")
	}
	if o.underline {
		rPr.WriteString(`<w:u w:val="single"/>`)
	}
	fmt.Fprintf(&rPr, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, o.size, o.size)
	rPr.WriteString("</w:rPr>")
	return "<w:r>" + rPr.String() + `<w:t xml:space="preserve">` + escapeXML(text) + "</w:t></w:r>"
}

// clauseXML renders a numbered/lettered clause with a hanging indent so wrapped
// lines align under the text, not under the marker.
func clauseXML(marker, text string) string {
	pPr := `<w:pPr><w:spacing w:before="40" w:after="40"/><w:ind w:left="454" w:hanging="454"/><w:jc w:val="both"/></w:pPr>`
	r := runXML(marker, runOpts{}) + `<w:r><w:tab/></w:r>` + runXML(text, runOpts{})
	return "<w:p>" + pPr + r + "</w:p>"
}

// noBorders is the shared table/cell border block that renders an invisible grid.
const noBorders = `<w:tblBorders>` +
	`<w:top w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`<w:left w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`<w:bottom w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`<w:right w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`<w:insideH w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`<w:insideV w:val="none" w:sz="0" w:space="0" w:color="auto"/>` +
	`</w:tblBorders>`

func identityTableXML(rows []kv) string {
	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/>` + noBorders + `<w:tblCellMar><w:top w:w="10" w:type="dxa"/><w:bottom w:w="10" w:type="dxa"/><w:left w:w="0" w:type="dxa"/><w:right w:w="80" w:type="dxa"/></w:tblCellMar></w:tblPr>`)
	sb.WriteString(`<w:tblGrid><w:gridCol w:w="2700"/><w:gridCol w:w="250"/><w:gridCol w:w="6200"/></w:tblGrid>`)
	for _, row := range rows {
		sb.WriteString("<w:tr>")
		sb.WriteString(cellXML(2700, paraXML(row.Label, paraOpts{after: 0, size: 22})))
		sb.WriteString(cellXML(250, paraXML(":", paraOpts{after: 0, size: 22})))
		sb.WriteString(cellXML(6200, paraXML(row.Value, paraOpts{after: 0, size: 22})))
		sb.WriteString("</w:tr>")
	}
	sb.WriteString("</w:tbl>")
	// A trailing empty paragraph so following content isn't glued to the table.
	sb.WriteString(`<w:p><w:pPr><w:spacing w:after="0" w:line="120" w:lineRule="exact"/></w:pPr></w:p>`)
	return sb.String()
}

func signatureTableXML(cols []signCol) string {
	if len(cols) == 0 {
		return ""
	}
	total := 9000
	colW := total / len(cols)
	var grid strings.Builder
	for range cols {
		fmt.Fprintf(&grid, `<w:gridCol w:w="%d"/>`, colW)
	}
	var cells strings.Builder
	for _, c := range cols {
		var inner strings.Builder
		for _, ln := range c.TopLines {
			inner.WriteString(paraXML(ln, paraOpts{align: "center", after: 0, size: 22}))
		}
		if c.Role != "" {
			inner.WriteString(paraXML(c.Role, paraOpts{align: "center", bold: true, after: 0, size: 22}))
		}
		// Signature whitespace (~3 blank lines).
		inner.WriteString(`<w:p><w:pPr><w:spacing w:before="720" w:after="0"/></w:pPr></w:p>`)
		if strings.TrimSpace(c.Name) != "" {
			inner.WriteString(paraXML(c.Name, paraOpts{align: "center", bold: true, underline: true, after: 0, size: 22}))
		}
		for _, ln := range c.SubLines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			inner.WriteString(paraXML(ln, paraOpts{align: "center", after: 0, size: 22}))
		}
		cells.WriteString(cellXML(colW, inner.String()))
	}
	return `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/>` + noBorders + `</w:tblPr><w:tblGrid>` +
		grid.String() + `</w:tblGrid><w:tr>` + cells.String() + `</w:tr></w:tbl>`
}

func cellXML(w int, inner string) string {
	if inner == "" {
		inner = "<w:p/>"
	}
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:vAlign w:val="top"/></w:tcPr>%s</w:tc>`, w, inner)
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// ---- static OOXML parts ---------------------------------------------------

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:cs="Times New Roman"/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
</w:styles>`

// A4 page with ~2cm margins.
const sectPrXML = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1021" w:bottom="1134" w:left="1247" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`
