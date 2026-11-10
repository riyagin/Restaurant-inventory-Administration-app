// seed-benefit-fund builds three employees to test Dana Kesejahteraan at
// resignation. Each has a 'benefit_fund' wage component of Rp 200.000/month and a
// month-by-month history from their join month up to last month; the current
// month is left open so it can be settled by the real payout.
//
//	SIM-DK-01 Satu Tahun            joined 12 months ago, never sick
//	                                pension 12 × 200.000          = Rp  2.400.000
//	SIM-DK-02 Sepuluh Tahun         joined 120 months ago, never sick
//	                                pension 120 × 200.000         = Rp 24.000.000
//	SIM-DK-03 Sepuluh Tahun Klaim   joined 120 months ago, claimed in 5 past months
//	                                (one over the allowance → kasbon, already repaid)
//	                                pension 24.000.000 − 775.000  = Rp 23.225.000
//	                                + this month, through the real service:
//	                                  claim 120.000 (covered)
//	                                  claim 250.000 → 80.000 covered, 170.000 kasbon (pending)
//
// The history is written as benefit_fund_movements (and benefit_claims for
// SIM-DK-03) without passing 120 months through payroll; its ledger counterpart
// is ONE opening entry per employee, Dr Saldo Awal (30900) / Cr Dana Pensiun,
// so the pension liability matches the per-employee balances and no simulated
// expense lands in a real month's P&L. This month's claims are posted for real.
//
// To test: resign the employee (Karyawan → edit → status Resign), then
// Dana Kesejahteraan → Cairkan. The payout settles the resign month first
// (contribution 200.000, unused part → pension) and then pays everything out:
//
//	SIM-DK-01  2.400.000 + 200.000             = Rp  2.600.000
//	SIM-DK-02 24.000.000 + 200.000             = Rp 24.200.000
//	SIM-DK-03 23.225.000 + 0 (month used up)   = Rp 23.225.000   (kasbon 170.000 still open)
//
// Usage (from server-go/):
//
//	go run ./cmd/seed-benefit-fund
//	go run ./cmd/seed-benefit-fund -branch="Warung Jambu"
//	go run ./cmd/seed-benefit-fund -clean
//
// Re-running is safe: every SIM-DK row is removed first. Ledger entries are
// removed by posting their reversal and then deleting both, so cached account
// balances never drift from the journal.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

const (
	codePrefix    = "SIM-DK-"
	monthly       = int64(200_000)
	baseSalary    = int64(3_000_000)
	componentName = "Dana Kesejahteraan"
	openingSource = "benefit_fund_opening" // simulated history, source_id = employee
)

type pastClaim struct {
	monthsAgo int
	day       int
	amount    int64
	desc      string
}

type simEmp struct {
	code, name   string
	monthsServed int
	claims       []pastClaim
}

func employees() []simEmp {
	return []simEmp{
		{code: "01", name: "Satu Tahun", monthsServed: 12},
		{code: "02", name: "Sepuluh Tahun", monthsServed: 120},
		{code: "03", name: "Sepuluh Tahun Klaim", monthsServed: 120, claims: []pastClaim{
			{30, 12, 150_000, "Rawat jalan — demam"},
			{18, 4, 200_000, "Klinik gigi"},
			{7, 9, 90_000, "Periksa dokter umum"},
			{7, 21, 60_000, "Obat lanjutan"},
			{3, 15, 450_000, "Rawat inap 1 malam — tipes"},
			{1, 6, 75_000, "Rawat jalan — batuk"},
		}},
	}
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func rupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += "."
		}
		out += string(c)
	}
	if neg {
		return "-Rp " + out
	}
	return "Rp " + out
}

// clean removes every SIM-DK row. Journal entries are reversed through
// service.Post (which moves the cached balances back) and then both the original
// and the reversal are deleted.
func clean(ctx context.Context, tx pgx.Tx, q *db.Queries) {
	const sim = `(SELECT id FROM employees WHERE employee_code LIKE '` + codePrefix + `%')`

	var closed int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payroll_lines l JOIN payroll_periods p ON p.id = l.payroll_period_id
		WHERE p.status <> 'open' AND l.employee_id IN `+sim).Scan(&closed); err != nil {
		log.Fatalf("cek payroll: %v", err)
	}
	if closed > 0 {
		log.Fatalf("karyawan SIM-DK sudah masuk periode penggajian yang ditutup — tidak dibersihkan otomatis")
	}

	rows, err := tx.Query(ctx, `
		SELECT je.id FROM journal_entries je
		WHERE (je.source_type IN ('benefit_payout', 'benefit_fund', '`+openingSource+`') AND je.source_id IN `+sim+`)
		   OR (je.source_type = 'benefit_claim' AND je.source_id IN (SELECT id FROM benefit_claims WHERE employee_id IN `+sim+`))
		   OR (je.source_type = 'kasbon' AND je.source_id IN (SELECT id FROM kasbons WHERE employee_id IN `+sim+`))`)
	if err != nil {
		log.Fatalf("cari jurnal simulasi: %v", err)
	}
	var entries []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			log.Fatal(err)
		}
		entries = append(entries, id)
	}
	rows.Close()

	qtx := q.WithTx(tx)
	for _, id := range entries {
		lrows, err := tx.Query(ctx, `SELECT account_id, amount FROM journal_lines WHERE entry_id = $1`, id)
		if err != nil {
			log.Fatal(err)
		}
		var lines []service.Line
		for lrows.Next() {
			var acct pgtype.UUID
			var amt int64
			if err := lrows.Scan(&acct, &amt); err != nil {
				log.Fatal(err)
			}
			lines = append(lines, service.Dr(acct.Bytes, -amt))
		}
		lrows.Close()
		rev, err := service.Post(ctx, qtx, service.Entry{
			Date: time.Now(), SourceType: "seed_cleanup", Description: "hapus data simulasi SIM-DK", Lines: lines,
		})
		if err != nil {
			log.Fatalf("balik jurnal simulasi: %v", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM journal_entries WHERE id = ANY($1)`, []pgtype.UUID{id, {Bytes: rev, Valid: true}}); err != nil {
			log.Fatalf("hapus jurnal simulasi: %v", err)
		}
	}

	for _, stmt := range []string{
		`DELETE FROM benefit_fund_movements WHERE employee_id IN ` + sim,
		`DELETE FROM benefit_claims WHERE employee_id IN ` + sim,
		`DELETE FROM kasbon_installments WHERE kasbon_id IN (SELECT id FROM kasbons WHERE employee_id IN ` + sim + `)`,
		`DELETE FROM kasbons WHERE employee_id IN ` + sim,
		`DELETE FROM payroll_line_components WHERE payroll_line_id IN (SELECT id FROM payroll_lines WHERE employee_id IN ` + sim + `)`,
		`DELETE FROM payroll_lines WHERE employee_id IN ` + sim,
		`DELETE FROM attendance_records WHERE employee_id IN ` + sim,
		`DELETE FROM leave_requests WHERE employee_id IN ` + sim,
		`DELETE FROM employee_wage_components WHERE wage_structure_id IN (SELECT id FROM wage_structures WHERE employee_id IN ` + sim + `)`,
		`DELETE FROM wage_structures WHERE employee_id IN ` + sim,
		`DELETE FROM employees WHERE employee_code LIKE '` + codePrefix + `%'`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			log.Fatalf("bersihkan data simulasi: %v\n%s", err, stmt)
		}
	}
}

func main() {
	branchName := flag.String("branch", "", "cabang karyawan simulasi (default: cabang pertama menurut nama)")
	cleanOnly := flag.Bool("clean", false, "hapus semua data SIM-DK lalu keluar")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()

	// Same as the API's startup: make sure migration 085 is applied.
	m, err := migrate.New("file://migrations", cfg.DBUrl)
	if err != nil {
		log.Fatalf("init migrasi: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrasi: %v", err)
	}

	pool, err := db.NewPool(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("koneksi database: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	clean(ctx, tx, q)
	if *cleanOnly {
		if err := tx.Commit(ctx); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Data simulasi SIM-DK dihapus.")
		return
	}

	// ── master data ────────────────────────────────────────────────────────────
	var branchID pgtype.UUID
	var bname string
	if *branchName != "" {
		err = tx.QueryRow(ctx, `SELECT id, name FROM branches WHERE name = $1`, *branchName).Scan(&branchID, &bname)
	} else {
		err = tx.QueryRow(ctx, `SELECT id, name FROM branches ORDER BY name LIMIT 1`).Scan(&branchID, &bname)
	}
	if err != nil {
		log.Fatalf("cabang tidak ditemukan: %v", err)
	}
	var positionID pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM positions ORDER BY name LIMIT 1`).Scan(&positionID); err != nil {
		log.Fatalf("belum ada jabatan: %v", err)
	}
	var compID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO wage_components (name, type, is_fixed, calc_method)
		VALUES ($1, 'benefit_fund', true, 'fixed')
		ON CONFLICT (name) DO UPDATE SET type = 'benefit_fund', is_active = true
		RETURNING id`, componentName).Scan(&compID); err != nil {
		log.Fatalf("komponen %s: %v", componentName, err)
	}
	cash, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.CashAccountNumber, Valid: true})
	if err != nil {
		log.Fatalf("akun Kas: %v", err)
	}
	openingEq, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: 30900, Valid: true})
	if err != nil {
		log.Fatalf("akun Saldo Awal (30900): %v", err)
	}
	pensionAcct, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.PensionFundAccountNumber, Valid: true})
	if err != nil {
		log.Fatalf("akun Dana Pensiun: %v", err)
	}

	today := service.TodayWIB()
	thisMonth := service.FirstOfMonth(today)
	var actor uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM users WHERE role IN ('superuser','admin') ORDER BY created_at LIMIT 1`).Scan(&actor)

	fmt.Printf("\n=== SIMULASI DANA KESEJAHTERAAN — cabang %s, iuran %s/bulan ===\n", bname, rupiah(monthly))

	for _, se := range employees() {
		code := codePrefix + se.code
		join := thisMonth.AddDate(0, -se.monthsServed, 0)
		var empID pgtype.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, status)
			 VALUES ($1, $2, $3, $4, $5, 'active') RETURNING id`,
			code, "SIM DK "+se.name, pgDate(join), positionID, branchID).Scan(&empID); err != nil {
			log.Fatalf("buat karyawan %s: %v", code, err)
		}
		var wsID pgtype.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO wage_structures (employee_id, base_salary, working_days_per_month, daily_rate, effective_date)
			 VALUES ($1, $2, 26, $3, $4) RETURNING id`,
			empID, baseSalary, baseSalary/26, pgDate(join)).Scan(&wsID); err != nil {
			log.Fatalf("struktur upah %s: %v", code, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount) VALUES ($1, $2, $3)`,
			wsID, compID, monthly); err != nil {
			log.Fatalf("komponen upah %s: %v", code, err)
		}

		// Past claims, by month.
		claimsByMonth := map[string][]pastClaim{}
		for _, c := range se.claims {
			k := thisMonth.AddDate(0, -c.monthsAgo, 0).Format("2006-01")
			claimsByMonth[k] = append(claimsByMonth[k], c)
		}

		mv := func(fund, typ string, amount int64, month, date time.Time, claim pgtype.UUID, note string) {
			if amount == 0 {
				return
			}
			if _, err := qtx.CreateBenefitMovement(ctx, &db.CreateBenefitMovementParams{
				EmployeeID: empID, Fund: fund, MovementType: typ, Amount: amount,
				PeriodMonth: pgDate(month), MovementDate: pgDate(date), ClaimID: claim,
				Note: note, CreatedBy: pgtype.UUID{Bytes: actor, Valid: actor != uuid.Nil},
			}); err != nil {
				log.Fatalf("mutasi %s: %v", code, err)
			}
		}

		var pension, claimedTotal int64
		seq := 0
		for mo := join; mo.Before(thisMonth); mo = mo.AddDate(0, 1, 0) {
			_, monthEnd := service.PeriodBounds(mo)
			mv(service.FundHealth, service.MovementContribution, monthly, mo, monthEnd, pgtype.UUID{}, "riwayat simulasi — iuran")

			var covered int64
			for _, c := range claimsByMonth[mo.Format("2006-01")] {
				date := time.Date(mo.Year(), mo.Month(), c.day, 0, 0, 0, 0, time.UTC)
				split := service.SplitClaim(c.amount, monthly, covered)
				covered += split.Covered
				claimedTotal += c.amount
				seq++

				var kasbonID pgtype.UUID
				if split.Excess > 0 {
					// Historic excess: a kasbon already repaid in two installments.
					if err := tx.QueryRow(ctx, `
						INSERT INTO kasbons (kasbon_number, employee_id, amount, details, sending_method,
						                     fund_source_account_id, request_date, resolution_month, status,
						                     approved_at, processed_at)
						VALUES ($1, $2, $3, $4, 'Tunai', $5, $6::date, $7, 'resolved', $6::date, $6::date) RETURNING id`,
						fmt.Sprintf("KSB-SIM-DK%s-%02d", se.code, seq), empID, split.Excess,
						"Kelebihan klaim kesehatan (riwayat simulasi): "+c.desc, cash.ID,
						pgDate(date), pgDate(mo.AddDate(0, 2, 0))).Scan(&kasbonID); err != nil {
						log.Fatalf("kasbon riwayat %s: %v", code, err)
					}
					half := split.Excess / 2
					for i, amt := range []int64{half, split.Excess - half} {
						if _, err := tx.Exec(ctx, `INSERT INTO kasbon_installments (kasbon_id, due_month, amount, status)
							VALUES ($1, $2, $3, 'deducted')`, kasbonID, pgDate(mo.AddDate(0, i+1, 0)), amt); err != nil {
							log.Fatalf("cicilan riwayat %s: %v", code, err)
						}
					}
				}
				claim, err := qtx.CreateBenefitClaim(ctx, &db.CreateBenefitClaimParams{
					ClaimNumber:         fmt.Sprintf("KLM-SIM-DK%s-%02d", se.code, seq),
					EmployeeID:          empID,
					ClaimDate:           pgDate(date),
					PeriodMonth:         pgDate(mo),
					TotalAmount:         c.amount,
					CoveredAmount:       split.Covered,
					ExcessAmount:        split.Excess,
					MonthlyAmount:       monthly,
					AvailableBefore:     split.Available,
					FundSourceAccountID: cash.ID,
					KasbonID:            kasbonID,
					Description:         c.desc + " (riwayat simulasi)",
					CreatedBy:           pgtype.UUID{Bytes: actor, Valid: actor != uuid.Nil},
				})
				if err != nil {
					log.Fatalf("klaim riwayat %s: %v", code, err)
				}
				mv(service.FundHealth, service.MovementClaim, -split.Covered, mo, date, claim.ID, c.desc)
			}

			sweep := service.MonthEndSweep(monthly, covered)
			mv(service.FundHealth, service.MovementSweepOut, -sweep, mo, monthEnd, pgtype.UUID{}, "riwayat simulasi — sisa → pensiun")
			mv(service.FundPension, service.MovementSweepIn, sweep, mo, monthEnd, pgtype.UUID{}, "riwayat simulasi — sisa dana kesehatan")
			pension += sweep
		}

		// Ledger counterpart of the simulated history.
		if pension > 0 {
			if _, err := service.Post(ctx, qtx, service.Entry{
				Date:        thisMonth.AddDate(0, 0, -1),
				SourceType:  openingSource,
				SourceID:    empID.Bytes,
				Description: fmt.Sprintf("Saldo awal dana pensiun (simulasi) — %s", code),
				CreatedBy:   actor,
				Lines: []service.Line{
					service.Dr(openingEq.ID.Bytes, pension),
					service.Cr(pensionAcct.ID.Bytes, pension),
				},
			}); err != nil {
				log.Fatalf("jurnal saldo awal %s: %v", code, err)
			}
		}

		fmt.Printf("\n%s  %s\n", code, "SIM DK "+se.name)
		fmt.Printf("  masuk %s (%d bulan), %d klaim riwayat senilai %s\n",
			join.Format("02 Jan 2006"), se.monthsServed, len(se.claims), rupiah(claimedTotal))
		fmt.Printf("  saldo dana pensiun sekarang: %s\n", rupiah(pension))

		// This month, through the real service.
		if se.code == "03" {
			for _, c := range []struct {
				day    int
				amount int64
				desc   string
			}{{3, 120_000, "Periksa dokter — flu"}, {8, 250_000, "Rontgen + obat"}} {
				d := time.Date(thisMonth.Year(), thisMonth.Month(), c.day, 0, 0, 0, 0, time.UTC)
				if d.After(today) {
					d = today
				}
				res, err := service.CreateClaim(ctx, qtx, service.ClaimInput{
					EmployeeID: empID, EmployeeName: "SIM DK " + se.name, ClaimDate: d,
					TotalAmount: c.amount, FundSourceAccountID: cash.ID.Bytes, Description: c.desc, Actor: actor,
					KasbonSendingMethod:   "Tunai",
					KasbonResolutionMonth: thisMonth.AddDate(0, 1, 0),
					KasbonInstallments:    []service.InstallmentInput{{DueMonth: thisMonth.AddDate(0, 1, 0), Amount: c.amount}},
				})
				if err != nil {
					log.Fatalf("klaim bulan ini %s: %v", code, err)
				}
				// The installment above was sized for the whole claim; resize to the excess.
				if res.Kasbon != nil {
					if _, err := tx.Exec(ctx, `UPDATE kasbon_installments SET amount = $2 WHERE kasbon_id = $1`, res.Kasbon.ID, res.Split.Excess); err != nil {
						log.Fatal(err)
					}
				}
				line := fmt.Sprintf("  bulan ini %s: klaim %s → ditanggung %s", d.Format("02 Jan"), rupiah(c.amount), rupiah(res.Split.Covered))
				if res.Kasbon != nil {
					line += fmt.Sprintf(", kasbon %s %s (pending)", res.Kasbon.KasbonNumber, rupiah(res.Split.Excess))
				}
				fmt.Println(line)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}

	fmt.Println(`
Perkiraan pencairan (resign bulan ini, payroll bulan ini belum dibuat):
  SIM-DK-01  2.400.000 + 200.000 iuran bulan terakhir = Rp  2.600.000
  SIM-DK-02 24.000.000 + 200.000                      = Rp 24.200.000
  SIM-DK-03 23.225.000 + 0 (jatah bulan ini habis)    = Rp 23.225.000  — kasbon 170.000 masih terbuka

Catatan: ketiganya berstatus aktif, jadi akan ikut bila payroll bulan ini di-generate.
Hapus dengan: go run ./cmd/seed-benefit-fund -clean`)
}
