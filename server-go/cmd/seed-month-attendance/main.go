// seed-month-attendance generates a full month of attendance data for every
// active employee so payroll generation has realistic inputs. Each employee is
// assigned one of 17 scenario patterns (round-robin by list index) chosen so that
// every attendance anomaly, every performance policy rule_type and every payroll
// deduction/addition path is exercised at least a few times.
//
//	 0 Normal            hadir penuh 08:00–17:00, tanpa anomali
//	 1 Telat ringan      check-in 08:25 → late 25m  (policy "Terlambat >15", 2 poin)
//	 2 Telat berat       check-in 09:45 → late 105m (policy "Terlambat >60", 5 poin)
//	                     + 08:30 → late 30m (tier 2 poin) di hari lain
//	 3 Pulang awal       out 15:30 → early 90m (2 poin); out 16:45 masih dalam toleransi
//	 4 Lupa absen pulang check-in saja               → is_missing_checkout (1 poin)
//	 5 Lupa absen masuk  check-out saja              → is_missing_checkin  (1 poin)
//	 6 Tidak absen       hadir tanpa punch sama sekali → is_no_punch       (2 poin)
//	 7 Absen tersebar    6 hari absen tidak berurutan → grace 4 hari, sisanya kena poin
//	 8 Absen beruntun    2 rentetan absen berturut-turut → consecutive_absent (5 poin)
//	 9 Setengah hari     half_day_late, masuk 12:00  → potongan jam + 5 poin
//	10 Setengah hari     half_day_early, pulang 12:00 → potongan jam + 5 poin
//	11 Lembur            pulang 20:00 + overtime_requests disetujui → overtime_hourly_amount
//	12 Kerja hari libur  hadir saat hari libur nasional → public_holiday_amount (×2)
//	13 Cuti sakit        3 hari cuti berbayar → tanpa potongan, tanpa pelanggaran
//	14 Cuti tanpa gaji   4 hari → unpaid_leave_deduction (4 × daily_rate)
//	15 Kasbon            kasbon 3 cicilan, 1 jatuh tempo bulan ini → kasbon_deduction
//	16 Campuran          telat + pulang awal + absen + lupa absen pulang + lembur
//
// Also (unless disabled by flags) seeds the wage-component catalog and attaches it
// to every open wage structure so the allowance / bonus / deduction, the
// per_present_day calc method and the min_score performance gate are all covered.
//
// Usage:
//
//	go run ./cmd/seed-month-attendance                  # current month
//	go run ./cmd/seed-month-attendance -month=2026-07
//	go run ./cmd/seed-month-attendance -components=false -payroll=false
//
// Reads DB credentials from server-go/.env. Re-running is safe: all data the
// seeder owns for the target month is deleted first.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

// wib is the wall-clock zone punches are recorded in, matching the RFC3339
// offsets the face/fingerprint devices send (+07:00).
var wib = time.FixedZone("WIB", 7*3600)

// ── small helpers ────────────────────────────────────────────────────────────

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: dateOnly(t), Valid: true}
}

// atTime returns `date` at hh:mm WIB.
func atTime(date time.Time, hour, min int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, min, 0, 0, wib)
}

func ptr(t time.Time) *time.Time { return &t }

func isoWeekday(t time.Time) int {
	if wd := int(t.Weekday()); wd == 0 {
		return 7
	} else {
		return wd
	}
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// ── model ────────────────────────────────────────────────────────────────────

type employee struct {
	id       string
	code     string
	name     string
	branch   string
	branchID string
	joinDate time.Time
	daily    int64
}

// dayRecord is one attendance_records row to write.
type dayRecord struct {
	date        time.Time
	status      string // present | absent | leave | holiday
	checkIn     *time.Time
	checkOut    *time.Time
	isHalfDay   bool
	halfDayType string // late | early
	note        string
}

// extras are the non-attendance rows a pattern needs (leave/overtime/kasbon).
type extras struct {
	leaveType     string // "" | "Sakit" | "Izin Tanpa Gaji"
	leaveStart    time.Time
	leaveEnd      time.Time
	leaveDays     int
	overtimeDates []time.Time
	overtimeHours float64
	kasbon        bool
}

type pattern struct {
	label string
	build func(days []time.Time, holidays map[string]bool) ([]dayRecord, extras)
}

// ── patterns ─────────────────────────────────────────────────────────────────

// normalDay is the baseline present record: 08:00 in, 17:00 out.
func normalDay(d time.Time) dayRecord {
	return dayRecord{date: d, status: "present", checkIn: ptr(atTime(d, 8, 0)), checkOut: ptr(atTime(d, 17, 0))}
}

func holidayDay(d time.Time) dayRecord {
	return dayRecord{date: d, status: "holiday"}
}

// eachDay walks the month's scheduled days, handing the builder a running index
// of *work* days (holidays excluded) so scenario offsets are stable regardless of
// where weekends and holidays fall.
func eachDay(days []time.Time, holidays map[string]bool, fn func(i int, d time.Time) dayRecord) []dayRecord {
	out := make([]dayRecord, 0, len(days))
	i := 0
	for _, d := range days {
		if holidays[d.Format("2006-01-02")] {
			out = append(out, holidayDay(d))
			continue
		}
		out = append(out, fn(i, d))
		i++
	}
	return out
}

func simple(label string, fn func(i int, d time.Time) dayRecord) pattern {
	return pattern{label: label, build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
		return eachDay(days, hol, fn), extras{}
	}}
}

// leavePattern marks `count` consecutive work days (starting at work-day index
// `from`) as leave and files the matching approved request.
func leavePattern(label, leaveType string, from, count int) pattern {
	return pattern{label: label, build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
		var leaveDates []time.Time
		recs := eachDay(days, hol, func(i int, d time.Time) dayRecord {
			if i >= from && i < from+count {
				leaveDates = append(leaveDates, d)
				return dayRecord{date: d, status: "leave"}
			}
			return normalDay(d)
		})
		ex := extras{}
		if len(leaveDates) > 0 {
			ex.leaveType = leaveType
			ex.leaveStart = leaveDates[0]
			ex.leaveEnd = leaveDates[len(leaveDates)-1]
			ex.leaveDays = len(leaveDates)
		}
		return recs, ex
	}}
}

func buildPatterns() []pattern {
	return []pattern{
		// 0 — clean baseline.
		simple("Normal (hadir penuh, tanpa anomali)", func(i int, d time.Time) dayRecord {
			return normalDay(d)
		}),

		// 1 — late past the 15-minute grace, below the 60-minute tier.
		simple("Telat ringan (08:25, tier 2 poin)", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%3 == 0 {
				r.checkIn = ptr(atTime(d, 8, 25))
			}
			return r
		}),

		// 2 — hits both late tiers so the highest-threshold selection is exercised.
		simple("Telat berat (09:45 tier 5 poin + 08:30 tier 2 poin)", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			switch i % 5 {
			case 0:
				r.checkIn = ptr(atTime(d, 9, 45))
			case 2:
				r.checkIn = ptr(atTime(d, 8, 30))
			}
			return r
		}),

		// 3 — one departure past the 30-minute early-leave tolerance, one inside it.
		simple("Pulang awal (15:30 kena, 16:45 masih toleransi)", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			switch i % 4 {
			case 1:
				r.checkOut = ptr(atTime(d, 15, 30))
			case 3:
				r.checkOut = ptr(atTime(d, 16, 45))
			}
			return r
		}),

		// 4 — check-in only.
		simple("Lupa absen pulang", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%4 == 2 {
				r.checkOut = nil
			}
			return r
		}),

		// 5 — check-out only.
		simple("Lupa absen masuk", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%5 == 3 {
				r.checkIn = nil
			}
			return r
		}),

		// 6 — present but never scanned (manual correction in real life).
		simple("Hadir tanpa absen sama sekali", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%6 == 4 {
				r.checkIn, r.checkOut = nil, nil
				r.note = "Koreksi manual: hadir tanpa scan"
			}
			return r
		}),

		// 7 — scattered absences: exercises the 4-day monthly absence grace.
		simple("Absen tersebar 6 hari (uji grace 4 hari)", func(i int, d time.Time) dayRecord {
			if contains([]int{2, 5, 9, 13, 18, 22}, i) {
				return dayRecord{date: d, status: "absent"}
			}
			return normalDay(d)
		}),

		// 8 — two back-to-back absence streaks → consecutive_absent fires twice.
		simple("Absen beruntun (2 rentetan berturut-turut)", func(i int, d time.Time) dayRecord {
			if contains([]int{4, 5, 6, 14, 15}, i) {
				return dayRecord{date: d, status: "absent"}
			}
			return normalDay(d)
		}),

		// 9 — half day, late arrival (4 lost hours).
		simple("Setengah hari - masuk siang 12:00", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%9 == 3 {
				r.checkIn = ptr(atTime(d, 12, 0))
				r.isHalfDay, r.halfDayType = true, "late"
				r.note = "Koreksi manual: setengah hari (masuk siang)"
			}
			return r
		}),

		// 10 — half day, early departure (5 lost hours).
		simple("Setengah hari - pulang siang 12:00", func(i int, d time.Time) dayRecord {
			r := normalDay(d)
			if i%9 == 5 {
				r.checkOut = ptr(atTime(d, 12, 0))
				r.isHalfDay, r.halfDayType = true, "early"
				r.note = "Koreksi manual: setengah hari (pulang siang)"
			}
			return r
		}),

		// 11 — formal overtime: late checkout plus an approved overtime request.
		{label: "Lembur (pulang 20:00, 3 jam disetujui)", build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
			var otDates []time.Time
			recs := eachDay(days, hol, func(i int, d time.Time) dayRecord {
				r := normalDay(d)
				if i%3 == 1 {
					r.checkOut = ptr(atTime(d, 20, 0))
					otDates = append(otDates, d)
				}
				return r
			})
			return recs, extras{overtimeDates: otDates, overtimeHours: 3}
		}},

		// 12 — works through the public holiday.
		{label: "Kerja saat hari libur nasional", build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
			out := make([]dayRecord, 0, len(days))
			for _, d := range days {
				out = append(out, normalDay(d)) // present even on the holiday
			}
			return out, extras{}
		}},

		// 13 — paid sick leave: no deduction, no violation.
		leavePattern("Cuti sakit berbayar (3 hari)", "Sakit", 6, 3),

		// 14 — unpaid leave: deducted at payroll.
		leavePattern("Cuti tanpa gaji (4 hari)", "Izin Tanpa Gaji", 8, 4),

		// 15 — kasbon installment due this month.
		{label: "Kasbon (cicilan jatuh tempo bulan ini)", build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
			return eachDay(days, hol, func(i int, d time.Time) dayRecord { return normalDay(d) }), extras{kasbon: true}
		}},

		// 16 — everything at once.
		{label: "Campuran (telat, pulang awal, absen, lembur)", build: func(days []time.Time, hol map[string]bool) ([]dayRecord, extras) {
			var otDates []time.Time
			recs := eachDay(days, hol, func(i int, d time.Time) dayRecord {
				if i%11 == 5 {
					return dayRecord{date: d, status: "absent"}
				}
				r := normalDay(d)
				switch i % 7 {
				case 0:
					r.checkIn = ptr(atTime(d, 8, 40))
				case 3:
					r.checkOut = ptr(atTime(d, 16, 0))
				case 6:
					r.checkOut = ptr(atTime(d, 19, 0))
					otDates = append(otDates, d)
				}
				if i%9 == 8 {
					r.checkOut = nil
				}
				return r
			})
			return recs, extras{overtimeDates: otDates, overtimeHours: 2}
		}},
	}
}

// ── wage component catalog (payroll coverage) ────────────────────────────────

type wageComponent struct {
	name       string
	typ        string
	calcMethod string
	minScore   *int
	amount     int64
}

func wageCatalog() []wageComponent {
	gate := 90
	return []wageComponent{
		{name: "Tunjangan Jabatan", typ: "allowance", calcMethod: "fixed", amount: 500_000},
		// Handed out in cash every day, so it is informational on the payslip only
		// (see wage_components.type 'daily_allowance') — never part of gross/net.
		{name: "Uang Makan", typ: "daily_allowance", calcMethod: "per_present_day", amount: 15_000},
		{name: "Tunjangan Kinerja", typ: "bonus", calcMethod: "fixed", minScore: &gate, amount: 300_000},
		{name: "Iuran BPJS", typ: "deduction", calcMethod: "fixed", amount: 120_000},
	}
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	monthFlag := flag.String("month", "", "target month YYYY-MM (default: current month)")
	seedComponents := flag.Bool("components", true, "seed the wage-component catalog and attach it to open wage structures")
	seedPayroll := flag.Bool("payroll", true, "create the payroll period for the month and generate its lines")
	flag.Parse()

	month := time.Now().UTC()
	if *monthFlag != "" {
		m, err := time.Parse("2006-01", *monthFlag)
		if err != nil {
			log.Fatalf("format -month tidak valid (YYYY-MM): %v", err)
		}
		month = m
	}
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)

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

	fmt.Printf("\n=== SEED ABSENSI %s ===\n", strings.ToUpper(start.Format("January 2006")))
	fmt.Printf("Periode: %s s/d %s\n\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	// ── leave types ───────────────────────────────────────────────────────────
	leaveTypeIDs := map[string]string{}
	for _, lt := range []struct {
		name            string
		paid, usesQuota bool
	}{
		{"Cuti Tahunan", true, true},
		{"Sakit", true, false},
		{"Izin Tanpa Gaji", false, false},
	} {
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO leave_types (name, is_paid, uses_quota, is_active)
			 VALUES ($1, $2, $3, true)
			 ON CONFLICT (name) DO UPDATE SET is_active = true
			 RETURNING id`, lt.name, lt.paid, lt.usesQuota).Scan(&id)
		if err != nil {
			log.Fatalf("upsert leave type %s: %v", lt.name, err)
		}
		leaveTypeIDs[lt.name] = id
	}
	fmt.Printf("[+] Tipe cuti: %d disiapkan\n", len(leaveTypeIDs))

	// ── public holiday (mid-month, on a work day) ─────────────────────────────
	holiday := time.Date(start.Year(), start.Month(), 15, 0, 0, 0, 0, time.UTC)
	for isoWeekday(holiday) == 7 {
		holiday = holiday.AddDate(0, 0, 1)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM public_holidays WHERE date BETWEEN $1 AND $2`, pgDate(start), pgDate(end)); err != nil {
		log.Fatalf("clear holidays: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO public_holidays (date, name) VALUES ($1, $2)`,
		pgDate(holiday), "Libur Nasional (Simulasi)"); err != nil {
		log.Fatalf("insert holiday: %v", err)
	}
	holidays := map[string]bool{holiday.Format("2006-01-02"): true}
	fmt.Printf("[+] Hari libur nasional: %s\n", holiday.Format("2006-01-02"))

	// ── employees ─────────────────────────────────────────────────────────────
	rows, err := pool.Query(ctx,
		`SELECT e.id::text, e.employee_code, e.full_name, COALESCE(b.name,'—'),
		        COALESCE(e.branch_id::text,''), e.join_date, COALESCE(w.daily_rate, 0)
		 FROM employees e
		 LEFT JOIN branches b ON b.id = e.branch_id
		 LEFT JOIN wage_structures w ON w.employee_id = e.id AND w.end_date IS NULL
		 WHERE e.status = 'active'
		 ORDER BY e.employee_code`)
	if err != nil {
		log.Fatalf("fetch employees: %v", err)
	}
	var emps []employee
	for rows.Next() {
		var e employee
		var join pgtype.Date
		if err := rows.Scan(&e.id, &e.code, &e.name, &e.branch, &e.branchID, &join, &e.daily); err != nil {
			log.Fatalf("scan employee: %v", err)
		}
		if join.Valid {
			e.joinDate = dateOnly(join.Time)
		}
		emps = append(emps, e)
	}
	rows.Close()
	if len(emps) == 0 {
		log.Fatal("tidak ada karyawan aktif di database")
	}
	fmt.Printf("[+] Karyawan aktif: %d\n", len(emps))

	empIDs := make([]string, len(emps))
	for i, e := range emps {
		empIDs[i] = e.id
	}

	// ── per-branch schedules ──────────────────────────────────────────────────
	schedules := map[string]service.Schedule{}
	srows, err := pool.Query(ctx, `SELECT branch_id::text, work_start, work_end, grace_minutes, early_leave_minutes, work_days FROM work_schedules`)
	if err != nil {
		log.Fatalf("fetch schedules: %v", err)
	}
	for srows.Next() {
		var branchID string
		var ws db.WorkSchedule
		if err := srows.Scan(&branchID, &ws.WorkStart, &ws.WorkEnd, &ws.GraceMinutes, &ws.EarlyLeaveMinutes, &ws.WorkDays); err != nil {
			log.Fatalf("scan schedule: %v", err)
		}
		schedules[branchID] = service.ScheduleFromRow(&ws)
	}
	srows.Close()
	schedFor := func(branchID string) service.Schedule {
		if s, ok := schedules[branchID]; ok {
			return s
		}
		return service.DefaultSchedule()
	}

	// ── wipe everything this seeder owns for the month ────────────────────────
	ids, from, to := empIDs, pgDate(start), pgDate(end)
	wipe := []struct {
		label, sql string
		args       []interface{}
	}{
		{"periode penggajian", `DELETE FROM payroll_periods WHERE period_month = $1`, []interface{}{from}},
		{"pelanggaran kinerja", `DELETE FROM performance_violations WHERE employee_id = ANY($1::uuid[]) AND date BETWEEN $2 AND $3`, []interface{}{ids, from, to}},
		{"skor kinerja", `DELETE FROM performance_scores WHERE employee_id = ANY($1::uuid[]) AND period_month = $2`, []interface{}{ids, from}},
		{"absensi", `DELETE FROM attendance_records WHERE employee_id = ANY($1::uuid[]) AND date BETWEEN $2 AND $3`, []interface{}{ids, from, to}},
		{"pengajuan cuti", `DELETE FROM leave_requests WHERE employee_id = ANY($1::uuid[]) AND start_date <= $3 AND end_date >= $2`, []interface{}{ids, from, to}},
		{"pengajuan lembur", `DELETE FROM overtime_requests WHERE employee_id = ANY($1::uuid[]) AND date BETWEEN $2 AND $3`, []interface{}{ids, from, to}},
		{"kasbon simulasi", `DELETE FROM kasbons WHERE kasbon_number LIKE 'KSB-SIM-%'`, nil},
	}
	for _, w := range wipe {
		if _, err := pool.Exec(ctx, w.sql, w.args...); err != nil {
			log.Fatalf("hapus %s: %v", w.label, err)
		}
	}
	fmt.Println("[~] Data lama bulan ini dihapus.")

	// ── wage components ───────────────────────────────────────────────────────
	if *seedComponents {
		for _, c := range wageCatalog() {
			var compID string
			err := pool.QueryRow(ctx,
				`INSERT INTO wage_components (name, type, is_fixed, is_active, calc_method, min_score)
				 VALUES ($1, $2, true, true, $3, $4)
				 ON CONFLICT (name) DO UPDATE
				   SET type = EXCLUDED.type, calc_method = EXCLUDED.calc_method,
				       min_score = EXCLUDED.min_score, is_active = true
				 RETURNING id`, c.name, c.typ, c.calcMethod, c.minScore).Scan(&compID)
			if err != nil {
				log.Fatalf("upsert wage component %s: %v", c.name, err)
			}
			if _, err := pool.Exec(ctx,
				`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount)
				 SELECT w.id, $1::uuid, $2 FROM wage_structures w WHERE w.end_date IS NULL
				 ON CONFLICT (wage_structure_id, wage_component_id) DO UPDATE SET amount = EXCLUDED.amount`,
				compID, c.amount); err != nil {
				log.Fatalf("attach wage component %s: %v", c.name, err)
			}
		}
		fmt.Printf("[+] Komponen gaji: %d dipasang ke semua struktur gaji aktif\n", len(wageCatalog()))
	}

	// a fund-source account for the simulated kasbons
	var kasbonAccountID string
	if err := pool.QueryRow(ctx,
		`SELECT id::text FROM accounts WHERE account_type = 'asset' ORDER BY account_number LIMIT 1`).Scan(&kasbonAccountID); err != nil {
		log.Printf("[!] Tidak ada akun aset untuk kasbon, pola kasbon dilewati: %v", err)
	}

	// ── insert per employee ───────────────────────────────────────────────────
	patterns := buildPatterns()

	var summaries []empSummary

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	for idx, e := range emps {
		pat := patterns[idx%len(patterns)]
		sched := schedFor(e.branchID)

		// Scheduled days for this employee: branch work days, from the later of the
		// month start and the join date.
		var days []time.Time
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			if !e.joinDate.IsZero() && d.Before(e.joinDate) {
				continue
			}
			if !contains(sched.WorkDays, isoWeekday(d)) {
				continue
			}
			days = append(days, d)
		}
		if len(days) == 0 {
			continue
		}

		recs, ex := pat.build(days, holidays)
		s := empSummary{code: e.code, name: e.name, branch: e.branch, pattern: pat.label}

		for _, r := range recs {
			st := &service.AttendanceState{
				Status:      r.status,
				CheckIn:     r.checkIn,
				CheckOut:    r.checkOut,
				IsHalfDay:   r.isHalfDay,
				HalfDayType: r.halfDayType,
			}
			if r.checkIn != nil {
				st.CheckInSource = "face"
			}
			if r.checkOut != nil {
				st.CheckOutSource = "face"
			}
			service.ComputeAnomalies(st, sched, true)
			// ComputeAnomalies clears the half-day fields (they only come from a manual
			// correction), so re-apply them and derive the lost minutes the same way the
			// corrections handler does.
			if r.isHalfDay {
				st.IsHalfDay, st.HalfDayType = true, r.halfDayType
				if r.halfDayType == "early" {
					st.HalfDayLostMinutes = service.ComputeEarlyLeaveLostMinutes(r.checkOut, sched)
				} else {
					st.HalfDayLostMinutes = service.ComputeLostMinutes(r.checkIn, sched)
				}
			}

			if _, err := tx.Exec(ctx,
				`INSERT INTO attendance_records
				   (employee_id, date, check_in, check_out, check_in_source, check_out_source,
				    status, is_late, late_minutes, is_early_leave, early_leave_minutes,
				    is_missing_checkout, is_missing_checkin, is_no_punch,
				    is_half_day, half_day_lost_minutes, half_day_type, note)
				 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
				e.id, pgDate(r.date), tsOrNil(st.CheckIn), tsOrNil(st.CheckOut),
				textOrNil(st.CheckInSource), textOrNil(st.CheckOutSource),
				st.Status, st.IsLate, st.LateMinutes, st.IsEarlyLeave, st.EarlyLeaveMinutes,
				st.IsMissingCheckout, st.IsMissingCheckin, st.IsNoPunch,
				st.IsHalfDay, st.HalfDayLostMinutes, textOrNil(st.HalfDayType), textOrNil(r.note),
			); err != nil {
				log.Fatalf("insert absensi %s %s: %v", e.code, r.date.Format("2006-01-02"), err)
			}

			switch st.Status {
			case "present":
				s.present++
			case "absent":
				s.absent++
			case "leave":
				s.leave++
			case "holiday":
				s.holidayDays++
			}
			if st.IsLate || st.IsEarlyLeave || st.IsMissingCheckout || st.IsMissingCheckin || st.IsNoPunch || st.IsHalfDay {
				s.anomalyDays++
			}
		}

		// Leave request backing the 'leave' days.
		if ex.leaveType != "" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO leave_requests (employee_id, leave_type_id, start_date, end_date, day_count, reason, status, decided_at)
				 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, 'approved', now())`,
				e.id, leaveTypeIDs[ex.leaveType], pgDate(ex.leaveStart), pgDate(ex.leaveEnd), ex.leaveDays,
				"Simulasi "+ex.leaveType); err != nil {
				log.Fatalf("insert cuti %s: %v", e.code, err)
			}
		}

		// Approved overtime requests (payroll sums these, not the raw check-outs).
		for _, d := range ex.overtimeDates {
			if _, err := tx.Exec(ctx,
				`INSERT INTO overtime_requests (employee_id, date, hours, reason, status, decided_at)
				 VALUES ($1::uuid, $2, $3, 'Simulasi lembur', 'approved', now())`,
				e.id, pgDate(d), ex.overtimeHours); err != nil {
				log.Fatalf("insert lembur %s: %v", e.code, err)
			}
		}

		// Kasbon: 3 000 000 over 3 monthly installments, the middle one due this month.
		if ex.kasbon && kasbonAccountID != "" {
			var kasbonID string
			if err := tx.QueryRow(ctx,
				`INSERT INTO kasbons (kasbon_number, employee_id, amount, details, sending_method,
				                      fund_source_account_id, request_date, resolution_month, status, processed_at)
				 VALUES ($1, $2::uuid, 3000000, 'Simulasi kasbon', 'transfer', $3::uuid, $4, $5, 'processed', now())
				 RETURNING id::text`,
				"KSB-SIM-"+e.code, e.id, kasbonAccountID,
				pgDate(start.AddDate(0, -1, 0)), pgDate(start.AddDate(0, 2, 0))).Scan(&kasbonID); err != nil {
				log.Fatalf("insert kasbon %s: %v", e.code, err)
			}
			for n := 0; n < 3; n++ {
				due := start.AddDate(0, n-1, 0)
				status := "pending"
				if n == 0 { // previous month already deducted
					status = "deducted"
				}
				if _, err := tx.Exec(ctx,
					`INSERT INTO kasbon_installments (kasbon_id, due_month, amount, status)
					 VALUES ($1::uuid, $2, 1000000, $3)`, kasbonID, pgDate(due), status); err != nil {
					log.Fatalf("insert cicilan kasbon %s: %v", e.code, err)
				}
			}
		}

		summaries = append(summaries, s)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}
	fmt.Println("[+] Absensi, cuti, lembur & kasbon tersimpan.")

	// ── performance evaluation ────────────────────────────────────────────────
	if err := service.EvaluateRange(ctx, q, start, end); err != nil {
		log.Fatalf("evaluasi kinerja: %v", err)
	}
	fmt.Println("[+] Evaluasi kinerja dijalankan (pelanggaran + skor bulanan).")

	// ── payroll period ────────────────────────────────────────────────────────
	if *seedPayroll {
		period, err := q.CreatePayrollPeriod(ctx, &db.CreatePayrollPeriodParams{
			PeriodMonth: pgDate(start),
			StartDate:   pgDate(start),
			EndDate:     pgDate(end),
		})
		if err != nil {
			log.Fatalf("buat periode penggajian: %v", err)
		}
		ptx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatalf("begin payroll tx: %v", err)
		}
		res, err := service.GenerateLines(ctx, q.WithTx(ptx), period)
		if err != nil {
			_ = ptx.Rollback(ctx)
			log.Fatalf("generate baris penggajian: %v", err)
		}
		if err := ptx.Commit(ctx); err != nil {
			log.Fatalf("commit payroll: %v", err)
		}
		fmt.Printf("[+] Periode penggajian %s dibuat — %d baris (%d dilewati).\n",
			start.Format("2006-01"), res.Created, len(res.SkippedNames))
	}

	printSummary(summaries)
	printPolicyReport(ctx, pool, start, end)
}

// ── null helpers for the raw inserts ─────────────────────────────────────────

func tsOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

func textOrNil(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ── reporting ────────────────────────────────────────────────────────────────

// empSummary is one row of the per-employee run report.
type empSummary struct {
	code, name, branch, pattern                      string
	present, absent, leave, holidayDays, anomalyDays int
}

func printSummary(summaries []empSummary) {
	sep := strings.Repeat("─", 118)
	fmt.Printf("\n=== RINGKASAN PER KARYAWAN ===\n%s\n", sep)
	fmt.Printf("%-8s %-24s %-14s %6s %6s %5s %6s %8s  %s\n",
		"Kode", "Nama", "Cabang", "Hadir", "Absen", "Cuti", "Libur", "Anomali", "Pola")
	fmt.Println(sep)
	clip := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	for _, s := range summaries {
		fmt.Printf("%-8s %-24s %-14s %6d %6d %5d %6d %8d  %s\n",
			s.code, clip(s.name, 24), clip(s.branch, 14),
			s.present, s.absent, s.leave, s.holidayDays, s.anomalyDays, s.pattern)
	}
	fmt.Println(sep)
}

// printPolicyReport shows how many violations each policy produced, so a run can
// be checked at a glance for rules that never fired.
func printPolicyReport(ctx context.Context, pool interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
}, start, end time.Time) {
	fmt.Printf("\n=== PELANGGARAN PER KEBIJAKAN ===\n")
	rows, err := pool.Query(ctx,
		`SELECT p.rule_type, p.name, count(v.id), COALESCE(sum(v.points), 0)
		 FROM performance_policies p
		 LEFT JOIN performance_violations v
		   ON v.policy_id = p.id AND v.date BETWEEN $1 AND $2
		 WHERE p.is_active
		 GROUP BY p.id, p.rule_type, p.name
		 ORDER BY p.rule_type, p.name`, pgDate(start), pgDate(end))
	if err != nil {
		fmt.Println("  gagal membaca pelanggaran:", err)
		return
	}
	defer rows.Close()
	type line struct {
		rule, name   string
		count, total int64
	}
	var lines []line
	for rows.Next() {
		var l line
		if err := rows.Scan(&l.rule, &l.name, &l.count, &l.total); err != nil {
			fmt.Println("  ", err)
			return
		}
		lines = append(lines, l)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].rule < lines[j].rule })
	for _, l := range lines {
		mark := "✓"
		if l.count == 0 {
			mark = "·"
		}
		fmt.Printf("  %s %-20s %-52s %5d kejadian, %5d poin\n", mark, l.rule, l.name, l.count, l.total)
	}
	fmt.Println("\n('·' = kebijakan tidak terpicu — untuk rule 'manual' ini normal, pelanggaran manual diinput HR.)")
}
