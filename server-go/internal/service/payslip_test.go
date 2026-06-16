package service

import (
	"bytes"
	"testing"
)

func TestBuildPayslipPDF_MagicBytes(t *testing.T) {
	d := PayslipData{
		CompanyName:  "PT Contoh Sejahtera",
		Address:      "Jl. Merdeka No. 1, Jakarta",
		EmployeeName: "Budi Santoso",
		EmployeeCode: "EMP-0001",
		Position:     "Kasir",
		Branch:       "Cabang Pusat",
		JoinDate:     "01 Jan 2024",
		PeriodLabel:  "Mei 2026",
		Earnings: []PayslipLineItem{
			{Label: "Gaji Pokok", Amount: 3000000},
			{Label: "Tunjangan Makan", Amount: 500000},
			{Label: "Lembur (2 hari)", Amount: 200000},
		},
		Deductions: []PayslipLineItem{
			{Label: "BPJS", Amount: 100000},
			{Label: "Kasbon (KSB-2026-0001)", Amount: 300000},
		},
		TotalEarnings:  3700000,
		TotalDeduction: 400000,
		NetPay:         3300000,
		Note:           "Pembayaran tepat waktu.",
		PayslipFooter:  "Dokumen ini sah tanpa tanda tangan basah.",
	}

	out, err := BuildPayslipPDF(d)
	if err != nil {
		t.Fatalf("BuildPayslipPDF error: %v", err)
	}
	if len(out) < 100 {
		t.Fatalf("PDF too small: %d bytes", len(out))
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Errorf("output does not start with %%PDF magic bytes, got %q", out[:4])
	}
}

// TestBuildPayslipPDF_MissingLogo ensures a non-existent logo path does not break
// rendering (robustness requirement).
func TestBuildPayslipPDF_MissingLogo(t *testing.T) {
	d := PayslipData{
		CompanyName:  "PT Tanpa Logo",
		EmployeeName: "Siti",
		EmployeeCode: "EMP-0002",
		LogoPath:     "/path/yang/tidak/ada/logo.png",
		PeriodLabel:  "Mei 2026",
		Earnings:      []PayslipLineItem{{Label: "Gaji Pokok", Amount: 2000000}},
		TotalEarnings: 2000000,
		NetPay:        2000000,
	}
	out, err := BuildPayslipPDF(d)
	if err != nil {
		t.Fatalf("BuildPayslipPDF with missing logo error: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Errorf("output missing PDF magic bytes")
	}
}

func TestFormatRupiah(t *testing.T) {
	cases := map[int64]string{
		0:        "Rp 0",
		1000:     "Rp 1.000",
		1500000:  "Rp 1.500.000",
		-300000:  "-Rp 300.000",
		12345678: "Rp 12.345.678",
	}
	for n, want := range cases {
		if got := formatRupiah(n); got != want {
			t.Errorf("formatRupiah(%d) = %q, want %q", n, got, want)
		}
	}
}
