package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// helper: build a pgtype.UUID from a deterministic byte so map lookups work.
func fakeUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func baseRef() HRImportRefData {
	makan := &db.WageComponent{ID: fakeUUID(10), Name: "Makan", Type: "allowance"}
	bpjs := &db.WageComponent{ID: fakeUUID(11), Name: "BPJS", Type: "deduction"}
	return HRImportRefData{
		Positions: map[string]pgtype.UUID{
			"barista": fakeUUID(1),
			"kasir":   fakeUUID(2),
		},
		Branches: map[string]pgtype.UUID{
			"pusat":  fakeUUID(3),
			"cabang": fakeUUID(4),
		},
		ExistingCodes:   map[string]bool{"EMP-0001": true},
		ExistingNameDob: map[string]bool{"budi|1990-01-01": true},
		Components: map[string]*db.WageComponent{
			"Makan": makan,
			"BPJS":  bpjs,
		},
		MaxEmployeeCodeSeq: 1,
	}
}

func componentCols() []*db.WageComponent {
	return []*db.WageComponent{
		{ID: fakeUUID(10), Name: "Makan", Type: "allowance"},
		{ID: fakeUUID(11), Name: "BPJS", Type: "deduction"},
	}
}

// 16 fixed columns + 2 component columns.
var hdr = append(append([]string{}, HRImportFixedHeaders...), "[Tunjangan] Makan", "[Potongan] BPJS")

func findRow(p *HRImportPreview, n int) *HRImportRow {
	for i := range p.Rows {
		if p.Rows[i].RowNumber == n {
			return &p.Rows[i]
		}
	}
	return nil
}

func TestParseHRImport_GoodRow(t *testing.T) {
	rows := [][]string{
		{"EMP-1000", "Andi", "1995-05-05", "2024-01-10", "Barista", "Pusat", "08123", "a@x.id", "Jl. A", "317", "BCA", "123", "Andi", "5000000", "26", "2024-01-10", "300000", "100000"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d: %+v", p.ErrorCount, p.Rows[0].Messages)
	}
	if p.OKCount != 1 {
		t.Fatalf("expected 1 ok, got %d", p.OKCount)
	}
	r := p.Rows[0]
	if r.BaseSalary != 5000000 {
		t.Errorf("base_salary should be whole rupiah 5000000, got %d", r.BaseSalary)
	}
	if r.WorkingDaysPerMonth != 26 {
		t.Errorf("working days = %d, want 26", r.WorkingDaysPerMonth)
	}
	if len(r.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(r.Components))
	}
}

// Component amounts must be stored UNCHANGED (whole rupiah), NOT ×100.
func TestParseHRImport_AmountStoredWholeRupiah(t *testing.T) {
	rows := [][]string{
		{"", "Citra", "1992-02-02", "2024-02-01", "Kasir", "Cabang", "", "", "", "", "", "", "", "7500000", "25", "2024-02-01", "250000", "50000"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	r := p.Rows[0]
	if r.BaseSalary != 7500000 {
		t.Errorf("base_salary stored = %d, want 7500000 (no ×100)", r.BaseSalary)
	}
	wantAmt := map[string]int64{"Makan": 250000, "BPJS": 50000}
	for _, c := range r.Components {
		if c.Amount != wantAmt[c.ComponentName] {
			t.Errorf("component %s amount = %d, want %d (whole rupiah, no ×100)", c.ComponentName, c.Amount, wantAmt[c.ComponentName])
		}
	}
}

func TestParseHRImport_BadDates(t *testing.T) {
	rows := [][]string{
		{"EMP-2000", "Dewi", "05-05-1995", "not-a-date", "Barista", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "01/02/2024", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.ErrorCount != 1 {
		t.Fatalf("expected 1 error row, got %d", p.ErrorCount)
	}
	if p.Rows[0].Status != "error" {
		t.Errorf("status = %s, want error", p.Rows[0].Status)
	}
}

func TestParseHRImport_UnknownBranch(t *testing.T) {
	rows := [][]string{
		{"EMP-3000", "Eka", "1990-03-03", "2024-03-01", "Barista", "TidakAda", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.Rows[0].Status != "error" {
		t.Fatalf("expected error for unknown branch, got %s", p.Rows[0].Status)
	}
}

func TestParseHRImport_UnknownPosition(t *testing.T) {
	rows := [][]string{
		{"EMP-4000", "Fajar", "1990-03-03", "2024-03-01", "Manajer", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.Rows[0].Status != "error" {
		t.Fatalf("expected error for unknown position, got %s", p.Rows[0].Status)
	}
}

func TestParseHRImport_DuplicateCodeVsExisting(t *testing.T) {
	rows := [][]string{
		{"EMP-0001", "Gita", "1990-03-03", "2024-03-01", "Barista", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.Rows[0].Status != "error" {
		t.Fatalf("expected error for code duplicate vs DB, got %s", p.Rows[0].Status)
	}
}

func TestParseHRImport_DuplicateCodeInFile(t *testing.T) {
	rows := [][]string{
		{"EMP-9000", "Hadi", "1990-03-03", "2024-03-01", "Barista", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
		{"EMP-9000", "Indra", "1991-04-04", "2024-03-01", "Kasir", "Cabang", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	// First occurrence ok, second is the duplicate.
	if findRow(p, 1).Status == "error" {
		t.Errorf("first occurrence should not be a dup error: %+v", findRow(p, 1).Messages)
	}
	if findRow(p, 2).Status != "error" {
		t.Errorf("second occurrence should be a dup error")
	}
}

func TestParseHRImport_AutoCodeGenerationNoCollision(t *testing.T) {
	rows := [][]string{
		{"", "Joko", "1990-03-03", "2024-03-01", "Barista", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
		{"", "Kiki", "1991-04-04", "2024-03-01", "Kasir", "Cabang", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	// MaxEmployeeCodeSeq=1, so next codes are EMP-0002, EMP-0003.
	c1 := findRow(p, 1).EmployeeCode
	c2 := findRow(p, 2).EmployeeCode
	if c1 != "EMP-0002" {
		t.Errorf("first auto code = %s, want EMP-0002", c1)
	}
	if c2 != "EMP-0003" {
		t.Errorf("second auto code = %s, want EMP-0003", c2)
	}
	if c1 == c2 {
		t.Errorf("auto-generated codes collided: %s == %s", c1, c2)
	}
	if p.ErrorCount != 0 {
		t.Errorf("auto-generated codes should not produce errors, got %d", p.ErrorCount)
	}
}

func TestParseHRImport_DuplicateNameDobWarning(t *testing.T) {
	rows := [][]string{
		{"EMP-5000", "Budi", "1990-01-01", "2024-03-01", "Barista", "Pusat", "", "", "", "", "", "", "", "5000000", "26", "2024-03-01", "0", "0"},
	}
	p := ParseHRImportRows("test.xlsx", hdr, rows, componentCols(), baseRef())
	if p.Rows[0].Status != "warning" {
		t.Fatalf("expected warning for duplicate name+dob, got %s", p.Rows[0].Status)
	}
	if p.WarningCount != 1 {
		t.Errorf("warning count = %d, want 1", p.WarningCount)
	}
}
