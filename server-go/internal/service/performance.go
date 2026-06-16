package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Performance scoring engine. The pure selection/score helpers are DB-free and
// unit-tested; the Evaluate* / Recompute* functions walk the DB and are shared by
// the nightly reconciliation tick, the manual backfill endpoint, and the
// attendance manual-correction re-evaluation.

// ── Pure helpers (DB-free, testable) ─────────────────────────────────────────

// ThresholdPolicy is the minimal view of a threshold-based policy (late /
// early_leave) used by the pure selection logic.
type ThresholdPolicy struct {
	ID        pgtype.UUID
	Threshold int // threshold_minutes
	Points    int
}

// selectHighestThreshold returns the policy with the highest threshold that is
// still <= minutes, or (nil, false) when none match. Only ONE policy is ever
// returned — e.g. with a >=15 (2pt) and a >=60 (5pt) policy, a 70-minute late
// selects only the 5pt policy, not both.
func selectHighestThreshold(minutes int, policies []ThresholdPolicy) (ThresholdPolicy, bool) {
	var best ThresholdPolicy
	found := false
	for _, p := range policies {
		if p.Threshold <= minutes {
			if !found || p.Threshold > best.Threshold {
				best = p
				found = true
			}
		}
	}
	return best, found
}

// SelectLatePolicy picks the single applicable late policy for a record that is
// `lateMinutes` minutes late, using the highest-matching-threshold rule.
func SelectLatePolicy(lateMinutes int, policies []ThresholdPolicy) (ThresholdPolicy, bool) {
	return selectHighestThreshold(lateMinutes, policies)
}

// SelectEarlyLeavePolicy picks the single applicable early-leave policy for a
// record that left `earlyLeaveMinutes` early, using the highest-matching-threshold
// rule.
func SelectEarlyLeavePolicy(earlyLeaveMinutes int, policies []ThresholdPolicy) (ThresholdPolicy, bool) {
	return selectHighestThreshold(earlyLeaveMinutes, policies)
}

// ComputeScore returns the materialized monthly score: max(0, 100 - totalPoints).
func ComputeScore(totalPoints int) int {
	score := 100 - totalPoints
	if score < 0 {
		return 0
	}
	return score
}

// MonthlyCapReached reports whether a policy capped at maxOccurrences (NULL =
// unlimited, modelled as a non-valid pgtype.Int4 by the caller) has already been
// applied `existing` times this month and may not be applied again.
func MonthlyCapReached(maxOccurrences *int, existing int) bool {
	if maxOccurrences == nil {
		return false // unlimited
	}
	return existing >= *maxOccurrences
}

// ── DB-touching engine ───────────────────────────────────────────────────────

// firstOfMonth returns the first day (UTC, time-zeroed) of the month containing d.
func firstOfMonth(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// thresholdPoliciesFromRows converts active threshold policy rows into the pure
// ThresholdPolicy slice used by the selection helpers.
func thresholdPoliciesFromRows(rows []*db.PerformancePolicy) []ThresholdPolicy {
	out := make([]ThresholdPolicy, 0, len(rows))
	for _, p := range rows {
		thr := 0
		if p.ThresholdMinutes.Valid {
			thr = int(p.ThresholdMinutes.Int32)
		}
		out = append(out, ThresholdPolicy{ID: p.ID, Threshold: thr, Points: int(p.Points)})
	}
	return out
}

// maxOccPtr converts a nullable max_occurrences_per_month column into the *int the
// pure cap predicate expects (nil = unlimited).
func maxOccPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

// applyAutoViolation inserts one auto violation for a record/policy if the
// monthly cap allows it. Insert is idempotent via the UNIQUE(policy_id,
// attendance_record_id) constraint (ON CONFLICT DO NOTHING).
func applyAutoViolation(ctx context.Context, qtx *db.Queries, rec *db.AttendanceRecord, policyID pgtype.UUID, points int, maxOcc pgtype.Int4) error {
	monthStart := pgtype.Date{Time: firstOfMonth(rec.Date.Time), Valid: true}

	if cap := maxOccPtr(maxOcc); cap != nil {
		count, err := qtx.CountPolicyOccurrencesInMonth(ctx, &db.CountPolicyOccurrencesInMonthParams{
			PolicyID:   policyID,
			EmployeeID: rec.EmployeeID,
			Date:       monthStart,
		})
		if err != nil {
			return err
		}
		// Subtract any existing violation for THIS record/policy so an idempotent
		// re-run does not falsely count itself toward the cap.
		// (ON CONFLICT DO NOTHING already makes the insert a no-op if it exists.)
		if MonthlyCapReached(cap, int(count)) {
			return nil
		}
	}

	return qtx.InsertAutoViolation(ctx, &db.InsertAutoViolationParams{
		EmployeeID:         rec.EmployeeID,
		PolicyID:           policyID,
		AttendanceRecordID: rec.ID,
		Date:               rec.Date,
		Points:             int32(points),
		Note:               pgtype.Text{},
	})
}

// evaluateRecord matches one finalized attendance record against all active
// policies and inserts the resulting auto violations. It does NOT recompute the
// score; callers batch that per employee+month.
func evaluateRecord(ctx context.Context, qtx *db.Queries, rec *db.AttendanceRecord) error {
	// late
	if rec.IsLate && rec.LateMinutes > 0 {
		rows, err := qtx.ListActivePerformancePoliciesByRule(ctx, "late")
		if err != nil {
			return err
		}
		if pol, ok := SelectLatePolicy(int(rec.LateMinutes), thresholdPoliciesFromRows(rows)); ok {
			max := maxOccForPolicy(rows, pol.ID)
			if err := applyAutoViolation(ctx, qtx, rec, pol.ID, pol.Points, max); err != nil {
				return err
			}
		}
	}

	// early_leave
	if rec.IsEarlyLeave && rec.EarlyLeaveMinutes > 0 {
		rows, err := qtx.ListActivePerformancePoliciesByRule(ctx, "early_leave")
		if err != nil {
			return err
		}
		if pol, ok := SelectEarlyLeavePolicy(int(rec.EarlyLeaveMinutes), thresholdPoliciesFromRows(rows)); ok {
			max := maxOccForPolicy(rows, pol.ID)
			if err := applyAutoViolation(ctx, qtx, rec, pol.ID, pol.Points, max); err != nil {
				return err
			}
		}
	}

	// missing_checkout — first active policy applies (no threshold).
	if rec.IsMissingCheckout {
		rows, err := qtx.ListActivePerformancePoliciesByRule(ctx, "missing_checkout")
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			p := rows[0]
			if err := applyAutoViolation(ctx, qtx, rec, p.ID, int(p.Points), p.MaxOccurrencesPerMonth); err != nil {
				return err
			}
		}
	}

	// absent_no_leave — status 'absent' (a 'leave' status carries no violation).
	if rec.Status == "absent" {
		rows, err := qtx.ListActivePerformancePoliciesByRule(ctx, "absent_no_leave")
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			p := rows[0]
			if err := applyAutoViolation(ctx, qtx, rec, p.ID, int(p.Points), p.MaxOccurrencesPerMonth); err != nil {
				return err
			}
		}
	}

	return nil
}

// maxOccForPolicy finds the max_occurrences column for a policy id within a row
// set (used after the pure selection picked one of them).
func maxOccForPolicy(rows []*db.PerformancePolicy, id pgtype.UUID) pgtype.Int4 {
	for _, p := range rows {
		if p.ID == id {
			return p.MaxOccurrencesPerMonth
		}
	}
	return pgtype.Int4{}
}

// EvaluateDay evaluates every finalized attendance record on `date`, inserting
// auto violations and recomputing each affected employee's monthly score.
func EvaluateDay(ctx context.Context, qtx *db.Queries, date time.Time) error {
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	pgDate := pgtype.Date{Time: dateOnly, Valid: true}

	recs, err := qtx.ListAttendanceRecordsForDate(ctx, pgDate)
	if err != nil {
		return err
	}

	affected := map[[16]byte]pgtype.UUID{}
	for _, rec := range recs {
		if err := evaluateRecord(ctx, qtx, rec); err != nil {
			return err
		}
		affected[rec.EmployeeID.Bytes] = rec.EmployeeID
	}

	monthStart := firstOfMonth(dateOnly)
	for _, empID := range affected {
		if err := RecomputeScore(ctx, qtx, empID, monthStart); err != nil {
			return err
		}
	}
	return nil
}

// EvaluateRange runs EvaluateDay for each day in [from, to] inclusive. Used by the
// manual backfill endpoint.
func EvaluateRange(ctx context.Context, qtx *db.Queries, from, to time.Time) error {
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	for !d.After(end) {
		if err := EvaluateDay(ctx, qtx, d); err != nil {
			return err
		}
		d = d.AddDate(0, 0, 1)
	}
	return nil
}

// RecomputeScore sums the violation points for the employee in the month
// containing periodMonth and upserts performance_scores = max(0, 100 - sum).
func RecomputeScore(ctx context.Context, qtx *db.Queries, employeeID pgtype.UUID, periodMonth time.Time) error {
	monthStart := pgtype.Date{Time: firstOfMonth(periodMonth), Valid: true}

	total, err := qtx.SumViolationPointsInMonth(ctx, &db.SumViolationPointsInMonthParams{
		EmployeeID: employeeID,
		Date:       monthStart,
	})
	if err != nil {
		return err
	}

	_, err = qtx.UpsertPerformanceScore(ctx, &db.UpsertPerformanceScoreParams{
		EmployeeID:  employeeID,
		PeriodMonth: monthStart,
		Score:       int32(ComputeScore(int(total))),
	})
	return err
}

// DeleteAutoViolationsForRecord deletes the auto violations of a single
// attendance record and re-evaluates that record, then recomputes the employee's
// monthly score. Called after a manual attendance correction (prompt 04 PUT).
func DeleteAutoViolationsForRecord(ctx context.Context, qtx *db.Queries, rec *db.AttendanceRecord) error {
	if err := qtx.DeleteAutoViolationsByRecord(ctx, rec.ID); err != nil {
		return err
	}
	if err := evaluateRecord(ctx, qtx, rec); err != nil {
		return err
	}
	return RecomputeScore(ctx, qtx, rec.EmployeeID, firstOfMonth(rec.Date.Time))
}
