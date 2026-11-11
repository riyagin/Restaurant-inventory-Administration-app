package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type sim struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	q      *db.Queries
	router http.Handler
	empIDs map[string]pgtype.UUID // DBCode -> id
	log    []string
	// devMode: loading into the app database. Live policies and HR settings
	// are kept, and absent days are reconciled for the SIM employees only.
	devMode     bool
	branchSched service.Schedule
}

func (s *sim) logf(f string, a ...any) {
	line := fmt.Sprintf(f, a...)
	s.log = append(s.log, line)
	fmt.Println("  " + line)
}

func newSim(ctx context.Context, url string) (*sim, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	s := &sim{ctx: ctx, pool: pool, q: db.New(pool), empIDs: map[string]pgtype.UUID{},
		branchSched: service.Schedule{WorkStartMinutes: branchStart, WorkEndMinutes: branchEnd, GraceMinutes: graceMin,
			EarlyLeaveMinutes: earlyTolMin, WorkDays: []int{1, 2, 3, 4, 5, 6, 7}}}

	ah := handler.NewAttendanceHandler(pool, s.q)
	r := chi.NewRouter()
	admin := &middleware.Claims{UserID: uuid.Nil, Username: "simulator", Role: middleware.RoleSuperuser}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(middleware.ContextWithClaims(r.Context(), admin)))
		})
	})
	r.Post("/fp/confirm", ah.FingerprintConfirm)
	r.Put("/att/{id}", ah.Update)
	r.Post("/att/{id}/half-day", ah.SetHalfDay)
	r.Post("/att/{id}/present-no-punch", ah.MarkPresentNoPunch)
	permH := handler.NewAttendancePermissionHandler(pool, s.q)
	r.Post("/permits", permH.Create)
	ph := handler.NewPerformanceHandler(pool, s.q)
	r.Get("/perf/adjustments", ph.AdjustmentPreview)
	r.Post("/perf/adjustments/apply", ph.AdjustmentApply)
	s.router = r
	return s, nil
}

// grantPermits files and approves every planned permit through the real
// handler, the day before it applies (approve_now as a manager would).
func (s *sim) grantPermits(truth []*dayTruth) error {
	for _, t := range truth {
		if t.Permit == "" {
			continue
		}
		b, _ := json.Marshal(map[string]any{
			"employee_id": uuid.UUID(s.empIDs[t.Emp.DBCode].Bytes).String(),
			"date":        t.Date.Format("2006-01-02"), "type": t.Permit,
			"reason": "Simulasi: izin H-1", "approve_now": true,
		})
		req := httptest.NewRequest(http.MethodPost, "/permits", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			return fmt.Errorf("permit %s %s: %d %s", t.Emp.DBCode, t.Permit, rec.Code, rec.Body.String())
		}
		s.logf("permit %-15s %s day %d -> HTTP %d", t.Permit, t.Emp.DBCode, t.Day, rec.Code)
	}
	return nil
}

// adjust drives the "Penyesuaian Evaluasi" screen: preview the month's
// re-evaluation, then apply it, through the real HTTP handlers.
func (s *sim) adjust(month string) (*service.AdjustmentPreview, error) {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/perf/adjustments?month="+month, nil))
	if rec.Code != 200 {
		return nil, fmt.Errorf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var p service.AdjustmentPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		return nil, err
	}
	reasons := map[string]int{}
	for _, e := range p.Employees {
		for _, c := range e.Changes {
			reasons[c.Reason]++
		}
	}
	s.logf("adjustment preview %s: %d employees, %d violations removed, %d added, %d new absent days, %d leave punches, %d days to review, reasons %v",
		month, len(p.Employees), p.Removed, p.Added, p.NewAbsent, len(p.LeavePunches), len(p.ReviewDays), reasons)
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/perf/adjustments/apply?month="+month, nil))
	if rec.Code != 200 {
		return nil, fmt.Errorf("apply: %d %s", rec.Code, rec.Body.String())
	}
	s.logf("adjustment apply %s -> HTTP %d", month, rec.Code)
	return &p, nil
}

// ── Seed ─────────────────────────────────────────────────────────────────────

func (s *sim) seed(emps []*simEmployee, truth []*dayTruth) error {
	ex := func(sql string, args ...any) error {
		_, err := s.pool.Exec(s.ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("%w\n%s", err, sql)
		}
		return nil
	}
	var branchID, posID pgtype.UUID
	if err := s.pool.QueryRow(s.ctx, `INSERT INTO branches (name) VALUES ($1) RETURNING id`, simBranchName).Scan(&branchID); err != nil {
		return err
	}
	if err := s.pool.QueryRow(s.ctx, `INSERT INTO positions (name, is_active) VALUES ($1, true) RETURNING id`, simPositionName).Scan(&posID); err != nil {
		return err
	}
	if err := ex(`INSERT INTO work_schedules (branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days)
		VALUES ($1, '08:30', '21:30', $2, $3, '{1,2,3,4,5,6,7}')`, branchID, graceMin, earlyTolMin); err != nil {
		return err
	}
	shiftIDs := map[string]pgtype.UUID{}
	for code, sh := range shifts {
		var id pgtype.UUID
		var s2, e2 any
		if sh.S2 >= 0 {
			s2, e2 = hm(sh.S2), hm(sh.E2)
		}
		if err := s.pool.QueryRow(s.ctx, `INSERT INTO shift_definitions (branch_id, code, name, start1, end1, start2, end2, break_minutes)
			VALUES ($1,$2,$3,$4::time,$5::time,$6::time,$7::time,$8) RETURNING id`,
			branchID, code, sh.Name, hm(sh.S1), hm(sh.E1), s2, e2, sh.Break).Scan(&id); err != nil {
			return err
		}
		shiftIDs[code] = id
	}
	if s.devMode {
		return s.seedPeople(emps, truth, ex, branchID, posID, shiftIDs)
	}
	// Live performance policies (copied from the dev database, 2026-11-10).
	pol := []struct {
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
		{"late", "Terlambat lebih dari 30 menit", 30, 10},
		{"manual", "Tidak melakukan check-in dan check-out dalam satu hari", nil, 10},
		{"missing_checkout", "Tidak check-out (lupa absen) tanpa konfirmasi", nil, 10},
	}
	for _, p := range pol {
		if err := ex(`INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active) VALUES ($1,$2,$3,$4,true)`,
			p.name, p.rule, p.thr, p.pts); err != nil {
			return err
		}
	}
	// The live rows are seeded after migrations ran, so apply 083 (late-tier
	// threshold fix) to them the way it will run against production data.
	if fix, err := os.ReadFile("migrations/083_late_policy_over_30_threshold.up.sql"); err == nil {
		if err := ex(string(fix)); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("read migration 083: %w", err)
	}
	if err := ex(`INSERT INTO hr_settings (id, absence_grace_days) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET absence_grace_days = EXCLUDED.absence_grace_days`, absenceGraceDays); err != nil {
		return err
	}
	return s.seedPeople(emps, truth, ex, branchID, posID, shiftIDs)
}

// seedPeople inserts the employees, their roster and approved leave.
func (s *sim) seedPeople(emps []*simEmployee, truth []*dayTruth, ex func(string, ...any) error,
	branchID, posID pgtype.UUID, shiftIDs map[string]pgtype.UUID) error {
	for _, e := range emps {
		var id pgtype.UUID
		if err := s.pool.QueryRow(s.ctx, `INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id)
			VALUES ($1,$2,'2025-01-06',$3,$4) RETURNING id`, e.DBCode, e.Name, posID, branchID).Scan(&id); err != nil {
			return err
		}
		s.empIDs[e.DBCode] = id
	}
	// Wage structure + uang makan (a daily_allowance paid in cash per present
	// day), so the evaluation screen can show what each employee took home.
	// Its own component, so the live "Uang Makan" configuration is never touched.
	var makanID pgtype.UUID
	if err := s.pool.QueryRow(s.ctx, `INSERT INTO wage_components (name, type, calc_method) VALUES ($1, 'daily_allowance', 'per_present_day') RETURNING id`,
		simMealComponent).Scan(&makanID); err != nil {
		return err
	}
	for i, e := range emps {
		var wsID pgtype.UUID
		if err := s.pool.QueryRow(s.ctx, `INSERT INTO wage_structures (employee_id, base_salary, working_days_per_month, daily_rate, effective_date)
			VALUES ($1, $2, 26, $3, '2025-01-06') RETURNING id`, s.empIDs[e.DBCode], dailyRate*26, dailyRate).Scan(&wsID); err != nil {
			return err
		}
		rate := int64(25_000) // crew; every fourth employee is a senior at 35.000
		if i%4 == 3 {
			rate = 35_000
		}
		if err := ex(`INSERT INTO employee_wage_components (wage_structure_id, wage_component_id, amount) VALUES ($1,$2,$3)`, wsID, makanID, rate); err != nil {
			return err
		}
	}
	// Roster + approved leave (UpsertLeaveAttendance is what leave approval runs).
	for _, t := range truth {
		id := s.empIDs[t.Emp.DBCode]
		if t.Kind == "leave" {
			if err := s.q.UpsertLeaveAttendance(s.ctx, &db.UpsertLeaveAttendanceParams{EmployeeID: id, Date: pgD(t.Date)}); err != nil {
				return err
			}
			continue
		}
		if !t.Rostered {
			continue
		}
		if err := ex(`INSERT INTO shift_assignments (employee_id, date, shift_id) VALUES ($1,$2,$3)`,
			id, pgD(t.Date), shiftIDs[t.Shift]); err != nil {
			return err
		}
	}
	return nil
}

func hm(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
func pgD(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

// ── Steps that mirror production code paths ──────────────────────────────────

// importDat posts an attlog.dat to the real FingerprintConfirm handler.
func (s *sim) importDat(name string, data []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/fp/confirm", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		return fmt.Errorf("import %s: %d %s", name, rec.Code, rec.Body.String())
	}
	s.logf("import %s -> %s", name, bytes.TrimSpace(rec.Body.Bytes()))
	return nil
}

// nightly mirrors the cmd/api ticker body for one day.
func (s *sim) nightly(d time.Time) error {
	if _, err := service.ReconcileAbsent(s.ctx, s.q, d); err != nil {
		return err
	}
	return service.EvaluateDay(s.ctx, s.q, d)
}

// backfill mirrors POST /api/hr/performance/evaluate (handler/performance.go).
func (s *sim) backfill(from, to time.Time) error {
	tx, err := s.pool.Begin(s.ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(s.ctx)
	qtx := s.q.WithTx(tx)
	if err := qtx.DeleteAutoViolationsForRange(s.ctx, &db.DeleteAutoViolationsForRangeParams{From: pgD(from), To: pgD(to)}); err != nil {
		return err
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if s.devMode {
			if err := s.reconcileSimOnly(s.ctx, qtx, d); err != nil {
				return err
			}
			continue
		}
		if _, err := service.ReconcileAbsent(s.ctx, qtx, d); err != nil {
			return err
		}
	}
	if err := service.EvaluateRange(s.ctx, qtx, from, to); err != nil {
		return err
	}
	return tx.Commit(s.ctx)
}

func (s *sim) recordID(code string, d time.Time) (string, error) {
	var id pgtype.UUID
	err := s.pool.QueryRow(s.ctx, `SELECT id FROM attendance_records WHERE employee_id=$1 AND date=$2`, s.empIDs[code], pgD(d)).Scan(&id)
	if err != nil {
		return "", err
	}
	return uuid.UUID(id.Bytes).String(), nil
}

// applyCorrection drives the real correction handlers the way the HR UI does.
func (s *sim) applyCorrection(t *dayTruth) error {
	id, err := s.recordID(t.Emp.DBCode, t.Date)
	if err != nil {
		return fmt.Errorf("record %s %d: %w", t.Emp.DBCode, t.Day, err)
	}
	ts := func(hhmm string) string { return at(t.Day, hhmm).Format(time.RFC3339) }
	var method, path string
	var body any
	switch t.Corr.Kind {
	case "no_punch":
		method, path, body = http.MethodPost, "/att/"+id+"/present-no-punch", map[string]any{"note": t.Corr.Note}
	case "half_late":
		method, path, body = http.MethodPost, "/att/"+id+"/half-day", map[string]any{"type": "late", "start_time": ts(t.Corr.At), "note": t.Corr.Note}
	case "half_early":
		method, path, body = http.MethodPost, "/att/"+id+"/half-day", map[string]any{"type": "early", "end_time": ts(t.Corr.At), "note": t.Corr.Note}
	case "set_checkout":
		v := ts(t.Corr.At)
		method, path, body = http.MethodPut, "/att/"+id, map[string]any{"check_out": v, "note": t.Corr.Note}
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		return fmt.Errorf("correction %s %s %d: %d %s", t.Corr.Kind, t.Emp.DBCode, t.Day, rec.Code, rec.Body.String())
	}
	s.logf("correction %-12s %s day %d -> HTTP %d", t.Corr.Kind, t.Emp.DBCode, t.Day, rec.Code)
	return nil
}

// ── Snapshot ─────────────────────────────────────────────────────────────────

type sysRecord struct {
	Code                              string
	Date                              time.Time
	Status                            string
	CheckIn, CheckOut                 *time.Time
	InSrc, OutSrc                     string
	IsLate                            bool
	LateMin                           int
	IsEarly                           bool
	EarlyMin                          int
	MissOut, MissIn, NoPunch, HalfDay bool
	HalfLost                          int
	HalfType                          string
	Points                            int
	Rules                             []string
	CutMin                            int
	CutAmt                            int64
}

type snapshot struct {
	Records map[string]*sysRecord // code|day
	Scores  map[string]int
	Cuts    map[string]int64
}

func key(code string, d int) string { return fmt.Sprintf("%s|%02d", code, d) }

func (s *sim) snapshot(emps []*simEmployee) (*snapshot, error) {
	snap := &snapshot{Records: map[string]*sysRecord{}, Scores: map[string]int{}, Cuts: map[string]int64{}}
	rows, err := s.pool.Query(s.ctx, `
		SELECT e.employee_code, ar.date, ar.status, ar.check_in, ar.check_out,
		       COALESCE(ar.check_in_source,''), COALESCE(ar.check_out_source,''),
		       ar.is_late, ar.late_minutes, ar.is_early_leave, ar.early_leave_minutes,
		       ar.is_missing_checkout, ar.is_missing_checkin, ar.is_no_punch,
		       ar.is_half_day, ar.half_day_lost_minutes, COALESCE(ar.half_day_type,''),
		       COALESCE((SELECT SUM(v.points) FROM performance_violations v WHERE v.attendance_record_id = ar.id),0)::int,
		       COALESCE((SELECT array_agg(p.name ORDER BY p.name) FROM performance_violations v JOIN performance_policies p ON p.id=v.policy_id WHERE v.attendance_record_id = ar.id), '{}')
		FROM attendance_records ar JOIN employees e ON e.id = ar.employee_id
		WHERE ar.date >= $1 AND ar.date <= $2`, pgD(day(1)), pgD(day(daysInMonth())))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		r := &sysRecord{}
		var d pgtype.Date
		var ci, co pgtype.Timestamptz
		var late, early, lost int32
		if err := rows.Scan(&r.Code, &d, &r.Status, &ci, &co, &r.InSrc, &r.OutSrc, &r.IsLate, &late, &r.IsEarly, &early,
			&r.MissOut, &r.MissIn, &r.NoPunch, &r.HalfDay, &lost, &r.HalfType, &r.Points, &r.Rules); err != nil {
			return nil, err
		}
		r.Date, r.LateMin, r.EarlyMin, r.HalfLost = d.Time, int(late), int(early), int(lost)
		if ci.Valid {
			t := ci.Time
			r.CheckIn = &t
		}
		if co.Valid {
			t := co.Time
			r.CheckOut = &t
		}
		snap.Records[key(r.Code, d.Time.Day())] = r
	}
	rows.Close()

	branch := service.Schedule{WorkStartMinutes: branchStart, WorkEndMinutes: branchEnd, GraceMinutes: graceMin, EarlyLeaveMinutes: earlyTolMin}
	for _, e := range emps {
		id := s.empIDs[e.DBCode]
		var score int32 = 100
		_ = s.pool.QueryRow(s.ctx, `SELECT score FROM performance_scores WHERE employee_id=$1 AND period_month=$2`, id, pgD(day(1))).Scan(&score)
		snap.Scores[e.DBCode] = int(score)
		cuts, err := service.AttendanceCutsForPeriod(s.ctx, s.q, id, day(1), day(daysInMonth()), dailyRate, branch)
		if err != nil {
			return nil, err
		}
		snap.Cuts[e.DBCode] = cuts.Amount
		for _, cd := range cuts.Days {
			dd, _ := time.Parse("2006-01-02", cd.Date)
			if r := snap.Records[key(e.DBCode, dd.Day())]; r != nil {
				r.CutMin, r.CutAmt = cd.CutMinutes, cd.Amount
			}
		}
	}
	return snap, nil
}
