package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// Cuti (a uses_quota leave type) needs no approval: it is saved approved with
// its allowance charged, or refused outright when the allowance is short.
// Other leave types still wait for a manager.
func TestCutiIsApprovedOnCreate(t *testing.T) {
	pool := testutil.OpenDB(t)
	bg := context.Background()
	userID := createTestUser(t, pool)
	ctx := middleware.ContextWithClaims(bg, testClaims(userID))
	h := handler.NewLeaveHandler(pool, db.New(pool))

	suffix := uuid.NewString()[:8]
	branchID, positionID, empID := uuid.New(), uuid.New(), uuid.New()
	cutiType, sakitType := uuid.New(), uuid.New()
	if _, err := pool.Exec(bg, `INSERT INTO branches (id, name) VALUES ($1, 'BrCuti-'||$2)`, branchID, suffix); err != nil {
		t.Fatalf("branch: %v", err)
	}
	if _, err := pool.Exec(bg, `INSERT INTO positions (id, name, is_active) VALUES ($1, 'PosCuti-'||$2, true)`, positionID, suffix); err != nil {
		t.Fatalf("position: %v", err)
	}
	if _, err := pool.Exec(bg, `INSERT INTO employees (id, employee_code, full_name, join_date, position_id, branch_id)
		VALUES ($1, 'CUTI-'||$2, 'Uji Cuti '||$2, '2026-01-05', $3, $4)`, empID, suffix, positionID, branchID); err != nil {
		t.Fatalf("employee: %v", err)
	}
	if _, err := pool.Exec(bg, `INSERT INTO leave_types (id, name, is_paid, uses_quota) VALUES
		($1, 'CutiUji-'||$3, true, true), ($2, 'SakitUji-'||$3, true, false)`, cutiType, sakitType, suffix); err != nil {
		t.Fatalf("leave types: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(bg, `DELETE FROM activity_log WHERE entity_type = 'leave_request' AND entity_id IN (SELECT id FROM leave_requests WHERE employee_id = $1)`, empID)
		pool.Exec(bg, `DELETE FROM leave_requests WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM attendance_records WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM leave_types WHERE id IN ($1, $2)`, cutiType, sakitType)
		pool.Exec(bg, `DELETE FROM employees WHERE id = $1`, empID)
		pool.Exec(bg, `DELETE FROM positions WHERE id = $1`, positionID)
		pool.Exec(bg, `DELETE FROM branches WHERE id = $1`, branchID)
	})

	req := func(typeID uuid.UUID, start, end string) map[string]any {
		return map[string]any{"employee_id": empID.String(), "leave_type_id": typeID.String(), "start_date": start, "end_date": end}
	}
	var out struct {
		Status string `json:"status"`
	}

	// Tue 2 Jul 2030 + Wed 3 Jul: 2 of the 4-day allowance, approved at once.
	rr := postJSON(t, h.CreateLeaveRequest, ctx, req(cutiType, "2030-07-02", "2030-07-03"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("cuti: want 201, got %d %s", rr.Code, rr.Body)
	}
	decodeJSON(t, rr, &out)
	if out.Status != "approved" {
		t.Fatalf("cuti status = %q, want approved", out.Status)
	}
	var used int
	if err := pool.QueryRow(bg, `SELECT COALESCE(SUM(cost), 0) FROM leave_quota_usage WHERE employee_id = $1`, empID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 2 {
		t.Fatalf("allowance used = %d, want 2", used)
	}

	// Thu 4 – Sat 6 Jul costs 1+1+2 = 4 > 2 left: refused, nothing saved.
	rr = postJSON(t, h.CreateLeaveRequest, ctx, req(cutiType, "2030-07-04", "2030-07-06"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("over allowance: want 409, got %d %s", rr.Code, rr.Body)
	}
	var n int
	if err := pool.QueryRow(bg, `SELECT COUNT(*) FROM leave_requests WHERE employee_id = $1`, empID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("leave requests = %d, want 1 (refused request must not be saved)", n)
	}

	// Sakit still waits for a manager.
	rr = postJSON(t, h.CreateLeaveRequest, ctx, req(sakitType, "2030-07-10", "2030-07-10"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("sakit: want 201, got %d %s", rr.Code, rr.Body)
	}
	decodeJSON(t, rr, &out)
	if out.Status != "pending" {
		t.Fatalf("sakit status = %q, want pending", out.Status)
	}
}
