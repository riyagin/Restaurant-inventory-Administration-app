// seed-attendance-cuts builds a sandbox for the late-arrival / early-departure pay
// cut: eleven SIM employees at one branch, each earning Rp 60.000 a day, each
// living one attendance scenario for a whole month on a real rostered shift. It
// then opens that month's payroll period, generates the lines, keeps only the SIM
// ones, and prints the expected cut beside the one payroll actually froze.
//
//	SIM-CUT-01 Tepat waktu           OPN  08:28–19:31          → 0
//	SIM-CUT-02 Dalam toleransi       OPN  08:35–19:21          → 0 (grace 5, tolerance 10)
//	SIM-CUT-03 Telat 6 menit         OPN  08:36–19:30          → 15 min/day
//	SIM-CUT-04 Telat 2 jam           OPN  10:30–19:30          → 120 min/day
//	SIM-CUT-05 Pulang 2 jam awal     OPN  08:30–17:30          → 120 min/day (= SIM-CUT-04)
//	SIM-CUT-06 Telat 10 + pulang 25  OPN  08:40–19:05          → 15 + 30 min/day
//	SIM-CUT-07 Scan lalu pulang      OPN  08:30–08:31          → whole day
//	SIM-CUT-08 Shift Middle telat 30 MID  10:00–22:00          → 30 min of 690
//	SIM-CUT-09 Shift Jumping pulang  JMP  08:30–21:00          → 60 min of 660
//	SIM-CUT-10 Tanpa shift telat 1j  —    09:30–21:30          → 60 min of branch hours
//	SIM-CUT-11 Campuran              OPN  rotates daily: on time, late, early,
//	                                      half-day correction, missing check-out,
//	                                      absent, late + early
//
// Usage:
//
//	go run ./cmd/seed-attendance-cuts                 # previous month
//	go run ./cmd/seed-attendance-cuts -month=2026-10
//	go run ./cmd/seed-attendance-cuts -clean          # remove every SIM-CUT row
//
// Re-running is safe: every SIM-CUT row is deleted first. The payroll period is
// only created when the month has none; a month whose period already holds real
// employees is refused rather than mixed with simulation lines.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

const (
	codePrefix = "SIM-CUT-"
	dailyRate  = 60_000
)

var wib = time.FixedZone("WIB", 7*3600)

// day is one scenario day. Blank in/out = no punch on that end.
type day struct {
	status   string // present | absent
	in, out  string // "HH:MM"
	halfDay  string // "" | "late" | "early" — a manager's half-day correction
	describe string
}

type scenario struct {
	name  string
	shift string // shift code at the branch; "" = no roster, branch hours apply
	day   func(i int) day
}

func every(in, out string) func(int) day {
	return func(int) day { return day{status: "present", in: in, out: out} }
}

func scenarios() []scenario {
	return []scenario{
		{"Tepat Waktu", "OPN", every("08:28", "19:31")},
		{"Dalam Toleransi", "OPN", every("08:35", "19:21")},
		{"Telat 6 Menit", "OPN", every("08:36", "19:30")},
		{"Telat 2 Jam", "OPN", every("10:30", "19:30")},
		{"Pulang 2 Jam Awal", "OPN", every("08:30", "17:30")},
		{"Telat 10 + Pulang 25 Awal", "OPN", every("08:40", "19:05")},
		{"Scan Masuk Lalu Pulang", "OPN", every("08:30", "08:31")},
		{"Shift Middle Telat 30", "MID", every("10:00", "22:00")},
		{"Shift Jumping Pulang 1 Jam Awal", "JMP", every("08:30", "21:00")},
		{"Tanpa Shift Telat 1 Jam", "", every("09:30", "21:30")},
		{"Campuran Harian", "OPN", func(i int) day {
			switch i % 7 {
			case 0:
				return day{status: "present", in: "08:30", out: "19:30", describe: "tepat waktu"}
			case 1:
				return day{status: "present", in: "08:45", out: "19:30", describe: "telat 15"}
			case 2:
				return day{status: "present", in: "08:30", out: "18:45", describe: "pulang 45 awal"}
			case 3:
				return day{status: "present", in: "13:00", out: "19:30", halfDay: "late", describe: "koreksi setengah hari"}
			case 4:
				return day{status: "present", in: "08:50", describe: "telat 20, lupa absen pulang"}
			case 5:
				return day{status: "absent", describe: "mangkir"}
			default:
				return day{status: "present", in: "11:30", out: "16:30", describe: "telat 3 jam + pulang 3 jam awal"}
			}
		}},
	}
}

func clock(d time.Time, hhmm string) *time.Time {
	if hhmm == "" {
		return nil
	}
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		log.Fatalf("jam tidak valid %q: %v", hhmm, err)
	}
	v := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, wib)
	return &v
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func nilIfZero[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
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

// clean removes every row the seeder owns, plus the month's period when only
// SIM lines are left in it.
func clean(ctx context.Context, tx pgx.Tx) {
	const sim = `(SELECT id FROM employees WHERE employee_code LIKE '` + codePrefix + `%')`
	for _, stmt := range []string{
		`DELETE FROM performance_violations WHERE employee_id IN ` + sim,
		`DELETE FROM performance_scores WHERE employee_id IN ` + sim,
		`DELETE FROM attendance_records WHERE employee_id IN ` + sim,
		`DELETE FROM shift_assignments WHERE employee_id IN ` + sim,
		`DELETE FROM payroll_line_components WHERE payroll_line_id IN (SELECT id FROM payroll_lines WHERE employee_id IN ` + sim + `)`,
		`DELETE FROM payroll_lines WHERE employee_id IN ` + sim,
		`DELETE FROM payroll_periods p WHERE p.status = 'open' AND NOT EXISTS (SELECT 1 FROM payroll_lines l WHERE l.payroll_period_id = p.id)`,
		`DELETE FROM wage_structures WHERE employee_id IN ` + sim,
		`DELETE FROM employees WHERE employee_code LIKE '` + codePrefix + `%'`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			log.Fatalf("bersihkan data simulasi: %v\n%s", err, stmt)
		}
	}
}

func main() {
	monthFlag := flag.String("month", "", "bulan simulasi YYYY-MM (default: bulan lalu)")
	branchName := flag.String("branch", "Warung Jambu", "cabang tempat karyawan simulasi (harus punya shift OPN, MID, JMP)")
	cleanOnly := flag.Bool("clean", false, "hapus semua data SIM-CUT lalu keluar")
	flag.Parse()

	now := time.Now()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	if *monthFlag != "" {
		m, err := time.Parse("2006-01", *monthFlag)
		if err != nil {
			log.Fatalf("format -month tidak valid (YYYY-MM): %v", err)
		}
		month = m
	}
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	daysInMonth := end.Day()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	clean(ctx, tx)
	if *cleanOnly {
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("commit: %v", err)
		}
		fmt.Println("Data simulasi SIM-CUT dihapus.")
		return
	}

	// A period that already holds real employees is not ours to touch.
	var foreign int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM payroll_periods p JOIN payroll_lines l ON l.payroll_period_id = p.id
		 WHERE p.period_month = $1`, pgDate(start)).Scan(&foreign); err != nil {
		log.Fatalf("cek periode: %v", err)
	}
	if foreign > 0 {
		log.Fatalf("periode penggajian %s sudah berisi %d baris karyawan sungguhan — pilih bulan lain dengan -month",
			start.Format("2006-01"), foreign)
	}

	// ── branch, schedule, shifts, position ────────────────────────────────────
	var branchID pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM branches WHERE name = $1`, *branchName).Scan(&branchID); err != nil {
		log.Fatalf("cabang %q tidak ditemukan: %v", *branchName, err)
	}
	branchSched := service.DefaultSchedule()
	if ws, err := q.WithTx(tx).GetWorkScheduleByBranch(ctx, branchID); err == nil && ws != nil {
		branchSched = service.ScheduleFromRow(ws)
	}
	shiftIDs := map[string]pgtype.UUID{}
	rows, err := tx.Query(ctx, `SELECT code, id FROM shift_definitions WHERE branch_id = $1`, branchID)
	if err != nil {
		log.Fatalf("ambil shift: %v", err)
	}
	for rows.Next() {
		var code string
		var id pgtype.UUID
		if err := rows.Scan(&code, &id); err != nil {
			log.Fatalf("scan shift: %v", err)
		}
		shiftIDs[code] = id
	}
	rows.Close()
	for _, c := range []string{"OPN", "MID", "JMP"} {
		if _, ok := shiftIDs[c]; !ok {
			log.Fatalf("cabang %q tidak punya shift %s", *branchName, c)
		}
	}
	var positionID pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM positions ORDER BY name LIMIT 1`).Scan(&positionID); err != nil {
		log.Fatalf("belum ada jabatan: %v", err)
	}

	fmt.Printf("\n=== SIMULASI POTONGAN TELAT / PULANG AWAL — %s ===\n", start.Format("January 2006"))
	fmt.Printf("Cabang %s: jam kerja %02d:%02d–%02d:%02d, toleransi telat %d mnt, toleransi pulang %d mnt\n",
		*branchName, branchSched.WorkStartMinutes/60, branchSched.WorkStartMinutes%60,
		branchSched.WorkEndMinutes/60, branchSched.WorkEndMinutes%60,
		branchSched.GraceMinutes, branchSched.EarlyLeaveMinutes)
	fmt.Printf("Upah harian %s · pembulatan %d menit per ujung hari\n\n", rupiah(dailyRate), service.AttendanceCutBlockMinutes)

	// ── employees, wages, roster, attendance ──────────────────────────────────
	type simEmp struct {
		id       pgtype.UUID
		code     string
		name     string
		shift    string
		expected int64
		expMin   int
	}
	var emps []simEmp
	for n, sc := range scenarios() {
		code := fmt.Sprintf("%s%02d", codePrefix, n+1)
		name := "SIM " + sc.name
		var empID pgtype.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, status)
			 VALUES ($1, $2, $3, $4, $5, 'active') RETURNING id`,
			code, name, pgDate(start.AddDate(0, -6, 0)), positionID, branchID).Scan(&empID); err != nil {
			log.Fatalf("buat karyawan %s: %v", code, err)
		}
		// Base pay = every day of the month at the daily rate, so net pay reads as
		// "days × 60.000 minus the cut".
		if _, err := tx.Exec(ctx,
			`INSERT INTO wage_structures (employee_id, base_salary, working_days_per_month, daily_rate, effective_date)
			 VALUES ($1, $2, $3, $4, $5)`,
			empID, int64(daysInMonth)*dailyRate, daysInMonth, dailyRate, pgDate(start.AddDate(0, -6, 0))); err != nil {
			log.Fatalf("buat struktur upah %s: %v", code, err)
		}

		e := simEmp{id: empID, code: code, name: name, shift: sc.shift}
		for i := 0; i < daysInMonth; i++ {
			d := start.AddDate(0, 0, i)
			if sc.shift != "" {
				if _, err := tx.Exec(ctx,
					`INSERT INTO shift_assignments (employee_id, date, shift_id) VALUES ($1, $2, $3)`,
					empID, pgDate(d), shiftIDs[sc.shift]); err != nil {
					log.Fatalf("roster %s %s: %v", code, d.Format("2006-01-02"), err)
				}
			}
			spec := sc.day(i)
			sched := service.ScheduleWithShift(ctx, tx, branchSched, empID, d)
			st := &service.AttendanceState{Status: spec.status, CheckIn: clock(d, spec.in), CheckOut: clock(d, spec.out)}
			if st.CheckIn != nil {
				st.CheckInSource = "face"
			}
			if st.CheckOut != nil {
				st.CheckOutSource = "face"
			}
			service.ComputeAnomalies(st, sched, true)
			if spec.halfDay != "" {
				st.IsHalfDay, st.HalfDayType = true, spec.halfDay
				st.HalfDayLostMinutes = service.ComputeLostMinutes(st.CheckIn, sched)
			}

			// Expected cut, from first principles: the shift's paid length, each end
			// rounded up to a block, capped at the day.
			if st.Status == "present" && !st.IsHalfDay {
				paid := sched.WorkEndMinutes - sched.WorkStartMinutes
				switch sc.shift {
				case "MID":
					paid = 12*60 + 30 - 60 // 09:30–22:00 minus 60 break
				case "JMP":
					paid = (14*60 - (8*60 + 30)) + (22*60 - (16*60 + 30)) // 08:30–14:00 + 16:30–22:00
				}
				m := 0
				if st.IsLate {
					m += service.RoundUpToBlock(st.LateMinutes, service.AttendanceCutBlockMinutes)
				}
				if st.IsEarlyLeave {
					m += service.RoundUpToBlock(st.EarlyLeaveMinutes, service.AttendanceCutBlockMinutes)
				}
				if m > paid {
					m = paid
				}
				e.expMin += m
				e.expected += int64(float64(dailyRate)*float64(m)/float64(paid) + 0.5)
			}

			if _, err := tx.Exec(ctx,
				`INSERT INTO attendance_records
				   (employee_id, date, check_in, check_out, check_in_source, check_out_source,
				    status, is_late, late_minutes, is_early_leave, early_leave_minutes,
				    is_missing_checkout, is_missing_checkin, is_no_punch,
				    is_half_day, half_day_lost_minutes, half_day_type, note)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
				empID, pgDate(d), st.CheckIn, st.CheckOut, nilIfZero(st.CheckInSource), nilIfZero(st.CheckOutSource),
				st.Status, st.IsLate, st.LateMinutes, st.IsEarlyLeave, st.EarlyLeaveMinutes,
				st.IsMissingCheckout, st.IsMissingCheckin, st.IsNoPunch,
				st.IsHalfDay, st.HalfDayLostMinutes, nilIfZero(st.HalfDayType), nilIfZero("Simulasi: "+spec.describe),
			); err != nil {
				log.Fatalf("absensi %s %s: %v", code, d.Format("2006-01-02"), err)
			}
		}
		emps = append(emps, e)
	}

	// ── payroll: generate everyone, keep only SIM lines ───────────────────────
	qtx := q.WithTx(tx)
	period, err := qtx.GetPayrollPeriodByMonth(ctx, pgDate(start))
	if err != nil {
		period, err = qtx.CreatePayrollPeriod(ctx, &db.CreatePayrollPeriodParams{
			PeriodMonth: pgDate(start), StartDate: pgDate(start), EndDate: pgDate(end),
		})
		if err != nil {
			log.Fatalf("buat periode penggajian: %v", err)
		}
	}
	if _, err := service.GenerateLines(ctx, qtx, period); err != nil {
		log.Fatalf("generate baris penggajian: %v", err)
	}
	for _, stmt := range []string{
		`DELETE FROM payroll_line_components WHERE payroll_line_id IN (
		   SELECT l.id FROM payroll_lines l JOIN employees e ON e.id = l.employee_id
		   WHERE l.payroll_period_id = $1 AND e.employee_code NOT LIKE '` + codePrefix + `%')`,
		`DELETE FROM payroll_lines l USING employees e
		 WHERE e.id = l.employee_id AND l.payroll_period_id = $1 AND e.employee_code NOT LIKE '` + codePrefix + `%'`,
	} {
		if _, err := tx.Exec(ctx, stmt, period.ID); err != nil {
			log.Fatalf("pangkas baris non-simulasi: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}

	report(ctx, pool, period.ID, emps, func(e simEmp) (string, string, string, int, int64) {
		return e.code, e.name, e.shift, e.expMin, e.expected
	})
	fmt.Printf("\nBuka HR → Penggajian → periode %s, lalu klik angka di kolom \"Telat/Pulang Awal\".\n", start.Format("2006-01"))
	fmt.Println("Kalkulator: HR → Penggajian → Simulasi Potongan. Hapus semua data: -clean")
}

func report[E any](ctx context.Context, pool *pgxpool.Pool, periodID pgtype.UUID, emps []E, fields func(E) (string, string, string, int, int64)) {
	fmt.Printf("%-11s %-36s %-5s %9s %13s %13s %13s  %s\n", "Kode", "Skenario", "Shift", "Menit", "Diharapkan", "Payroll", "Gaji Bersih", "")
	ok := true
	for _, e := range emps {
		code, name, shift, expMin, expected := fields(e)
		var minutes int32
		var cut, net int64
		if err := pool.QueryRow(ctx,
			`SELECT l.attendance_cut_minutes, l.attendance_cut_deduction, l.net_pay
			 FROM payroll_lines l JOIN employees e ON e.id = l.employee_id
			 WHERE l.payroll_period_id = $1 AND e.employee_code = $2`, periodID, code).Scan(&minutes, &cut, &net); err != nil {
			log.Fatalf("baca baris %s: %v", code, err)
		}
		mark := "✓"
		if int(minutes) != expMin || cut != expected {
			mark, ok = "✗ SELISIH", false
		}
		if shift == "" {
			shift = "—"
		}
		fmt.Printf("%-11s %-36s %-5s %9d %13s %13s %13s  %s\n", code, name, shift, minutes, rupiah(expected), rupiah(cut), rupiah(net), mark)
	}
	if ok {
		fmt.Println("\nSemua potongan payroll sesuai perhitungan manual.")
	} else {
		fmt.Println("\nADA SELISIH — periksa baris bertanda ✗.")
	}
}
