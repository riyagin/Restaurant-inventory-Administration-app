// sim-hrd-policy exercises the Kebijakan HRD AA rules (migration 088) end to
// end on a throwaway database, through the app's REAL handlers and services,
// and compares every outcome with the figure the policy document prescribes.
//
// It never touches the dev database: it drops and recreates hrd_policy_sim,
// migrates it to 087, seeds "pre-088" data (the live performance policies, an
// old sick leave and an old payroll line), applies 088, and then runs:
//
//   - the migration's own data changes;
//   - the per-component grade scale (Tier B: Service Charge 50%, the rest 0%);
//   - two months of attendance with late days, Alpha, unpaid leave on a weekday
//     and a Saturday, sick leave with and without a doctor's note, and
//     national holidays on a Thursday and a Saturday;
//   - the evaluation with the current 4-day absence grace and with grace 0;
//   - payroll periods across the switch from calendar month to the 26–25
//     cut-off, and the October lines (tiers, leave pay cut, holiday incentive);
//   - Kebijakan Kasbon through the kasbon endpoints.
//
// Usage (from server-go/):
//
//	go run ./cmd/sim-hrd-policy -out "../../hrd-policy-simulation"
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
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

const simDB = "hrd_policy_sim"

const (
	dailyRate  = int64(100_000)
	baseSalary = int64(2_600_000)
	mealRate   = int64(25_000)
	scAmount   = int64(1_000_000)
	kjAmount   = int64(350_000)
	jbAmount   = int64(500_000)
)

type emp struct {
	Code, Name, Type string
	Supervisor       bool
	Story            string
}

var emps = []emp{
	{"E01", "Andi", "permanent", true, "Hadir penuh, tidak ada pelanggaran"},
	{"E02", "Budi", "permanent", true, "Terlambat 15 mnt ×3 + 5 mnt ×1"},
	{"E03", "Citra", "permanent", false, "Alpha 1 hari (7 Okt)"},
	{"E04", "Dewi", "permanent", false, "Alpha 2 hari berturut-turut (7–8 Okt)"},
	{"E05", "Eko", "permanent", false, "Izin tanpa gaji Rabu 14 Okt + Sabtu 17 Okt"},
	{"E06", "Fajar", "permanent", false, "Sakit 19–20 Okt tanpa surat dokter"},
	{"E07", "Gita", "permanent", false, "Sakit 19–20 Okt, surat dokter diserahkan kemudian"},
	{"E08", "Hadi", "permanent", false, "Masuk pada libur nasional Kamis 1 Okt & Sabtu 10 Okt"},
	{"E09", "Intan", "contract", false, "Karyawan kontrak (PKWT), hadir penuh"},
}

type check struct {
	Area, Case, Want, Got string
	Pass                  bool
}

type simr struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	q      *db.Queries
	router http.Handler
	emp    map[string]pgtype.UUID
	comp   map[string]pgtype.UUID
	types  map[string]pgtype.UUID
	checks []check
	log    []string
}

func (s *simr) logf(f string, a ...any) {
	line := fmt.Sprintf(f, a...)
	s.log = append(s.log, line)
	fmt.Println("  " + line)
}

func (s *simr) expect(area, name string, want, got any) {
	w, g := fmt.Sprint(want), fmt.Sprint(got)
	c := check{area, name, w, g, w == g}
	s.checks = append(s.checks, c)
	mark := "PASS"
	if !c.Pass {
		mark = "FAIL"
	}
	fmt.Printf("  [%s] %s — %s: want %s, got %s\n", mark, area, name, w, g)
}

// expectContains passes when got contains want.
func (s *simr) expectContains(area, name, want, got string) {
	c := check{area, name, "…" + want + "…", got, strings.Contains(got, want)}
	s.checks = append(s.checks, c)
	mark := "PASS"
	if !c.Pass {
		mark = "FAIL"
	}
	fmt.Printf("  [%s] %s — %s: want …%s…, got %s\n", mark, area, name, want, got)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (s *simr) exec(sql string, args ...any) {
	if _, err := s.pool.Exec(s.ctx, sql, args...); err != nil {
		log.Fatalf("%v\n%s", err, sql)
	}
}

func (s *simr) call(method, path string, body any) (int, map[string]any, string) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	raw := strings.TrimSpace(rec.Body.String())
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, raw
}

func uid(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }
func dt(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	must(err)
	return t
}
func pgD(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// wib returns the instant of hh:mm WIB on date d.
func wib(d time.Time, hh, mm int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, service.WIB)
}

func main() {
	out := flag.String("out", "../../hrd-policy-simulation", "output folder")
	flag.Parse()
	ctx := context.Background()
	cfg, err := config.Load()
	must(err)
	must(os.MkdirAll(*out, 0o755))

	today := service.TodayWIB()
	fmt.Printf("Simulasi Kebijakan HRD — database %s, hari ini (WIB) %s\n", simDB, today.Format("2006-01-02"))

	// ── 1. Throwaway database, migrated to 087 ──────────────────────────────
	url := strings.Replace(cfg.DBUrl, "/"+cfg.DBName, "/"+simDB, 1)
	admin, err := pgx.Connect(ctx, strings.Replace(cfg.DBUrl, "/"+cfg.DBName, "/postgres", 1))
	must(err)
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+simDB+" WITH (FORCE)")
	must(err)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+simDB)
	must(err)
	admin.Close(ctx)

	m, err := migrate.New("file://migrations", url)
	must(err)
	// 042 inserts an opening journal entry with no lines, which an empty
	// database rejects; it touches no HR table (same skip as sim-attendance-month).
	if err := m.Migrate(41); err != nil && err != migrate.ErrNoChange {
		log.Fatal(err)
	}
	must(m.Force(42))
	must(m.Migrate(87))

	pool, err := pgxpool.New(ctx, url)
	must(err)
	defer pool.Close()
	s := &simr{ctx: ctx, pool: pool, q: db.New(pool), emp: map[string]pgtype.UUID{},
		comp: map[string]pgtype.UUID{}, types: map[string]pgtype.UUID{}}
	s.buildRouter()

	fmt.Println("\n[1] Seed data before migration 088")
	s.seedBase()
	s.seedPre088()

	fmt.Println("\n[2] Apply migration 088")
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatal(err)
	}
	m.Close()
	s.checkMigration()

	fmt.Println("\n[3] Settings and grade scale through the HTTP handlers")
	s.settingsAndGrades()

	fmt.Println("\n[4] Attendance Sep–Oct 2026, evaluation, leave approvals")
	s.seedAttendance()
	s.evaluate()
	s.fileLeaves()
	grace4 := s.scores("2026-10-01")
	s.exec(`UPDATE hr_settings SET absence_grace_days = 0 WHERE id = 1`)
	s.reevaluate(dt("2026-09-01"), dt("2026-10-31"))
	grace0 := s.scores("2026-10-01")
	s.checkScores(grace4, grace0)

	fmt.Println("\n[5] Payroll periods across the cut-off switch, October lines")
	s.payroll()
	lines := s.octoberLines()
	s.checkLines(lines)
	s.reviewHoliday(lines)

	fmt.Println("\n[6] Kebijakan Kasbon")
	s.kasbon(today)

	writeReport(*out, today, s, grace4, grace0, lines)
	pass, fail := 0, 0
	for _, c := range s.checks {
		if c.Pass {
			pass++
		} else {
			fail++
		}
	}
	fmt.Printf("\n%d checks: %d pass, %d fail. Report: %s\n", len(s.checks), pass, fail, filepath.Join(*out, "report.md"))
	if fail > 0 {
		os.Exit(1)
	}
}

func (s *simr) buildRouter() {
	r := chi.NewRouter()
	admin := &middleware.Claims{UserID: uuid.Nil, Username: "simulator", Role: middleware.RoleSuperuser}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(middleware.ContextWithClaims(r.Context(), admin)))
		})
	})
	wh := handler.NewHRWagesHandler(s.pool, s.q)
	r.Put("/grades", wh.SaveGrades)
	r.Get("/policy", wh.GetPolicySettings)
	r.Put("/policy", wh.SavePolicySettings)
	lh := handler.NewLeaveHandler(s.pool, s.q)
	r.Post("/leave", lh.CreateLeaveRequest)
	r.Get("/leave", lh.ListLeaveRequests)
	r.Post("/leave/{id}/approve", lh.ApproveLeaveRequest)
	r.Put("/leave/{id}/doctor-note", lh.SetDoctorNote)
	ph := handler.NewPayrollHandler(s.pool, s.q)
	r.Post("/periods", ph.CreatePeriod)
	r.Post("/lines/{id}/review", ph.ReviewLine)
	kh := handler.NewKasbonHandler(s.pool, s.q)
	r.Post("/kasbons", kh.Create)
	r.Get("/kasbons/eligibility", kh.Eligibility)
	r.Get("/kasbons/{id}", kh.Get)
	r.Post("/kasbons/{id}/approve", kh.Approve)
	s.router = r
}

// ── Seed ─────────────────────────────────────────────────────────────────────

func (s *simr) seedBase() {
	var branchID, crew, sup pgtype.UUID
	must(s.pool.QueryRow(s.ctx, `INSERT INTO branches (name) VALUES ('Cabang Simulasi') RETURNING id`).Scan(&branchID))
	s.exec(`INSERT INTO work_schedules (branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days)
		VALUES ($1, '08:00', '16:00', 0, 0, '{1,2,3,4,5,6,7}')`, branchID)
	must(s.pool.QueryRow(s.ctx, `INSERT INTO positions (name, is_active) VALUES ('Crew', true) RETURNING id`).Scan(&crew))
	must(s.pool.QueryRow(s.ctx, `INSERT INTO positions (name, is_active) VALUES ('Supervisor', true) RETURNING id`).Scan(&sup))

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
		must(s.pool.QueryRow(s.ctx, `INSERT INTO wage_components (name, type, calc_method, grade_linked) VALUES ($1,$2,$3,$4) RETURNING id`,
			c.name, c.typ, c.method, c.linked).Scan(&id))
		s.comp[c.key] = id
	}

	for _, e := range emps {
		pos := crew
		if e.Supervisor {
			pos = sup
		}
		var id, ws pgtype.UUID
		must(s.pool.QueryRow(s.ctx, `INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id, employment_type)
			VALUES ($1,$2,'2025-01-06',$3,$4,$5) RETURNING id`, e.Code, e.Name, pos, branchID, e.Type).Scan(&id))
		s.emp[e.Code] = id
		must(s.pool.QueryRow(s.ctx, `INSERT INTO wage_structures (employee_id, base_salary, working_days_per_month, daily_rate, effective_date)
			VALUES ($1,$2,26,$3,'2025-01-06') RETURNING id`, id, baseSalary, dailyRate).Scan(&ws))
		add := func(key string, amt int64) {
			s.exec(`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount) VALUES ($1,$2,$3)`, ws, s.comp[key], amt)
		}
		add("meal", mealRate)
		add("sc", scAmount)
		add("kj", kjAmount)
		if e.Supervisor {
			add("jb", jbAmount)
		}
	}

	// The live performance policies (copied from the dev database on
	// 2026-11-10, with 083's >30 fix applied), as production holds them
	// before 088.
	for _, p := range []struct {
		rule, name string
		thr        any
		pts        int
	}{
		{"absent_no_leave", "Mangkir Tanpa Keterangan", nil, 30},
		{"consecutive_absent", "Mangkir lebih dari 1 hari berturut-turut", 2, 40},
		{"half_day_early", "Izin Pulang Lebih Awal (Maks. H-1)", nil, 4},
		{"half_day_late", "Izin Masuk Siang (Maks. H-1)", nil, 4},
		{"late", "Terlambat 1 - 10 menit", 1, 2},
		{"late", "Terlambat 11 - 20 menit", 11, 4},
		{"late", "Terlambat 21 - 30 menit", 21, 8},
		{"late", "Terlambat lebih dari 30 menit", 31, 10},
		{"manual", "Tidak melakukan check-in dan check-out dalam satu hari", nil, 10},
		{"missing_checkout", "Tidak check-out (lupa absen) tanpa konfirmasi", nil, 10},
	} {
		s.exec(`INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active) VALUES ($1,$2,$3,$4,true)`,
			p.name, p.rule, p.thr, p.pts)
	}
	s.exec(`UPDATE hr_settings SET absence_grace_days = 4 WHERE id = 1`)

	rows, err := s.pool.Query(s.ctx, `SELECT id, name FROM leave_types`)
	must(err)
	for rows.Next() {
		var id pgtype.UUID
		var name string
		must(rows.Scan(&id, &name))
		s.types[name] = id
	}
	rows.Close()
	s.logf("9 karyawan, 4 komponen gaji, 10 kebijakan poin (data live 10 Nov), jenis cuti: %d", len(s.types))
}

func (s *simr) seedPre088() {
	// An approved sick leave from August, filed before the doctor's-note rule.
	s.exec(`INSERT INTO leave_requests (employee_id, leave_type_id, start_date, end_date, day_count, status)
		VALUES ($1,$2,'2026-08-10','2026-08-10',1,'approved')`, s.emp["E06"], s.types["Sakit"])
	// August payroll as the calendar-month period it was, with one line.
	var pid, ws pgtype.UUID
	must(s.pool.QueryRow(s.ctx, `INSERT INTO payroll_periods (period_month, start_date, end_date)
		VALUES ('2026-08-01','2026-08-01','2026-08-31') RETURNING id`).Scan(&pid))
	must(s.pool.QueryRow(s.ctx, `SELECT id FROM wage_structures WHERE employee_id = $1`, s.emp["E01"]).Scan(&ws))
	s.exec(`INSERT INTO payroll_lines (payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate)
		VALUES ($1,$2,$3,$4,$5)`, pid, s.emp["E01"], ws, baseSalary, dailyRate)
	s.logf("sakit Agustus (E06) tanpa penanda surat dokter; periode gaji Agustus 1–31 Agu + 1 baris")
}

func (s *simr) checkMigration() {
	pts := func(rule string, thr int) int {
		var p int
		must(s.pool.QueryRow(s.ctx, `SELECT points FROM performance_policies WHERE rule_type=$1 AND threshold_minutes=$2`, rule, thr).Scan(&p))
		return p
	}
	s.expect("Migrasi 088", "Terlambat 1–10 mnt", 2, pts("late", 1))
	s.expect("Migrasi 088", "Terlambat 11–20 mnt (4 → 3)", 3, pts("late", 11))
	s.expect("Migrasi 088", "Terlambat 21–30 mnt (8 → 4)", 4, pts("late", 21))
	s.expect("Migrasi 088", "Terlambat >30 mnt (tidak diatur kebijakan, dibiarkan)", 10, pts("late", 31))
	for _, c := range []struct {
		rule, name string
		pts        int
	}{
		{"unpaid_leave", "Izin Satu Hari", 8},
		{"sick_no_note", "Sakit tanpa surat dokter", 8},
		{"missing_checkin", "Tidak check-in", 10},
	} {
		var p int
		err := s.pool.QueryRow(s.ctx, `SELECT points FROM performance_policies WHERE rule_type=$1 AND is_active`, c.rule).Scan(&p)
		if err != nil {
			p = -1
		}
		s.expect("Migrasi 088", "Kebijakan baru: "+c.name, c.pts, p)
	}
	var n int
	must(s.pool.QueryRow(s.ctx, `SELECT count(*) FROM performance_policies WHERE rule_type='manual' AND name ILIKE 'Meninggalkan tempat kerja%'`).Scan(&n))
	s.expect("Migrasi 088", "Kebijakan manual: meninggalkan tempat kerja tanpa izin", 1, n)

	var sick, unpaid bool
	must(s.pool.QueryRow(s.ctx, `SELECT requires_doctor_note FROM leave_types WHERE name='Sakit'`).Scan(&sick))
	must(s.pool.QueryRow(s.ctx, `SELECT requires_doctor_note FROM leave_types WHERE name='Izin Tanpa Gaji'`).Scan(&unpaid))
	s.expect("Migrasi 088", "Jenis 'Sakit' wajib surat dokter", true, sick)
	s.expect("Migrasi 088", "Jenis 'Izin Tanpa Gaji' tidak wajib surat dokter", false, unpaid)
	var note bool
	must(s.pool.QueryRow(s.ctx, `SELECT has_doctor_note FROM leave_requests WHERE employee_id=$1`, s.emp["E06"]).Scan(&note))
	s.expect("Migrasi 088", "Sakit lama tidak dihukum surut (dianggap ada surat)", true, note)
	var rate int64
	must(s.pool.QueryRow(s.ctx, `SELECT public_holiday_rate FROM payroll_lines`).Scan(&rate))
	s.expect("Migrasi 088", "Baris gaji lama: tarif libur = gaji harian", dailyRate, rate)
	set, err := s.q.GetHRPolicySettings(s.ctx)
	must(err)
	s.expect("Migrasi 088", "Pengaturan default (cut-off, kasbon)",
		"25 / 10–20 / 500000 / 2× / PKWTT / 70 / 90",
		fmt.Sprintf("%d / %d–%d / %d / %d× / %s / %d / %d", set.PayrollCutoffDay, set.KasbonWindowStartDay, set.KasbonWindowEndDay,
			set.KasbonMaxAmount, set.KasbonMaxInstallments, map[bool]string{true: "PKWTT", false: "semua"}[set.KasbonPermanentOnly],
			set.KasbonMinScore, set.KasbonPriorityScore))
}

func (s *simr) settingsAndGrades() {
	code, _, raw := s.call("PUT", "/policy", map[string]any{
		"payroll_cutoff_day": 30, "kasbon_window_start_day": 10, "kasbon_window_end_day": 20,
		"kasbon_max_amount": 500000, "kasbon_max_installments": 2, "kasbon_permanent_only": true,
		"kasbon_min_score": 70, "kasbon_priority_score": 90,
	})
	s.expect("Pengaturan", "Cut-off tanggal 30 ditolak", 400, code)
	s.logf("PUT /policy cut-off 30 -> %d %s", code, raw)

	sc, kj := uid(s.comp["sc"]), uid(s.comp["kj"])
	code, _, raw = s.call("PUT", "/grades", map[string]any{"grades": []map[string]any{
		{"label": "A", "min_score": 90, "pay_percent": 100},
		{"label": "B", "min_score": 70, "pay_percent": 0, "component_percents": map[string]int{sc: 50}},
		{"label": "C", "min_score": 0, "pay_percent": 0, "component_percents": map[string]int{sc: 60}},
	}})
	s.expect("Skala grade", "Tier C membayar SC lebih tinggi dari Tier B ditolak", 400, code)
	s.logf("PUT /grades (tidak monoton) -> %d %s", code, raw)

	code, _, raw = s.call("PUT", "/grades", map[string]any{"grades": []map[string]any{
		{"label": "A", "min_score": 90, "pay_percent": 100},
		{"label": "B", "min_score": 70, "pay_percent": 0, "component_percents": map[string]int{sc: 50}},
		{"label": "C", "min_score": 0, "pay_percent": 0},
	}})
	s.expect("Skala grade", "Skala kebijakan disimpan (A 100%, B: SC 50% lain 0%, C 0%)", 200, code)
	s.logf("PUT /grades (kebijakan) -> %d %s", code, raw)
	_ = kj
}

// seedAttendance writes 1 Sep – 31 Oct for every employee: present 08:00–16:00
// WIB, with the scenario days changed. A holiday is a day off for everyone but
// E08, who works it.
func (s *simr) seedAttendance() {
	s.exec(`INSERT INTO public_holidays (date, name) VALUES ('2026-10-01','Libur Simulasi (Kamis)'), ('2026-10-10','Libur Simulasi (Sabtu)')`)
	holidays := map[string]bool{"2026-10-01": true, "2026-10-10": true}
	late := map[string]map[string]int{"E02": {"2026-10-05": 15, "2026-10-06": 15, "2026-10-07": 15, "2026-10-08": 5}}
	absent := map[string][]string{
		"E03": {"2026-10-07"},
		"E04": {"2026-10-07", "2026-10-08"},
		"E05": {"2026-10-14", "2026-10-17"},
		"E06": {"2026-10-19", "2026-10-20"},
		"E07": {"2026-10-19", "2026-10-20"},
	}
	n := 0
	for _, e := range emps {
		abs := map[string]bool{}
		for _, d := range absent[e.Code] {
			abs[d] = true
		}
		for d := dt("2026-09-01"); !d.After(dt("2026-10-31")); d = d.AddDate(0, 0, 1) {
			key := d.Format("2006-01-02")
			id := s.emp[e.Code]
			switch {
			case holidays[key] && e.Code != "E08":
				s.exec(`INSERT INTO attendance_records (employee_id, date, status) VALUES ($1,$2,'holiday')`, id, pgD(d))
			case abs[key]:
				s.exec(`INSERT INTO attendance_records (employee_id, date, status) VALUES ($1,$2,'absent')`, id, pgD(d))
			default:
				lm := late[e.Code][key]
				s.exec(`INSERT INTO attendance_records (employee_id, date, check_in, check_out, check_in_source, check_out_source,
					status, is_late, late_minutes) VALUES ($1,$2,$3,$4,'fingerprint','fingerprint','present',$5,$6)`,
					id, pgD(d), wib(d, 8, lm), wib(d, 16, 0), lm > 0, lm)
			}
			n++
		}
	}
	s.logf("%d catatan absensi (1 Sep – 31 Okt), libur nasional Kamis 1 Okt & Sabtu 10 Okt", n)
}

func (s *simr) inTx(f func(qtx *db.Queries) error) {
	tx, err := s.pool.Begin(s.ctx)
	must(err)
	defer tx.Rollback(s.ctx)
	must(f(s.q.WithTx(tx)))
	must(tx.Commit(s.ctx))
}

// evaluate mirrors the nightly EvaluateDay over the two months.
func (s *simr) evaluate() {
	s.inTx(func(qtx *db.Queries) error {
		return service.EvaluateRange(s.ctx, qtx, dt("2026-09-01"), dt("2026-10-31"))
	})
	s.logf("evaluasi malam (EvaluateDay) 1 Sep – 31 Okt, toleransi absen 4 hari")
}

// reevaluate mirrors POST /api/hr/performance/evaluate (ReevaluateRange).
func (s *simr) reevaluate(from, to time.Time) {
	s.inTx(func(qtx *db.Queries) error { return service.ReevaluateRange(s.ctx, qtx, from, to) })
	s.logf("evaluasi ulang 1 Sep – 31 Okt dengan toleransi absen 0 hari")
}

// fileLeaves files and approves the leave through the real handlers — after the
// days were already judged absent, as a backdated approval would be.
func (s *simr) fileLeaves() {
	before := s.scores("2026-10-01")
	file := func(code, typ, from, to string, note bool) string {
		c, body, raw := s.call("POST", "/leave", map[string]any{
			"employee_id": uid(s.emp[code]), "leave_type_id": uid(s.types[typ]),
			"start_date": from, "end_date": to, "reason": "Simulasi", "confirm_backdated": true, "has_doctor_note": note,
		})
		if c != http.StatusCreated {
			log.Fatalf("create leave %s: %d %s", code, c, raw)
		}
		id := fmt.Sprint(body["id"])
		c, _, raw = s.call("POST", "/leave/"+id+"/approve", map[string]any{"note": "Simulasi"})
		if c != http.StatusOK {
			log.Fatalf("approve leave %s: %d %s", code, c, raw)
		}
		s.logf("cuti %s %s %s–%s disetujui (surat dokter: %v)", code, typ, from, to, note)
		return id
	}
	file("E05", "Izin Tanpa Gaji", "2026-10-14", "2026-10-14", false)
	file("E05", "Izin Tanpa Gaji", "2026-10-17", "2026-10-17", false)
	file("E06", "Sakit", "2026-10-19", "2026-10-20", false)
	e07 := file("E07", "Sakit", "2026-10-19", "2026-10-20", false)
	mid := s.scores("2026-10-01")

	c, _, raw := s.call("PUT", "/leave/"+e07+"/doctor-note", map[string]any{"has_doctor_note": true})
	s.expect("Cuti", "HR mencatat surat dokter E07", 200, c)
	s.logf("PUT doctor-note E07 -> %d %s", c, raw)
	after := s.scores("2026-10-01")

	s.expect("Cuti", "E06 sebelum cuti disetujui (Alpha 2 hari berturut, toleransi 4)", 60, before["E06"])
	s.expect("Cuti", "E06 setelah sakit disetujui tanpa surat (−8 ×2)", 84, mid["E06"])
	s.expect("Cuti", "E07 sakit tanpa surat (−8 ×2)", 84, mid["E07"])
	s.expect("Cuti", "E07 setelah surat dokter dicatat", 100, after["E07"])
	s.expect("Cuti", "E05 izin tanpa gaji Rabu + Sabtu (−8 ×2)", 84, mid["E05"])

	_, _, raw = s.call("GET", "/leave?employee_id="+uid(s.emp["E07"]), nil)
	s.expectContains("Cuti", "Daftar cuti memuat has_doctor_note", `"has_doctor_note":true`, raw)
}

func (s *simr) scores(month string) map[string]int {
	out := map[string]int{}
	for _, e := range emps {
		v := 100
		var sc int32
		if err := s.pool.QueryRow(s.ctx, `SELECT score FROM performance_scores WHERE employee_id=$1 AND period_month=$2`,
			s.emp[e.Code], month).Scan(&sc); err == nil {
			v = int(sc)
		}
		out[e.Code] = v
	}
	return out
}

func (s *simr) checkScores(g4, g0 map[string]int) {
	want4 := map[string]int{"E01": 100, "E02": 89, "E03": 100, "E04": 60, "E05": 84, "E06": 84, "E07": 100, "E08": 100, "E09": 100}
	want0 := map[string]int{"E01": 100, "E02": 89, "E03": 70, "E04": 0, "E05": 84, "E06": 84, "E07": 100, "E08": 100, "E09": 100}
	for _, e := range emps {
		s.expect("Poin Oktober (toleransi 4)", e.Code+" "+e.Story, want4[e.Code], g4[e.Code])
	}
	for _, e := range emps {
		s.expect("Poin Oktober (toleransi 0)", e.Code+" "+e.Story, want0[e.Code], g0[e.Code])
	}
}

// ── Payroll ─────────────────────────────────────────────────────────────────

func (s *simr) payroll() {
	for _, c := range []struct{ month, start, end string }{
		{"2026-09", "2026-09-01", "2026-09-25"}, // switch-over: August ended 31 Aug
		{"2026-10", "2026-09-26", "2026-10-25"},
	} {
		code, _, raw := s.call("POST", "/periods", map[string]any{"period_month": c.month})
		if code != http.StatusCreated && code != http.StatusOK {
			log.Fatalf("create period %s: %d %s", c.month, code, raw)
		}
		var st, en time.Time
		must(s.pool.QueryRow(s.ctx, `SELECT start_date, end_date FROM payroll_periods WHERE period_month=$1`, c.month+"-01").Scan(&st, &en))
		s.expect("Periode gaji", "Periode "+c.month, c.start+" s/d "+c.end, st.Format("2006-01-02")+" s/d "+en.Format("2006-01-02"))
	}
	st, en, err := service.ResolvePayrollPeriodBounds(s.ctx, s.q, dt("2026-11-01"))
	must(err)
	s.expect("Periode gaji", "Periode 2026-11 (pratinjau)", "2026-10-26 s/d 2026-11-25", st.Format("2006-01-02")+" s/d "+en.Format("2006-01-02"))
}

type line struct {
	ID                                     pgtype.UUID
	Code                                   string
	Score                                  pgtype.Int4
	Grade                                  pgtype.Text
	Allowance, Bonus, Holiday, HolidayRate int64
	HolidayDays                            float64
	UnpaidDays                             int32
	Unpaid, Cut, Gross, Net                int64
	SC, KJ, JB, Meal                       int64
}

func (s *simr) octoberLines() map[string]*line {
	rows, err := s.pool.Query(s.ctx, `
		SELECT pl.id, e.employee_code, pl.performance_score, pl.performance_grade, pl.allowance_total, pl.bonus_total,
		       pl.public_holiday_amount, pl.public_holiday_rate, pl.public_holiday_days::float8, pl.unpaid_leave_days,
		       pl.unpaid_leave_deduction, pl.attendance_cut_deduction, pl.gross_pay, pl.net_pay
		FROM payroll_lines pl
		JOIN payroll_periods pp ON pp.id = pl.payroll_period_id
		JOIN employees e ON e.id = pl.employee_id
		WHERE pp.period_month = '2026-10-01'`)
	must(err)
	out := map[string]*line{}
	for rows.Next() {
		l := &line{}
		must(rows.Scan(&l.ID, &l.Code, &l.Score, &l.Grade, &l.Allowance, &l.Bonus, &l.Holiday, &l.HolidayRate, &l.HolidayDays,
			&l.UnpaidDays, &l.Unpaid, &l.Cut, &l.Gross, &l.Net))
		out[l.Code] = l
	}
	rows.Close()
	for _, l := range out {
		cr, err := s.pool.Query(s.ctx, `SELECT name, amount FROM payroll_line_components WHERE payroll_line_id=$1`, l.ID)
		must(err)
		for cr.Next() {
			var name string
			var amt int64
			must(cr.Scan(&name, &amt))
			switch name {
			case "Service Charge":
				l.SC = amt
			case "Uang Kerajinan":
				l.KJ = amt
			case "Insentif Jabatan":
				l.JB = amt
			case "Uang Makan":
				l.Meal = amt
			}
		}
		cr.Close()
	}
	return out
}

type want struct {
	grade            string
	sc, kj, jb, meal int64
	hDays            float64
	hRate, hAmt      int64
	uDays            int32
	unpaid, cut      int64
}

// Expected October lines, computed from the policy by hand.
// Period 26 Sep – 25 Oct = 30 days; holidays 1 Oct (Thu) and 10 Oct (Sat).
// Late pay cut: 15 or 5 min → one 15-min block of a 480-min day =
// 100.000 × 15 / 480 = 3.125 per day.
var wants = map[string]want{
	"E01": {"A", scAmount, kjAmount, jbAmount, 28 * mealRate, 0, mealRate, 0, 0, 0, 0},
	"E02": {"B", 500_000, 0, 0, 28 * mealRate, 0, mealRate, 0, 0, 0, 4 * 3_125},
	"E03": {"B", 500_000, 0, 0, 27 * mealRate, 0, mealRate, 0, 0, 0, 0},
	"E04": {"C", 0, 0, 0, 26 * mealRate, 0, mealRate, 0, 0, 0, 0},
	"E05": {"B", 500_000, 0, 0, 26 * mealRate, 0, mealRate, 0, 2, dailyRate + 2*dailyRate, 0},
	"E06": {"B", 500_000, 0, 0, 26 * mealRate, 0, mealRate, 0, 2, 2 * dailyRate, 0},
	"E07": {"A", scAmount, kjAmount, 0, 26 * mealRate, 0, mealRate, 0, 0, 0, 0},
	"E08": {"A", scAmount, kjAmount, 0, 30 * mealRate, 1, mealRate, 2 * mealRate, 0, 0, 0},
	"E09": {"A", scAmount, kjAmount, 0, 28 * mealRate, 0, mealRate, 0, 0, 0, 0},
}

func (s *simr) checkLines(lines map[string]*line) {
	for _, e := range emps {
		l, w := lines[e.Code], wants[e.Code]
		if l == nil {
			s.expect("Gaji Oktober", e.Code+" baris ada", true, false)
			continue
		}
		a := "Gaji Oktober " + e.Code
		s.expect(a, "grade", w.grade, l.Grade.String)
		s.expect(a, "Service Charge", w.sc, l.SC)
		s.expect(a, "Uang Kerajinan", w.kj, l.KJ)
		s.expect(a, "Insentif Jabatan", w.jb, l.JB)
		s.expect(a, "Uang makan (info, 25.000 × hari hadir)", w.meal, l.Meal)
		s.expect(a, "Insentif libur: hari / tarif / jumlah", fmt.Sprintf("%.0f / %d / %d", w.hDays, w.hRate, w.hAmt),
			fmt.Sprintf("%.0f / %d / %d", l.HolidayDays, l.HolidayRate, l.Holiday))
		s.expect(a, "Potongan cuti: hari / rupiah", fmt.Sprintf("%d / %d", w.uDays, w.unpaid), fmt.Sprintf("%d / %d", l.UnpaidDays, l.Unpaid))
		s.expect(a, "Potongan terlambat", w.cut, l.Cut)
		gross := baseSalary + w.kj + w.jb + w.sc + w.hAmt
		s.expect(a, "Bruto", gross, l.Gross)
		s.expect(a, "Netto", gross-w.unpaid-w.cut, l.Net)
	}
}

// reviewHoliday re-saves E08's line through the review endpoint: the holiday
// amount must be re-priced at the frozen uang makan rate, not the daily wage.
func (s *simr) reviewHoliday(lines map[string]*line) {
	l := lines["E08"]
	code, _, raw := s.call("POST", "/lines/"+uid(l.ID)+"/review", map[string]any{"public_holiday_days": 1, "review_note": "Simulasi"})
	if code != http.StatusOK {
		log.Fatalf("review: %d %s", code, raw)
	}
	var amt int64
	must(s.pool.QueryRow(s.ctx, `SELECT public_holiday_amount FROM payroll_lines WHERE id=$1`, l.ID).Scan(&amt))
	s.expect("Gaji Oktober E08", "Review ulang: insentif libur tetap 2 × uang makan", 2*mealRate, amt)
}

// ── Kasbon ──────────────────────────────────────────────────────────────────

func (s *simr) kasbon(today time.Time) {
	reqM := service.FirstOfMonth(today)
	nextM := reqM.AddDate(0, 1, 0)
	ym := func(t time.Time) string { return t.Format("2006-01") }
	month := reqM.Format("2006-01-02")

	// Running score this month: E02 80 (evaluation), E04 60 (refused).
	s.exec(`INSERT INTO performance_scores (employee_id, period_month, score) VALUES ($1,$3,80), ($2,$3,60)`,
		s.emp["E02"], s.emp["E04"], month)
	var fund pgtype.UUID
	must(s.pool.QueryRow(s.ctx, `INSERT INTO accounts (name, account_number, account_type) VALUES ('Kas Simulasi', 11999, 'asset') RETURNING id`).Scan(&fund))

	// Keep today inside the window for the main cases, whatever day this runs.
	day := today.Day()
	setWindow := func(a, b int) {
		code, _, raw := s.call("PUT", "/policy", map[string]any{
			"payroll_cutoff_day": 25, "kasbon_window_start_day": a, "kasbon_window_end_day": b,
			"kasbon_max_amount": 500000, "kasbon_max_installments": 2, "kasbon_permanent_only": true,
			"kasbon_min_score": 70, "kasbon_priority_score": 90,
		})
		if code != 200 {
			log.Fatalf("set window: %d %s", code, raw)
		}
	}
	if day < 10 || day > 20 {
		setWindow(min(day, 10), max(day, 20))
		s.logf("hari ini tgl %d di luar 10–20: jendela dilebarkan sementara ke %d–%d untuk kasus lain", day, min(day, 10), max(day, 20))
	}

	type inst struct {
		DueMonth string `json:"due_month"`
		Amount   int64  `json:"amount"`
	}
	create := func(code string, amount int64, resolution time.Time, installments []inst) (int, map[string]any, string) {
		body := map[string]any{
			"employee_id": uid(s.emp[code]), "amount": amount, "details": "Simulasi", "sending_method": "Tunai",
			"fund_source_account_id": uid(fund), "resolution_month": ym(resolution),
		}
		if installments != nil {
			body["installments"] = installments
		}
		return s.call("POST", "/kasbons", body)
	}
	const area = "Kasbon"

	c, b, raw := create("E01", 500_000, nextM, []inst{{ym(reqM), 250_000}, {ym(nextM), 250_000}})
	s.expect(area, "K1 E01 Rp500.000, 2 cicilan mulai bulan ini → diterima", 201, c)
	k1 := fmt.Sprint(b["id"])
	_, _, raw = s.call("GET", "/kasbons/"+k1, nil)
	s.expectContains(area, "K1 jalur prioritas (poin 100)", `"approval_track":"priority"`, raw)

	c, _, raw = create("E01", 100_000, reqM, nil)
	s.expect(area, "K2 E01 kasbon kedua saat K1 belum lunas → ditolak", 422, c)
	s.expectContains(area, "K2 alasan", "belum lunas", raw)

	c, _, raw = create("E03", 600_000, reqM, nil)
	s.expect(area, "K3 E03 Rp600.000 → ditolak", 422, c)
	s.expectContains(area, "K3 alasan", "500.000", raw)

	c, _, raw = create("E03", 300_000, nextM.AddDate(0, 1, 0),
		[]inst{{ym(reqM), 100_000}, {ym(nextM), 100_000}, {ym(nextM.AddDate(0, 1, 0)), 100_000}})
	s.expect(area, "K4 E03 3 cicilan → ditolak", 422, c)
	s.expectContains(area, "K4 alasan", "maksimal 2", raw)

	c, _, raw = create("E03", 300_000, nextM, nil)
	s.expect(area, "K5 E03 potong mulai bulan depan → ditolak", 422, c)
	s.expectContains(area, "K5 alasan", "bulan pengajuan", raw)

	c, _, raw = create("E09", 300_000, reqM, nil)
	s.expect(area, "K6 E09 karyawan kontrak → ditolak", 422, c)
	s.expectContains(area, "K6 alasan", "PKWTT", raw)

	c, b, raw = create("E02", 200_000, reqM, nil)
	s.expect(area, "K7 E02 poin 80 → diterima", 201, c)
	k7 := fmt.Sprint(b["id"])
	_, _, raw = s.call("GET", "/kasbons/"+k7, nil)
	s.expectContains(area, "K7 jalur evaluasi (poin 80)", `"approval_track":"evaluation"`, raw)

	c, _, raw = create("E04", 200_000, reqM, nil)
	s.expect(area, "K8 E04 poin 60 → ditolak", 422, c)
	s.expectContains(area, "K8 alasan", "tidak disetujui", raw)

	c, _, raw = s.call("POST", "/kasbons/"+k7+"/approve", map[string]any{"installments": []inst{
		{ym(reqM), 50_000}, {ym(nextM), 50_000}, {ym(nextM.AddDate(0, 1, 0)), 100_000}}})
	s.expect(area, "K9 penyetuju mengubah K7 jadi 3 cicilan → ditolak", 422, c)
	c, _, raw = s.call("POST", "/kasbons/"+k7+"/approve", map[string]any{"installments": []inst{
		{ym(reqM), 100_000}, {ym(nextM), 100_000}}})
	s.expect(area, "K10 penyetuju menyetujui K7 dengan 2 cicilan", 200, c)

	c, b, raw = s.call("GET", "/kasbons/eligibility?employee_id="+uid(s.emp["E04"]), nil)
	s.expect(area, "K11 pratinjau kelayakan E04", "false / 60", fmt.Sprintf("%v / %v", b["eligible"], b["score"]))

	c, _, raw = create("E03", 300_000, reqM, nil)
	s.expect(area, "K12 E03 Rp300.000 dipotong bulan ini → diterima", 201, c)

	// Window: close it around today.
	if day > 1 {
		setWindow(1, day-1)
	} else {
		setWindow(2, 31)
	}
	c, _, raw = create("E05", 100_000, reqM, nil)
	s.expect(area, "K13 E05 di luar periode pengajuan → ditolak", 422, c)
	s.expectContains(area, "K13 alasan", "pengajuan hanya dibuka", raw)
	setWindow(10, 20)
	_ = raw
}

// ── Report ──────────────────────────────────────────────────────────────────

func rp(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	str := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "−" + b.String()
	}
	return b.String()
}

func writeReport(dir string, today time.Time, s *simr, g4, g0 map[string]int, lines map[string]*line) {
	var b strings.Builder
	pass, fail := 0, 0
	for _, c := range s.checks {
		if c.Pass {
			pass++
		} else {
			fail++
		}
	}
	fmt.Fprintf(&b, "# Simulasi Kebijakan HRD AA (HRD-AA-001)\n\n")
	fmt.Fprintf(&b, "Dijalankan %s pada database sementara `%s` (database aplikasi tidak disentuh). Hari ini (WIB): %s.\n\n",
		time.Now().Format("2006-01-02 15:04"), simDB, today.Format("2006-01-02"))
	fmt.Fprintf(&b, "**%d pemeriksaan: %d lulus, %d gagal.**\n\n", len(s.checks), pass, fail)

	fmt.Fprintf(&b, "## Gaji Oktober 2026 (periode 26 Sep – 25 Okt)\n\n")
	fmt.Fprintf(&b, "Gaji pokok 2.600.000, gaji harian 100.000, uang makan 25.000/hari. Skala: A ≥90 semua 100%%; B ≥70 Service Charge 50%%, lainnya 0%%; C 0%%.\n\n")
	fmt.Fprintf(&b, "| Kode | Skenario | Poin (toleransi 4) | Poin (toleransi 0) | Grade | Service Charge | Kerajinan | Jabatan | Insentif libur | Potongan cuti | Potongan telat | Bruto | Netto |\n")
	fmt.Fprintf(&b, "|---|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, e := range emps {
		l := lines[e.Code]
		if l == nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", e.Code, e.Story, g4[e.Code], g0[e.Code],
			l.Grade.String, rp(l.SC), rp(l.KJ), rp(l.JB), rp(l.Holiday), rp(l.Unpaid), rp(l.Cut), rp(l.Gross), rp(l.Net))
	}

	areas := []string{}
	byArea := map[string][]check{}
	for _, c := range s.checks {
		if _, ok := byArea[c.Area]; !ok {
			areas = append(areas, c.Area)
		}
		byArea[c.Area] = append(byArea[c.Area], c)
	}
	fmt.Fprintf(&b, "\n## Semua pemeriksaan\n")
	for _, a := range areas {
		fmt.Fprintf(&b, "\n### %s\n\n| | Kasus | Diharapkan | Hasil |\n|---|---|---|---|\n", a)
		for _, c := range byArea[a] {
			mark := "✅"
			if !c.Pass {
				mark = "❌"
			}
			got := c.Got
			if len(got) > 160 {
				got = got[:160] + "…"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", mark, c.Case, esc(c.Want), esc(got))
		}
	}
	fmt.Fprintf(&b, "\n## Log\n\n```\n%s\n```\n", strings.Join(s.log, "\n"))
	must(os.WriteFile(filepath.Join(dir, "report.md"), []byte(b.String()), 0o644))

	var csv strings.Builder
	csv.WriteString("area,case,expected,actual,pass\n")
	for _, c := range s.checks {
		fmt.Fprintf(&csv, "%q,%q,%q,%q,%v\n", c.Area, c.Case, c.Want, c.Got, c.Pass)
	}
	must(os.WriteFile(filepath.Join(dir, "checks.csv"), []byte(csv.String()), 0o644))
}

func esc(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ") }
