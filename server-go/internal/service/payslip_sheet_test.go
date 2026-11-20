package service

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
)

func sampleSlip(name string) PayslipData {
	return PayslipData{
		CompanyName:   "PT Sumber Rejeki Nusantara",
		Address:       "Jl. Raya Darmo No. 123, Surabaya, Jawa Timur 60241",
		PayslipFooter: "Slip ini dicetak otomatis dan sah tanpa tanda tangan.",
		EmployeeName:  name,
		EmployeeCode:  "EMP-0012",
		Position:      "Kepala Koki",
		Branch:        "Cabang Darmo",
		JoinDate:      "01 Jan 2024",
		PeriodLabel:   "Juli 2026",
		Earnings: []PayslipLineItem{
			{Label: "Gaji Pokok", Amount: 4500000},
			{Label: "Tunjangan Jabatan", Amount: 750000},
			{Label: "Tunjangan Transport", Amount: 300000},
			{Label: "Lembur (12 jam)", Amount: 480000},
		},
		Deductions: []PayslipLineItem{
			{Label: "BPJS Kesehatan", Amount: 120000},
			{Label: "Kasbon (KSB-2026-004)", Amount: 500000},
			{Label: "Setengah Hari (4 jam)", Amount: 75000},
		},
		DailyPaid:      []PayslipLineItem{{Label: "Uang Makan (24 hari)", Amount: 480000}},
		TotalEarnings:  6030000,
		TotalDeduction: 695000,
		NetPay:         5335000,
		Note:           "Lembur bulan ini disetujui manajer cabang.",
	}
}

// TestBuildPayslipSheetPDF_TwoUp verifies the sheet is A4 landscape (297x210mm)
// and pairs two slips per page, so cutting down the middle yields A5 portrait.
func TestBuildPayslipSheetPDF_TwoUp(t *testing.T) {
	cases := []struct {
		n     int
		pages int
	}{{1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 3}}
	for _, c := range cases {
		slips := make([]PayslipData, c.n)
		for i := range slips {
			slips[i] = sampleSlip("Budi Santoso")
		}
		out, err := BuildPayslipSheetPDF(slips)
		if err != nil {
			t.Fatalf("n=%d: %v", c.n, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF-")) {
			t.Fatalf("n=%d: not a PDF", c.n)
		}
		if got := bytes.Count(out, []byte("/Type /Page\n")); got != c.pages {
			t.Errorf("n=%d: got %d pages, want %d", c.n, got, c.pages)
		}
		// A4 landscape in PDF points: 297mm x 210mm = 841.89 x 595.28.
		box := regexp.MustCompile(`/MediaBox \[0 0 841\.89 595\.28\]`)
		if !box.Match(out) {
			t.Errorf("n=%d: page is not A4 landscape", c.n)
		}
	}
}

// TestPayslipPanelFitsA5Half guards the layout against silent overflow: auto page
// break is off, so a slip taller than the A5 half would just be clipped. A dense
// slip (many components on both sides) must still end above the footer band.
func TestPayslipPanelFitsA5Half(t *testing.T) {
	d := sampleSlip("Nama Karyawan Yang Cukup Panjang Sekali")
	for i := 0; i < 8; i++ {
		d.Earnings = append(d.Earnings, PayslipLineItem{Label: fmt.Sprintf("Tunjangan Tambahan %d", i+1), Amount: 250000})
		d.Deductions = append(d.Deductions, PayslipLineItem{Label: fmt.Sprintf("Potongan Tambahan %d", i+1), Amount: 50000})
	}
	d.DailyPaid = append(d.DailyPaid, PayslipLineItem{Label: "Uang Transport Harian (24 hari)", Amount: 240000})
	d.Kasbons = []PayslipKasbon{
		{Number: "KSB-2026-0001", Amount: 500000, Deducted: 250000, Remaining: 250000},
		{Number: "KSB-2026-0002", Amount: 300000, Deducted: 150000, Remaining: 150000},
	}
	// A worst-case KINERJA block: every offense of the late/leave/absence table.
	// 40 dated rows: more than an A5 half can hold, so the table must cap
	// itself and summarise the rest.
	d.Performance = &PayslipPerformance{Score: 0, Grade: "C", GradeLinked: true,
		WindowNote: "Poin: 1–31 Okt · potongan gaji: 26 Sep – 25 Okt"}
	for i := 0; i < 40; i++ {
		d.Performance.Rows = append(d.Performance.Rows, PayslipOffense{
			Date:   time.Date(2026, 10, 1+i%28, 0, 0, 0, 0, time.UTC),
			Label:  fmt.Sprintf("Terlambat 11 - 20 menit — masuk 08:%02d, telat %d mnt, baris yang cukup panjang", 10+i%9, 10+i%9),
			Points: 3, Amount: 3125,
		})
	}

	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(panelPadX, panelPadY, panelPadX)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()

	// Both halves must fit, so check the right one too (it uses the same widths).
	for _, originX := range []float64{0, panelW} {
		end := drawPayslipPanel(pdf, d, originX)
		if limit := sheetH - panelBotY - 6; end > limit {
			t.Errorf("originX=%.1f: body ends at %.1fmm, past the %.1fmm footer band", originX, end, limit)
		}
	}
}
