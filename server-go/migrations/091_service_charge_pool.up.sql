-- 091: Service Charge pool per branch per month.
--
-- Service Charge used to be a fixed amount on each employee's wage structure,
-- scaled by the performance grade. It is now a POT: before evaluation, HR looks
-- at a branch's net revenue for the month and sets how much of it is shared out
-- (a typed amount, or a percentage of that revenue). The pot is split between
-- the branch's employees whose wage structure carries the Service Charge
-- component, one share each scaled by their grade's Service Charge percentage
-- (A 100% → 1 share, B 50% → ½ share, 0% → nothing). A share someone forfeits
-- is therefore redistributed to the others: the pot is always paid out in full
-- while at least one employee is eligible (service/service_charge.go).
--
-- Payroll uses the share as the Service Charge component amount instead of the
-- fixed amount × grade. A branch with no pool for the month keeps the old rule.

-- Which wage component is "Service Charge". A singleton rather than a column on
-- wage_components: several queries read that table with SELECT *, and a new
-- column would break their scans until sqlc can regenerate the whole package.
CREATE TABLE service_charge_settings (
  id                INT         PRIMARY KEY CHECK (id = 1),
  wage_component_id UUID        REFERENCES wage_components(id) ON DELETE SET NULL,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO service_charge_settings (id, wage_component_id)
VALUES (1, (SELECT id FROM wage_components
            WHERE lower(trim(name)) = 'service charge' AND type IN ('bonus', 'allowance')
            ORDER BY created_at LIMIT 1));

CREATE TABLE service_charge_pools (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id      UUID        NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  period_month   DATE        NOT NULL CHECK (EXTRACT(DAY FROM period_month) = 1),
  -- Net revenue of the branch for the month when the pool was saved (frozen;
  -- the screen also shows the live figure so a late POS import is visible).
  revenue_amount BIGINT      NOT NULL DEFAULT 0,
  input_mode     TEXT        NOT NULL CHECK (input_mode IN ('amount', 'percent')),
  -- Percentage in basis points (525 = 5,25%) when input_mode = 'percent'.
  percent_bp     INT         CHECK (percent_bp IS NULL OR percent_bp BETWEEN 0 AND 10000),
  pool_amount    BIGINT      NOT NULL CHECK (pool_amount >= 0),
  note           TEXT,
  updated_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (branch_id, period_month),
  CHECK (input_mode <> 'percent' OR percent_bp IS NOT NULL)
);

CREATE INDEX idx_service_charge_pools_month ON service_charge_pools (period_month);
