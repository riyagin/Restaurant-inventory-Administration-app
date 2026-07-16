-- HR THR (Tunjangan Hari Raya). A THR run mirrors a payroll period but computes a
-- single religious-holiday allowance per employee based on tenure (masa kerja) at the
-- run's payment_date. Per the rule implemented in service/thr.go:
--   masa kerja >= 12 bulan  -> THR = 1 bulan gaji pokok
--   masa kerja  < 12 bulan  -> THR = ceil(bulan masa kerja) / 12 * 1 bulan gaji pokok
--
-- Money is whole rupiah (BIGINT, no ×100), consistent with wages/payroll. months_worked
-- is the rounded-up tenure in months at payment_date; thr_ratio is months_worked/12
-- capped at 1.0 (stored NUMERIC(6,4) for display/audit). computed_amount is the
-- system figure; thr_amount is the reviewer-adjustable final amount (defaults equal).

CREATE TABLE thr_runs (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT        NOT NULL,                   -- e.g. "THR Idul Fitri 2026"
  payment_date  DATE        NOT NULL,                   -- reference date for tenure calc
  status        TEXT        NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','closed','paid')),
  created_by    UUID        REFERENCES users(id),
  closed_at     TIMESTAMPTZ,
  paid_at       TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_thr_runs_payment_date ON thr_runs (payment_date);
CREATE INDEX idx_thr_runs_status       ON thr_runs (status);

CREATE TABLE thr_lines (
  id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  thr_run_id        UUID         NOT NULL REFERENCES thr_runs(id) ON DELETE CASCADE,
  employee_id       UUID         NOT NULL REFERENCES employees(id),
  wage_structure_id UUID         NOT NULL REFERENCES wage_structures(id),
  base_salary       BIGINT       NOT NULL,
  join_date         DATE         NOT NULL,
  months_worked     INT          NOT NULL DEFAULT 0,     -- rounded-up tenure in months
  thr_ratio         NUMERIC(6,4) NOT NULL DEFAULT 0,     -- months_worked/12, capped at 1.0
  computed_amount   BIGINT       NOT NULL DEFAULT 0,     -- system-computed THR
  thr_amount        BIGINT       NOT NULL DEFAULT 0,     -- reviewer-adjustable final THR
  reviewed          BOOLEAN      NOT NULL DEFAULT false,
  reviewed_by       UUID         REFERENCES users(id),
  reviewed_at       TIMESTAMPTZ,
  review_note       TEXT,
  UNIQUE (thr_run_id, employee_id)
);

CREATE INDEX idx_thr_lines_run      ON thr_lines (thr_run_id);
CREATE INDEX idx_thr_lines_employee ON thr_lines (employee_id);
