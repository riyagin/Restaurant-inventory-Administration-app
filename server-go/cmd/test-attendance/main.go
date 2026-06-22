// test-attendance seeds 5 test employees and runs attendance scenarios to verify
// the clock-in/clock-out logic end-to-end against the real database.
//
// Employees created:
//   TEST001, TEST002, TEST003  — Branch A (first branch alphabetically)
//   TEST004                    — Branch B (second branch)
//   TEST005                    — Branch C (third branch, or Branch B if only 2 exist)
//
// Scenarios run for today's date:
//   TEST001  Normal    check-in 07:55, check-out 17:05  → hadir, no anomalies
//   TEST002  Late      check-in 08:35, check-out 17:00  → hadir, terlambat 35 mnt
//   TEST003  Early     check-in 08:00, check-out 16:20  → hadir, pulang awal
//   TEST004  No out    check-in 08:00, no check-out     → hadir, tidak absen pulang
//   TEST005  Absent    no record (reconcile inserts it)  → absen
//
// Usage: go run ./server-go/cmd/test-attendance
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

// ── helpers ───────────────────────────────────────────────────────────────────

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		log.Fatalf("invalid UUID %q: %v", s, err)
	}
	return u
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

// todayAt returns a timestamp for today at hh:mm in local time.
func todayAt(hour, minute int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
}

// ── branch/position helpers ───────────────────────────────────────────────────

type branch struct {
	id   string
	name string
}

func fetchBranches(ctx context.Context, pool interface{ QueryRow(context.Context, string, ...any) interface{ Scan(...any) error }; Query(context.Context, string, ...any) (interface{ Next() bool; Scan(...any) error; Close() }, error) }) []branch {
	// use raw pool
	return nil // implemented inline below
}

func check(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

// ── result reporting ──────────────────────────────────────────────────────────

type result struct {
	code    string
	name    string
	branch  string
	status  string
	checkIn string
	checkOut string
	anomalies []string
}

func anomalyStr(s *service.AttendanceState) []string {
	var a []string
	if s.IsLate {
		a = append(a, fmt.Sprintf("Terlambat %d mnt", s.LateMinutes))
	}
	if s.IsEarlyLeave {
		a = append(a, fmt.Sprintf("Pulang Awal %d mnt", s.EarlyLeaveMinutes))
	}
	if s.IsMissingCheckout {
		a = append(a, "Tidak Absen Pulang")
	}
	return a
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.In(time.Local).Format("15:04")
}

func printResults(results []result) {
	sep := strings.Repeat("─", 80)
	fmt.Println(sep)
	fmt.Printf("%-8s  %-20s  %-12s  %-8s  %-6s  %-6s  %s\n",
		"Kode", "Nama", "Cabang", "Status", "Masuk", "Pulang", "Anomali")
	fmt.Println(sep)
	for _, r := range results {
		anomStr := "—"
		if len(r.anomalies) > 0 {
			anomStr = strings.Join(r.anomalies, ", ")
		}
		fmt.Printf("%-8s  %-20s  %-12s  %-8s  %-6s  %-6s  %s\n",
			r.code, r.name, r.branch, r.status, r.checkIn, r.checkOut, anomStr)
	}
	fmt.Println(sep)
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	cfg, err := config.Load()
	check(err, "load config")

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DBUrl)
	check(err, "connect db")
	defer pool.Close()

	q := db.New(pool)
	today := time.Now()
	todayPg := pgDate(today)

	// ── 1. fetch branches ────────────────────────────────────────────────────
	rows, err := pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name`)
	check(err, "fetch branches")
	var branches []branch
	for rows.Next() {
		var b branch
		check(rows.Scan(&b.id, &b.name), "scan branch")
		branches = append(branches, b)
	}
	rows.Close()
	if len(branches) < 2 {
		log.Fatalf("need at least 2 branches; found %d. Add branches first.", len(branches))
	}
	branchA := branches[0]
	branchB := branches[1]
	branchC := branches[len(branches)-1] // same as B if only 2 exist

	fmt.Printf("\n=== TEST ATTENDANCE SEED ===\n")
	fmt.Printf("Tanggal  : %s\n", today.Format("2006-01-02 (Monday)"))
	fmt.Printf("Cabang A : %s\n", branchA.name)
	fmt.Printf("Cabang B : %s\n", branchB.name)
	if branchC.id != branchB.id {
		fmt.Printf("Cabang C : %s\n", branchC.name)
	}
	fmt.Println()

	// ── 2. upsert position ───────────────────────────────────────────────────
	var posID string
	err = pool.QueryRow(ctx,
		`INSERT INTO positions (name, is_active) VALUES ('Staff Test', true)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
	).Scan(&posID)
	check(err, "upsert position")

	// ── 3. upsert 5 test employees ───────────────────────────────────────────
	type empSeed struct {
		code     string
		name     string
		branchID string
	}
	seeds := []empSeed{
		{"TEST001", "Andi Prasetyo",   branchA.id},
		{"TEST002", "Dewi Kusuma",     branchA.id},
		{"TEST003", "Rizky Firmansyah", branchA.id},
		{"TEST004", "Sari Wulandari",  branchB.id},
		{"TEST005", "Hendra Gunawan",  branchC.id},
	}

	type emp struct {
		id     string
		code   string
		name   string
		branch string
	}
	var emps []emp
	for _, s := range seeds {
		var id string
		err = pool.QueryRow(ctx,
			`INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, status)
			 VALUES ($1, $2, CURRENT_DATE, $3::uuid, $4::uuid, 'active')
			 ON CONFLICT (employee_code) DO UPDATE
			   SET full_name = EXCLUDED.full_name,
			       branch_id = EXCLUDED.branch_id,
			       status    = 'active'
			 RETURNING id`,
			s.code, s.name, posID, s.branchID,
		).Scan(&id)
		check(err, "upsert employee "+s.code)
		branchName := branchA.name
		if s.branchID == branchB.id {
			branchName = branchB.name
		}
		if s.branchID == branchC.id && branchC.id != branchB.id {
			branchName = branchC.name
		}
		emps = append(emps, emp{id: id, code: s.code, name: s.name, branch: branchName})
		fmt.Printf("[+] Karyawan %s (%s) di %s\n", s.name, s.code, branchName)
	}

	// ── 4. clear today's existing test records ────────────────────────────────
	empIDs := make([]string, len(emps))
	for i, e := range emps {
		empIDs[i] = e.id
	}
	_, err = pool.Exec(ctx,
		`DELETE FROM performance_violations
		 WHERE attendance_record_id IN (
		   SELECT id FROM attendance_records
		   WHERE employee_id = ANY($1::uuid[]) AND date = $2
		 )`,
		empIDs, todayPg.Time,
	)
	check(err, "clear violations")
	_, err = pool.Exec(ctx,
		`DELETE FROM attendance_records
		 WHERE employee_id = ANY($1::uuid[]) AND date = $2`,
		empIDs, todayPg.Time,
	)
	check(err, "clear records")

	// ── 5. default schedule (used when branch has no work_schedule row) ───────
	sched := service.DefaultSchedule() // 08:00–17:00, grace 15 min, early leave 30 min
	dayIsOver := true                  // simulate post-work-hours so missing-checkout fires

	// ── 6. run scenarios ──────────────────────────────────────────────────────
	fmt.Printf("\n=== RUNNING SCENARIOS ===\n")

	type scenario struct {
		empIdx   int
		label    string
		checkIn  *time.Time
		checkOut *time.Time
	}

	t := func(h, m int) *time.Time { v := todayAt(h, m); return &v }

	scenarios := []scenario{
		{0, "Normal (masuk tepat, pulang tepat)",    t(7, 55),  t(17, 5)},
		{1, "Terlambat (masuk 08:35)",               t(8, 35),  t(17, 0)},
		{2, "Pulang Awal (pulang 16:20)",            t(8, 0),   t(16, 20)},
		{3, "Tidak Absen Pulang (masuk saja)",       t(8, 0),   nil},
		// emp index 4 → no record (will be reconciled as absent)
	}

	var results []result
	for _, sc := range scenarios {
		e := emps[sc.empIdx]
		state := service.EmptyState()

		if sc.checkIn != nil {
			service.MergeAttendanceEvent(state, service.AttendanceEvent{
				Timestamp: *sc.checkIn,
				Source:    "face",
				Direction: "check_in",
			})
		}
		if sc.checkOut != nil {
			service.MergeAttendanceEvent(state, service.AttendanceEvent{
				Timestamp: *sc.checkOut,
				Source:    "face",
				Direction: "check_out",
			})
		}
		service.ComputeAnomalies(state, sched, dayIsOver)

		params := &db.InsertAttendanceRecordParams{
			EmployeeID: mustUUID(e.id),
			Date:       todayPg,
		}
		service.FillInsertParams(params, state)

		_, err = q.InsertAttendanceRecord(ctx, params)
		check(err, "insert record for "+e.code)

		results = append(results, result{
			code:      e.code,
			name:      e.name,
			branch:    e.branch,
			status:    state.Status,
			checkIn:   fmtTime(state.CheckIn),
			checkOut:  fmtTime(state.CheckOut),
			anomalies: anomalyStr(state),
		})
		fmt.Printf("[✓] %-22s → %s\n", e.name, sc.label)
	}

	// ── 7. reconcile (marks TEST005 as absent) ────────────────────────────────
	fmt.Printf("\n[~] Menjalankan rekonsiliasi untuk %s...\n", today.Format("2006-01-02"))
	recRes, err := service.ReconcileAbsent(ctx, q, today)
	check(err, "reconcile")
	fmt.Printf("[✓] Rekonsiliasi selesai: %d absen dibuat, %d dilewati\n",
		recRes.AbsentCreated, recRes.Skipped)

	// fetch TEST005's reconciled record
	e5 := emps[4]
	rec5, err := q.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{
		EmployeeID: mustUUID(e5.id),
		Date:       todayPg,
	})
	if err == nil {
		results = append(results, result{
			code:     e5.code,
			name:     e5.name,
			branch:   e5.branch,
			status:   rec5.Status,
			checkIn:  "—",
			checkOut: "—",
		})
	} else {
		results = append(results, result{
			code:   e5.code,
			name:   e5.name,
			branch: e5.branch,
			status: "(tidak ada record)",
		})
	}

	// ── 8. print summary ──────────────────────────────────────────────────────
	fmt.Printf("\n=== HASIL AKHIR (%s) ===\n", today.Format("2006-01-02"))
	printResults(results)

	// ── 9. verify anomaly flags from DB ──────────────────────────────────────
	fmt.Println("\n=== VERIFIKASI DARI DATABASE ===")
	dbRows, err := pool.Query(ctx,
		`SELECT e.employee_code, e.full_name,
		        ar.status, ar.check_in, ar.check_out,
		        ar.is_late, ar.late_minutes,
		        ar.is_early_leave, ar.early_leave_minutes,
		        ar.is_missing_checkout
		 FROM attendance_records ar
		 JOIN employees e ON e.id = ar.employee_id
		 WHERE ar.employee_id = ANY($1::uuid[]) AND ar.date = $2
		 ORDER BY e.employee_code`,
		empIDs, todayPg.Time,
	)
	check(err, "verify query")
	defer dbRows.Close()

	type dbRow struct {
		code, name, status    string
		checkIn, checkOut     pgtype.Timestamptz
		isLate                bool
		lateMin               int32
		isEarly               bool
		earlyMin              int32
		isMissingCheckout     bool
	}
	sep := strings.Repeat("─", 90)
	fmt.Println(sep)
	fmt.Printf("%-8s  %-20s  %-8s  %-5s  %-5s  %-6s  %-5s  %-6s  %s\n",
		"Kode", "Nama", "Status", "Masuk", "Pulang", "Late?", "LateM", "Early?", "NoOut?")
	fmt.Println(sep)
	for dbRows.Next() {
		var r dbRow
		err = dbRows.Scan(
			&r.code, &r.name, &r.status,
			&r.checkIn, &r.checkOut,
			&r.isLate, &r.lateMin,
			&r.isEarly, &r.earlyMin,
			&r.isMissingCheckout,
		)
		check(err, "scan verify row")
		inStr := "—"
		if r.checkIn.Valid {
			inStr = r.checkIn.Time.In(time.Local).Format("15:04")
		}
		outStr := "—"
		if r.checkOut.Valid {
			outStr = r.checkOut.Time.In(time.Local).Format("15:04")
		}
		fmt.Printf("%-8s  %-20s  %-8s  %-5s  %-5s  %-6v  %-5d  %-6v  %v\n",
			r.code, r.name, r.status, inStr, outStr,
			r.isLate, r.lateMin, r.isEarly, r.isMissingCheckout)
	}
	fmt.Println(sep)
	fmt.Println("\nSelesai. Buka dashboard absensi untuk melihat hasilnya.")
}
