package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

// Evaluation adjustment ("Penyesuaian Evaluasi").
//
// Automatic violations are written by the nightly job, which evaluates a day as
// it stands at that moment. When attendance for that day arrives later — a
// fingerprint file imported days afterwards, a manager correction — the stored
// violations go stale:
//
//   - an absence penalty stays on a day that is now present (the import flipped
//     the record but nobody removed the violation);
//   - a late / missing-check-out flag written by the import never becomes a
//     violation, because the day was already evaluated;
//   - the monthly absence grace and the consecutive-absence rule were counted
//     against "absences" that were only days not yet imported.
//
// ReevaluateRange rebuilds the automatic violations of a range from the current
// records. PreviewReevaluation runs it inside a transaction that is rolled back,
// and reports what would change, so HR can review before applying.

// EvaluationRange is the range the adjustment screen works on for a month: the
// whole month, but never past yesterday (WIB) — a day still in progress is not
// judged.
func EvaluationRange(month time.Time) (from, to time.Time, ok bool) {
	from = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	to = from.AddDate(0, 1, -1)
	if y := TodayWIB().AddDate(0, 0, -1); to.After(y) {
		to = y
	}
	return from, to, !to.Before(from)
}

// ReevaluateRange deletes the automatic violations in [from, to], marks absent
// every scheduled day still without a record (up to yesterday), and evaluates the
// range again from the current attendance records. Manual violations are kept.
// Backs both the "Evaluasi" backfill endpoint and the adjustment screen.
func ReevaluateRange(ctx context.Context, qtx *db.Queries, from, to time.Time) error {
	if err := qtx.DeleteAutoViolationsForRange(ctx, &db.DeleteAutoViolationsForRangeParams{
		From: pgtype.Date{Time: from, Valid: true},
		To:   pgtype.Date{Time: to, Valid: true},
	}); err != nil {
		return err
	}
	reconcileTo := to
	if y := TodayWIB().AddDate(0, 0, -1); reconcileTo.After(y) {
		reconcileTo = y
	}
	for d := from; !d.After(reconcileTo); d = d.AddDate(0, 0, 1) {
		if _, err := ReconcileAbsent(ctx, qtx, d); err != nil {
			return err
		}
	}
	return EvaluateRange(ctx, qtx, from, to)
}

// ── Preview ──────────────────────────────────────────────────────────────────

// AdjustmentChange is one automatic violation that re-evaluation would remove or add.
type AdjustmentChange struct {
	Date       string `json:"date"`
	Change     string `json:"change"` // removed | added
	RuleType   string `json:"rule_type"`
	PolicyName string `json:"policy_name"`
	Points     int    `json:"points"`
	Status     string `json:"status"` // attendance status of the day now
	Reason     string `json:"reason"` // stale_absence | not_scored | absence_recount | recalculated
}

type AdjustmentEmployee struct {
	EmployeeID   pgtype.UUID        `json:"employee_id"`
	EmployeeCode string             `json:"employee_code"`
	FullName     string             `json:"full_name"`
	BranchName   string             `json:"branch_name"`
	ScoreBefore  int                `json:"score_before"`
	ScoreAfter   int                `json:"score_after"`
	Changes      []AdjustmentChange `json:"changes"`
}

// LeavePunch is a punch recorded on an approved leave day (is_leave_punch).
type LeavePunch struct {
	RecordID     pgtype.UUID        `json:"record_id"`
	EmployeeID   pgtype.UUID        `json:"employee_id"`
	EmployeeCode string             `json:"employee_code"`
	FullName     string             `json:"full_name"`
	BranchName   string             `json:"branch_name"`
	Date         string             `json:"date"`
	CheckIn      pgtype.Timestamptz `json:"check_in"`
	CheckOut     pgtype.Timestamptz `json:"check_out"`
}

// ReviewDay is a present day far outside the shift — very late or very early —
// with no permit covering it and not yet reviewed. A manager either grants a
// permit after the fact (the day becomes a half day) or keeps it as it is.
type ReviewDay struct {
	RecordID     pgtype.UUID        `json:"record_id"`
	EmployeeID   pgtype.UUID        `json:"employee_id"`
	EmployeeCode string             `json:"employee_code"`
	FullName     string             `json:"full_name"`
	BranchName   string             `json:"branch_name"`
	Date         string             `json:"date"`
	CheckIn      pgtype.Timestamptz `json:"check_in"`
	CheckOut     pgtype.Timestamptz `json:"check_out"`
	LateMinutes  int                `json:"late_minutes"`
	EarlyMinutes int                `json:"early_minutes"`
	Kind         string             `json:"kind"` // late | early | both
}

type AdjustmentPreview struct {
	From         string               `json:"from"`
	To           string               `json:"to"`
	Employees    []AdjustmentEmployee `json:"employees"` // only employees whose violations or score change
	LeavePunches []LeavePunch         `json:"leave_punches"`
	ReviewDays   []ReviewDay          `json:"review_days"`
	ReviewLate   int                  `json:"review_late_minutes"`
	ReviewEarly  int                  `json:"review_early_minutes"`
	Removed      int                  `json:"removed"`
	Added        int                  `json:"added"`
	NewAbsent    int                  `json:"new_absent"` // days without any record that would be marked absent
}

type autoViolation struct {
	employeeID [16]byte
	date       string
	policyID   [16]byte
	ruleType   string
	policyName string
	points     int
}

func (v autoViolation) key() string {
	return fmt.Sprintf("%x|%s|%x", v.employeeID, v.date, v.policyID)
}

func listAutoViolations(ctx context.Context, tx pgx.Tx, from, to time.Time) (map[string]autoViolation, error) {
	rows, err := tx.Query(ctx, `
		SELECT v.employee_id, v.date, v.policy_id, p.rule_type, p.name, v.points
		FROM performance_violations v
		JOIN performance_policies p ON p.id = v.policy_id
		WHERE v.source = 'auto' AND v.date >= $1 AND v.date <= $2`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]autoViolation{}
	for rows.Next() {
		var emp, pol pgtype.UUID
		var d pgtype.Date
		var v autoViolation
		if err := rows.Scan(&emp, &d, &pol, &v.ruleType, &v.policyName, &v.points); err != nil {
			return nil, err
		}
		v.employeeID, v.policyID, v.date = emp.Bytes, pol.Bytes, d.Time.Format("2006-01-02")
		out[v.key()] = v
	}
	return out, rows.Err()
}

func monthScores(ctx context.Context, tx pgx.Tx, month time.Time) (map[[16]byte]int, error) {
	rows, err := tx.Query(ctx, `SELECT employee_id, score FROM performance_scores WHERE period_month = $1`,
		pgtype.Date{Time: month, Valid: true})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[16]byte]int{}
	for rows.Next() {
		var id pgtype.UUID
		var s int32
		if err := rows.Scan(&id, &s); err != nil {
			return nil, err
		}
		out[id.Bytes] = int(s)
	}
	return out, rows.Err()
}

func dayStatuses(ctx context.Context, tx pgx.Tx, from, to time.Time) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT employee_id, date, status FROM attendance_records WHERE date >= $1 AND date <= $2`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id pgtype.UUID
		var d pgtype.Date
		var st string
		if err := rows.Scan(&id, &d, &st); err != nil {
			return nil, err
		}
		out[fmt.Sprintf("%x|%s", id.Bytes, d.Time.Format("2006-01-02"))] = st
	}
	return out, rows.Err()
}

// classify names why a violation appears or disappears on re-evaluation.
func classify(change string, v autoViolation, status string) string {
	absence := v.ruleType == "absent_no_leave" || v.ruleType == "consecutive_absent"
	switch {
	case change == "removed" && absence && status != "absent":
		return "stale_absence" // the day is present/leave now; the absence penalty is left over
	case change == "added" && !absence:
		return "not_scored" // the anomaly is on the record but was never turned into a violation
	case absence:
		return "absence_recount" // grace / streak counted again on the real absences
	default:
		return "recalculated"
	}
}

// PreviewReevaluation reports what ReevaluateRange would change, without
// changing anything.
func PreviewReevaluation(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (*AdjustmentPreview, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // always: this is a dry run
	qtx := db.New(pool).WithTx(tx)
	monthStart := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)

	before, err := listAutoViolations(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}
	scoresBefore, err := monthScores(ctx, tx, monthStart)
	if err != nil {
		return nil, err
	}
	var recordsBefore int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM attendance_records WHERE date >= $1 AND date <= $2`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true}).Scan(&recordsBefore); err != nil {
		return nil, err
	}

	if err := ReevaluateRange(ctx, qtx, from, to); err != nil {
		return nil, err
	}

	after, err := listAutoViolations(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}
	scoresAfter, err := monthScores(ctx, tx, monthStart)
	if err != nil {
		return nil, err
	}
	statuses, err := dayStatuses(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}
	var recordsAfter int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM attendance_records WHERE date >= $1 AND date <= $2`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true}).Scan(&recordsAfter); err != nil {
		return nil, err
	}

	p := &AdjustmentPreview{
		From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
		Employees: []AdjustmentEmployee{}, LeavePunches: []LeavePunch{}, ReviewDays: []ReviewDay{},
		NewAbsent: recordsAfter - recordsBefore,
	}
	changes := map[[16]byte][]AdjustmentChange{}
	add := func(change string, v autoViolation) {
		st := statuses[fmt.Sprintf("%x|%s", v.employeeID, v.date)]
		changes[v.employeeID] = append(changes[v.employeeID], AdjustmentChange{
			Date: v.date, Change: change, RuleType: v.ruleType, PolicyName: v.policyName,
			Points: v.points, Status: st, Reason: classify(change, v, st),
		})
	}
	for k, v := range before {
		if _, ok := after[k]; !ok {
			add("removed", v)
			p.Removed++
		}
	}
	for k, v := range after {
		if _, ok := before[k]; !ok {
			add("added", v)
			p.Added++
		}
	}
	// Employees whose stored score differs even without violation changes (a
	// score row that was never recomputed) are included too.
	affected := map[[16]byte]bool{}
	for id := range changes {
		affected[id] = true
	}
	for id, s := range scoresAfter {
		b, ok := scoresBefore[id]
		if !ok {
			b = 100
		}
		if b != s {
			affected[id] = true
		}
	}

	for id := range affected {
		var e AdjustmentEmployee
		e.EmployeeID = pgtype.UUID{Bytes: id, Valid: true}
		if err := tx.QueryRow(ctx, `SELECT e.employee_code, e.full_name, COALESCE(b.name, '')
			FROM employees e LEFT JOIN branches b ON b.id = e.branch_id WHERE e.id = $1`, e.EmployeeID).
			Scan(&e.EmployeeCode, &e.FullName, &e.BranchName); err != nil {
			return nil, err
		}
		e.ScoreBefore, e.ScoreAfter = 100, 100
		if s, ok := scoresBefore[id]; ok {
			e.ScoreBefore = s
		}
		if s, ok := scoresAfter[id]; ok {
			e.ScoreAfter = s
		}
		e.Changes = changes[id]
		if e.Changes == nil {
			e.Changes = []AdjustmentChange{}
		}
		sort.Slice(e.Changes, func(i, j int) bool {
			if e.Changes[i].Date != e.Changes[j].Date {
				return e.Changes[i].Date < e.Changes[j].Date
			}
			return e.Changes[i].Change > e.Changes[j].Change
		})
		p.Employees = append(p.Employees, e)
	}
	sort.Slice(p.Employees, func(i, j int) bool {
		di := p.Employees[i].ScoreAfter - p.Employees[i].ScoreBefore
		dj := p.Employees[j].ScoreAfter - p.Employees[j].ScoreBefore
		if di != dj {
			return abs(di) > abs(dj)
		}
		return p.Employees[i].FullName < p.Employees[j].FullName
	})

	rows, err := tx.Query(ctx, `
		SELECT ar.id, ar.employee_id, e.employee_code, e.full_name, COALESCE(b.name, ''), ar.date, ar.check_in, ar.check_out
		FROM attendance_records ar
		JOIN employees e ON e.id = ar.employee_id
		LEFT JOIN branches b ON b.id = e.branch_id
		WHERE ar.is_leave_punch AND ar.date >= $1 AND ar.date <= $2
		ORDER BY ar.date, e.full_name`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var lp LeavePunch
		var d pgtype.Date
		if err := rows.Scan(&lp.RecordID, &lp.EmployeeID, &lp.EmployeeCode, &lp.FullName, &lp.BranchName, &d, &lp.CheckIn, &lp.CheckOut); err != nil {
			return nil, err
		}
		lp.Date = d.Time.Format("2006-01-02")
		p.LeavePunches = append(p.LeavePunches, lp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	p.ReviewLate, p.ReviewEarly = 120, 120
	if rs, err := qtx.GetAttendanceReviewSettings(ctx); err == nil {
		p.ReviewLate, p.ReviewEarly = int(rs.ReviewLateMinutes), int(rs.ReviewEarlyMinutes)
	}
	rrows, err := tx.Query(ctx, `
		SELECT ar.id, ar.employee_id, e.employee_code, e.full_name, COALESCE(b.name, ''), ar.date,
		       ar.check_in, ar.check_out,
		       CASE WHEN ar.is_late THEN ar.late_minutes ELSE 0 END,
		       CASE WHEN ar.is_early_leave THEN ar.early_leave_minutes ELSE 0 END
		FROM attendance_records ar
		JOIN employees e ON e.id = ar.employee_id
		LEFT JOIN branches b ON b.id = e.branch_id
		WHERE ar.date >= $1 AND ar.date <= $2
		  AND ar.status = 'present' AND NOT ar.is_half_day AND ar.excused_by IS NULL
		  AND NOT ar.review_resolved
		  AND ((ar.is_late AND ar.late_minutes >= $3) OR (ar.is_early_leave AND ar.early_leave_minutes >= $4))
		ORDER BY ar.date, e.full_name`,
		pgtype.Date{Time: from, Valid: true}, pgtype.Date{Time: to, Valid: true}, p.ReviewLate, p.ReviewEarly)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var rd ReviewDay
		var d pgtype.Date
		var late, early int32
		if err := rrows.Scan(&rd.RecordID, &rd.EmployeeID, &rd.EmployeeCode, &rd.FullName, &rd.BranchName, &d,
			&rd.CheckIn, &rd.CheckOut, &late, &early); err != nil {
			return nil, err
		}
		rd.Date, rd.LateMinutes, rd.EarlyMinutes = d.Time.Format("2006-01-02"), int(late), int(early)
		switch {
		case rd.LateMinutes >= p.ReviewLate && rd.EarlyMinutes >= p.ReviewEarly && rd.LateMinutes > 0 && rd.EarlyMinutes > 0:
			rd.Kind = "both"
		case rd.LateMinutes >= p.ReviewLate && rd.LateMinutes > 0:
			rd.Kind = "late"
		default:
			rd.Kind = "early"
		}
		p.ReviewDays = append(p.ReviewDays, rd)
	}
	return p, rrows.Err()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
