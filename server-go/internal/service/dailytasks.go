package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Daily operational tasks.
//
// A handful of back-office duties must happen every day — record the day's
// purchasing, import each branch's POS sales. They are shared: whoever gets to
// one does it for everyone, so a task belongs to the organisation and to a date,
// not to a person.
//
// Completion is **derived from the data that already exists**, never ticked off.
// A purchase invoice dated D answers D's purchasing task; a POS import whose
// lines land on branch B answers B's task. That is what keeps the board honest —
// it cannot say "done" when the work wasn't, it needs no backfilling, and every
// day of existing history scores correctly the first time it is read. Only
// `manual` definitions, which by definition leave no data trail, consult
// daily_task_completions.

// Task types. A type is the rule for deciding whether an instance is satisfied.
const (
	TaskTypePurchasing = "purchasing"
	TaskTypePOSImport  = "pos_import"
	TaskTypeManual     = "manual"
	// TaskTypePettyCash is the twice-daily cash box count. Satisfied only when
	// *both* ends are recorded: an opening with no closing is a day nobody closed,
	// which is precisely the case the duty exists to catch.
	TaskTypePettyCash = "petty_cash"
)

// Task scopes. per_branch expands into one instance per branch per day, so a new
// branch carries its daily duties from the moment it exists.
const (
	TaskScopeGlobal    = "global"
	TaskScopePerBranch = "per_branch"
)

// DefaultTaskLookbackDays bounds how far back an unfinished task keeps being
// reported. Beyond this it is history, not a to-do.
const DefaultTaskLookbackDays = 14

const dateLayout = "2006-01-02"

// uuidString renders a pgtype.UUID, mapping NULL to "" so a global task's empty
// branch and a missing row read the same way.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// TaskDefinition is a recurring daily duty.
type TaskDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TaskType    string `json:"task_type"`
	Scope       string `json:"scope"`
	TargetRole  string `json:"target_role"`
	LinkPath    string `json:"link_path"`
	// StartsOn is the first date the duty applies, "" for no bound.
	StartsOn string `json:"starts_on"`
	// DueOffsetDays is how many days after the task's own date the work becomes
	// actionable — 1 for POS import, whose data only arrives the next morning.
	// An instance before that point is not shown at all: nagging about work that
	// cannot yet be done is noise, and counting it as missed is wrong.
	DueOffsetDays int    `json:"due_offset_days"`
	GraceDays     int    `json:"grace_days"`
	IsActive      bool   `json:"is_active"`
	SortOrder     int    `json:"sort_order"`
	CreatedAt     string `json:"created_at"`
}

// TaskInstance is one definition on one date (and, for per-branch tasks, one
// branch) together with whether it has been satisfied.
type TaskInstance struct {
	DefinitionID string `json:"definition_id"`
	Name         string `json:"name"`
	TaskType     string `json:"task_type"`
	BranchID     string `json:"branch_id,omitempty"`
	BranchName   string `json:"branch_name,omitempty"`
	Date         string `json:"date"`
	LinkPath     string `json:"link_path"`
	Done         bool   `json:"done"`
	DoneBy       string `json:"done_by,omitempty"`
	// DoneOnDate is true when the record that satisfies this task was entered on
	// the task's own date rather than caught up later. It is what separates "the
	// work happened" from "the work happened on time".
	DoneOnDate bool `json:"done_on_date"`
	Overdue    bool `json:"overdue"`
}

// satisfaction is how one task instance was answered.
type satisfaction struct {
	user   string
	onDate bool
}

// Label renders the instance the way it is shown in a notification.
func (t TaskInstance) Label() string {
	if t.BranchName != "" {
		return fmt.Sprintf("%s — %s", t.Name, t.BranchName)
	}
	return t.Name
}

type branchRef struct{ ID, Name string }

// instanceKey identifies one task instance. branch is "" for global tasks.
type instanceKey struct{ defID, branch, date string }

// ListTaskDefinitions returns the configured duties, newest sort order first.
// role filters to definitions aimed at that role; "" returns all.
func ListTaskDefinitions(ctx context.Context, pool *pgxpool.Pool, role string, activeOnly bool) ([]TaskDefinition, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, description, task_type, scope, target_role, link_path,
		       starts_on, due_offset_days, grace_days, is_active, sort_order, created_at
		FROM daily_task_definitions
		WHERE ($1::text = '' OR target_role = $1)
		  AND ($2::bool = false OR is_active)
		ORDER BY sort_order, name`, role, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TaskDefinition{}
	for rows.Next() {
		var (
			d         TaskDefinition
			id        pgtype.UUID
			startsOn  pgtype.Date
			createdAt time.Time
		)
		if err := rows.Scan(&id, &d.Name, &d.Description, &d.TaskType, &d.Scope, &d.TargetRole,
			&d.LinkPath, &startsOn, &d.DueOffsetDays, &d.GraceDays, &d.IsActive, &d.SortOrder, &createdAt); err != nil {
			return nil, err
		}
		d.ID = uuidString(id)
		if startsOn.Valid {
			d.StartsOn = startsOn.Time.Format(dateLayout)
		}
		d.CreatedAt = createdAt.Format(time.RFC3339)
		out = append(out, d)
	}
	return out, rows.Err()
}

// TaskBoard expands every active definition over [from, to] and reports which
// instances are satisfied. Instances are returned newest date first.
//
// `today` is passed in rather than read from the clock so callers (and tests)
// control the boundary: a task for today is not yet overdue, one for an earlier
// date beyond its grace period is.
func TaskBoard(ctx context.Context, pool *pgxpool.Pool, role string, from, to, today time.Time) ([]TaskInstance, error) {
	defs, err := ListTaskDefinitions(ctx, pool, role, true)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return []TaskInstance{}, nil
	}

	branches, err := listBranches(ctx, pool)
	if err != nil {
		return nil, err
	}

	purchasing, err := purchasingDoneDates(ctx, pool, from, to)
	if err != nil {
		return nil, err
	}
	posImports, err := posImportDoneByBranch(ctx, pool, from, to)
	if err != nil {
		return nil, err
	}
	pettyCash, err := pettyCashCountedByBranch(ctx, pool, from, to)
	if err != nil {
		return nil, err
	}
	manual, err := manualCompletions(ctx, pool, from, to)
	if err != nil {
		return nil, err
	}

	out := []TaskInstance{}
	for d := to; !d.Before(from); d = d.AddDate(0, 0, -1) {
		date := d.Format(dateLayout)
		for _, def := range defs {
			// A duty cannot be late for days before it applied.
			if def.StartsOn != "" && date < def.StartsOn {
				continue
			}
			// Nor for days whose work is not yet possible: POS sales for date D
			// only land the following morning, so D's import is not actionable
			// until D + due_offset_days.
			if today.Before(d.AddDate(0, 0, def.DueOffsetDays)) {
				continue
			}
			targets := []branchRef{{}}
			if def.Scope == TaskScopePerBranch {
				targets = branches
			}
			for _, b := range targets {
				inst := TaskInstance{
					DefinitionID: def.ID,
					Name:         def.Name,
					TaskType:     def.TaskType,
					BranchID:     b.ID,
					BranchName:   b.Name,
					Date:         date,
					LinkPath:     def.LinkPath,
				}
				var (
					sat satisfaction
					ok  bool
				)
				switch def.TaskType {
				case TaskTypePurchasing:
					sat, ok = purchasing[date]
				case TaskTypePOSImport:
					sat, ok = posImports[instanceKey{date: date, branch: b.ID}]
				case TaskTypePettyCash:
					sat, ok = pettyCash[instanceKey{date: date, branch: b.ID}]
				default:
					sat, ok = manual[instanceKey{defID: def.ID, branch: b.ID, date: date}]
				}
				inst.Done, inst.DoneBy, inst.DoneOnDate = ok, sat.user, sat.onDate
				if !inst.Done {
					// Overdue once the actionable window and its grace have passed.
					inst.Overdue = d.Before(today.AddDate(0, 0, -(def.DueOffsetDays + def.GraceDays)))
				}
				out = append(out, inst)
			}
		}
	}
	return out, nil
}

// PendingTasks is TaskBoard reduced to what still needs doing.
func PendingTasks(ctx context.Context, pool *pgxpool.Pool, role string, lookbackDays int, today time.Time) ([]TaskInstance, error) {
	if lookbackDays <= 0 {
		lookbackDays = DefaultTaskLookbackDays
	}
	all, err := TaskBoard(ctx, pool, role, today.AddDate(0, 0, -lookbackDays), today, today)
	if err != nil {
		return nil, err
	}
	pending := []TaskInstance{}
	for _, t := range all {
		if !t.Done {
			pending = append(pending, t)
		}
	}
	return pending, nil
}

// CompleteManualTask records a manual definition as done. Derived types are
// rejected: their answer lives in the data, and a tick that contradicts it would
// be a lie the board has no way to detect.
func CompleteManualTask(ctx context.Context, pool *pgxpool.Pool, defID, branchID string, date time.Time, userID pgtype.UUID, note string) error {
	var taskType string
	if err := pool.QueryRow(ctx, `SELECT task_type FROM daily_task_definitions WHERE id = $1`, defID).Scan(&taskType); err != nil {
		return err
	}
	if taskType != TaskTypeManual {
		return fmt.Errorf("tugas %q diselesaikan otomatis dari data, tidak dapat ditandai manual", taskType)
	}
	var branch any
	if branchID != "" {
		branch = branchID
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_task_completions (definition_id, branch_id, task_date, completed_by, note)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (definition_id, COALESCE(branch_id, '00000000-0000-0000-0000-000000000000'::uuid), task_date)
		DO UPDATE SET completed_by = $4, completed_at = now(), note = $5`,
		defID, branch, date, userID, note)
	return err
}

// ── satisfaction sources ────────────────────────────────────────────────────

// purchasingDoneDates returns the dates that carry at least one real purchase
// invoice, mapped to the username who recorded the first one. Cancelled invoices
// don't count, and dispatch mirror invoices are expense-typed so they never
// appear here in the first place.
func purchasingDoneDates(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (map[string]satisfaction, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (i.date) i.date, COALESCE(u.username, ''), (i.created_at::date = i.date)
		FROM invoices i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.invoice_type = 'purchase'
		  AND i.payment_status <> 'cancelled'
		  AND i.date BETWEEN $1 AND $2
		ORDER BY i.date, i.created_at`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]satisfaction{}
	for rows.Next() {
		var (
			d   time.Time
			sat satisfaction
		)
		if err := rows.Scan(&d, &sat.user, &sat.onDate); err != nil {
			return nil, err
		}
		out[d.Format(dateLayout)] = sat
	}
	return out, rows.Err()
}

// pettyCashCountedByBranch reports which (date, branch) pairs have a *complete*
// cash box count. Both ends are required: an opening with no closing is a day
// nobody closed, which is exactly the omission this duty exists to catch, so
// counting it as done would defeat the point.
//
// Attribution goes to whoever recorded the closing — the count is only finished
// at that moment, and that is the person who owns the variance if there is one.
func pettyCashCountedByBranch(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (map[instanceKey]satisfaction, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.count_date, c.branch_id, COALESCE(u.username, ''),
		       (c.closing_at::date = c.count_date)
		FROM petty_cash_counts c
		LEFT JOIN users u ON u.id = c.closing_by
		WHERE c.closing_amount IS NOT NULL
		  AND c.count_date BETWEEN $1 AND $2`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[instanceKey]satisfaction{}
	for rows.Next() {
		var (
			d        time.Time
			branchID pgtype.UUID
			sat      satisfaction
		)
		if err := rows.Scan(&d, &branchID, &sat.user, &sat.onDate); err != nil {
			return nil, err
		}
		out[instanceKey{date: d.Format(dateLayout), branch: uuidString(branchID)}] = sat
	}
	return out, rows.Err()
}

// posImportDoneByBranch reports which (date, branch) pairs already have a POS
// import. pos_imports carries no branch column — the branch is implied by the
// accounts its lines post to — so this resolves it the same way the branch P&L
// does, by walking the chart of accounts down from each branch's own accounts.
func posImportDoneByBranch(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (map[instanceKey]satisfaction, error) {
	rows, err := pool.Query(ctx, `
		WITH RECURSIVE direct AS (
		  SELECT b.id AS branch_id, b.revenue_account_id AS account_id FROM branches b WHERE b.revenue_account_id IS NOT NULL
		  UNION SELECT b.id, b.expense_account_id  FROM branches b  WHERE b.expense_account_id  IS NOT NULL
		  UNION SELECT d.branch_id, d.revenue_account_id  FROM divisions d WHERE d.revenue_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
		  UNION SELECT d.branch_id, d.expense_account_id  FROM divisions d WHERE d.expense_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
		  UNION SELECT d.branch_id, d.discount_account_id FROM divisions d WHERE d.discount_account_id IS NOT NULL AND d.branch_id IS NOT NULL
		),
		owned AS (
		  SELECT account_id, branch_id, 0 AS depth FROM direct
		  UNION ALL
		  SELECT a.id, o.branch_id, o.depth + 1
		  FROM accounts a JOIN owned o ON a.parent_id = o.account_id
		),
		owner AS (
		  SELECT DISTINCT ON (account_id) account_id, branch_id FROM owned ORDER BY account_id, depth
		)
		SELECT DISTINCT ON (pi.date, owner.branch_id)
		       pi.date, owner.branch_id, COALESCE(u.username, ''), (pi.created_at::date = pi.date)
		FROM pos_imports pi
		JOIN pos_import_lines pil ON pil.import_id = pi.id
		JOIN owner ON owner.account_id = pil.account_id
		LEFT JOIN users u ON u.id = pi.created_by
		WHERE pi.date BETWEEN $1 AND $2
		ORDER BY pi.date, owner.branch_id, pi.created_at`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[instanceKey]satisfaction{}
	for rows.Next() {
		var (
			d      time.Time
			branch pgtype.UUID
			sat    satisfaction
		)
		if err := rows.Scan(&d, &branch, &sat.user, &sat.onDate); err != nil {
			return nil, err
		}
		out[instanceKey{date: d.Format(dateLayout), branch: uuidString(branch)}] = sat
	}
	return out, rows.Err()
}

func manualCompletions(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (map[instanceKey]satisfaction, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.definition_id, c.branch_id, c.task_date, COALESCE(u.username, ''),
		       (c.completed_at::date = c.task_date)
		FROM daily_task_completions c
		LEFT JOIN users u ON u.id = c.completed_by
		WHERE c.task_date BETWEEN $1 AND $2`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[instanceKey]satisfaction{}
	for rows.Next() {
		var (
			def, branch pgtype.UUID
			d           time.Time
			sat         satisfaction
		)
		if err := rows.Scan(&def, &branch, &d, &sat.user, &sat.onDate); err != nil {
			return nil, err
		}
		out[instanceKey{defID: uuidString(def), branch: uuidString(branch), date: d.Format(dateLayout)}] = sat
	}
	return out, rows.Err()
}

func listBranches(ctx context.Context, pool *pgxpool.Pool) ([]branchRef, error) {
	rows, err := pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []branchRef{}
	for rows.Next() {
		var (
			id   pgtype.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, branchRef{ID: uuidString(id), Name: name})
	}
	return out, rows.Err()
}
