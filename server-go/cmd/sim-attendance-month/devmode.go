package main

// -dev mode: load the October simulation into the app's own database
// (DB_NAME from .env) so the employees, attendance, permits, violations and
// scores show up in the local app.
//
// Everything the simulator creates hangs off one branch, "Warung Jambu (SIM)",
// and one position, "Crew (SIM)". Each run first deletes everything attached
// to that branch, so it can be re-run safely. -dev-clean only deletes.
//
// The live performance policies and HR settings are used as they are; the
// simulator never writes to them. Absent days are reconciled for the SIM
// employees only, so real employees get no October records.

import (
	"context"
	"fmt"
	"time"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/service"
)

const (
	simBranchName   = "Warung Jambu (SIM)"
	simPositionName = "Crew (SIM)"
	// simMealComponent is the simulator's own daily_allowance wage component.
	simMealComponent = "Uang Makan (SIM)"
)

// cleanDev removes every row the simulator left in the app database.
func cleanDev(ctx context.Context, s *sim) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	emps := `(SELECT e.id FROM employees e JOIN branches b ON b.id = e.branch_id WHERE b.name = '` + simBranchName + `')`
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+emps+` x`).Scan(&n); err != nil {
		return 0, err
	}
	for _, t := range []string{
		"performance_violations", "performance_scores", "attendance_punches", "attendance_permissions",
		"attendance_records", "shift_assignments", "shift_change_requests", "leave_requests",
		"leave_balances", "leave_quota_usage", "leave_monthly_quotas", "overtime_requests",
		"payroll_lines", "wage_structures", "kasbons", "employee_face_photos", "employee_documents",
		"thr_lines", "benefit_claims", "benefit_fund_movements",
	} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE employee_id IN `+emps); err != nil {
			return 0, fmt.Errorf("clean %s: %w", t, err)
		}
	}
	for _, q := range []string{
		`DELETE FROM employees WHERE id IN ` + emps,
		`DELETE FROM shift_definitions WHERE branch_id IN (SELECT id FROM branches WHERE name = '` + simBranchName + `')`,
		`DELETE FROM work_schedules WHERE branch_id IN (SELECT id FROM branches WHERE name = '` + simBranchName + `')`,
		`DELETE FROM branches WHERE name = '` + simBranchName + `'`,
		`DELETE FROM positions WHERE name = '` + simPositionName + `'`,
		`DELETE FROM wage_components WHERE name = '` + simMealComponent + `'`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			return 0, fmt.Errorf("clean: %w\n%s", err, q)
		}
	}
	return n, tx.Commit(ctx)
}

// checkDevCodes refuses to run when a real employee already uses one of the
// simulator's employee codes.
func checkDevCodes(ctx context.Context, s *sim, emps []*simEmployee) error {
	for _, e := range emps {
		var name string
		err := s.pool.QueryRow(ctx, `SELECT full_name FROM employees WHERE employee_code = $1`, e.DBCode).Scan(&name)
		if err == nil {
			return fmt.Errorf("employee code %s is already used by %q in the app database; not loading the simulation", e.DBCode, name)
		}
	}
	return nil
}

// reconcileSimOnly is service.ReconcileAbsent limited to the SIM employees:
// a work day with no record becomes absent, unless an approved Dinas Luar
// permit makes it present.
func (s *sim) reconcileSimOnly(ctx context.Context, q *db.Queries, d time.Time) error {
	date := pgD(d)
	for _, id := range s.empIDs {
		if _, err := q.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{EmployeeID: id, Date: date}); err == nil {
			continue
		}
		perms, err := q.ListApprovedPermissionTypes(ctx, &db.ListApprovedPermissionTypesParams{EmployeeID: id, Date: date})
		if err != nil {
			return err
		}
		offSite := false
		for _, p := range perms {
			offSite = offSite || p == "off_site"
		}
		if offSite {
			sched := service.ScheduleWithShiftQ(ctx, q, s.branchSched, id, d)
			if _, _, err := service.RebuildDay(ctx, q, id, d, sched, service.RebuildOptions{}); err != nil {
				return err
			}
			continue
		}
		if err := q.InsertAbsentRecord(ctx, &db.InsertAbsentRecordParams{EmployeeID: id, Date: date}); err != nil {
			return err
		}
	}
	return nil
}
