-- Daily operational tasks, and the staff KPIs measured against them.
--
-- The back-office ("staff") side of the business has a handful of duties that
-- must happen every single day: record the day's purchasing, and import each
-- branch's POS sales. These are *shared* — whoever gets to it first does it for
-- everyone — so they belong to the organisation, not to a person.
--
-- Completion is DERIVED, not ticked. A purchase invoice dated D satisfies D's
-- purchasing task; a POS import whose lines land on branch B satisfies B's task
-- for that date. Nothing to check off, nothing that can claim "done" when the
-- work wasn't, and every day of existing history is scored correctly the moment
-- this ships. Only `manual` definitions — duties with no data trail — record a
-- row in daily_task_completions.

-- ── Who recorded a purchase ────────────────────────────────────────────────
-- invoices never tracked its author, which is fine for the ledger but leaves
-- per-person KPI attribution impossible. Backfilled from the activity log,
-- which has recorded the user behind every CREATE since the beginning.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;

UPDATE invoices i
SET created_by = al.user_id
FROM (
  SELECT DISTINCT ON (entity_id) entity_id, user_id
  FROM activity_log
  -- 048 normalised activity_log casing to lowercase; compare case-insensitively
  -- so this backfill works on databases from either side of that migration.
  WHERE lower(action) = 'create' AND lower(entity_type) = 'invoice'
    AND entity_id IS NOT NULL AND user_id IS NOT NULL
  ORDER BY entity_id, created_at
) al
WHERE al.entity_id = i.id AND i.created_by IS NULL;

CREATE INDEX IF NOT EXISTS idx_invoices_created_by ON invoices (created_by);

-- ── Task definitions ───────────────────────────────────────────────────────
-- `scope = per_branch` expands to one task per active branch at query time
-- rather than storing a row per branch, so opening a new branch immediately
-- carries its own daily duties without anyone remembering to add them.
CREATE TABLE IF NOT EXISTS daily_task_definitions (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT        NOT NULL,
  description TEXT        NOT NULL DEFAULT '',
  task_type   TEXT        NOT NULL CHECK (task_type IN ('purchasing', 'pos_import', 'manual')),
  scope       TEXT        NOT NULL DEFAULT 'global' CHECK (scope IN ('global', 'per_branch')),
  target_role TEXT        NOT NULL DEFAULT 'staff',
  link_path   TEXT        NOT NULL DEFAULT '',
  -- First date this duty applies. NULL means "as far back as anyone looks",
  -- which is what the seeded duties want: purchasing and POS import have been
  -- expected every day since the business opened, so existing history should be
  -- scored, not excused. A duty invented today defaults to today instead — you
  -- cannot be late for something that did not exist yet.
  starts_on   DATE,
  -- Days of slack before an unfinished task is called overdue. 0 = the day is
  -- over and it wasn't done.
  grace_days  INT         NOT NULL DEFAULT 0 CHECK (grace_days >= 0),
  is_active   BOOLEAN     NOT NULL DEFAULT true,
  sort_order  INT         NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only `manual` definitions write here; derived types are answered by the data.
CREATE TABLE IF NOT EXISTS daily_task_completions (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  definition_id UUID        NOT NULL REFERENCES daily_task_definitions(id) ON DELETE CASCADE,
  branch_id     UUID        REFERENCES branches(id) ON DELETE CASCADE,
  task_date     DATE        NOT NULL,
  completed_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  completed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  note          TEXT        NOT NULL DEFAULT ''
);

-- One completion per task instance. branch_id is NULL for global tasks, and
-- NULL never equals NULL in a unique constraint, hence the coalesced index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_task_completions_instance
  ON daily_task_completions (definition_id, COALESCE(branch_id, '00000000-0000-0000-0000-000000000000'::uuid), task_date);

-- ── Staff KPIs ─────────────────────────────────────────────────────────────
-- Each KPI measures one task definition. The metrics split deliberately:
--   completion_rate — team: share of task-days anyone completed. Same number for
--                     every staff member; it measures the desk, not the person.
--   same_day_rate   — personal: of the instances this person completed, how many
--                     landed on the task's own date rather than being caught up.
--   completed_count — personal: how many instances this person completed.
CREATE TABLE IF NOT EXISTS staff_kpis (
  id            UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT          NOT NULL,
  definition_id UUID          NOT NULL REFERENCES daily_task_definitions(id) ON DELETE CASCADE,
  metric        TEXT          NOT NULL CHECK (metric IN ('completion_rate', 'same_day_rate', 'completed_count')),
  target_value  NUMERIC(6, 2) NOT NULL CHECK (target_value >= 0),
  weight        INT           NOT NULL DEFAULT 1 CHECK (weight > 0),
  is_active     BOOLEAN       NOT NULL DEFAULT true,
  created_at    TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_staff_kpis_active ON staff_kpis (is_active);

-- ── Seed the two duties named in the brief ─────────────────────────────────
INSERT INTO daily_task_definitions (name, description, task_type, scope, target_role, link_path, sort_order)
SELECT 'Pembelian Harian', 'Catat invoice pembelian hari ini.', 'purchasing', 'global', 'staff', '/invoices/new', 10
WHERE NOT EXISTS (SELECT 1 FROM daily_task_definitions WHERE task_type = 'purchasing');

INSERT INTO daily_task_definitions (name, description, task_type, scope, target_role, link_path, sort_order)
SELECT 'Import POS', 'Impor penjualan POS untuk setiap cabang.', 'pos_import', 'per_branch', 'staff', '/sales/import', 20
WHERE NOT EXISTS (SELECT 1 FROM daily_task_definitions WHERE task_type = 'pos_import');
