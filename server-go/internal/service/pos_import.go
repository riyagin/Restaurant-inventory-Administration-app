package service

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type POSByPayment struct {
	Payment string  `json:"payment"`
	Gross   float64 `json:"gross"`
	Disc    float64 `json:"disc"`
}

type POSCategory struct {
	Name      string         `json:"name"`
	Gross     float64        `json:"gross"`
	Disc      float64        `json:"disc"`
	Net       float64        `json:"net"`
	ByPayment []POSByPayment `json:"byPayment"`
}

type POSPayment struct {
	Name  string  `json:"name"`
	Gross float64 `json:"gross"`
	Disc  float64 `json:"disc"`
	Net   float64 `json:"net"`
}

type POSRow struct {
	NoPenjualan string  `json:"no_penjualan"`
	Category    string  `json:"category"`
	Product     string  `json:"product"`
	Gross       float64 `json:"gross"`
	Disc        float64 `json:"disc"`
	Biaya       float64 `json:"biaya"`
	Net         float64 `json:"net"`
	Payment     string  `json:"payment"`
}

type POSSkippedRow struct {
	NoPenjualan string  `json:"no_penjualan"`
	Category    string  `json:"category"`
	Product     string  `json:"product"`
	Gross       float64 `json:"gross"`
	Disc        float64 `json:"disc"`
	Biaya       float64 `json:"biaya"`
	Net         float64 `json:"net"`
	Payment     string  `json:"payment"`
	Status      string  `json:"status"`
}

type POSBiayaRow struct {
	NoPenjualan string  `json:"no_penjualan"`
	Category    string  `json:"category"`
	Product     string  `json:"product"`
	Biaya       float64 `json:"biaya"`
}

type POSDateSummary struct {
	Date       string  `json:"date"`
	TotalNet   float64 `json:"totalNet"`
	TotalGross float64 `json:"totalGross"`
	TotalDisc  float64 `json:"totalDisc"`
	TotalBiaya float64 `json:"totalBiaya"`
}

type POSParseResult struct {
	Filename    string           `json:"filename"`
	Date        string           `json:"date"`
	Dates       []POSDateSummary `json:"dates"`
	Categories  []POSCategory    `json:"categories"`
	Payments    []POSPayment     `json:"payments"`
	Rows        []POSRow         `json:"rows"`
	BiayaRows   []POSBiayaRow    `json:"biayaRows"`
	SkippedRows []POSSkippedRow  `json:"skippedRows"`
	Total       float64          `json:"total"`
	TotalGross  float64          `json:"totalGross"`
	TotalDisc   float64          `json:"totalDisc"`
	TotalBiaya  float64          `json:"totalBiaya"`
}

// ParsePOSExcel reads an uploaded Excel file and returns structured import data.
// Column detection is header-flexible — columns are found by name, not fixed index.
func ParsePOSExcel(file io.Reader, filename string) (*POSParseResult, error) {
	f, err := excelize.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca file Excel: %w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("file Excel tidak memiliki sheet")
	}

	rows, err := f.GetRows(sheetName, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("gagal membaca baris: %w", err)
	}

	// Find header row by scanning for "Kategori Produk".
	headerRow := -1
	var headers []string
	for i, row := range rows {
		for _, cell := range row {
			if cell == "Kategori Produk" {
				headerRow = i
				headers = row
				break
			}
		}
		if headerRow != -1 {
			break
		}
	}
	if headerRow == -1 {
		return nil, fmt.Errorf(`format tidak dikenal: header "Kategori Produk" tidak ditemukan`)
	}

	// findCol returns the first column index matching any of the given names (case-insensitive).
	findCol := func(names ...string) int {
		for _, name := range names {
			for i, h := range headers {
				if strings.EqualFold(strings.TrimSpace(h), name) {
					return i
				}
			}
		}
		return -1
	}

	colNoPenjualan := findCol("No Penjualan")
	colTanggal := findCol("Tanggal Penjualan", "Tanggal")
	colKategori := findCol("Kategori Produk")
	colProduk := findCol("Nama Produk")
	colGross := findCol("Penjualan Kotor")
	colDisc := findCol("Diskon")
	colBiaya := findCol("Biaya Tambahan")
	colPayment := findCol("Jenis Pembayaran")
	colStatus := findCol("Status")

	// Validate required columns.
	required := map[string]int{
		"Kategori Produk":  colKategori,
		"Penjualan Kotor":  colGross,
		"Diskon":           colDisc,
		"Jenis Pembayaran": colPayment,
		"Status":           colStatus,
	}
	for name, idx := range required {
		if idx == -1 {
			return nil, fmt.Errorf(`format tidak dikenal: kolom "%s" tidak ditemukan`, name)
		}
	}

	getCell := func(row []string, col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return row[col]
	}

	parseFloat := func(s string) float64 {
		v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v
	}

	// parseDate converts DD/MM/YYYY or ISO strings to YYYY-MM-DD.
	parseDate := func(raw string) string {
		if raw == "" {
			return ""
		}
		parts := strings.Split(raw, "/")
		if len(parts) == 3 {
			day := strings.TrimSpace(parts[0])
			month := strings.TrimSpace(parts[1])
			year := strings.TrimSpace(parts[2])
			if len(day) < 2 {
				day = "0" + day
			}
			if len(month) < 2 {
				month = "0" + month
			}
			return year + "-" + month + "-" + day
		}
		if idx := strings.Index(raw, "T"); idx != -1 {
			return raw[:idx]
		}
		return raw
	}

	type internalRow struct {
		NoPenjualan string
		Category    string
		Product     string
		Gross       float64
		Disc        float64
		Biaya       float64
		Net         float64
		Payment     string
		DateRaw     string
	}

	var dataRows []internalRow
	var skippedRows []POSSkippedRow

	var currentAN, currentStatus, currentDate string

	for _, row := range rows[headerRow+1:] {
		// Stop at TOTAL row.
		if len(row) > 0 && row[0] == "TOTAL" {
			break
		}

		// Payment method inherits from previous non-empty cell.
		if v := strings.TrimSpace(getCell(row, colPayment)); v != "" && v != "-" {
			currentAN = v
		}

		// Date inherits from previous non-empty cell.
		if colTanggal != -1 {
			if v := strings.TrimSpace(getCell(row, colTanggal)); v != "" {
				currentDate = v
			}
		}

		category := strings.TrimSpace(getCell(row, colKategori))
		if category == "" {
			continue
		}

		gross := parseFloat(getCell(row, colGross))
		disc := parseFloat(getCell(row, colDisc))
		biaya := 0.0
		if colBiaya != -1 {
			biaya = parseFloat(getCell(row, colBiaya))
		}

		noPenjualan := strings.TrimSpace(getCell(row, colNoPenjualan))
		product := strings.TrimSpace(getCell(row, colProduk))

		// Status inherits from previous non-empty cell.
		if v := strings.TrimSpace(getCell(row, colStatus)); v != "" {
			currentStatus = v
		}

		payment := currentAN
		if payment == "" {
			payment = "(tidak diketahui)"
		}

		if !strings.EqualFold(currentStatus, "dibayar") {
			status := currentStatus
			if status == "" {
				status = "(kosong)"
			}
			skippedRows = append(skippedRows, POSSkippedRow{
				NoPenjualan: noPenjualan,
				Category:    category,
				Product:     product,
				Gross:       gross,
				Disc:        disc,
				Biaya:       biaya,
				Net:         gross - disc,
				Payment:     payment,
				Status:      status,
			})
			continue
		}

		dataRows = append(dataRows, internalRow{
			NoPenjualan: noPenjualan,
			Category:    category,
			Product:     product,
			Gross:       gross,
			Disc:        disc,
			Biaya:       biaya,
			Net:         gross - disc,
			Payment:     payment,
			DateRaw:     currentDate,
		})
	}

	// First date from paid rows.
	var firstDate string
	for _, r := range dataRows {
		if r.DateRaw != "" {
			firstDate = r.DateRaw
			break
		}
	}
	saleDate := parseDate(firstDate)

	// Aggregation maps.
	catMap := map[string]*POSCategory{}
	catPayMap := map[string]map[string]*POSByPayment{}
	payMap := map[string]*POSPayment{}
	dateMap := map[string]*POSDateSummary{}

	var posRows []POSRow
	var biayaRows []POSBiayaRow

	for _, row := range dataRows {
		// Category aggregate.
		if catMap[row.Category] == nil {
			catMap[row.Category] = &POSCategory{Name: row.Category}
		}
		catMap[row.Category].Gross += row.Gross
		catMap[row.Category].Disc += row.Disc
		catMap[row.Category].Net += row.Net

		// Category × payment breakdown.
		if catPayMap[row.Category] == nil {
			catPayMap[row.Category] = map[string]*POSByPayment{}
		}
		if catPayMap[row.Category][row.Payment] == nil {
			catPayMap[row.Category][row.Payment] = &POSByPayment{Payment: row.Payment}
		}
		catPayMap[row.Category][row.Payment].Gross += row.Gross
		catPayMap[row.Category][row.Payment].Disc += row.Disc

		// Payment aggregate.
		if payMap[row.Payment] == nil {
			payMap[row.Payment] = &POSPayment{Name: row.Payment}
		}
		payMap[row.Payment].Gross += row.Gross
		payMap[row.Payment].Disc += row.Disc
		payMap[row.Payment].Net += row.Net

		// Date aggregate.
		d := parseDate(row.DateRaw)
		if d == "" {
			d = saleDate
		}
		if dateMap[d] == nil {
			dateMap[d] = &POSDateSummary{Date: d}
		}
		dateMap[d].TotalNet += row.Net
		dateMap[d].TotalGross += row.Gross
		dateMap[d].TotalDisc += row.Disc
		dateMap[d].TotalBiaya += row.Biaya

		posRows = append(posRows, POSRow{
			NoPenjualan: row.NoPenjualan,
			Category:    row.Category,
			Product:     row.Product,
			Gross:       row.Gross,
			Disc:        row.Disc,
			Biaya:       row.Biaya,
			Net:         row.Net,
			Payment:     row.Payment,
		})

		if row.Biaya > 0 {
			biayaRows = append(biayaRows, POSBiayaRow{
				NoPenjualan: row.NoPenjualan,
				Category:    row.Category,
				Product:     row.Product,
				Biaya:       row.Biaya,
			})
		}
	}

	// Build categories slice with byPayment breakdowns.
	var categories []POSCategory
	for name, cat := range catMap {
		c := *cat
		for _, bp := range catPayMap[name] {
			c.ByPayment = append(c.ByPayment, *bp)
		}
		categories = append(categories, c)
	}

	// Build payments slice.
	var payments []POSPayment
	for _, p := range payMap {
		payments = append(payments, *p)
	}

	// Build dates slice sorted ascending.
	var dates []POSDateSummary
	for _, d := range dateMap {
		dates = append(dates, *d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Date < dates[j].Date })

	// Totals.
	var totalNet, totalGross, totalDisc, totalBiaya float64
	for _, r := range dataRows {
		totalNet += r.Net
		totalGross += r.Gross
		totalDisc += r.Disc
		totalBiaya += r.Biaya
	}

	// Ensure non-nil slices for JSON serialization.
	if posRows == nil {
		posRows = []POSRow{}
	}
	if biayaRows == nil {
		biayaRows = []POSBiayaRow{}
	}
	if skippedRows == nil {
		skippedRows = []POSSkippedRow{}
	}
	if categories == nil {
		categories = []POSCategory{}
	}
	if payments == nil {
		payments = []POSPayment{}
	}
	if dates == nil {
		dates = []POSDateSummary{}
	}

	return &POSParseResult{
		Filename:    filename,
		Date:        saleDate,
		Dates:       dates,
		Categories:  categories,
		Payments:    payments,
		Rows:        posRows,
		BiayaRows:   biayaRows,
		SkippedRows: skippedRows,
		Total:       totalNet,
		TotalGross:  totalGross,
		TotalDisc:   totalDisc,
		TotalBiaya:  totalBiaya,
	}, nil
}
