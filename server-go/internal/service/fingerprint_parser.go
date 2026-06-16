package service

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
)

// ParsedPunch is one raw punch parsed from a fingerprint device export, before
// any matching against employees or merging into attendance records.
type ParsedPunch struct {
	EmployeeCode string    `json:"employee_code"`
	Timestamp    time.Time `json:"timestamp"`
}

// FingerprintParser turns an uploaded device export into a flat list of punches.
// The real fingerprint device format is PENDING from the user. By coding to this
// interface, a later session can drop in the real format (e.g. a proprietary
// binary/ZK export) as a new implementation WITHOUT touching the import handler
// or merge flow — the handler depends only on this interface.
type FingerprintParser interface {
	// Parse reads the export and returns punches. Rows it cannot parse are
	// returned as errors in the second value (non-fatal: the caller decides);
	// a hard failure (unreadable file) returns a non-nil error.
	Parse(file io.Reader) ([]ParsedPunch, []string, error)
}

// CSVFingerprintParser is the placeholder implementation: a generic CSV export
// with one punch per row.
//
// COLUMN ORDER (configurable):
//
//	CodeColumn      — 0-based index of the employee_code column (default 0)
//	TimestampColumn — 0-based index of the timestamp column     (default 1)
//	HasHeader       — when true, the first row is skipped
//	TimeLayouts     — accepted timestamp layouts, tried in order
//
// Default expects: `employee_code,timestamp` with an RFC3339 / common datetime
// timestamp, e.g.  EMP-0001,2026-06-09T07:58:00+07:00
//
// TODO(real-device-format): replace/augment with the actual fingerprint scanner
// export format once the user provides a sample. Implement FingerprintParser in
// a new type and swap it in at the handler's construction site; nothing else in
// the import flow needs to change.
type CSVFingerprintParser struct {
	CodeColumn      int
	TimestampColumn int
	HasHeader       bool
	TimeLayouts     []string
}

// NewCSVFingerprintParser returns the default generic-CSV parser:
// `employee_code,timestamp`, header row present.
func NewCSVFingerprintParser() *CSVFingerprintParser {
	return &CSVFingerprintParser{
		CodeColumn:      0,
		TimestampColumn: 1,
		HasHeader:       true,
		TimeLayouts: []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04",
			"02/01/2006 15:04:05",
			"02/01/2006 15:04",
		},
	}
}

// Parse implements FingerprintParser for the generic CSV format.
func (p *CSVFingerprintParser) Parse(file io.Reader) ([]ParsedPunch, []string, error) {
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("gagal membaca file CSV: %w", err)
	}

	var punches []ParsedPunch
	var rowErrors []string

	start := 0
	if p.HasHeader && len(records) > 0 {
		start = 1
	}

	for i := start; i < len(records); i++ {
		row := records[i]
		// Skip fully blank rows.
		blank := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}

		if p.CodeColumn >= len(row) || p.TimestampColumn >= len(row) {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: kolom tidak lengkap", i+1))
			continue
		}

		code := strings.TrimSpace(row[p.CodeColumn])
		rawTs := strings.TrimSpace(row[p.TimestampColumn])
		if code == "" {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: kode karyawan kosong", i+1))
			continue
		}

		ts, ok := parseFingerprintTime(rawTs, p.TimeLayouts)
		if !ok {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: format waktu \"%s\" tidak dikenal", i+1, rawTs))
			continue
		}

		punches = append(punches, ParsedPunch{EmployeeCode: code, Timestamp: ts})
	}

	return punches, rowErrors, nil
}

func parseFingerprintTime(raw string, layouts []string) (time.Time, bool) {
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// DATFingerprintParser parses the tab-separated .dat export from ZKTeco
// fingerprint devices. Format (no header):
//
//	<employee_id>\t<datetime>\t...(ignored)
//
// Only column 0 (employee ID) and column 1 (datetime "2006-01-02 15:04:05")
// are used. All other columns are ignored.
type DATFingerprintParser struct{}

func NewDATFingerprintParser() *DATFingerprintParser {
	return &DATFingerprintParser{}
}

func (p *DATFingerprintParser) Parse(file io.Reader) ([]ParsedPunch, []string, error) {
	scanner := bufio.NewScanner(file)
	var punches []ParsedPunch
	var rowErrors []string
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: format tidak valid (kurang dari 2 kolom)", lineNum))
			continue
		}

		code := strings.TrimSpace(fields[0])
		if code == "" {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: kode karyawan kosong", lineNum))
			continue
		}

		rawTs := strings.TrimSpace(fields[1])
		ts, err := time.Parse("2006-01-02 15:04:05", rawTs)
		if err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("baris %d: format waktu \"%s\" tidak dikenal", lineNum, rawTs))
			continue
		}

		punches = append(punches, ParsedPunch{EmployeeCode: code, Timestamp: ts})
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("gagal membaca file: %w", err)
	}

	return punches, rowErrors, nil
}
