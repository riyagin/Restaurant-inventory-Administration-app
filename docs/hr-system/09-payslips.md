# Prompt 09 — Payslip Generation (PDF)

> Read `docs/hr-system/00-overview.md` first. Requires prompt 08 (payroll) completed.

## Goal

Generate Indonesian-language PDF payslips (Slip Gaji) per employee per payroll period, downloadable individually or as a batch ZIP. Available only for `closed` or `paid` periods (data is immutable by then).

## Implementation

1. **PDF library**: use a pure-Go PDF lib (e.g. `github.com/go-pdf/fpdf` or `github.com/johnfercher/maroto/v2` — pick one, justify in code comment). No external binaries (must run on the Ubuntu VPS under PM2).
2. **Company header settings**: add `hr_settings` singleton (company name, address, logo path, payslip footer text) + small admin form (can live on `/hr/attendance/settings` page renamed to a general `/hr/settings`, or its own section — keep it simple).
3. Payslips are **rendered on demand** from `payroll_lines` + `payroll_line_components` snapshots — no new stored documents (optionally cache to `server/uploads/payslips/` keyed by line id; invalidate never, since closed lines are immutable).

## Payslip Layout (A4)

```
[Logo]  <Company Name>                    SLIP GAJI
        <Address>                          Periode: <Month YYYY>

Karyawan : <name> (<employee_code>)        Jabatan : <position>
Cabang   : <branch>                        Tanggal Bergabung : <join_date>

PENDAPATAN                          POTONGAN
Gaji Pokok          Rp x            Potongan Komponen   Rp x   (per-component rows)
<allowance rows>    Rp x            Kasbon (KSB-...)    Rp x
<bonus rows>        Rp x            Cuti Tanpa Gaji (n hari) Rp x
Lembur (n hari)     Rp x
Hari Libur (n hari) Rp x
-----------------------------       ---------------------------
Total Pendapatan    Rp x            Total Potongan      Rp x

                       GAJI BERSIH (Take Home Pay): Rp x
<terbilang — amount in Indonesian words, e.g. "lima juta rupiah">

Catatan: <review_note if any>          <footer text>
```

All currency `id-ID` formatted from cents. Implement a small `terbilang` (number-to-Indonesian-words) helper with unit tests.

## Endpoints (admin/manager)

| Endpoint | Notes |
|---|---|
| `GET /api/hr/payroll/lines/:id/payslip` | Single PDF (`Content-Disposition: attachment`, filename `slip-gaji-<code>-<YYYY-MM>.pdf`) |
| `GET /api/hr/payroll/periods/:id/payslips` | ZIP of all payslips in the period |
| `GET/PUT /api/hr/settings` + `POST /api/hr/settings/logo` | Company info for the header |

Reject with 409 if period is still `open`.

## Frontend

1. PayrollPeriodDetail (prompt 08): add per-row "Slip Gaji" download icon (visible when closed/paid) and a header "Unduh Semua Slip" (ZIP) button. Handle blob downloads in `api.js`.
2. **HRSettings** (`/hr/settings`, admin) — company name, address, logo upload, footer text.

## Definition of Done

Standard checklist + tests: `terbilang` (0, exact thousands, juta/miliar, e.g. 1.500.000), 409 on open period, PDF endpoint returns valid PDF magic bytes, ZIP contains one file per line.
