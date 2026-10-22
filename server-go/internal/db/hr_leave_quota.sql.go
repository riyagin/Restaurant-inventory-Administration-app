// Hand-written to match queries/hr_leave_quota.sql (sqlc output for this
// package does not round-trip; keep the two files in step by hand).

package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const insertLeaveQuotaUsage = `-- name: InsertLeaveQuotaUsage :exec
INSERT INTO leave_quota_usage (leave_request_id, employee_id, date, cost)
VALUES ($1, $2, $3, $4)
ON CONFLICT (employee_id, date) DO NOTHING
`

type InsertLeaveQuotaUsageParams struct {
	LeaveRequestID pgtype.UUID `json:"leave_request_id"`
	EmployeeID     pgtype.UUID `json:"employee_id"`
	Date           pgtype.Date `json:"date"`
	Cost           int32       `json:"cost"`
}

func (q *Queries) InsertLeaveQuotaUsage(ctx context.Context, arg *InsertLeaveQuotaUsageParams) error {
	_, err := q.db.Exec(ctx, insertLeaveQuotaUsage, arg.LeaveRequestID, arg.EmployeeID, arg.Date, arg.Cost)
	return err
}

const deleteLeaveQuotaUsageAfter = `-- name: DeleteLeaveQuotaUsageAfter :exec
DELETE FROM leave_quota_usage
WHERE leave_request_id = $1 AND date > $2
`

type DeleteLeaveQuotaUsageAfterParams struct {
	LeaveRequestID pgtype.UUID `json:"leave_request_id"`
	Date           pgtype.Date `json:"date"`
}

func (q *Queries) DeleteLeaveQuotaUsageAfter(ctx context.Context, arg *DeleteLeaveQuotaUsageAfterParams) error {
	_, err := q.db.Exec(ctx, deleteLeaveQuotaUsageAfter, arg.LeaveRequestID, arg.Date)
	return err
}

const sumLeaveQuotaUsage = `-- name: SumLeaveQuotaUsage :one
SELECT COALESCE(SUM(cost), 0)::int
FROM leave_quota_usage
WHERE employee_id = $1 AND date >= $2 AND date < $3
`

type SumLeaveQuotaUsageParams struct {
	EmployeeID pgtype.UUID `json:"employee_id"`
	From       pgtype.Date `json:"from"`
	To         pgtype.Date `json:"to"`
}

func (q *Queries) SumLeaveQuotaUsage(ctx context.Context, arg *SumLeaveQuotaUsageParams) (int32, error) {
	row := q.db.QueryRow(ctx, sumLeaveQuotaUsage, arg.EmployeeID, arg.From, arg.To)
	var used int32
	err := row.Scan(&used)
	return used, err
}

const getLeaveMonthlyQuota = `-- name: GetLeaveMonthlyQuota :one
SELECT quota_days FROM leave_monthly_quotas
WHERE employee_id = $1 AND month = $2
`

type GetLeaveMonthlyQuotaParams struct {
	EmployeeID pgtype.UUID `json:"employee_id"`
	Month      pgtype.Date `json:"month"`
}

func (q *Queries) GetLeaveMonthlyQuota(ctx context.Context, arg *GetLeaveMonthlyQuotaParams) (int32, error) {
	row := q.db.QueryRow(ctx, getLeaveMonthlyQuota, arg.EmployeeID, arg.Month)
	var quota int32
	err := row.Scan(&quota)
	return quota, err
}

const upsertLeaveMonthlyQuota = `-- name: UpsertLeaveMonthlyQuota :exec
INSERT INTO leave_monthly_quotas (employee_id, month, quota_days)
VALUES ($1, $2, $3)
ON CONFLICT (employee_id, month) DO UPDATE SET quota_days = EXCLUDED.quota_days
`

type UpsertLeaveMonthlyQuotaParams struct {
	EmployeeID pgtype.UUID `json:"employee_id"`
	Month      pgtype.Date `json:"month"`
	QuotaDays  int32       `json:"quota_days"`
}

func (q *Queries) UpsertLeaveMonthlyQuota(ctx context.Context, arg *UpsertLeaveMonthlyQuotaParams) error {
	_, err := q.db.Exec(ctx, upsertLeaveMonthlyQuota, arg.EmployeeID, arg.Month, arg.QuotaDays)
	return err
}

const listLeaveQuotaMonths = `-- name: ListLeaveQuotaMonths :many
SELECT m.month::date AS month,
       COALESCE(u.used, 0)::int AS used_days,
       q.quota_days
FROM (
  SELECT date_trunc('month', date)::date AS month FROM leave_quota_usage
  WHERE employee_id = $1 AND date >= $2 AND date < $3
  UNION
  SELECT month FROM leave_monthly_quotas
  WHERE employee_id = $1 AND month >= $2 AND month < $3
) m
LEFT JOIN (
  SELECT date_trunc('month', date)::date AS month, SUM(cost) AS used
  FROM leave_quota_usage
  WHERE employee_id = $1 AND date >= $2 AND date < $3
  GROUP BY 1
) u ON u.month = m.month
LEFT JOIN leave_monthly_quotas q ON q.employee_id = $1 AND q.month = m.month
ORDER BY m.month
`

type ListLeaveQuotaMonthsParams struct {
	EmployeeID pgtype.UUID `json:"employee_id"`
	From       pgtype.Date `json:"from"`
	To         pgtype.Date `json:"to"`
}

type ListLeaveQuotaMonthsRow struct {
	Month     pgtype.Date `json:"month"`
	UsedDays  int32       `json:"used_days"`
	QuotaDays pgtype.Int4 `json:"quota_days"` // NULL = no override
}

func (q *Queries) ListLeaveQuotaMonths(ctx context.Context, arg *ListLeaveQuotaMonthsParams) ([]*ListLeaveQuotaMonthsRow, error) {
	rows, err := q.db.Query(ctx, listLeaveQuotaMonths, arg.EmployeeID, arg.From, arg.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*ListLeaveQuotaMonthsRow
	for rows.Next() {
		var i ListLeaveQuotaMonthsRow
		if err := rows.Scan(&i.Month, &i.UsedDays, &i.QuotaDays); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listMonthlyLeaveBalances = `-- name: ListMonthlyLeaveBalances :many
SELECT e.id, e.full_name, e.employee_code, COALESCE(b.name, 'Tanpa Cabang')::text AS branch_name,
       COALESCE(q.quota_days, $2::int)::int AS quota_days,
       (q.quota_days IS NOT NULL) AS overridden,
       COALESCE((SELECT SUM(u.cost) FROM leave_quota_usage u
                 WHERE u.employee_id = e.id AND u.date >= $1
                   AND u.date < ($1::date + interval '1 month')), 0)::int AS used_days
FROM employees e
LEFT JOIN branches b ON b.id = e.branch_id
LEFT JOIN leave_monthly_quotas q ON q.employee_id = e.id AND q.month = $1
WHERE e.status = 'active'
ORDER BY COALESCE(b.name, 'Tanpa Cabang'), e.full_name
`

type ListMonthlyLeaveBalancesParams struct {
	Month        pgtype.Date `json:"month"`
	DefaultQuota int32       `json:"default_quota"`
}

type ListMonthlyLeaveBalancesRow struct {
	EmployeeID   pgtype.UUID `json:"employee_id"`
	EmployeeName string      `json:"employee_name"`
	EmployeeCode string      `json:"employee_code"`
	BranchName   string      `json:"branch_name"`
	QuotaDays    int32       `json:"quota_days"`
	Overridden   bool        `json:"overridden"`
	UsedDays     int32       `json:"used_days"`
}

func (q *Queries) ListMonthlyLeaveBalances(ctx context.Context, arg *ListMonthlyLeaveBalancesParams) ([]*ListMonthlyLeaveBalancesRow, error) {
	rows, err := q.db.Query(ctx, listMonthlyLeaveBalances, arg.Month, arg.DefaultQuota)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*ListMonthlyLeaveBalancesRow
	for rows.Next() {
		var i ListMonthlyLeaveBalancesRow
		if err := rows.Scan(&i.EmployeeID, &i.EmployeeName, &i.EmployeeCode, &i.BranchName, &i.QuotaDays, &i.Overridden, &i.UsedDays); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
