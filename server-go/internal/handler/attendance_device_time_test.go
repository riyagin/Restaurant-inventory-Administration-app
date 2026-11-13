package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
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

	// The kiosk's "today" screen reports the server's check-in, verified.
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
