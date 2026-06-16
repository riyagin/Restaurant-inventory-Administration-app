-- HR Performance Scoring: configurable violation policies, recorded violations,
-- and materialized monthly scores. Monthly score starts at 100; violations deduct
-- points. Auto violations are derived from attendance anomalies by the evaluation
-- engine (nightly tick + manual backfill); manual violations are entered by hand.

-- Configurable deduction policies.
CREATE TABLE performance_policies (
  id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name                      TEXT        NOT NULL,
  rule_type                 TEXT        NOT NULL
                              CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'absent_no_leave', 'manual')),
  threshold_minutes         INT,                                  -- for late/early_leave: applies when minutes >= threshold
  points                    INT         NOT NULL CHECK (points > 0),
  max_occurrences_per_month INT,                                  -- NULL = unlimited
  is_active                 BOOLEAN     NOT NULL DEFAULT true,
  created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_performance_policies_rule_active ON performance_policies (rule_type, is_active);

-- Individual violations. Auto violations are idempotent per (policy, attendance
-- record); manual violations carry a NULL policy_id and NULL attendance_record_id.
CREATE TABLE performance_violations (
  id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id          UUID        NOT NULL REFERENCES employees(id),
  policy_id            UUID        REFERENCES performance_policies(id),
  attendance_record_id UUID        REFERENCES attendance_records(id),
  date                 DATE        NOT NULL,
  points               INT         NOT NULL,
  source               TEXT        NOT NULL CHECK (source IN ('auto', 'manual')),
  note                 TEXT,
  created_by           UUID        REFERENCES users(id),
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (policy_id, attendance_record_id)
);

CREATE INDEX idx_performance_violations_emp_date ON performance_violations (employee_id, date);
CREATE INDEX idx_performance_violations_record   ON performance_violations (attendance_record_id);

-- Materialized monthly score per employee.
CREATE TABLE performance_scores (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id  UUID NOT NULL REFERENCES employees(id),
  period_month DATE NOT NULL,                                     -- first day of the month
  score        INT  NOT NULL DEFAULT 100,
  UNIQUE (employee_id, period_month)
);

CREATE INDEX idx_performance_scores_emp_month ON performance_scores (employee_id, period_month);
