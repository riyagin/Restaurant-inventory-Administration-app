package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// The backlog left by the switch to auto-approved Cuti: pending requests filed
// before it. The preview must predict the real run exactly — including a later
// request failing because earlier ones in the same batch used the allowance —
// and change nothing; the real run then approves what the preview said.
func TestBulkApprovalPreviewThenApprove(t *testing.T) {
	pool := testutil.OpenDB(t)
	bg := context.Background()
	userID := createTestUser(t, pool)
	ctx := middleware.ContextWithClaims(bg, testClaims(userID))
	queries := db.New(pool)
	h := handler.NewBulkApprovalHandler(pool, queries, handler.NewLeaveHandler(pool, queries), handler.NewShiftHandler(pool, queries))

	suffix := uuid.NewString()[:8]
	branchID, positionID, empID := uuid.New(), uuid.New(), uuid.New()
	cutiType, sakitType := uuid.New(), uuid.New()
	l1, l2, l3, l4, o1 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(bg, sql, args...); err != nil {
			t.Fatalf("%s: %v", strings.Fields(sql)[2], err)
		}
	}
	mustExec(`INSERT INTO branches (id, name) VALUES ($1, 'BrBulk-'||$2)`, branchID, suffix)
	mustExec(`INSERT INTO positions (id, name, is_active) VALUES ($1, 'PosBulk-'||$2, true)`, positionID, suffix)
	mustExec(`INSERT INTO employees (id, employee_code, full_name, join_date, position_id, branch_id)
		VALUES ($1, 'BULK-'||$2, 'Uji Bulk '||$2, '2026-01-05', $3, $4)`, empID, suffix, positionID, branchID)
	mustExec(`INSERT INTO leave_types (id, name, is_paid, uses_quota) VALUES
		($1, 'CutiBulk-'||$3, true, true), ($2, 'SakitBulk-'||$3, true, false)`, cutiType, sakitType, suffix)
	t.Cleanup(func() {
		pool.Exec(bg, `DELETE FROM activity_log WHERE entity_id IN ($1, $2, $3, $4, $5)`, l1, l2, l3, l4, o1)
		pool.Exec(bg, `DELETE FROM leave_quota_usage WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM leave_requests WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM overtime_requests WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM attendance_records WHERE employee_id = $1`, empID)
		pool.Exec(bg, `DELETE FROM leave_types WHERE id IN ($1, $2)`, cutiType, sakitType)
		pool.Exec(bg, `DELETE FROM employees WHERE id = $1`, empID)
		pool.Exec(bg, `DELETE FROM positions WHERE id = $1`, positionID)
		pool.Exec(bg, `DELETE FROM branches WHERE id = $1`, branchID)
	})

	// Period Jul 2030 (26 Jun – 25 Jul), allowance 4. Weekdays cost 1.
	//   l1 Tue 2 – Wed 3 Jul  cuti, 2   → ok   (2 used)
	//   l2 Thu 4 – Fri 5 Jul  cuti, 2   → ok   (4 used)
	//   l3 Mon 8 Jul          cuti, 1   → fails: l1+l2 already took the allowance
	//   l4 Wed 10 Jul         sakit, backdated → skipped unless included
	//   o1 Thu 11 Jul         overtime 2h → ok
	mustExec(`INSERT INTO leave_requests (id, employee_id, leave_type_id, start_date, end_date, day_count, status, is_backdated) VALUES
		($1, $5, $6, '2030-07-02', '2030-07-03', 2, 'pending', false),
		($2, $5, $6, '2030-07-04', '2030-07-05', 2, 'pending', false),
		($3, $5, $6, '2030-07-08', '2030-07-08', 1, 'pending', false),
		($4, $5, $7, '2030-07-10', '2030-07-10', 1, 'pending', true)`, l1, l2, l3, l4, empID, cutiType, sakitType)
	mustExec(`INSERT INTO overtime_requests (id, employee_id, date, hours, status) VALUES ($1, $2, '2030-07-11', 2, 'pending')`, o1, empID)

	type item struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	statuses := func(items []item) map[string]string {
		m := map[string]string{}
		for _, it := range items {
			m[it.ID] = it.Status
		}
		return m
	}
	expect := func(label string, got map[string]string, want map[uuid.UUID]string) {
		t.Helper()
		for id, w := range want {
			if got[id.String()] != w {
				t.Errorf("%s: %s = %q, want %q (all: %v)", label, id, got[id.String()], w, got)
			}
		}
	}
	countWhere := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(bg, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// ── Preview without backdated ────────────────────────────────────────────
	var prev struct {
		Items []item `json:"items"`
	}
	rr := postJSON(t, h.Preview, ctx, map[string]any{"branch_id": branchID.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body)
	}
	decodeJSON(t, rr, &prev)
	if len(prev.Items) != 5 {
		t.Fatalf("preview items = %d, want 5: %+v", len(prev.Items), prev.Items)
	}
	expect("preview", statuses(prev.Items), map[uuid.UUID]string{l1: "ok", l2: "ok", l3: "failed", l4: "skipped", o1: "ok"})
	for _, it := range prev.Items {
		if it.ID == l3.String() && !strings.Contains(it.Error, "jatah libur") {
			t.Errorf("l3 error = %q, want the allowance message", it.Error)
		}
	}

	// The preview changed nothing.
	if n := countWhere(`SELECT COUNT(*) FROM leave_requests WHERE employee_id = $1 AND status <> 'pending'`, empID); n != 0 {
		t.Fatalf("preview decided %d leave requests", n)
	}
	if n := countWhere(`SELECT COUNT(*) FROM overtime_requests WHERE employee_id = $1 AND status <> 'pending'`, empID); n != 0 {
		t.Fatalf("preview decided the overtime request")
	}
	if n := countWhere(`SELECT COUNT(*) FROM leave_quota_usage WHERE employee_id = $1`, empID); n != 0 {
		t.Fatalf("preview left %d quota usage rows", n)
	}
	if n := countWhere(`SELECT COUNT(*) FROM activity_log WHERE entity_id IN ($1, $2, $3, $4, $5)`, l1, l2, l3, l4, o1); n != 0 {
		t.Fatalf("preview left %d activity log rows", n)
	}

	// ── Preview including backdated ──────────────────────────────────────────
	rr = postJSON(t, h.Preview, ctx, map[string]any{"branch_id": branchID.String(), "include_backdated": true})
	decodeJSON(t, rr, &prev)
	expect("preview+backdated", statuses(prev.Items), map[uuid.UUID]string{l1: "ok", l2: "ok", l3: "failed", l4: "ok", o1: "ok"})

	// ── Real run, in preview order ───────────────────────────────────────────
	send := make([]map[string]string, 0, len(prev.Items))
	for _, it := range prev.Items {
		send = append(send, map[string]string{"type": it.Type, "id": it.ID})
	}
	var res struct {
		Results []item `json:"results"`
	}
	rr = postJSON(t, h.Approve, ctx, map[string]any{"items": send, "include_backdated": true, "note": "backlog"})
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rr.Code, rr.Body)
	}
	decodeJSON(t, rr, &res)
	expect("approve", statuses(res.Results), map[uuid.UUID]string{l1: "ok", l2: "ok", l3: "failed", l4: "ok", o1: "ok"})

	if n := countWhere(`SELECT COUNT(*) FROM leave_requests WHERE id IN ($1, $2, $3) AND status = 'approved'`, l1, l2, l4); n != 3 {
		t.Errorf("approved leave = %d, want 3", n)
	}
	if n := countWhere(`SELECT COUNT(*) FROM leave_requests WHERE id = $1 AND status = 'pending'`, l3); n != 1 {
		t.Errorf("l3 should still be pending")
	}
	if n := countWhere(`SELECT COUNT(*) FROM overtime_requests WHERE id = $1 AND status = 'approved'`, o1); n != 1 {
		t.Errorf("overtime should be approved")
	}
	if n := countWhere(`SELECT COALESCE(SUM(cost), 0) FROM leave_quota_usage WHERE employee_id = $1`, empID); n != 4 {
		t.Errorf("allowance used = %d, want 4", n)
	}
	var desc string
	if err := pool.QueryRow(bg, `SELECT description FROM activity_log WHERE entity_id = $1`, l4).Scan(&desc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "MUNDUR TANGGAL") || !strings.Contains(desc, "persetujuan massal") {
		t.Errorf("backdated log = %q, want MUNDUR TANGGAL + persetujuan massal", desc)
	}

	// Running it again finds nothing left to approve except l3.
	rr = postJSON(t, h.Approve, ctx, map[string]any{"items": send, "include_backdated": true})
	decodeJSON(t, rr, &res)
	expect("re-run", statuses(res.Results), map[uuid.UUID]string{l1: "failed", l2: "failed", l3: "failed", l4: "failed", o1: "failed"})
}
