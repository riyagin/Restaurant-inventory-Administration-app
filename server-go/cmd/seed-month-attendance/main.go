// seed-month-attendance generates a full month of attendance data for June 2026
// covering all active employees so the payroll generation has realistic inputs.
//
// Scenarios distributed across employees (by index mod 5):
//
//	Pattern 0 — Normal       : hadir all working days, 08:00–17:00, no anomalies
//	Pattern 1 — Overtime     : hadir all days, checkout 19:00 on Tue/Thu → overtime_hours
//	Pattern 2 — Unpaid Leave : hadir most days; 3-day approved unpaid leave Jun 9–11 → unpaid_leave_deduction
//	Pattern 3 — Holiday Work : present on Jun 1 public holiday + normal rest of month → public_holiday_amount
//	Pattern 4 — Mixed        : late some days, 2 absent days, checkout 18:30 Fridays
//
// June 1 2026 is seeded as a public holiday (Hari Pancasila) so that
// pattern 3 exercises CountPresentOnHolidays in payroll generation.
//
// Usage: go run ./server-go/cmd/seed-month-attendance
// Reads DB credentials from server-go/.env
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

const seedMonth = "2026-06"

// ── date helpers ──────────────────────────────────────────────────────────────

func dateOf(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: dateOf(t.Year(), int(t.Month()), t.Day()), Valid: true}
}

func pgTS(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		log.Fatalf("invalid UUID %q: %v", s, err)
	}
	return u
}

// atTime returns the given date at hh:mm UTC+7 (WIB), stored as UTC.
func atTime(date time.Time, hour, min int) time.Time {
	wib := time.FixedZone("WIB", 7*3600)
	return time.Date(date.Year(), date.Month(), date.Day(), hour, min, 0, 0, wib)
}

// workingDays returns all 30 days of June 2026 (Sun–Sat, 7-day work week).
func workingDays() []time.Time {
	var days []time.Time
	for d := 1; d <= 30; d++ {
		days = append(days, dateOf(2026, 6, d))
	}
	return days
}

var holidayDate = dateOf(2026, 6, 1) // Hari Pancasila — seeded below

// isHoliday reports whether d is Jun 1 (our seeded public holiday).
func isHoliday(d time.Time) bool {
	return d.Year() == holidayDate.Year() &&
		d.Month() == holidayDate.Month() &&
		d.Day() == holidayDate.Day()
}

// ── pattern builders ──────────────────────────────────────────────────────────

type dayRecord struct {
	date     time.Time
	checkIn  *time.Time
	checkOut *time.Time
	source   string
	status   string // "" = derive from merge
}

// pattern0 — normal: 08:00 in, 17:00 out every working day (skip holiday).
func pattern0(days []time.Time) []dayRecord {
	var out []dayRecord
	for _, d := range days {
		if isHoliday(d) {
			out = append(out, dayRecord{date: d, status: "holiday"})
			continue
		}
		in := atTime(d, 8, 0)
		co := atTime(d, 17, 0)
		out = append(out, dayRecord{date: d, checkIn: &in, checkOut: &co, source: "face"})
	}
	return out
}

// pattern1 — overtime: normal hours, but checkout 19:00 on Tuesdays and Thursdays.
func pattern1(days []time.Time) []dayRecord {
	var out []dayRecord
	for _, d := range days {
		if isHoliday(d) {
			out = append(out, dayRecord{date: d, status: "holiday"})
			continue
		}
		in := atTime(d, 8, 0)
		wd := int(d.Weekday())
		coHour := 17
		if wd == 2 || wd == 4 { // Tue, Thu
			coHour = 19
		}
		co := atTime(d, coHour, 0)
		out = append(out, dayRecord{date: d, checkIn: &in, checkOut: &co, source: "face"})
	}
	return out
}

// pattern2 — unpaid leave Jun 9–11 (Tue–Thu); absent those days; present rest.
// Leave request is inserted separately.
func pattern2(days []time.Time) []dayRecord {
	leaveRange := map[int]bool{9: true, 10: true, 11: true}
	var out []dayRecord
	for _, d := range days {
		if isHoliday(d) {
			out = append(out, dayRecord{date: d, status: "holiday"})
			continue
		}
		if leaveRange[d.Day()] {
			out = append(out, dayRecord{date: d, status: "leave"})
			continue
		}
		in := atTime(d, 8, 5)
		co := atTime(d, 17, 0)
		out = append(out, dayRecord{date: d, checkIn: &in, checkOut: &co, source: "face"})
	}
	return out
}

// pattern3 — holiday work: present on Jun 1, normal rest of month.
func pattern3(days []time.Time) []dayRecord {
	var out []dayRecord
	for _, d := range days {
		in := atTime(d, 8, 0)
		co := atTime(d, 17, 0)
		// Present even on the public holiday.
		out = append(out, dayRecord{date: d, checkIn: &in, checkOut: &co, source: "face"})
	}
	return out
}

// pattern4 — mixed: 2 absent days (Jun 15, Jun 22), late on Mondays, 18:30 out Fridays.
func pattern4(days []time.Time) []dayRecord {
	absentDays := map[int]bool{15: true, 22: true}
	var out []dayRecord
	for _, d := range days {
		if isHoliday(d) {
			out = append(out, dayRecord{date: d, status: "holiday"})
			continue
		}
		if absentDays[d.Day()] {
			out = append(out, dayRecord{date: d, status: "absent"})
			continue
		}
		wd := int(d.Weekday())
		inHour, inMin := 8, 0
		if wd == 1 { // Monday — late
			inHour, inMin = 8, 35
		}
		coHour, coMin := 17, 0
		if wd == 5 { // Friday — slight overtime
			coHour, coMin = 18, 30
		}
		in := atTime(d, inHour, inMin)
		co := atTime(d, coHour, coMin)
		out = append(out, dayRecord{date: d, checkIn: &in, checkOut: &co, source: "face"})
	}
	return out
}

var patternFuncs = []func([]time.Time) []dayRecord{
	pattern0, pattern1, pattern2, pattern3, pattern4,
}

var patternLabels = []string{
	"Normal (hadir penuh, jam standar)",
	"Overtime (lembur Sel & Kam 19:00)",
	"Cuti Tak Berbayar (Jun 9–11)",
	"Hadir Hari Libur (Jun 1 Pancasila)",
	"Mixed (2 absen, telat Senin, lembur Jum)",
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
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
	sched := service.DefaultSchedule()
	days := workingDays()

	fmt.Printf("\n=== SEED ABSENSI BULAN JUNI 2026 ===\n")
	fmt.Printf("Hari kerja (7 hari/minggu): %d hari\n\n", len(days))

	// ── 1. seed Jun 1 as public holiday ──────────────────────────────────────
	_, err = pool.Exec(ctx,
		`INSERT INTO public_holidays (id, date, name)
		 VALUES (gen_random_uuid(), $1::date, $2)
		 ON CONFLICT (date) DO UPDATE SET name = EXCLUDED.name`,
		"2026-06-01", "Hari Pancasila",
	)
	if err != nil {
		log.Fatalf("insert holiday: %v", err)
	}
	fmt.Println("[+] Hari libur: Hari Pancasila (1 Jun 2026)")

	// ── 2. upsert unpaid leave type ───────────────────────────────────────────
	var unpaidLeaveTypeID string
	err = pool.QueryRow(ctx,
		`INSERT INTO leave_types (id, name, is_paid, uses_quota, is_active)
		 VALUES (gen_random_uuid(), 'Cuti Tidak Berbayar', false, false, true)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
	).Scan(&unpaidLeaveTypeID)
	if err != nil {
		log.Fatalf("upsert unpaid leave type: %v", err)
	}
	fmt.Printf("[+] Tipe cuti: Cuti Tidak Berbayar (id=%s)\n\n", unpaidLeaveTypeID)

	// ── 3. fetch all active employees ─────────────────────────────────────────
	rows, err := pool.Query(ctx,
		`SELECT e.id, e.employee_code, e.full_name, COALESCE(b.name,'—') AS branch
		 FROM employees e
		 LEFT JOIN branches b ON b.id = e.branch_id
		 WHERE e.status = 'active'
		 ORDER BY b.name, e.employee_code`,
	)
	if err != nil {
		log.Fatalf("fetch employees: %v", err)
	}
	type emp struct{ id, code, name, branch string }
	var emps []emp
	for rows.Next() {
		var e emp
		if err := rows.Scan(&e.id, &e.code, &e.name, &e.branch); err != nil {
			log.Fatalf("scan emp: %v", err)
		}
		emps = append(emps, e)
	}
	rows.Close()

	if len(emps) == 0 {
		log.Fatal("tidak ada karyawan aktif di database")
	}
	fmt.Printf("Karyawan aktif: %d\n\n", len(emps))

	// ── 4. collect all employee IDs for cleanup ───────────────────────────────
	empIDs := make([]string, len(emps))
	for i, e := range emps {
		empIDs[i] = e.id
	}

	// Delete existing June 2026 records for all active employees.
	_, err = pool.Exec(ctx,
		`DELETE FROM performance_violations
		 WHERE attendance_record_id IN (
		   SELECT id FROM attendance_records
		   WHERE employee_id = ANY($1::uuid[])
		     AND date >= '2026-06-01' AND date <= '2026-06-30'
		 )`,
		empIDs,
	)
	if err != nil {
		log.Fatalf("clear violations: %v", err)
	}
	_, err = pool.Exec(ctx,
		`DELETE FROM attendance_records
		 WHERE employee_id = ANY($1::uuid[])
		   AND date >= '2026-06-01' AND date <= '2026-06-30'`,
		empIDs,
	)
	if err != nil {
		log.Fatalf("clear attendance: %v", err)
	}
	// Clear leave requests for pattern2 employees in June.
	_, err = pool.Exec(ctx,
		`DELETE FROM leave_requests
		 WHERE employee_id = ANY($1::uuid[])
		   AND leave_type_id = $2::uuid
		   AND start_date >= '2026-06-01' AND end_date <= '2026-06-30'`,
		empIDs, unpaidLeaveTypeID,
	)
	if err != nil {
		log.Fatalf("clear leave requests: %v", err)
	}
	fmt.Println("[~] Data Juni lama dihapus.")

	// ── 5. insert records per employee ────────────────────────────────────────
	type summary struct {
		code, name, branch, pattern string
		present, absent, leave, holiday, overtime int
	}
	var summaries []summary

	for i, e := range emps {
		pat := i % len(patternFuncs)
		label := patternLabels[pat]
		records := patternFuncs[pat](days)

		var nPresent, nAbsent, nLeave, nHoliday, nOvertime int
		empUUID := mustUUID(e.id)

		for _, rec := range records {
			d := pgDate(rec.date)

			if rec.status == "absent" {
				_, err = pool.Exec(ctx,
					`INSERT INTO attendance_records (id, employee_id, date, status)
					 VALUES (gen_random_uuid(), $1, $2, 'absent')`,
					e.id, d.Time,
				)
				if err != nil {
					log.Fatalf("insert absent %s %s: %v", e.code, rec.date.Format("01-02"), err)
				}
				nAbsent++
				continue
			}

			if rec.status == "leave" {
				_, err = pool.Exec(ctx,
					`INSERT INTO attendance_records (id, employee_id, date, status)
					 VALUES (gen_random_uuid(), $1, $2, 'leave')`,
					e.id, d.Time,
				)
				if err != nil {
					log.Fatalf("insert leave %s %s: %v", e.code, rec.date.Format("01-02"), err)
				}
				nLeave++
				continue
			}

			if rec.status == "holiday" {
				_, err = pool.Exec(ctx,
					`INSERT INTO attendance_records (id, employee_id, date, status)
					 VALUES (gen_random_uuid(), $1, $2, 'holiday')`,
					e.id, d.Time,
				)
				if err != nil {
					log.Fatalf("insert holiday %s %s: %v", e.code, rec.date.Format("01-02"), err)
				}
				nHoliday++
				continue
			}

			// Present — run through service logic for proper anomaly flags.
			state := service.EmptyState()
			if rec.checkIn != nil {
				service.MergeAttendanceEvent(state, service.AttendanceEvent{
					Timestamp: *rec.checkIn,
					Source:    rec.source,
					Direction: "check_in",
				})
			}
			if rec.checkOut != nil {
				service.MergeAttendanceEvent(state, service.AttendanceEvent{
					Timestamp: *rec.checkOut,
					Source:    rec.source,
					Direction: "check_out",
				})
			}
			dayIsOver := time.Now().After(rec.date.Add(time.Duration(sched.WorkEndMinutes) * time.Minute))
			service.ComputeAnomalies(state, sched, dayIsOver)

			params := &db.InsertAttendanceRecordParams{
				EmployeeID: empUUID,
				Date:       d,
			}
			service.FillInsertParams(params, state)

			_, err = q.InsertAttendanceRecord(ctx, params)
			if err != nil {
				log.Fatalf("insert present %s %s: %v", e.code, rec.date.Format("01-02"), err)
			}
			nPresent++
			if rec.checkOut != nil && service.ComputeOvertimeMinutes(rec.checkOut, sched) > 0 {
				nOvertime++
			}
		}

		// Insert approved unpaid leave request for pattern2 employees.
		if pat == 2 {
			_, err = pool.Exec(ctx,
				`INSERT INTO leave_requests (id, employee_id, leave_type_id, start_date, end_date, day_count, reason, status)
				 VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '2026-06-09', '2026-06-11', 3, 'Keperluan keluarga', 'approved')`,
				e.id, unpaidLeaveTypeID,
			)
			if err != nil {
				log.Fatalf("insert leave request %s: %v", e.code, err)
			}
		}

		_ = empUUID
		summaries = append(summaries, summary{
			code: e.code, name: e.name, branch: e.branch, pattern: label,
			present: nPresent, absent: nAbsent, leave: nLeave, holiday: nHoliday, overtime: nOvertime,
		})
		fmt.Printf("[✓] %-10s %-22s  pola %d\n", e.code, e.name, pat)
	}

	// ── 6. print summary ──────────────────────────────────────────────────────
	sep := strings.Repeat("─", 105)
	fmt.Printf("\n=== RINGKASAN ABSENSI JUNI 2026 ===\n%s\n", sep)
	fmt.Printf("%-10s %-22s %-16s  %6s %6s %5s %7s %8s  Pola\n",
		"Kode", "Nama", "Cabang", "Hadir", "Absen", "Cuti", "Libur", "OT Days")
	fmt.Println(sep)
	for _, s := range summaries {
		branch := s.branch
		if len(branch) > 15 {
			branch = branch[:15]
		}
		name := s.name
		if len(name) > 21 {
			name = name[:21]
		}
		fmt.Printf("%-10s %-22s %-16s  %6d %6d %5d %7d %8d  %s\n",
			s.code, name, branch,
			s.present, s.absent, s.leave, s.holiday, s.overtime,
			s.pattern)
	}
	fmt.Println(sep)

	// Totals.
	total := func(fn func(s summary) int) int {
		n := 0
		for _, s := range summaries {
			n += fn(s)
		}
		return n
	}
	fmt.Printf("%-51s  %6d %6d %5d %7d %8d\n", "TOTAL",
		total(func(s summary) int { return s.present }),
		total(func(s summary) int { return s.absent }),
		total(func(s summary) int { return s.leave }),
		total(func(s summary) int { return s.holiday }),
		total(func(s summary) int { return s.overtime }),
	)
	fmt.Println(sep)

	fmt.Printf(`
=== SKENARIO PAYROLL YANG TERCAKUP ===
  • overtime_hourly_amount : karyawan pola 1 & 4 (checkout lewat 17:00)
  • public_holiday_amount  : karyawan pola 3 (hadir 1 Jun - Hari Pancasila)
  • unpaid_leave_deduction : karyawan pola 2 (cuti tak berbayar 9-11 Jun, 3 hari)
  • anomali kehadiran       : karyawan pola 4 (terlambat Senin, 2 hari absen)

Buat periode penggajian Juni 2026 di dashboard lalu klik "Generate Lines".
`)
}
