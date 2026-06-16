-- HR wage module: reusable component catalog + versioned wage structures.

CREATE TABLE wage_components (
  id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT        NOT NULL UNIQUE,
  type       TEXT        NOT NULL CHECK (type IN ('allowance', 'bonus', 'deduction')),
  is_fixed   BOOLEAN     NOT NULL DEFAULT true,
  is_active  BOOLEAN     NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wage_structures (
  id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id            UUID        NOT NULL REFERENCES employees(id),
  base_salary            BIGINT      NOT NULL,
  working_days_per_month INT         NOT NULL CHECK (working_days_per_month BETWEEN 1 AND 31),
  daily_rate             BIGINT      NOT NULL,
  effective_date         DATE        NOT NULL,
  end_date               DATE,
  created_by             UUID        REFERENCES users(id),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (employee_id, effective_date)
);

-- Exactly one open version (end_date IS NULL) per employee.
CREATE UNIQUE INDEX idx_wage_structures_open ON wage_structures (employee_id) WHERE end_date IS NULL;
CREATE INDEX idx_wage_structures_employee ON wage_structures (employee_id);

CREATE TABLE employee_wage_components (
  id                UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  wage_structure_id UUID   NOT NULL REFERENCES wage_structures(id) ON DELETE CASCADE,
  wage_component_id UUID   NOT NULL REFERENCES wage_components(id),
  amount            BIGINT NOT NULL,
  UNIQUE (wage_structure_id, wage_component_id)
);

CREATE INDEX idx_employee_wage_components_structure ON employee_wage_components (wage_structure_id);
CREATE INDEX idx_employee_wage_components_component ON employee_wage_components (wage_component_id);
