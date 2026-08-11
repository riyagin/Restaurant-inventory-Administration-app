package service

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Staff KPIs.
//
// The back office is measured on the daily duties it owns (see dailytasks.go).
// Three metrics, deliberately split between what the *desk* achieved and what a
// *person* did, because the tasks are shared and pinning a missed day on one
// individual would be fiction:
//
//	completion_rate — team. Share of task-days anyone completed. Identical for
//	                  every staff member; it measures whether the work happened.
//	same_day_rate   — personal. Of the instances this person completed, how many
//	                  landed on the task's own date instead of being caught up.
//	completed_count — personal. How many instances this person completed.
//
// A person is reached through employees.user_id: HR links an employee record to
// a login, and only linked employees can be scored.

const (
	KPIMetricCompletionRate = "completion_rate"
	KPIMetricSameDayRate    = "same_day_rate"
	KPIMetricCompletedCount = "completed_count"
)

type StaffKPI struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	DefinitionID   string  `json:"definition_id"`
	DefinitionName string  `json:"definition_name"`
	Metric         string  `json:"metric"`
	TargetValue    float64 `json:"target_value"`
	Weight         int     `json:"weight"`
	IsActive       bool    `json:"is_active"`
}

// KPIResult is one KPI evaluated for one employee over one month.
type KPIResult struct {
	KPIID       string  `json:"kpi_id"`
	Name        string  `json:"name"`
	Metric      string  `json:"metric"`
	TargetValue float64 `json:"target_value"`
	ActualValue float64 `json:"actual_value"`
	Weight      int     `json:"weight"`
	// Achievement is actual/target as a percentage, capped at 100 so one
	// spectacular KPI cannot paper over a neglected one.
	Achievement float64 `json:"achievement"`
	Personal    bool    `json:"personal"`
}

// KPIScorecard is an employee's month.
type KPIScorecard struct {
	EmployeeID   string      `json:"employee_id"`
	EmployeeName string      `json:"employee_name"`
	EmployeeCode string      `json:"employee_code"`
	Username     string      `json:"username"`
	Score        float64     `json:"score"`
	Results      []KPIResult `json:"results"`
}

// ListStaffKPIs returns the configured KPIs with their task's name resolved.
func ListStaffKPIs(ctx context.Context, pool *pgxpool.Pool, activeOnly bool) ([]StaffKPI, error) {
	rows, err := pool.Query(ctx, `
		SELECT k.id, k.name, k.definition_id, d.name, k.metric, k.target_value, k.weight, k.is_active
		FROM staff_kpis k
		JOIN daily_task_definitions d ON d.id = k.definition_id
		WHERE ($1::bool = false OR k.is_active)
		ORDER BY k.name`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StaffKPI{}
	for rows.Next() {
		var (
			k      StaffKPI
			id     pgtype.UUID
			defID  pgtype.UUID
			target pgtype.Numeric
		)
		if err := rows.Scan(&id, &k.Name, &defID, &k.DefinitionName, &k.Metric, &target, &k.Weight, &k.IsActive); err != nil {
			return nil, err
		}
		k.ID, k.DefinitionID = uuidString(id), uuidString(defID)
		k.TargetValue = numericToFloat(target)
		out = append(out, k)
	}
	return out, rows.Err()
}

// ScoreStaffKPIs evaluates every active KPI for every employee linked to a
// login, over the calendar month containing `month`.
func ScoreStaffKPIs(ctx context.Context, pool *pgxpool.Pool, month time.Time) ([]KPIScorecard, error) {
	kpis, err := ListStaffKPIs(ctx, pool, true)
	if err != nil {
		return nil, err
	}
	staff, err := linkedStaff(ctx, pool)
	if err != nil {
		return nil, err
	}
	if len(kpis) == 0 || len(staff) == 0 {
		return []KPIScorecard{}, nil
	}

	from := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)
	// The month may not be over; nobody is judged on days that haven't happened.
	if today := time.Now().UTC().Truncate(24 * time.Hour); to.After(today) {
		to = today
	}
	if to.Before(from) {
		return []KPIScorecard{}, nil
	}

	board, err := TaskBoard(ctx, pool, "", from, to, to)
	if err != nil {
		return nil, err
	}

	// Per definition: how many instances existed, how many were done, and per
	// username how many that person did — split by whether the record was entered
	// on the task's own date or caught up afterwards.
	type tally struct {
		total, done int
		byUser      map[string]int
		sameDayUser map[string]int
	}
	stats := map[string]*tally{}
	for _, t := range board {
		s := stats[t.DefinitionID]
		if s == nil {
			s = &tally{byUser: map[string]int{}, sameDayUser: map[string]int{}}
			stats[t.DefinitionID] = s
		}
		s.total++
		if !t.Done {
			continue
		}
		s.done++
		if t.DoneBy != "" {
			s.byUser[t.DoneBy]++
			if t.DoneOnDate {
				s.sameDayUser[t.DoneBy]++
			}
		}
	}

	out := make([]KPIScorecard, 0, len(staff))
	for _, p := range staff {
		card := KPIScorecard{
			EmployeeID: p.employeeID, EmployeeName: p.fullName,
			EmployeeCode: p.code, Username: p.username,
			Results: []KPIResult{},
		}
		var weighted, weights float64
		for _, k := range kpis {
			s := stats[k.DefinitionID]
			if s == nil || s.total == 0 {
				continue
			}
			res := KPIResult{
				KPIID: k.ID, Name: k.Name, Metric: k.Metric,
				TargetValue: k.TargetValue, Weight: k.Weight,
			}
			switch k.Metric {
			case KPIMetricCompletionRate:
				res.ActualValue = pct(s.done, s.total)
			case KPIMetricSameDayRate:
				res.Personal = true
				res.ActualValue = pct(s.sameDayUser[p.username], s.byUser[p.username])
			case KPIMetricCompletedCount:
				res.Personal = true
				res.ActualValue = float64(s.byUser[p.username])
			}
			res.Achievement = 100
			if k.TargetValue > 0 {
				res.Achievement = math.Min(100, res.ActualValue/k.TargetValue*100)
			}
			weighted += res.Achievement * float64(k.Weight)
			weights += float64(k.Weight)
			card.Results = append(card.Results, res)
		}
		if weights > 0 {
			card.Score = math.Round(weighted/weights*10) / 10
		}
		out = append(out, card)
	}
	return out, nil
}

type linkedPerson struct{ employeeID, fullName, code, username string }

// linkedStaff returns active employees that carry a login. Not every employee
// has one — most shop-floor staff never touch the system — so an unlinked
// employee simply has no KPI scorecard.
func linkedStaff(ctx context.Context, pool *pgxpool.Pool) ([]linkedPerson, error) {
	rows, err := pool.Query(ctx, `
		SELECT e.id, e.full_name, e.employee_code, u.username
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.status = 'active'
		ORDER BY e.full_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []linkedPerson{}
	for rows.Next() {
		var (
			id pgtype.UUID
			p  linkedPerson
		)
		if err := rows.Scan(&id, &p.fullName, &p.code, &p.username); err != nil {
			return nil, err
		}
		p.employeeID = uuidString(id)
		out = append(out, p)
	}
	return out, rows.Err()
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return math.Round(float64(part)/float64(whole)*1000) / 10
}

func numericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid {
		return 0
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}
