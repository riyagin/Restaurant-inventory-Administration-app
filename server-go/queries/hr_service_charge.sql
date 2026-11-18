-- Service Charge pool (migration 091). See service/service_charge.go.

-- name: GetServiceChargeComponentID :one
SELECT wage_component_id FROM service_charge_settings WHERE id = 1;

-- name: SetServiceChargeComponentID :exec
INSERT INTO service_charge_settings (id, wage_component_id, updated_at)
VALUES (1, $1, now())
ON CONFLICT (id) DO UPDATE SET wage_component_id = EXCLUDED.wage_component_id, updated_at = now();

-- name: GetServiceChargePool :one
SELECT * FROM service_charge_pools
WHERE branch_id = $1 AND period_month = $2;

-- name: ListServiceChargePoolsByMonth :many
SELECT * FROM service_charge_pools
WHERE period_month = $1;

-- name: UpsertServiceChargePool :one
INSERT INTO service_charge_pools
  (branch_id, period_month, revenue_amount, input_mode, percent_bp, pool_amount, note, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (branch_id, period_month) DO UPDATE SET
  revenue_amount = EXCLUDED.revenue_amount,
  input_mode     = EXCLUDED.input_mode,
  percent_bp     = EXCLUDED.percent_bp,
  pool_amount    = EXCLUDED.pool_amount,
  note           = EXCLUDED.note,
  updated_by     = EXCLUDED.updated_by,
  updated_at     = now()
RETURNING *;

-- name: DeleteServiceChargePool :exec
DELETE FROM service_charge_pools WHERE branch_id = $1 AND period_month = $2;
