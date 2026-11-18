package service

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildPOSFile writes a Runchise-style sheet: a few metadata rows, the header,
// item rows where per-transaction cells (payment, status) appear only on a
// transaction's first item, and a TOTAL row.
func buildPOSFile(t *testing.T, header []any, rows [][]any) *bytes.Buffer {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sh := f.GetSheetName(0)
	all := append([][]any{{"Daftar Transaksi"}, {"Periode", "2026-11-17"}, {}}, header)
	all = append(all, rows...)
	all = append(all, []any{"TOTAL"})
	for i, r := range all {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow(sh, cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func TestParsePOSExcelFormats(t *testing.T) {
	cases := []struct {
		name   string
		header []any
		rows   [][]any
	}{
		{
			name:   "old export: single Diskon column",
			header: []any{"No Penjualan", "Tanggal Penjualan", "Kategori Produk", "Nama Produk", "Penjualan Kotor", "Diskon", "Biaya Tambahan", "Jenis Pembayaran", "Status"},
			rows: [][]any{
				{"S-1", "17/11/2026", "Mie & Bakso", "MIE AYAM", 28000, 3000, 0, "Cash", "Dibayar"},
				{"", "", "Minuman", "ES TEH", 8000, 0, "", "", ""},
				{"S-2", "17/11/2026", "Minuman", "AIR MINERAL", 8000, 0, 0, "Cash", "Void"},
			},
		},
		{
			// Runchise, Nov 2026: the discount is split and its sum is "Total Diskon".
			name: "new export: Total Diskon + split discount columns",
			header: []any{"Nama Lokasi", "No Receipt", "No Penjualan", "Tanggal Penjualan", "Tipe Order", "Kategori Produk", "Nama Produk",
				"Penjualan Kotor", "Total Diskon", "Diskon Penjualan", "Diskon Produk", "Tambahan Biaya", "Biaya Tambahan", "Jenis Pembayaran", "Status"},
			rows: [][]any{
				{"Cimanggu", "R1", "S-1", "17/11/2026", "Bungkus", "Mie & Bakso", "MIE AYAM", 28000, 3000, 2000, 1000, 0, 0, "Cash", "Dibayar"},
				{"", "", "", "", "Bungkus", "Minuman", "ES TEH", 8000, 0, 0, 0, 0, "", "", ""},
				{"Cimanggu", "R2", "S-2", "17/11/2026", "Bungkus", "Minuman", "AIR MINERAL", 8000, 0, 0, 0, 0, 0, "Cash", "Void"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ParsePOSExcel(buildPOSFile(t, tc.header, tc.rows), "x.xlsx")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if res.Date != "2026-11-17" {
				t.Errorf("date = %q", res.Date)
			}
			if len(res.Rows) != 2 || len(res.SkippedRows) != 1 {
				t.Fatalf("rows = %d, skipped = %d; want 2 and 1", len(res.Rows), len(res.SkippedRows))
			}
			if res.TotalGross != 36000 || res.TotalDisc != 3000 || res.Total != 33000 {
				t.Errorf("gross/disc/net = %v/%v/%v; want 36000/3000/33000", res.TotalGross, res.TotalDisc, res.Total)
			}
			// The second item inherits the first item's payment method.
			if len(res.Payments) != 1 || res.Payments[0].Name != "Cash" {
				t.Errorf("payments = %+v; want only Cash", res.Payments)
			}
		})
	}
}
