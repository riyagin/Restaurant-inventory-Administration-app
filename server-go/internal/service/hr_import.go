package service

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/xuri/excelize/v2"

	"inventory-app/server-go/internal/db"
)

// MONEY CONVENTION — IMPORTANT.
// The prompt spec text says "convert rupiah to cents on import (×100)". We
// DELIBERATELY DO NOT do that conversion. Prompt 02 stored all wage amounts
// (base_salary, component amounts, daily_rate) as WHOLE RUPIAH in their BIGINT
// columns, matching the existing app's invoices/sales/accounts convention. To
// keep the HR module internally consistent, the importer stores the SAME
// whole-rupiah integers — the Excel sheet already holds whole rupiah, parsed
// to int64 as-is, NO ×100. (Daily rate is derived via ComputeDailyRate.)

// Fixed (non-component) header columns of the "Karyawan" sheet, in order.
// These names double as the canonical template header row.
var HRImportFixedHeaders = []string{
	"employee_code",
	"full_name",
	"dob (YYYY-MM-DD)",
	"join_date (YYYY-MM-DD)",
	"position",
	"branch",
	"phone",
	"email",
	"address",
	"national_id",
	"bank_name",
	"bank_account_number",
	"bank_account_holder",
	"base_salary",
	"working_days_per_month",
	"effective_date (YYYY-MM-DD)",
}

// HRImportComponentValue is one wage component amount parsed for a row.
type HRImportComponentValue struct {
	ComponentName string `json:"component_name"`
	ComponentType string `json:"component_type"`
	Amount        int64  `json:"amount"`
}

// HRImportRow is a single parsed + validated employee row in the preview.
// Amounts are WHOLE RUPIAH (see money convention note above).
type HRImportRow struct {
	RowNumber           int                      `json:"row_number"` // 1-based, excludes header
	EmployeeCode        string                   `json:"employee_code"`
	FullName            string                   `json:"full_name"`
	Dob                 string                   `json:"dob"`
	JoinDate            string                   `json:"join_date"`
	Position            string                   `json:"position"`
	Branch              string                   `json:"branch"`
	Phone               string                   `json:"phone"`
	Email               string                   `json:"email"`
	Address             string                   `json:"address"`
	NationalID          string                   `json:"national_id"`
	BankName            string                   `json:"bank_name"`
	BankAccountNumber   string                   `json:"bank_account_number"`
	BankAccountHolder   string                   `json:"bank_account_holder"`
	BaseSalary          int64                    `json:"base_salary"`
	WorkingDaysPerMonth int32                    `json:"working_days_per_month"`
	EffectiveDate       string                   `json:"effective_date"`
	Components          []HRImportComponentValue `json:"components"`
	Status              string                   `json:"status"` // ok | warning | error
	Messages            []string                 `json:"messages"`
}

// HRImportPreview is the full parse result returned by the parse endpoint and
// persisted (as JSONB) in hr_import_batches.payload.
type HRImportPreview struct {
	Filename      string        `json:"filename"`
	FixedHeaders  []string      `json:"fixed_headers"`
	ComponentCols []string      `json:"component_columns"` // display header e.g. "[Tunjangan] Makan"
	Rows          []HRImportRow `json:"rows"`
	TotalRows     int           `json:"total_rows"`
	OKCount       int           `json:"ok_count"`
	WarningCount  int           `json:"warning_count"`
	ErrorCount    int           `json:"error_count"`
}

// HRImportRefData carries the DB-derived reference sets the validator needs.
// All maps are keyed by lower-cased names so matching is case-insensitive.
type HRImportRefData struct {
	Positions          map[string]pgtype.UUID // lower(name) -> position id
	Branches           map[string]pgtype.UUID // lower(name) -> branch id
	ExistingCodes      map[string]bool        // existing employee_code (as stored)
	ExistingNameDob    map[string]bool        // lower(full_name)|dob -> exists
	Components         map[string]*db.WageComponent
	MaxEmployeeCodeSeq int32
}

// componentTypeLabel maps a wage_component type to its Indonesian template prefix.
func componentTypeLabel(t string) string {
	switch t {
	case "allowance":
		return "Tunjangan"
	case "bonus":
		return "Bonus"
	case "deduction":
		return "Potongan"
	default:
		return t
	}
}

// ComponentColumnHeader returns the template column header for a component, e.g.
// "[Tunjangan] Makan".
func ComponentColumnHeader(c *db.WageComponent) string {
	return fmt.Sprintf("[%s] %s", componentTypeLabel(c.Type), c.Name)
}

// parseHRImportDate accepts "YYYY-MM-DD" or a native Excel serial date string
// (RawCellValue numeric). Returns the normalized "YYYY-MM-DD" string.
func parseHRImportDate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	// Strip a trailing time component if a full timestamp slipped in.
	if idx := strings.Index(raw, "T"); idx != -1 {
		raw = raw[:idx]
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.Format("2006-01-02"), true
	}
	// Native Excel serial date (days since 1899-12-30).
	if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
		epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		t := epoch.AddDate(0, 0, int(f))
		return t.Format("2006-01-02"), true
	}
	return "", false
}

// parseRupiah parses a whole-rupiah integer string. Accepts thousands
// separators ("." or ",") and an optional trailing ".0"/".00" so spreadsheet
// numeric cells (e.g. "5000000.00") import cleanly. Returns (value, ok).
// Negative or fractional rupiah are rejected.
func parseRupiah(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true // empty handled by caller (required vs optional)
	}
	// Drop a trailing zero-only fractional part from numeric cells.
	if dot := strings.IndexByte(raw, '.'); dot != -1 {
		frac := raw[dot+1:]
		if strings.Trim(frac, "0") == "" {
			raw = raw[:dot]
		}
	}
	cleaned := strings.NewReplacer(".", "", ",", "", " ", "").Replace(raw)
	if cleaned == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// ParseHRImportRows is the pure, DB-free core: given the spreadsheet rows
// (header + data), the ordered component column headers, the wage_component
// records they map to, and reference data, it validates every row and returns
// the preview. New auto-generated codes are tracked in-memory so blanks across
// the same batch never collide, and freshly assigned codes count as duplicates
// for later rows.
func ParseHRImportRows(filename string, headerRow []string, dataRows [][]string, componentCols []*db.WageComponent, ref HRImportRefData) *HRImportPreview {
	fixedCount := len(HRImportFixedHeaders)

	displayComponentCols := make([]string, len(componentCols))
	for i, c := range componentCols {
		displayComponentCols[i] = ComponentColumnHeader(c)
	}

	preview := &HRImportPreview{
		Filename:      filename,
		FixedHeaders:  HRImportFixedHeaders,
		ComponentCols: displayComponentCols,
		Rows:          []HRImportRow{},
	}

	// Local copies/working sets so we never mutate the caller's maps.
	seenCodes := map[string]bool{}
	for code := range ref.ExistingCodes {
		seenCodes[strings.ToLower(code)] = true
	}
	nextSeq := ref.MaxEmployeeCodeSeq

	get := func(cells []string, idx int) string {
		if idx < 0 || idx >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[idx])
	}

	for i, cells := range dataRows {
		// Skip fully blank rows (common trailing rows from Excel).
		blank := true
		for _, c := range cells {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}

		row := HRImportRow{
			RowNumber:         i + 1,
			EmployeeCode:      get(cells, 0),
			FullName:          get(cells, 1),
			Dob:               get(cells, 2),
			JoinDate:          get(cells, 3),
			Position:          get(cells, 4),
			Branch:            get(cells, 5),
			Phone:             get(cells, 6),
			Email:             get(cells, 7),
			Address:           get(cells, 8),
			NationalID:        get(cells, 9),
			BankName:          get(cells, 10),
			BankAccountNumber: get(cells, 11),
			BankAccountHolder: get(cells, 12),
			EffectiveDate:     get(cells, 15),
			Status:            "ok",
			Messages:          []string{},
		}

		addErr := func(msg string) {
			row.Status = "error"
			row.Messages = append(row.Messages, msg)
		}
		addWarn := func(msg string) {
			if row.Status != "error" {
				row.Status = "warning"
			}
			row.Messages = append(row.Messages, msg)
		}

		// ── Required fields ──
		if row.FullName == "" {
			addErr("Nama lengkap wajib diisi")
		}
		if row.Position == "" {
			addErr("Jabatan wajib diisi")
		}
		if row.Branch == "" {
			addErr("Cabang wajib diisi")
		}

		// ── Dates ──
		if d, ok := parseHRImportDate(row.Dob); ok {
			row.Dob = d
		} else if row.Dob != "" {
			addErr("Format tanggal lahir tidak valid (gunakan YYYY-MM-DD)")
		}

		if row.JoinDate == "" {
			addErr("Tanggal bergabung wajib diisi")
		} else if d, ok := parseHRImportDate(row.JoinDate); ok {
			row.JoinDate = d
		} else {
			addErr("Format tanggal bergabung tidak valid (gunakan YYYY-MM-DD)")
		}

		if row.EffectiveDate == "" {
			addErr("Tanggal berlaku gaji wajib diisi")
		} else if d, ok := parseHRImportDate(row.EffectiveDate); ok {
			row.EffectiveDate = d
		} else {
			addErr("Format tanggal berlaku tidak valid (gunakan YYYY-MM-DD)")
		}

		// ── Position / branch lookup (case-insensitive, no auto-create) ──
		if row.Position != "" {
			if _, ok := ref.Positions[strings.ToLower(row.Position)]; !ok {
				addErr(fmt.Sprintf("Jabatan \"%s\" tidak ditemukan", row.Position))
			}
		}
		if row.Branch != "" {
			if _, ok := ref.Branches[strings.ToLower(row.Branch)]; !ok {
				addErr(fmt.Sprintf("Cabang \"%s\" tidak ditemukan", row.Branch))
			}
		}

		// ── base_salary (whole rupiah, required, non-negative) ──
		rawSalary := get(cells, 13)
		if rawSalary == "" {
			addErr("Gaji pokok wajib diisi")
		} else if v, ok := parseRupiah(rawSalary); ok {
			row.BaseSalary = v
		} else {
			addErr("Gaji pokok harus berupa angka bulat non-negatif (rupiah)")
		}

		// ── working_days_per_month (1..31, required) ──
		rawWD := get(cells, 14)
		if rawWD == "" {
			addErr("Hari kerja per bulan wajib diisi")
		} else if wd, err := strconv.Atoi(strings.TrimSpace(rawWD)); err == nil {
			if wd < 1 || wd > 31 {
				addErr("Hari kerja per bulan harus antara 1 dan 31")
			} else {
				row.WorkingDaysPerMonth = int32(wd)
			}
		} else {
			addErr("Hari kerja per bulan harus berupa angka")
		}

		// ── Component amounts (whole rupiah, NO ×100) ──
		for ci, comp := range componentCols {
			rawAmt := get(cells, fixedCount+ci)
			if rawAmt == "" {
				continue // blank component cell = not assigned
			}
			v, ok := parseRupiah(rawAmt)
			if !ok {
				addErr(fmt.Sprintf("Nilai komponen \"%s\" harus angka bulat non-negatif (rupiah)", comp.Name))
				continue
			}
			row.Components = append(row.Components, HRImportComponentValue{
				ComponentName: comp.Name,
				ComponentType: comp.Type,
				Amount:        v, // whole rupiah, stored as-is
			})
		}

		// ── employee_code: blank => auto-generate; else dup check ──
		if row.EmployeeCode == "" {
			nextSeq++
			row.EmployeeCode = NextEmployeeCode(nextSeq - 1)
			seenCodes[strings.ToLower(row.EmployeeCode)] = true
		} else {
			key := strings.ToLower(row.EmployeeCode)
			if seenCodes[key] {
				addErr(fmt.Sprintf("Kode karyawan \"%s\" sudah digunakan (di file atau di database)", row.EmployeeCode))
			} else {
				seenCodes[key] = true
			}
		}

		// ── Duplicate full_name + dob in DB => warning (still importable) ──
		if row.FullName != "" && row.Dob != "" {
			ndKey := strings.ToLower(row.FullName) + "|" + row.Dob
			if ref.ExistingNameDob[ndKey] {
				addWarn(fmt.Sprintf("Karyawan dengan nama \"%s\" dan tanggal lahir sama sudah ada", row.FullName))
			}
		}

		preview.Rows = append(preview.Rows, row)
	}

	for i := range preview.Rows {
		switch preview.Rows[i].Status {
		case "error":
			preview.ErrorCount++
		case "warning":
			preview.WarningCount++
		default:
			preview.OKCount++
		}
	}
	preview.TotalRows = len(preview.Rows)
	return preview
}

// ParseHRImportExcel reads an uploaded .xlsx, locates the "Karyawan" sheet,
// extracts the header + data rows, resolves component columns against the
// supplied active components, and delegates to ParseHRImportRows.
func ParseHRImportExcel(file io.Reader, filename string, components []*db.WageComponent, ref HRImportRefData) (*HRImportPreview, error) {
	f, err := excelize.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca file Excel: %w", err)
	}
	defer f.Close()

	sheet := "Karyawan"
	if idx, _ := f.GetSheetIndex(sheet); idx == -1 {
		// Fall back to the first sheet if it isn't named exactly "Karyawan".
		sheet = f.GetSheetName(0)
		if sheet == "" {
			return nil, fmt.Errorf("file Excel tidak memiliki sheet")
		}
	}

	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("gagal membaca baris: %w", err)
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("sheet \"%s\" kosong", sheet)
	}

	header := rows[0]
	if len(header) < len(HRImportFixedHeaders) {
		return nil, fmt.Errorf("format header tidak dikenal: kolom tetap tidak lengkap")
	}

	// Map component columns positionally: they follow the fixed columns, in the
	// same order the template wrote them (sorted active components).
	componentCols := components

	var dataRows [][]string
	if len(rows) > 1 {
		dataRows = rows[1:]
	}

	return ParseHRImportRows(filename, header, dataRows, componentCols, ref), nil
}

// HRImportConfirmResult summarizes a confirmed batch.
type HRImportConfirmResult struct {
	EmployeesCreated int `json:"employees_created"`
}

// ConfirmHRImport inserts every row of a previously-parsed preview within the
// caller's transaction (qtx). It is all-or-nothing: the first failure returns
// an error and the caller rolls back. For each row it creates the employee,
// then an INITIAL wage structure + components via the prompt-02 wage service
// (so daily_rate + versioning invariants hold). Amounts are stored as WHOLE
// RUPIAH (see money convention note above).
func ConfirmHRImport(ctx context.Context, qtx *db.Queries, preview *HRImportPreview, ref HRImportRefData, createdBy pgtype.UUID) (*HRImportConfirmResult, error) {
	componentByName := map[string]*db.WageComponent{}
	for k, c := range ref.Components {
		componentByName[strings.ToLower(k)] = c
	}

	res := &HRImportConfirmResult{}

	for _, row := range preview.Rows {
		if row.Status == "error" {
			return nil, fmt.Errorf("baris %d masih memiliki kesalahan; impor dibatalkan", row.RowNumber)
		}

		positionID, ok := ref.Positions[strings.ToLower(row.Position)]
		if !ok {
			return nil, fmt.Errorf("baris %d: jabatan \"%s\" tidak ditemukan", row.RowNumber, row.Position)
		}
		branchID, ok := ref.Branches[strings.ToLower(row.Branch)]
		if !ok {
			return nil, fmt.Errorf("baris %d: cabang \"%s\" tidak ditemukan", row.RowNumber, row.Branch)
		}

		dob, err := dateOrEmpty(row.Dob)
		if err != nil {
			return nil, fmt.Errorf("baris %d: tanggal lahir tidak valid", row.RowNumber)
		}
		joinDate, err := dateOrEmpty(row.JoinDate)
		if err != nil || !joinDate.Valid {
			return nil, fmt.Errorf("baris %d: tanggal bergabung tidak valid", row.RowNumber)
		}
		effDate, err := time.Parse("2006-01-02", row.EffectiveDate)
		if err != nil {
			return nil, fmt.Errorf("baris %d: tanggal berlaku tidak valid", row.RowNumber)
		}

		empID, err := qtx.CreateEmployee(ctx, &db.CreateEmployeeParams{
			EmployeeCode:      row.EmployeeCode,
			FullName:          row.FullName,
			Dob:               dob,
			JoinDate:          joinDate,
			PositionID:        positionID,
			BranchID:          branchID,
			Phone:             textOrEmpty(row.Phone),
			Email:             textOrEmpty(row.Email),
			Address:           textOrEmpty(row.Address),
			NationalID:        textOrEmpty(row.NationalID),
			BankName:          textOrEmpty(row.BankName),
			BankAccountNumber: textOrEmpty(row.BankAccountNumber),
			BankAccountHolder: textOrEmpty(row.BankAccountHolder),
			UserID:            pgtype.UUID{},
			Status:            "active",
		})
		if err != nil {
			return nil, fmt.Errorf("baris %d: gagal membuat karyawan %s: %w", row.RowNumber, row.FullName, err)
		}

		var comps []WageComponentInput
		for _, cv := range row.Components {
			comp, ok := componentByName[strings.ToLower(cv.ComponentName)]
			if !ok {
				return nil, fmt.Errorf("baris %d: komponen \"%s\" tidak ditemukan", row.RowNumber, cv.ComponentName)
			}
			comps = append(comps, WageComponentInput{
				ComponentID: comp.ID,
				Amount:      cv.Amount, // whole rupiah, NO ×100
			})
		}

		if _, err := CreateWageVersion(ctx, qtx, CreateWageVersionParams{
			EmployeeID:    empID,
			BaseSalary:    row.BaseSalary, // whole rupiah, NO ×100
			WorkingDays:   row.WorkingDaysPerMonth,
			EffectiveDate: effDate,
			CreatedBy:     createdBy,
			Components:    comps,
		}); err != nil {
			return nil, fmt.Errorf("baris %d: gagal membuat struktur gaji untuk %s: %w", row.RowNumber, row.FullName, err)
		}

		res.EmployeesCreated++
	}

	return res, nil
}

// textOrEmpty returns a valid pgtype.Text for non-empty strings, NULL otherwise.
func textOrEmpty(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// dateOrEmpty parses "YYYY-MM-DD"; empty string yields a NULL date.
func dateOrEmpty(s string) (pgtype.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}
