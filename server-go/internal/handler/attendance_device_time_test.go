package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
	"inventory-app/server-go/internal/testutil"
)

// deviceFixture is a branch-bound kiosk plus one employee of that branch.
type deviceFixture struct {
	router  http.Handler
	key     string
	empID   uuid.UUID
	empCode string
}

func newDeviceFixture(t *testing.T, pool *pgxpool.Pool) deviceFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	var branchID, positionID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM branches ORDER BY created_at LIMIT 1`).Scan(&branchID); err != nil {
		t.Skipf("no branch to attach a device to: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM positions ORDER BY created_at LIMIT 1`).Scan(&positionID); err != nil {
		t.Skipf("no position for the fixture employee: %v", err)
	}

	key := "test-device-key-" + uuid.NewString()
	var deviceID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO attendance_devices (name, branch_id, api_key_hash)
		VALUES ($1, $2, $3) RETURNING id`,
		"Kiosk Test "+suffix, branchID, middleware.HashDeviceKey(key)).Scan(&deviceID); err != nil {
		t.Fatalf("create device: %v", err)
	}
	empCode := "TST-TIME-" + suffix
	var empID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id)
		VALUES ($1, $2, CURRENT_DATE, $3, $4) RETURNING id`,
		empCode, "Uji Jam "+suffix, positionID, branchID).Scan(&empID); err != nil {
		t.Fatalf("create employee: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM attendance_records WHERE employee_id = $1`, empID)
		pool.Exec(c, `DELETE FROM attendance_punches WHERE employee_id = $1`, empID)
		pool.Exec(c, `DELETE FROM employees WHERE id = $1`, empID)
		pool.Exec(c, `DELETE FROM attendance_devices WHERE id = $1`, deviceID)
	})

	queries := db.New(pool)
	h := handler.NewAttendanceDeviceHandler(pool, queries)
	r := chi.NewRouter()
	r.Use(middleware.DeviceAuth(queries))
	r.Post("/event", h.Event)
	r.Get("/today", h.Today)
	r.Get("/employees/{code}/history", h.History)
	return deviceFixture{router: r, key: key, empID: empID, empCode: empCode}
}

func (f deviceFixture) post(t *testing.T, form url.Values) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Device-Key", f.key)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("event: status %d: %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// A kiosk whose clock was set back three hours still records the server's time,
// and a retry of the same event does not log a second punch.
func TestDeviceEventUsesServerTime(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := newDeviceFixture(t, pool)
	ctx := context.Background()

	spoofed := time.Now().Add(-3 * time.Hour).Format(time.RFC3339)
	eventID := uuid.NewString()
	form := url.Values{
		"employee_code":   {f.empCode},
		"event_type":      {"check_in"},
		"timestamp":       {spoofed},
		"age_ms":          {"40"},
		"client_event_id": {eventID},
	}
	before := time.Now()
	first := f.post(t, form)

	got, err := time.Parse(time.RFC3339, first["timestamp"].(string))
	if err != nil {
		t.Fatalf("timestamp %v: %v", first["timestamp"], err)
	}
	if d := got.Sub(before); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("punch recorded at %s, want server time ≈ %s (device claimed %s)", got, before, spoofed)
	}
	if first["time_source"] != "server" || first["time_verified"] != true {
		t.Fatalf("time_source = %v verified = %v, want server/true", first["time_source"], first["time_verified"])
	}

	var src string
	var reported time.Time
	if err := pool.QueryRow(ctx, `
		SELECT time_source, device_reported_at FROM attendance_punches
		WHERE employee_id = $1`, f.empID).Scan(&src, &reported); err != nil {
		t.Fatalf("read punch: %v", err)
	}
	if src != "server" {
		t.Fatalf("stored time_source = %s, want server", src)
	}
	// device_reported_at keeps the spoofed claim for audit.
	if want, _ := time.Parse(time.RFC3339, spoofed); !reported.Equal(want) {
		t.Fatalf("device_reported_at = %s, want the device's claim %s", reported, want)
	}

	// Retry after a lost response: the app re-sends the same id with a larger age.
	time.Sleep(1100 * time.Millisecond)
	form.Set("age_ms", "1140")
	retry := f.post(t, form)
	if retry["duplicate"] != true || retry["timestamp"] != first["timestamp"] {
		t.Fatalf("retry: duplicate=%v timestamp=%v, want true and %v", retry["duplicate"], retry["timestamp"], first["timestamp"])
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM attendance_punches WHERE employee_id = $1`, f.empID).Scan(&n)
	if n != 1 {
		t.Fatalf("punches after retry = %d, want 1", n)
	}

	// The kiosk's "today" screen reports the server's check-in, verified —
	// when the punch belongs to today. Shortly after midnight the shift window
	// assigns it to yesterday's work day, which "today" rightly doesn't list.
	var workDate time.Time
	if err := pool.QueryRow(ctx, `SELECT work_date FROM attendance_punches WHERE employee_id = $1`, f.empID).Scan(&workDate); err != nil {
		t.Fatalf("read work date: %v", err)
	}
	if !workDate.Equal(service.TodayWIB()) {
		t.Logf("punch at %v belongs to work day %s; skipping the today-list check", first["timestamp"], workDate.Format("2006-01-02"))
		return
	}
	req := httptest.NewRequest(http.MethodGet, "/today", nil)
	req.Header.Set("X-Device-Key", f.key)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("today: status %d: %s", rr.Code, rr.Body.String())
	}
	var today struct {
		Employees []struct {
			EmployeeCode    string  `json:"employee_code"`
			CheckIn         *string `json:"check_in"`
			CheckInVerified bool    `json:"check_in_verified"`
		} `json:"employees"`
	}
	json.Unmarshal(rr.Body.Bytes(), &today)
	found := false
	for _, e := range today.Employees {
		if e.EmployeeCode != f.empCode {
			continue
		}
		found = true
		if e.CheckIn == nil || *e.CheckIn != first["timestamp"] || !e.CheckInVerified {
			t.Fatalf("today entry: check_in=%v verified=%v, want %v/true", e.CheckIn, e.CheckInVerified, first["timestamp"])
		}
	}
	if !found {
		t.Fatalf("employee %s missing from today's list", f.empCode)
	}
}

// Without age_ms (a rebooted kiosk or an old app build) a timestamp far from
// the server's clock is kept but marked unverified, and a future one refused.
func TestDeviceEventWithoutAgeIsUnverified(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := newDeviceFixture(t, pool)

	past := time.Now().Add(-40 * time.Minute).Format(time.RFC3339)
	out := f.post(t, url.Values{
		"employee_code": {f.empCode},
		"event_type":    {"check_in"},
		"timestamp":     {past},
	})
	if out["time_source"] != "device" || out["time_verified"] != false || out["timestamp"] == nil {
		t.Fatalf("got time_source=%v verified=%v, want device/false", out["time_source"], out["time_verified"])
	}

	future := url.Values{
		"employee_code": {f.empCode},
		"event_type":    {"check_in"},
		"timestamp":     {time.Now().Add(time.Hour).Format(time.RFC3339)},
	}
	req := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(future.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Device-Key", f.key)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("future timestamp: status %d, want 400", rr.Code)
	}
}

// The kiosk's 30-day history: every day of the window, newest first, with the
// late minutes, Alpha outcome and totals the employee needs.
func TestDeviceAttendanceHistory(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := newDeviceFixture(t, pool)
	ctx := context.Background()

	today := service.TodayWIB()
	day := func(back int) string { return today.AddDate(0, 0, -back).Format("2006-01-02") }
	// 1 day ago late 12 min; 2 days ago Alpha; 40 days ago (outside the window).
	if _, err := pool.Exec(ctx, `
		INSERT INTO attendance_records (employee_id, date, status, check_in, check_in_source, is_late, late_minutes)
		VALUES ($1, $2, 'present', $3, 'manual', true, 12)`,
		f.empID, day(1), today.AddDate(0, 0, -1).Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, back := range []int{2, 40} {
		if _, err := pool.Exec(ctx, `INSERT INTO attendance_records (employee_id, date, status) VALUES ($1, $2, 'absent')`,
			f.empID, day(back)); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/employees/"+f.empCode+"/history", nil)
	req.Header.Set("X-Device-Key", f.key)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		History service.AttendanceHistory `json:"history"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	h := out.History
	if len(h.Days) != service.HistoryWindowDays || h.Days[0].Date != day(0) || h.Days[29].Date != day(29) {
		t.Fatalf("window: %d days, first %s last %s", len(h.Days), h.Days[0].Date, h.Days[len(h.Days)-1].Date)
	}
	late := h.Days[1]
	if !late.IsLate || late.LateMinutes != 12 || late.CheckIn == nil || !late.CheckInVerified {
		t.Fatalf("late day: %+v", late)
	}
	alpha := h.Days[2]
	if alpha.Status == nil || *alpha.Status != "absent" || alpha.Alpha == nil {
		t.Fatalf("alpha day: %+v", alpha)
	}
	if h.Summary.Late != 1 || h.Summary.LateMinutes != 12 || h.Summary.Alpha != 1 || h.Summary.Present != 1 {
		t.Fatalf("summary: %+v", h.Summary)
	}

	// Another branch's device may not read it.
	req = httptest.NewRequest(http.MethodGet, "/employees/NO-SUCH-CODE/history", nil)
	req.Header.Set("X-Device-Key", f.key)
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown code: %d", rr.Code)
	}
}

// Two kiosks sending the same employee's punches at the same moment (a queued
// offline punch flushing while they punch live elsewhere) must both end up in
// the day's record. Without the per-employee lock, each transaction rebuilds
// the day from a punch log that lacks the other's punch and the second UPDATE
// overwrites the first.
func TestConcurrentDevicePunchesBothCount(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx := context.Background()
	const rounds = 12
	lost := 0
	for i := 0; i < rounds; i++ {
		f := newDeviceFixture(t, pool)
		// Seed the day's record first so both events take the UPDATE path,
		// which is where an update can be lost (an INSERT collision fails loudly).
		f.post(t, url.Values{"employee_code": {f.empCode}, "event_type": {"auto"},
			"age_ms": {"1200000"}, "client_event_id": {uuid.NewString()}}) // 20 min ago

		var wg sync.WaitGroup
		for _, age := range []string{"0", "600000"} { // now, and 10 min ago
			wg.Add(1)
			go func(age string) {
				defer wg.Done()
				form := url.Values{"employee_code": {f.empCode}, "event_type": {"auto"},
					"age_ms": {age}, "client_event_id": {uuid.NewString()}}
				req := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("X-Device-Key", f.key)
				f.router.ServeHTTP(httptest.NewRecorder(), req)
			}(age)
		}
		wg.Wait()

		// The record must match a fresh derivation from the full punch log:
		// first punch = check-in, last = check-out.
		var firstPunch, lastPunch, checkIn, checkOut time.Time
		if err := pool.QueryRow(ctx, `
			SELECT min(p.punched_at), max(p.punched_at), ar.check_in, ar.check_out
			FROM attendance_punches p
			JOIN attendance_records ar ON ar.employee_id = p.employee_id AND ar.date = p.work_date
			WHERE p.employee_id = $1
			GROUP BY ar.check_in, ar.check_out`, f.empID).Scan(&firstPunch, &lastPunch, &checkIn, &checkOut); err != nil {
			t.Fatalf("round %d: read record: %v", i, err)
		}
		if !checkIn.Equal(firstPunch) || !checkOut.Equal(lastPunch) {
			lost++
			t.Logf("round %d: record in=%s out=%s, punch log %s..%s", i, checkIn, checkOut, firstPunch, lastPunch)
		}
	}
	if lost > 0 {
		t.Fatalf("%d of %d rounds lost a punch", lost, rounds)
	}
}

// A kiosk that was offline sends a punch for a day the nightly job already
// judged as Alpha. The day becomes present, and its Alpha penalty must go.
func TestLatePunchLiftsAlphaPenalty(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := newDeviceFixture(t, pool)
	ctx := context.Background()
	q := db.New(pool)
	t.Cleanup(func() { // runs before the fixture's own cleanup (LIFO)
		c := context.Background()
		pool.Exec(c, `DELETE FROM performance_violations WHERE employee_id = $1`, f.empID)
		pool.Exec(c, `DELETE FROM performance_scores WHERE employee_id = $1`, f.empID)
	})

	today := service.TodayWIB()
	day := today.AddDate(0, 0, -3)
	if day.Day() < 2 {
		t.Skip("needs earlier days in the same month for the absence grace")
	}
	// Use up the monthly absence grace on earlier days, so the target day's
	// Alpha does draw a violation (as it would in real life).
	grace := service.AbsenceGraceDays(ctx, q)
	if grace >= day.Day()-1 {
		t.Skipf("absence grace %d leaves no room before %s", grace, day.Format("2006-01-02"))
	}
	for i := 1; i <= grace; i++ {
		pool.Exec(ctx, `INSERT INTO attendance_records (employee_id, date, status) VALUES ($1, $2, 'absent')`,
			f.empID, time.Date(day.Year(), day.Month(), i, 0, 0, 0, 0, time.UTC))
	}
	var recID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO attendance_records (employee_id, date, status) VALUES ($1, $2, 'absent') RETURNING id`,
		f.empID, day).Scan(&recID); err != nil {
		t.Fatal(err)
	}
	// What the nightly job does for that day.
	rec, err := q.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{
		EmployeeID: pgtype.UUID{Bytes: f.empID, Valid: true}, Date: pgtype.Date{Time: day, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteAutoViolationsForRecord(ctx, q, rec); err != nil {
		t.Fatal(err)
	}
	countAlpha := func() int {
		var n int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM performance_violations v
			JOIN performance_policies p ON p.id = v.policy_id
			WHERE v.attendance_record_id = $1 AND p.rule_type = 'absent_no_leave'`, recID).Scan(&n)
		return n
	}
	if countAlpha() == 0 {
		t.Skip("no active absent_no_leave policy in this database")
	}

	// The late punch: midday of that day, sent now with its true age.
	target := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, service.WIB)
	age := time.Since(target).Milliseconds()
	f.post(t, url.Values{"employee_code": {f.empCode}, "event_type": {"auto"},
		"age_ms": {fmt.Sprint(age)}, "client_event_id": {uuid.NewString()}})

	var status string
	pool.QueryRow(ctx, `SELECT status FROM attendance_records WHERE id = $1`, recID).Scan(&status)
	if status != "present" {
		t.Fatalf("day status = %q, want present", status)
	}
	if n := countAlpha(); n != 0 {
		t.Fatalf("Alpha violation still on the day after the late punch (%d)", n)
	}
}
