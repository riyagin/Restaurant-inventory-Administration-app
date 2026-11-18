package service

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Punches for one employee can arrive from several kiosks at once (a queued
// offline event flushing while the employee punches live elsewhere), and from
// a fingerprint import. Each path inserts its punch and then rebuilds the day's
// record from the punch log. Under READ COMMITTED, two such transactions each
// rebuild from a log that lacks the other's uncommitted punch, and the second
// UPDATE overwrites the first: the record silently drops a punch until
// something rebuilds that day again.
//
// LockEmployeeAttendance serialises those transactions per employee. It is a
// transaction-scoped advisory lock: taken before the rebuild reads the log,
// released at commit, so the waiting transaction reads the log only after the
// other's punch is committed. Callers locking several employees must use
// LockEmployeesAttendance, which takes them in a fixed order (no deadlocks).

// attendanceLockSeed separates this lock from the face-photo lock (seed 81),
// which hashes the same employee id.
const attendanceLockSeed = 86

// LockEmployeeAttendance takes the per-employee attendance lock for the rest of
// the transaction tx.
func LockEmployeeAttendance(ctx context.Context, tx db.DBTX, employeeID pgtype.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, $2))`,
		employeeID, attendanceLockSeed); err != nil {
		return fmt.Errorf("attendance lock: %w", err)
	}
	return nil
}

// LockEmployeesAttendance locks several employees in ascending id order.
func LockEmployeesAttendance(ctx context.Context, tx db.DBTX, employeeIDs []pgtype.UUID) error {
	ids := make([]pgtype.UUID, 0, len(employeeIDs))
	seen := map[[16]byte]bool{}
	for _, id := range employeeIDs {
		if !seen[id.Bytes] {
			seen[id.Bytes] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i].Bytes[:], ids[j].Bytes[:]) < 0 })
	for _, id := range ids {
		if err := LockEmployeeAttendance(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// ReevaluateFinishedDay re-judges a rebuilt record's automatic violations when
// its day is already over. The nightly job judges each day once; a punch that
// arrives afterwards (an offline kiosk event, a fingerprint import) changes the
// record — typically absent → present — and without this the Alpha penalty it
// was given stays on the employee's score. Violations are inserted idempotently,
// so re-judging a day the nightly job has not reached yet is harmless.
func ReevaluateFinishedDay(ctx context.Context, q *db.Queries, rec *db.AttendanceRecord) error {
	if rec == nil || !dateOnly(rec.Date.Time).Before(TodayWIB()) {
		return nil
	}
	return DeleteAutoViolationsForRecord(ctx, q, rec)
}
