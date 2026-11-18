// sample-payslips builds four example payslips in the app database, through
// the app's real code paths, and writes them as PDFs:
//
//	CONTOH-01 Rina  — clean month: no violation, Tier A, every benefit paid
//	CONTOH-02 Bayu  — lateness in every tier (1–10, 11–20, 21–30, >30 min),
//	                  Izin Masuk Siang and Izin Pulang Lebih Awal
//	CONTOH-03 Sari  — Izin Satu Hari on a Wednesday and a Saturday, sick leave
//	                  without a doctor's note, a missed check-out and a missed
//	                  check-in
//	CONTOH-04 Dodi  — five Alpha days (two in a row), leaving work without
//	                  permission, a day with no punches at all
//
// Everything hangs off one branch, "Contoh Slip Gaji", and wage components
// suffixed "(Contoh)", so it never mixes with real data; -clean removes it all.
// The lines go into the month's payroll period, which must exist and be open
// (the slips are rendered without closing it, so nothing is posted to the
// ledger). The grade scale is set to Kebijakan HRD's: A ≥ 90 pays everything,
// B ≥ 70 pays Service Charge at 50% and nothing else, C pays nothing.
//
// Usage (from server-go/):
//
//	go run ./cmd/sample-payslips -month=2026-10 -out "../../contoh-slip-gaji"
//	go run ./cmd/sample-payslips -clean
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

const (
	branchName = "Contoh Slip Gaji"
	codePrefix = "CONTOH-"
	suffix     = " (Contoh)"
	dailyRate  = int64(100_000)
	baseSalary = int64(2_600_000)
	mealRate   = int64(25_000)
)

type person struct {
	code, name, file, story string
	supervisor              bool
}

var people = []person{
	{"CONTOH-01", "Rina Kusuma", "01-bersih", "Bulan bersih: tidak ada pelanggaran", true},
	{"CONTOH-02", "Bayu Saputra", "02-terlambat-dan-izin-setengah-hari", "Terlambat di setiap tingkat + izin masuk siang + izin pulang awal", true},
	{"CONTOH-03", "Sari Wulandari", "03-izin-sakit-dan-lupa-absen", "Izin satu hari (Rabu & Sabtu), sakit tanpa surat dokter, tidak check-out, tidak check-in", false},
	{"CONTOH-04", "Dodi Firmansyah", "04-alpha-dan-pelanggaran-manual", "5× Alpha (2 berturut-turut), keluar kantor tanpa izin, tidak check-in & check-out", false},
}

type app struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	q      *db.Queries
	router http.Handler
	slip   *handler.PayslipHandler
	emp    map[string]pgtype.UUID
	comp   map[string]pgtype.UUID
	fails  int
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (a *app) exec(sql string, args ...any) {
	if _, err := a.pool.Exec(a.ctx, sql, args...); err != nil {
		log.Fatalf("%v\n%s", err, sql)
	}
}

func (a *app) call(method, path string, body any) (int, map[string]any, string) {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, strings.TrimSpace(rec.Body.String())
}

func (a *app) expect(what string, want, got any) {
	w, g := fmt.Sprint(want), fmt.Sprint(got)
	mark := "ok  "
	if w != g {
		mark = "BEDA"
		a.fails++
	}
	fmt.Printf("    [%s] %-44s want %-12s got %s\n", mark, what, w, g)
}

func uid(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }

var wib = time.FixedZone("WIB", 7*3600)

func at(d time.Time, hh, mm int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, wib)
}

func main() {
	monthFlag := flag.String("month", "2026-10", "payroll month YYYY-MM (its period must exist and be open)")
	out := flag.String("out", "../../contoh-slip-gaji", "output folder for the PDFs")
	clean := flag.Bool("clean", false, "remove every sample row and exit")
	flag.Parse()

	ctx := context.Background()
	cfg, err := config.Load()
	must(err)
	pool, err := pgxpool.New(ctx, cfg.DBUrl)
	must(err)
	defer pool.Close()
	a := &app{ctx: ctx, pool: pool, q: db.New(pool), emp: map[string]pgtype.UUID{}, comp: map[string]pgtype.UUID{}}
	a.buildRouter(cfg.UploadsDir)

	a.cleanAll()
	if *clean {
		fmt.Println("Semua data contoh dihapus.")
		return
	}

	month, err := time.Parse("2006-01", *monthFlag)
	must(err)
	period, err := a.q.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: month, Valid: true})
	if err != nil {
		log.Fatalf("periode gaji %s tidak ditemukan — buat dulu periodenya", *monthFlag)
	}
	if period.Status != "open" {
		log.Fatalf("periode gaji %s sudah %s; contoh hanya dibuat di periode yang masih terbuka", *monthFlag, period.Status)
	}
	from, to := period.StartDate.Time, period.EndDate.Time
	fmt.Printf("Periode %s: %s s/d %s\n", *monthFlag, from.Format("2006-01-02"), to.Format("2006-01-02"))

	fmt.Println("\n[1] Cabang, komponen gaji, skala grade, karyawan")
	a.seedBase()
	fmt.Println("[2] Absensi & evaluasi poin")
	a.seedAttendance(from, to)
	a.evaluate(from, to)
	fmt.Println("[3] Libur / izin / sakit lewat handler pengajuan")
	a.fileLeaves()
	a.manualViolations()
	fmt.Println("[4] Baris gaji (regenerate) + catatan")
	lines := a.payroll(period)
	fmt.Println("[5] Pemeriksaan angka")
	a.verify(lines)
	fmt.Println("[6] PDF")
	must(os.MkdirAll(*out, 0o755))
	a.render(lines, *out)
	writeReadme(*out, *monthFlag)

	if a.fails > 0 {
		fmt.Printf("\n%d angka berbeda dari perhitungan manual — periksa di atas.\n", a.fails)
		os.Exit(1)
	}
	fmt.Printf("\nSelesai. PDF di %s\n", *out)
}

func (a *app) buildRouter(uploads string) {
	r := chi.NewRouter()
	claims := &middleware.Claims{UserID: uuid.Nil, Username: "contoh-slip", Role: middleware.RoleSuperuser}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(middleware.ContextWithClaims(r.Context(), claims)))
		})
	})
	wh := handler.NewHRWagesHandler(a.pool, a.q)
	r.Put("/grades", wh.SaveGrades)
	lh := handler.NewLeaveHandler(a.pool, a.q)
	r.Post("/leave", lh.CreateLeaveRequest)
	r.Post("/leave/{id}/approve", lh.ApproveLeaveRequest)
	ph := handler.NewPayrollHandler(a.pool, a.q)
	r.Post("/periods/{id}/regenerate-line/{employeeId}", ph.RegenerateLine)
	r.Post("/lines/{id}/review", ph.ReviewLine)
	a.slip = handler.NewPayslipHandler(a.pool, a.q)
	a.slip.SetUploadsDir(uploads)
	a.router = r
}

// cleanAll deletes everything the tool created, in foreign-key order.
func (a *app) cleanAll() {
	emps := `(SELECT id FROM employees WHERE employee_code LIKE '` + codePrefix + `%')`
	for _, sql := range []string{
		`DELETE FROM payroll_lines WHERE employee_id IN ` + emps,
		`DELETE FROM performance_violations WHERE employee_id IN ` + emps,
		`DELETE FROM performance_scores WHERE employee_id IN ` + emps,
		`DELETE FROM attendance_permissions WHERE employee_id IN ` + emps,
		`DELETE FROM attendance_punches WHERE employee_id IN ` + emps,
		`DELETE FROM attendance_records WHERE employee_id IN ` + emps,
		`DELETE FROM leave_quota_usage WHERE employee_id IN ` + emps,
		`DELETE FROM leave_requests WHERE employee_id IN ` + emps,
		`DELETE FROM employee_wage_components WHERE wage_structure_id IN (SELECT id FROM wage_structures WHERE employee_id IN ` + emps + `)`,
		`DELETE FROM wage_structures WHERE employee_id IN ` + emps,
		`DELETE FROM employees WHERE employee_code LIKE '` + codePrefix + `%'`,
		`DELETE FROM work_schedules WHERE branch_id IN (SELECT id FROM branches WHERE name = '` + branchName + `')`,
		`DELETE FROM branches WHERE name = '` + branchName + `'`,
		`DELETE FROM positions WHERE name LIKE '%` + suffix + `'`,
		`DELETE FROM wage_components WHERE name LIKE '%` + suffix + `'`,
	} {
		a.exec(sql)
	}
}

func (a *app) seedBase() {
	var branch, crew, sup pgtype.UUID
	must(a.pool.QueryRow(a.ctx, `INSERT INTO branches (name) VALUES ($1) RETURNING id`, branchName).Scan(&branch))
	a.exec(`INSERT INTO work_schedules (branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days)
		VALUES ($1, '08:00', '16:00', 0, 0, '{1,2,3,4,5,6,7}')`, branch)
	must(a.pool.QueryRow(a.ctx, `INSERT INTO positions (name, is_active) VALUES ($1, true) RETURNING id`, "Crew"+suffix).Scan(&crew))
	must(a.pool.QueryRow(a.ctx, `INSERT INTO positions (name, is_active) VALUES ($1, true) RETURNING id`, "Supervisor"+suffix).Scan(&sup))

	for _, c := range []struct {
		key, name, typ, method string
		linked                 bool
	}{
		{"meal", "Uang Makan", "daily_allowance", "per_present_day", false},
		{"sc", "Service Charge", "bonus", "fixed", true},
		{"kj", "Uang Kerajinan", "allowance", "fixed", true},
		{"jb", "Insentif Jabatan", "allowance", "fixed", true},
	} {
		var id pgtype.UUID
		must(a.pool.QueryRow(a.ctx, `INSERT INTO wage_components (name, type, calc_method, grade_linked) VALUES ($1,$2,$3,$4) RETURNING id`,
			c.name+suffix, c.typ, c.method, c.linked).Scan(&id))
		a.comp[c.key] = id
	}

	// Kebijakan HRD scale. Saving through the handler validates it and replaces
	// the company scale; only grade-linked components are affected by it.
	code, _, raw := a.call("PUT", "/grades", map[string]any{"grades": []map[string]any{
		{"label": "A", "min_score": 90, "pay_percent": 100},
		{"label": "B", "min_score": 70, "pay_percent": 0, "component_percents": map[string]int{uid(a.comp["sc"]): 50}},
		{"label": "C", "min_score": 0, "pay_percent": 0},
	}})
	if code != 200 {
		log.Fatalf("simpan skala grade: %d %s", code, raw)
	}
	fmt.Println("    skala grade: A ≥90 100% · B ≥70 Service Charge 50%, lainnya 0% · C 0%")

	// The payslip header reads the company name from HR settings.
	a.exec(`UPDATE hr_settings SET company_name = 'Mie Ayam Bakso AA' WHERE id = 1 AND btrim(company_name) = ''`)

	for _, p := range people {
		pos := crew
		if p.supervisor {
			pos = sup
		}
		var id, ws pgtype.UUID
		must(a.pool.QueryRow(a.ctx, `INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, employment_type)
			VALUES ($1,$2,'2025-01-06',$3,$4,'permanent') RETURNING id`, p.code, p.name, pos, branch).Scan(&id))
		a.emp[p.code] = id
		must(a.pool.QueryRow(a.ctx, `INSERT INTO wage_structures (employee_id, base_salary, working_days_per_month, daily_rate, effective_date)
			VALUES ($1,$2,26,$3,'2025-01-06') RETURNING id`, id, baseSalary, dailyRate).Scan(&ws))
		add := func(key string, amt int64) {
			a.exec(`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount) VALUES ($1,$2,$3)`, ws, a.comp[key], amt)
		}
		add("meal", mealRate)
		add("sc", 1_000_000)
		add("kj", 350_000)
		if p.supervisor {
			add("jb", 500_000)
		}
		fmt.Printf("    %s %s\n", p.code, p.name)
	}
}

// day describes one scenario day; the default is present 08:00–16:00.
type day struct {
	kind    string // present | absent | late | halfLate | halfEarly | noOut | noIn | noPunch
	lateMin int
	lostMin int
}

// scenarioDays keys are day-of-month in the period's month.
var scenarioDays = map[string]map[int]day{
	"CONTOH-02": {
		5: {kind: "late", lateMin: 7}, 6: {kind: "late", lateMin: 15}, 8: {kind: "late", lateMin: 25},
		12: {kind: "late", lateMin: 45}, 14: {kind: "halfLate", lostMin: 120}, 16: {kind: "halfEarly", lostMin: 180},
	},
	"CONTOH-03": {
		7: {kind: "absent"}, 10: {kind: "absent"}, // become Izin Tanpa Gaji
		19: {kind: "absent"}, 20: {kind: "absent"}, // become Sakit (no note)
		22: {kind: "noOut"}, 23: {kind: "noIn"},
	},
	"CONTOH-04": {
		6: {kind: "absent"}, 7: {kind: "absent"}, 13: {kind: "absent"}, 15: {kind: "absent"}, 20: {kind: "absent"},
		27: {kind: "noPunch"},
	},
}

func (a *app) seedAttendance(from, to time.Time) {
	n := 0
	for _, p := range people {
		id := a.emp[p.code]
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			sd, ok := scenarioDays[p.code][d.Day()]
			if !ok || d.Month() != to.Month() {
				sd = day{kind: "present"}
			}
			date := pgtype.Date{Time: d, Valid: true}
			switch sd.kind {
			case "present":
				a.exec(`INSERT INTO attendance_records (employee_id, date, check_in, check_out, check_in_source, check_out_source, status)
					VALUES ($1,$2,$3,$4,'fingerprint','fingerprint','present')`, id, date, at(d, 8, 0), at(d, 16, 0))
			case "late":
				a.exec(`INSERT INTO attendance_records (employee_id, date, check_in, check_out, check_in_source, check_out_source, status, is_late, late_minutes)
					VALUES ($1,$2,$3,$4,'fingerprint','fingerprint','present',true,$5)`, id, date, at(d, 8, sd.lateMin), at(d, 16, 0), sd.lateMin)
			case "halfLate", "halfEarly":
				typ, permit, in, out := "late", "late_arrival", at(d, 8, 0).Add(time.Duration(sd.lostMin)*time.Minute), at(d, 16, 0)
				if sd.kind == "halfEarly" {
					typ, permit, in, out = "early", "early_departure", at(d, 8, 0), at(d, 16, 0).Add(-time.Duration(sd.lostMin)*time.Minute)
				}
				a.exec(`INSERT INTO attendance_permissions (employee_id, date, type, reason, status, decided_at)
					VALUES ($1,$2,$3,'Contoh: izin diajukan H-1','approved',now())`, id, date, permit)
				a.exec(`INSERT INTO attendance_records (employee_id, date, check_in, check_out, check_in_source, check_out_source, status,
					is_half_day, half_day_type, half_day_lost_minutes, excused_by)
					VALUES ($1,$2,$3,$4,'fingerprint','fingerprint','present',true,$5,$6,$7)`, id, date, in, out, typ, sd.lostMin, permit)
			case "absent":
				a.exec(`INSERT INTO attendance_records (employee_id, date, status) VALUES ($1,$2,'absent')`, id, date)
			case "noOut":
				a.exec(`INSERT INTO attendance_records (employee_id, date, check_in, check_in_source, status, is_missing_checkout)
					VALUES ($1,$2,$3,'fingerprint','present',true)`, id, date, at(d, 8, 0))
			case "noIn":
				a.exec(`INSERT INTO attendance_records (employee_id, date, check_out, check_out_source, status, is_missing_checkin)
					VALUES ($1,$2,$3,'fingerprint','present',true)`, id, date, at(d, 16, 0))
			case "noPunch":
				a.exec(`INSERT INTO attendance_records (employee_id, date, status, is_no_punch) VALUES ($1,$2,'present',true)`, id, date)
			}
			n++
		}
	}
	fmt.Printf("    %d catatan absensi\n", n)
}

// evaluate judges every sample record in date order with the same per-record
// engine the nightly job and corrections use — only the sample employees, so
// real employees' violations are untouched.
func (a *app) evaluate(from, to time.Time) {
	tx, err := a.pool.Begin(a.ctx)
	must(err)
	defer tx.Rollback(a.ctx)
	qtx := a.q.WithTx(tx)
	for _, p := range people {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			rec, err := qtx.GetAttendanceRecordByEmployeeDate(a.ctx, &db.GetAttendanceRecordByEmployeeDateParams{
				EmployeeID: a.emp[p.code], Date: pgtype.Date{Time: d, Valid: true}})
			must(err)
			must(service.DeleteAutoViolationsForRecord(a.ctx, qtx, rec))
		}
	}
	must(tx.Commit(a.ctx))
	fmt.Println("    evaluasi poin per catatan (mesin yang sama dengan job malam)")
}

func (a *app) leaveType(name string) string {
	var id pgtype.UUID
	must(a.pool.QueryRow(a.ctx, `SELECT id FROM leave_types WHERE name = $1`, name).Scan(&id))
	return uid(id)
}

func (a *app) fileLeaves() {
	file := func(code, typ, from, to string) {
		c, body, raw := a.call("POST", "/leave", map[string]any{
			"employee_id": uid(a.emp[code]), "leave_type_id": a.leaveType(typ),
			"start_date": from, "end_date": to, "reason": "Contoh slip gaji", "confirm_backdated": true,
		})
		if c != http.StatusCreated {
			log.Fatalf("pengajuan %s: %d %s", code, c, raw)
		}
		c, _, raw = a.call("POST", "/leave/"+fmt.Sprint(body["id"])+"/approve", map[string]any{"note": "Contoh"})
		if c != http.StatusOK {
			log.Fatalf("persetujuan %s: %d %s", code, c, raw)
		}
		fmt.Printf("    %s %-16s %s – %s disetujui\n", code, typ, from, to)
	}
	file("CONTOH-03", "Izin Tanpa Gaji", "2026-10-07", "2026-10-07")
	file("CONTOH-03", "Izin Tanpa Gaji", "2026-10-10", "2026-10-10")
	file("CONTOH-03", "Sakit", "2026-10-19", "2026-10-20")
}

// manualViolations records the offenses attendance cannot detect, as HR would
// from the policy page, then recomputes the month's score.
func (a *app) manualViolations() {
	add := func(code, date, pattern string) {
		var pid pgtype.UUID
		var name string
		var pts int32
		must(a.pool.QueryRow(a.ctx, `SELECT id, name, points FROM performance_policies
			WHERE rule_type = 'manual' AND is_active AND name ILIKE $1 ORDER BY name LIMIT 1`, pattern).Scan(&pid, &name, &pts))
		a.exec(`INSERT INTO performance_violations (employee_id, policy_id, date, points, source, note)
			VALUES ($1,$2,$3,$4,'manual',$5)`, a.emp[code], pid, date, pts, name)
		fmt.Printf("    %s manual: %s (−%d)\n", code, name, pts)
	}
	add("CONTOH-04", "2026-10-26", "%tanpa izin%")
	add("CONTOH-04", "2026-10-27", "%check-in dan check-out%")
	tx, err := a.pool.Begin(a.ctx)
	must(err)
	defer tx.Rollback(a.ctx)
	must(service.RecomputeScore(a.ctx, a.q.WithTx(tx), a.emp["CONTOH-04"], time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	must(tx.Commit(a.ctx))
}

// payroll gives each sample employee a line in the period through the real
// single-line regenerate (a placeholder row first, since regenerate replaces an
// existing line), then reviews it with a note.
func (a *app) payroll(period *db.PayrollPeriod) map[string]pgtype.UUID {
	lines := map[string]pgtype.UUID{}
	for _, p := range people {
		var ws pgtype.UUID
		must(a.pool.QueryRow(a.ctx, `SELECT id FROM wage_structures WHERE employee_id = $1`, a.emp[p.code]).Scan(&ws))
		a.exec(`INSERT INTO payroll_lines (payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate)
			VALUES ($1,$2,$3,$4,$5)`, period.ID, a.emp[p.code], ws, baseSalary, dailyRate)
		c, _, raw := a.call("POST", "/periods/"+uid(period.ID)+"/regenerate-line/"+uid(a.emp[p.code]), map[string]any{})
		if c != http.StatusOK {
			log.Fatalf("regenerate %s: %d %s", p.code, c, raw)
		}
		var id pgtype.UUID
		must(a.pool.QueryRow(a.ctx, `SELECT id FROM payroll_lines WHERE payroll_period_id=$1 AND employee_id=$2`, period.ID, a.emp[p.code]).Scan(&id))
		lines[p.code] = id
		c, _, raw = a.call("POST", "/lines/"+uid(id)+"/review", map[string]any{
			"review_note": "CONTOH — data simulasi, bukan karyawan sebenarnya. " + p.story + ".",
		})
		if c != http.StatusOK {
			log.Fatalf("review %s: %d %s", p.code, c, raw)
		}
	}
	fmt.Println("    4 baris gaji dibuat lewat regenerate-line + review")
	return lines
}

type want struct {
	score              int
	grade              string
	sc, kj, jb, meal   int64
	leaveDays          int32
	leaveCut, alphaCut int64
	lateCut, halfDay   int64
	gross, net         int64
}

// Expected figures, worked out by hand from Kebijakan HRD and the dev
// performance policies (absence grace 4 days as configured):
//   - 02: −2 −3 −4 −10 (late tiers) −4 −4 (half days) = 73 → B. Late cut on a
//     480-minute day: 15+15+30+45 = 105 min → 100.000 × 105/480 = 21.875.
//     Half days: 300 min at 12.500/h = 62.500.
//   - 03: −8 −8 (Izin Satu Hari) −16 (2 sick days, no note) −10 (no check-out)
//     −10 (no check-in) = 48 → C. Pay: Wed 1× + Sat 2× + 2 sick days = 5 × 100.000.
//   - 04: Alpha 6,7,13,15,20 Okt: the first 4 are within the grace, the 5th
//     costs −30; 6–7 in a row costs −40; manual −10 −10 → 10 → C. The 5 Alpha
//     days cost 5 allowance days against 4: one day unpaid, 100.000.
var wants = map[string]want{
	"CONTOH-01": {100, "A", 1_000_000, 350_000, 500_000, 31 * mealRate, 0, 0, 0, 0, 0, 4_450_000, 4_450_000},
	"CONTOH-02": {73, "B", 500_000, 0, 0, 31 * mealRate, 0, 0, 0, 21_875, 62_500, 3_100_000, 3_015_625},
	"CONTOH-03": {48, "C", 0, 0, 0, 27 * mealRate, 4, 500_000, 0, 0, 0, 2_600_000, 2_100_000},
	"CONTOH-04": {10, "C", 0, 0, 0, 26 * mealRate, 1, 100_000, 100_000, 0, 0, 2_600_000, 2_500_000},
}

func (a *app) verify(lines map[string]pgtype.UUID) {
	for _, p := range people {
		w := wants[p.code]
		fmt.Printf("  %s %s\n", p.code, p.name)
		var score pgtype.Int4
		var grade pgtype.Text
		var days int32
		var leave, alpha, late, half, gross, net int64
		must(a.pool.QueryRow(a.ctx, `SELECT performance_score, performance_grade, unpaid_leave_days, unpaid_leave_deduction,
			alpha_deduction, attendance_cut_deduction, half_day_deduction, gross_pay, net_pay FROM payroll_lines WHERE id=$1`,
			lines[p.code]).Scan(&score, &grade, &days, &leave, &alpha, &late, &half, &gross, &net))
		comp := func(key string) int64 {
			var v int64
			_ = a.pool.QueryRow(a.ctx, `SELECT amount FROM payroll_line_components WHERE payroll_line_id=$1 AND wage_component_id=$2`,
				lines[p.code], a.comp[key]).Scan(&v)
			return v
		}
		a.expect("poin", w.score, score.Int32)
		a.expect("grade", w.grade, grade.String)
		a.expect("Service Charge", w.sc, comp("sc"))
		a.expect("Uang Kerajinan", w.kj, comp("kj"))
		a.expect("Insentif Jabatan", w.jb, comp("jb"))
		a.expect("Uang makan (dibayar harian)", w.meal, comp("meal"))
		a.expect("hari tanpa gaji / potongannya", fmt.Sprintf("%d/%d", w.leaveDays, w.leaveCut), fmt.Sprintf("%d/%d", days, leave))
		a.expect("bagian Alpha di potongan itu", w.alphaCut, alpha)
		a.expect("potongan terlambat", w.lateCut, late)
		a.expect("potongan setengah hari", w.halfDay, half)
		a.expect("bruto", w.gross, gross)
		a.expect("netto", w.net, net)
	}
}

func (a *app) render(lines map[string]pgtype.UUID, out string) {
	for _, p := range people {
		req := httptest.NewRequest("GET", "/", nil).WithContext(a.ctx)
		pdf, err := a.slip.RenderPayslipPDF(req, lines[p.code])
		must(err)
		name := fmt.Sprintf("%s-%s.pdf", p.file, p.code)
		must(os.WriteFile(filepath.Join(out, name), pdf, 0o644))
		fmt.Printf("    %s (%d KB)\n", name, len(pdf)/1024)
	}
}

func writeReadme(out, month string) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Contoh Slip Gaji — %s\n\n", month)
	b.WriteString("Dibuat oleh `server-go/cmd/sample-payslips` di database lokal, lewat kode aplikasi yang sebenarnya ")
	b.WriteString("(pengajuan & persetujuan libur, mesin evaluasi poin, regenerate baris gaji, review, dan pembuat PDF slip).\n\n")
	b.WriteString("Karyawan contoh ada di cabang **Contoh Slip Gaji** (kode `CONTOH-01`…`04`). Hapus semuanya dengan ")
	b.WriteString("`go run ./cmd/sample-payslips -clean` dari folder `server-go`.\n\n")
	b.WriteString("Gaji pokok 2.600.000 · gaji harian 100.000 · uang makan 25.000/hari · Service Charge 1.000.000 · Uang Kerajinan 350.000 · Insentif Jabatan 500.000 (supervisor).\n")
	b.WriteString("Skala grade: A ≥ 90 semua 100% · B ≥ 70 Service Charge 50%, lainnya 0% · C 0%. Toleransi absen: 4 hari (pengaturan saat ini).\n\n")
	b.WriteString("| File | Karyawan | Skenario | Poin | Grade | Netto |\n|---|---|---|---:|---|---:|\n")
	for _, p := range people {
		w := wants[p.code]
		fmt.Fprintf(&b, "| %s-%s.pdf | %s | %s | %d | %s | %s |\n", p.file, p.code, p.name, p.story, w.score, w.grade, rupiah(w.net))
	}
	must(os.WriteFile(filepath.Join(out, "README.md"), []byte(b.String()), 0o644))
}

func rupiah(v int64) string {
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return "Rp " + b.String()
}
