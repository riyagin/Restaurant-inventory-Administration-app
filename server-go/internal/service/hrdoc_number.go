package service

import (
	"fmt"
	"strings"
	"time"
)

// Document numbering for generated HR letters.
//
// HR configures a format template once (HR settings) and the server hands out
// the numbers, so nobody retypes "001/HRD/VII/2026" and no two letters share a
// number. The template is plain text with {placeholders}; anything not
// recognised is left alone, which keeps room for house style like a branch code.

// DefaultDocNumberFormat is used when the stored format is blank.
const DefaultDocNumberFormat = "{nomor}/HRD/{bulan_romawi}/{tahun}"

// romanMonths indexes 1–12; index 0 is unused so the month number can be used
// directly.
var romanMonths = [...]string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}

// docTypeCodes are the short codes substituted for {jenis}.
var docTypeCodes = map[string]string{
	DocTypePKWT:     "PKWT",
	DocTypePKWTT:    "PKWTT",
	DocTypeSP:       "SP",
	DocTypePaklarng: "PKL",
}

// FormatDocNumber renders a document number from the configured template.
//
// Placeholders:
//
//	{nomor}         running counter, zero-padded to 3 digits (001)
//	{nomor_polos}   running counter, unpadded (1)
//	{jenis}         document type code (PKWT, PKWTT, SP, PKL)
//	{bulan_romawi}  month in Roman numerals (VII)
//	{bulan}         month, zero-padded (07)
//	{tahun}         four-digit year (2026)
//	{tahun_pendek}  two-digit year (26)
func FormatDocNumber(format string, counter int32, docType string, date time.Time) string {
	format = strings.TrimSpace(format)
	if format == "" {
		format = DefaultDocNumberFormat
	}
	if date.IsZero() {
		date = time.Now()
	}
	month := int(date.Month())
	code := docTypeCodes[strings.TrimSpace(docType)]
	if code == "" {
		code = "HRD"
	}
	return strings.NewReplacer(
		"{nomor}", fmt.Sprintf("%03d", counter),
		"{nomor_polos}", fmt.Sprintf("%d", counter),
		"{jenis}", code,
		"{bulan_romawi}", romanMonths[month],
		"{bulan}", fmt.Sprintf("%02d", month),
		"{tahun}", fmt.Sprintf("%04d", date.Year()),
		"{tahun_pendek}", fmt.Sprintf("%02d", date.Year()%100),
	).Replace(format)
}
