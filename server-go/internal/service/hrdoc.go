package service

import (
	"fmt"
	"strings"
	"time"
)

// HR document generation.
//
// Four Indonesian HR document types are produced from a single, format-neutral
// block model (see []Block below), which is then rendered to either DOCX
// (hrdoc_docx.go) or PDF (hrdoc_pdf.go). Keeping the legal text in one place —
// the Build* template functions here — means the DOCX and PDF versions can never
// drift apart, and there is exactly one spot to update when regulations change.
//
// Legal basis baked into the templates:
//   - PKWT / PKWTT mandatory contents: Pasal 54 ayat (1) UU 13/2003 (perjanjian
//     kerja tertulis sekurang-kurangnya memuat: para pihak, jabatan, tempat
//     pekerjaan, upah & cara pembayaran, syarat kerja, jangka waktu, tempat &
//     tanggal dibuat, tanda tangan).
//   - PKWT: no probation (masa percobaan batal demi hukum — Pasal 58 UU 13/2003
//     jo. UU 6/2023) and mandatory uang kompensasi at end of term (Pasal 15–17
//     PP 35/2021: 12 bulan masa kerja = 1x upah sebulan).
//   - PKWTT: probation up to 3 months (Pasal 60 UU 13/2003); hak pesangon/PHK
//     mengikuti PP 35/2021.
//   - Surat Peringatan: SP I/II/III each valid 6 months (Pasal 154A jo. lampiran
//     UU 6/2023 Cipta Kerja; PP 35/2021 Pasal 52).
//   - Paklaring: employer's obligation to issue a work-reference letter
//     (surat keterangan / referensi) on termination — Pasal 1602y KUHPerdata.

// DocTypePKWT etc. are the accepted values of HRDocInput.Type.
const (
	DocTypePKWT     = "pkwt"
	DocTypePKWTT    = "pkwtt"
	DocTypeSP       = "surat_peringatan"
	DocTypePaklarng = "paklaring"
)

// HRDocInput is the fully-user-supplied payload for a document. Every field is a
// plain string except money (rupiah, whole units) and small counts, so the
// frontend form maps to it one-to-one. Dates arrive as "YYYY-MM-DD".
type HRDocInput struct {
	Type string `json:"type"`

	// Company / letterhead.
	CompanyName    string `json:"company_name"`
	CompanyAddress string `json:"company_address"`
	CompanyPhone   string `json:"company_phone"`
	CompanyEmail   string `json:"company_email"`
	City           string `json:"city"`            // kota tempat penandatanganan
	DocumentNumber string `json:"document_number"` // nomor surat
	DocumentDate   string `json:"document_date"`   // YYYY-MM-DD

	// Perusahaan signatory (PIHAK PERTAMA / yang menerangkan).
	SignatoryName     string `json:"signatory_name"`
	SignatoryPosition string `json:"signatory_position"`
	SignatoryNID      string `json:"signatory_national_id"`

	// Employee identity (PIHAK KEDUA / yang bersangkutan).
	EmployeeName       string `json:"employee_name"`
	EmployeeGender     string `json:"employee_gender"` // "L" | "P" | free text
	EmployeeBirthPlace string `json:"employee_birth_place"`
	EmployeeBirthDate  string `json:"employee_birth_date"`
	EmployeeNationalID string `json:"employee_national_id"` // NIK KTP
	EmployeeAddress    string `json:"employee_address"`
	EmployeePhone      string `json:"employee_phone"`
	EmployeePosition   string `json:"employee_position"` // jabatan
	EmployeeDivision   string `json:"employee_division"`

	// Employment terms (contract types).
	PlaceOfWork     string `json:"place_of_work"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Salary          int64  `json:"salary"`        // rupiah, whole units
	SalaryPeriod    string `json:"salary_period"` // "bulan" (default) | "hari"
	PaymentInfo     string `json:"payment_info"`
	JobDescription  string `json:"job_description"`
	WorkingHours    string `json:"working_hours"`
	ProbationMonths int    `json:"probation_months"` // PKWTT only, 0..3

	// Surat Peringatan.
	WarningLevel        string `json:"warning_level"` // "1" | "2" | "3"
	ViolationDate       string `json:"violation_date"`
	ViolationDetail     string `json:"violation_detail"`
	PreviousWarningRef  string `json:"previous_warning_ref"`
	Consequence         string `json:"consequence"`
	ValidityMonths      int    `json:"validity_months"` // default 6
	ImprovementExpected string `json:"improvement_expected"`

	// Paklaring.
	ReasonLeaving string `json:"reason_leaving"`
	ConductNote   string `json:"conduct_note"`
}

// blockKind enumerates the renderable block types shared by both renderers.
type blockKind int

const (
	kindTitle     blockKind = iota // centered, bold, large
	kindSubtitle                   // centered, small (e.g. "Nomor: ...")
	kindHeading                    // left, bold (Pasal / section heading)
	kindParagraph                  // justified body text
	kindClause                     // marker in the gutter + justified text
	kindIdentity                   // borderless label/value table
	kindSpacer                     // vertical gap
	kindSignature                  // one or more signing columns
	kindLetterhead                 // company header block (name + address lines)
)

// kv is one row of an identity block ("Nama" : "Budi").
type kv struct{ Label, Value string }

// signCol is one signing column of a signature block.
type signCol struct {
	TopLines []string // e.g. {"Bandung, 30 Juli 2026"} or {"Hormat kami,"}
	Role     string   // optional, bold, e.g. "PIHAK PERTAMA"
	Name     string   // signer, rendered underlined
	SubLines []string // under the name, e.g. {"Direktur"}
}

// Block is one unit of document content, format-neutral.
type Block struct {
	Kind    blockKind
	Text    string
	Marker  string // clause marker, e.g. "1.", "a.", "Pasal 3"
	Items   []kv
	Cols    []signCol
	Lines   []string // letterhead extra lines
	Spacing float64  // spacer height in mm (pdf) — docx uses a blank paragraph
}

// HRDocument is the rendered-neutral result of a template: a title used for the
// download filename and the ordered content blocks.
type HRDocument struct {
	FilenameBase string
	Blocks       []Block
}

// ---- Indonesian date & number helpers ------------------------------------

var idMonthNames = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

var idWeekdays = map[time.Weekday]string{
	time.Sunday: "Minggu", time.Monday: "Senin", time.Tuesday: "Selasa",
	time.Wednesday: "Rabu", time.Thursday: "Kamis", time.Friday: "Jumat",
	time.Saturday: "Sabtu",
}

// parseDateID parses a "YYYY-MM-DD" string, returning ok=false when empty/bad.
func parseDateID(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// tanggalIndo renders "30 Juli 2026". Empty/invalid input returns "".
func tanggalIndo(s string) string {
	t, ok := parseDateID(s)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d %s %d", t.Day(), idMonthNames[int(t.Month())], t.Year())
}

// hariTanggalIndo renders "Kamis, 30 Juli 2026".
func hariTanggalIndo(s string) string {
	t, ok := parseDateID(s)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s, %s", idWeekdays[t.Weekday()], tanggalIndo(s))
}

// genderIndo maps L/P to the spelled words; anything else passes through.
func genderIndo(g string) string {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "L", "LAKI-LAKI", "PRIA", "M":
		return "Laki-laki"
	case "P", "PEREMPUAN", "WANITA", "F":
		return "Perempuan"
	default:
		return strings.TrimSpace(g)
	}
}

// rupiahTerbilang renders "Rp 1.500.000 (satu juta lima ratus ribu rupiah)".
// It reuses the package's existing Terbilang() (service/terbilang.go) so the
// spelled-out amount matches the payslip's.
func rupiahTerbilang(n int64) string {
	return fmt.Sprintf("%s (%s rupiah)", formatRupiah(n), Terbilang(n))
}

// dash returns v, or a fill-in dash line when v is blank, so a printed template
// always shows a place to write rather than an empty gap.
func dash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "………………………………"
	}
	return strings.TrimSpace(v)
}

// ---- Public entrypoint ---------------------------------------------------

// BuildHRDocument dispatches to the right template. Unknown types return nil.
func BuildHRDocument(in HRDocInput) (*HRDocument, error) {
	switch in.Type {
	case DocTypePKWT:
		return buildPKWT(in), nil
	case DocTypePKWTT:
		return buildPKWTT(in), nil
	case DocTypeSP:
		return buildSuratPeringatan(in), nil
	case DocTypePaklarng:
		return buildPaklaring(in), nil
	default:
		return nil, fmt.Errorf("jenis dokumen tidak dikenal: %q", in.Type)
	}
}

// letterhead builds the company header block used by every document.
func letterhead(in HRDocInput) Block {
	lines := []string{}
	if a := strings.TrimSpace(in.CompanyAddress); a != "" {
		lines = append(lines, a)
	}
	contact := []string{}
	if p := strings.TrimSpace(in.CompanyPhone); p != "" {
		contact = append(contact, "Telp. "+p)
	}
	if e := strings.TrimSpace(in.CompanyEmail); e != "" {
		contact = append(contact, "Email: "+e)
	}
	if len(contact) > 0 {
		lines = append(lines, strings.Join(contact, "  •  "))
	}
	name := strings.TrimSpace(in.CompanyName)
	if name == "" {
		name = "NAMA PERUSAHAAN"
	}
	return Block{Kind: kindLetterhead, Text: strings.ToUpper(name), Lines: lines}
}

func spacer(mm float64) Block { return Block{Kind: kindSpacer, Spacing: mm} }

// identityFromInput assembles the standard employee identity rows (Pasal 54
// ayat (1) huruf b: nama, jenis kelamin, umur/alamat).
func employeeIdentity(in HRDocInput) []kv {
	rows := []kv{{"Nama", dash(in.EmployeeName)}}
	if g := genderIndo(in.EmployeeGender); g != "" {
		rows = append(rows, kv{"Jenis Kelamin", g})
	}
	ttl := ""
	if in.EmployeeBirthPlace != "" || in.EmployeeBirthDate != "" {
		ttl = strings.TrimSpace(in.EmployeeBirthPlace)
		if bd := tanggalIndo(in.EmployeeBirthDate); bd != "" {
			if ttl != "" {
				ttl += ", "
			}
			ttl += bd
		}
		rows = append(rows, kv{"Tempat/Tgl. Lahir", ttl})
	}
	if in.EmployeeNationalID != "" {
		rows = append(rows, kv{"No. KTP (NIK)", in.EmployeeNationalID})
	}
	if in.EmployeeAddress != "" {
		rows = append(rows, kv{"Alamat", in.EmployeeAddress})
	}
	if in.EmployeePhone != "" {
		rows = append(rows, kv{"No. Telepon", in.EmployeePhone})
	}
	if in.EmployeePosition != "" {
		rows = append(rows, kv{"Jabatan", in.EmployeePosition})
	}
	return rows
}

// signPlaceDate renders "Kota, 30 Juli 2026" for the signature top line.
func signPlaceDate(in HRDocInput) string {
	city := strings.TrimSpace(in.City)
	d := tanggalIndo(in.DocumentDate)
	switch {
	case city != "" && d != "":
		return city + ", " + d
	case d != "":
		return d
	case city != "":
		return city + ", ………………………"
	default:
		return "………………………, ………………………"
	}
}
