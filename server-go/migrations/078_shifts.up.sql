-- Shift system. Each branch owns its own set of shift definitions ("versions"),
-- and manpower planning assigns one shift per employee per day.
--
-- A shift is one or two segments: the "jumping" shift at Warung Jambu works a
-- morning block, goes home, and comes back for the evening, so a single
-- start/end pair cannot describe it. break_minutes is the unpaid allowance
-- inside the span, which is how 09.30-22.00 is an 11.5-hour shift.
CREATE TABLE shift_definitions (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id     UUID        NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  code          TEXT        NOT NULL,              -- short label printed in grid cells
  name          TEXT        NOT NULL,
  start1        TIME        NOT NULL,
  end1          TIME        NOT NULL,
  start2        TIME,
  end2          TIME,
  break_minutes INT         NOT NULL DEFAULT 0 CHECK (break_minutes >= 0),
  -- Only employees flagged employees.is_delivery_staff may be rostered on it.
  delivery_only BOOLEAN     NOT NULL DEFAULT false,
  color         TEXT        NOT NULL DEFAULT '#dbeafe',
  sort_order    INT         NOT NULL DEFAULT 0,
  is_active     BOOLEAN     NOT NULL DEFAULT true,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (branch_id, code),
  CHECK (end1 > start1),
  CHECK ((start2 IS NULL) = (end2 IS NULL)),
  CHECK (start2 IS NULL OR (start2 >= end1 AND end2 > start2))
);

-- Home division (which kitchen/counter the person normally covers) and whether
-- they are delivery-order staff. A day's assignment may still override the
-- division; NULL there means "home division".
ALTER TABLE employees
  ADD COLUMN division_id       UUID    REFERENCES divisions(id) ON DELETE SET NULL,
  ADD COLUMN is_delivery_staff BOOLEAN NOT NULL DEFAULT false;

-- The effective roster: one shift per employee per day. No row = not rostered.
-- Removing a shift deletes the row (hard deletes only).
CREATE TABLE shift_assignments (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  date        DATE        NOT NULL,
  shift_id    UUID        NOT NULL REFERENCES shift_definitions(id) ON DELETE RESTRICT,
  division_id UUID        REFERENCES divisions(id) ON DELETE SET NULL,
  updated_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (employee_id, date)
);
CREATE INDEX idx_shift_assignments_date ON shift_assignments (date);

-- A change to *today's* roster made by someone who cannot approve. It does not
-- touch shift_assignments until a manager approves it, so the roster in force
-- stays the one that was approved. shift_id NULL = the request removes the shift.
-- Past days are read-only and never get a request; future days apply directly.
CREATE TABLE shift_change_requests (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id   UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  date          DATE        NOT NULL,
  shift_id      UUID        REFERENCES shift_definitions(id) ON DELETE CASCADE,
  division_id   UUID        REFERENCES divisions(id) ON DELETE SET NULL,
  -- What was in force when the request was made, for the approver to compare.
  prev_shift_id UUID        REFERENCES shift_definitions(id) ON DELETE SET NULL,
  reason        TEXT,
  status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected', 'superseded')),
  decision_note TEXT,
  requested_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  requested_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  decided_at    TIMESTAMPTZ
);
-- At most one open request per cell; a second edit revises it in place.
CREATE UNIQUE INDEX uq_shift_change_requests_pending
  ON shift_change_requests (employee_id, date) WHERE status = 'pending';

-- Seed the shift versions in use today, matched by branch name. A deployment
-- whose branches are named differently simply gets none and sets them up on
-- the manpower planning page.
INSERT INTO shift_definitions (branch_id, code, name, start1, end1, start2, end2, break_minutes, delivery_only, color, sort_order)
SELECT b.id, s.code, s.name, s.start1::time, s.end1::time, s.start2::time, s.end2::time, s.break_minutes, s.delivery_only, s.color, s.sort_order
FROM branches b
JOIN (VALUES
  ('Warung Jambu', 'JMP',  'Shift Jumping', '08:30', '14:00', '16:30', '22:00', 0,  false, '#fde68a', 1),
  ('Warung Jambu', 'OPN',  'Shift Opening', '08:30', '19:30', NULL,    NULL,    0,  false, '#bfdbfe', 2),
  ('Warung Jambu', 'MID',  'Shift Middle',  '09:30', '22:00', NULL,    NULL,    60, true,  '#ddd6fe', 3),
  ('Warung Jambu', 'LONG', 'Longshift',     '08:30', '22:00', NULL,    NULL,    0,  false, '#bbf7d0', 4),
  ('Cimanggu',     'LONG', 'Longshift',     '08:30', '22:00', NULL,    NULL,    0,  false, '#bbf7d0', 1),
  ('Dewi Sartika', 'LONG', 'Longshift',     '06:00', '18:00', NULL,    NULL,    0,  false, '#bbf7d0', 1),
  ('Pasar Anyar',  'LONG', 'Longshift',     '06:00', '18:00', NULL,    NULL,    0,  false, '#bbf7d0', 1)
) AS s(branch_name, code, name, start1, end1, start2, end2, break_minutes, delivery_only, color, sort_order)
  ON lower(b.name) = lower(s.branch_name)
ON CONFLICT (branch_id, code) DO NOTHING;
