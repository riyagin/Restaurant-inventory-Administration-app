package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
	"inventory-app/server-go/internal/testutil"
)

// Alpha against real attendance rows, inside a rolled-back transaction.
// November 2026 allowance overridden to 2:
//
//	Mon 2nd  absent → cost 1, covered        (1 left)
//	Sat 7th  absent → cost 2, can't fit      → unpaid at 2× daily rate, 1 kept
//	Mon 9th  absent → cost 1, covered        (0 left)
//	Tue 10th present, Wed 11th leave         → not Alpha
func TestAlphaAgainstAttendanceRecords(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) // nothing here is ever committed
	q := db.New(tx)

	var branchID, positionID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM branches LIMIT 1`).Scan(&branchID); err != nil {
		t.Skipf("no branch: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM positions LIMIT 1`).Scan(&positionID); err != nil {
		t.Skipf("no position: %v", err)
	}
	var empID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO employees (employee_code, full_name, join_date, position_id, branch_id)
		VALUES ($1, 'Uji Alpha', '2026-01-01', $2, $3) RETURNING id`,
		"TST-ALPHA-"+uuid.NewString()[:8], positionID, branchID).Scan(&empID); err != nil {
		t.Fatalf("employee: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO leave_monthly_quotas (employee_id, month, quota_days) VALUES ($1, '2026-11-01', 2)`, empID); err != nil {
		t.Fatalf("quota: %v", err)
	}
	for date, status := range map[string]string{
		"2026-11-02": "absent", "2026-11-07": "absent", "2026-11-09": "absent",
		"2026-11-10": "present", "2026-11-11": "leave",
		"2026-10-25": "absent", // previous period (calendar or 26–25): outside the window below
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO attendance_records (employee_id, date, status) VALUES ($1, $2, $3)`,
			empID, date, status); err != nil {
			t.Fatalf("attendance %s: %v", date, err)
		}
	}
	emp := pgtype.UUID{Bytes: empID, Valid: true}
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	am, err := service.AlphaForMonth(ctx, q, emp, nov)
	if err != nil {
		t.Fatal(err)
	}
	if am.Quota != 2 || len(am.Days) != 3 || am.AlphaUsed() != 2 {
		t.Fatalf("month: quota=%d alpha days=%d used=%d, want 2/3/2", am.Quota, len(am.Days), am.AlphaUsed())
	}

	const rate = 100_000
	days, cut, err := service.AlphaPayCut(ctx, q, emp, nov, nov.AddDate(0, 1, -1), rate)
	if err != nil {
		t.Fatal(err)
	}
	if days != 1 || cut != 2*rate {
		t.Fatalf("pay cut: %d days / Rp %d, want 1 day / Rp %d (weekend, 2×)", days, cut, 2*rate)
	}

	// A leave request approved over the Saturday turns it into leave: excluded
	// from Alpha, the same check ApproveLeave runs.
	sat := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	am, err = service.AlphaForMonth(ctx, q, emp, nov, sat)
	if err != nil {
		t.Fatal(err)
	}
	if len(am.Days) != 2 || am.AlphaUsed() != 2 {
		t.Fatalf("excluding Sat: %d days, used %d; want 2 / 2", len(am.Days), am.AlphaUsed())
	}

	// The all-employee balance path sees the same three days.
	cutoff, err := service.LeaveCutoffDay(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	from, to := service.LeavePeriodRange(nov, cutoff)
	by, err := service.AbsentDatesByEmployeeMonth(ctx, q, from, to, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	b := service.NewMonthlyLeaveBalance(nov, 2, 0, true, nov.AddDate(0, 0, 16), cutoff).WithAlpha(by[empID][nov])
	if b.UsedDays != 2 || b.UnusedDays != 0 || b.AlphaUnpaidDays != 1 {
		t.Fatalf("balance %+v", b)
	}
}
