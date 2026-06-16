-- HR Payroll (prompt 08). Monthly payroll periods + per-employee snapshot lines.
-- Each payroll_line snapshots the employee's wage structure (base_salary, daily_rate,
-- wage_structure_id), fixed allowance/bonus/deduction totals, overtime & public-holiday
-- pay, kasbon installment deductions and unpaid-leave deductions. Lines must be reviewed
-- before the period can be closed; closing posts the total payroll expense to each
-- branch's expense account and marks the kasbon installments deducted.
--
-- Money is whole rupiah (int64 / BIGINT, no ×100), consistent with wages/kasbon.
-- overtime_days / public_holiday_days are NUMERIC(5,2); multipliers NUMERIC(4,2). The
-- Go layer maps these to/from float64 at the db boundary (money stays int64).

CREATE TABLE payroll_periods (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  period_month  DATE        NOT NULL UNIQUE,            -- first day of month
  start_date    DATE        NOT NULL,
  end_date      DATE        NOT NULL,
  status        TEXT        NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','closed','paid')),
  created_by    UUID        REFERENCES users(id),
  closed_at     TIMESTAMPTZ,
  paid_at       TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_payroll_periods_month  ON payroll_periods (period_month);
CREATE INDEX idx_payroll_periods_status ON payroll_periods (status);

CREATE TABLE payroll_lines (
  id                        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  payroll_period_id         UUID         NOT NULL REFERENCES payroll_periods(id) ON DELETE CASCADE,
  employee_id               UUID         NOT NULL REFERENCES employees(id),
  wage_structure_id         UUID         NOT NULL REFERENCES wage_structures(id),
  base_salary               BIGINT       NOT NULL,
  daily_rate                BIGINT       NOT NULL,
  overtime_days             NUMERIC(5,2) NOT NULL DEFAULT 0,
  public_holiday_days       NUMERIC(5,2) NOT NULL DEFAULT 0,
  overtime_amount           BIGINT       NOT NULL DEFAULT 0,
  public_holiday_amount     BIGINT       NOT NULL DEFAULT 0,
  allowance_total           BIGINT       NOT NULL DEFAULT 0,
  bonus_total               BIGINT       NOT NULL DEFAULT 0,
  component_deduction_total BIGINT       NOT NULL DEFAULT 0,
  kasbon_deduction          BIGINT       NOT NULL DEFAULT 0,
  unpaid_leave_days         INT          NOT NULL DEFAULT 0,
  unpaid_leave_deduction    BIGINT       NOT NULL DEFAULT 0,
  gross_pay                 BIGINT       NOT NULL DEFAULT 0,
  net_pay                   BIGINT       NOT NULL DEFAULT 0,
  performance_score         INT,
  reviewed                  BOOLEAN      NOT NULL DEFAULT false,
  reviewed_by               UUID         REFERENCES users(id),
  reviewed_at               TIMESTAMPTZ,
  review_note               TEXT,
  UNIQUE (payroll_period_id, employee_id)
);

CREATE INDEX idx_payroll_lines_period   ON payroll_lines (payroll_period_id);
CREATE INDEX idx_payroll_lines_employee ON payroll_lines (employee_id);

CREATE TABLE payroll_line_components (
  id                UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  payroll_line_id   UUID   NOT NULL REFERENCES payroll_lines(id) ON DELETE CASCADE,
  wage_component_id UUID   REFERENCES wage_components(id),
  name              TEXT   NOT NULL,
  type              TEXT   NOT NULL,
  amount            BIGINT NOT NULL
);

CREATE INDEX idx_payroll_line_components_line ON payroll_line_components (payroll_line_id);

CREATE TABLE payroll_settings (
  id                  INT          PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  overtime_multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.5,
  holiday_multiplier  NUMERIC(4,2) NOT NULL DEFAULT 2.0
);

INSERT INTO payroll_settings (id, overtime_multiplier, holiday_multiplier)
VALUES (1, 1.5, 2.0)
ON CONFLICT (id) DO NOTHING;
