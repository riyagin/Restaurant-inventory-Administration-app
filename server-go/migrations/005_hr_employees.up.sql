-- Part A: relax/extend users.role to allow admin | manager | staff.
-- The initial schema had no CHECK constraint on users.role; add one that
-- explicitly permits the three valid roles (manager is new in this prompt).
ALTER TABLE users
  ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'manager', 'staff'));

-- Part B: HR positions + employees.
CREATE TABLE positions (
  id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT        NOT NULL UNIQUE,
  is_active  BOOLEAN     NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE employees (
  id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_code       TEXT        NOT NULL UNIQUE,
  full_name           TEXT        NOT NULL,
  dob                 DATE,
  join_date           DATE        NOT NULL,
  position_id         UUID        NOT NULL REFERENCES positions(id),
  branch_id           UUID        NOT NULL REFERENCES branches(id),
  phone               TEXT,
  email               TEXT,
  address             TEXT,
  national_id         TEXT,
  bank_name           TEXT,
  bank_account_number TEXT,
  bank_account_holder TEXT,
  photo_path          TEXT,
  user_id             UUID        REFERENCES users(id) ON DELETE SET NULL,
  status              TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_employees_branch_id   ON employees (branch_id);
CREATE INDEX idx_employees_position_id ON employees (position_id);
CREATE INDEX idx_employees_status      ON employees (status);
CREATE INDEX idx_employees_full_name   ON employees (lower(full_name));
