-- HR bulk import: persisted parse batches.
-- A parse run stores its full validated preview payload as JSONB so that
-- confirm is an all-or-nothing operation against the exact rows the user
-- reviewed (rather than re-parsing/re-uploading the file).

CREATE TABLE hr_import_batches (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  uploaded_by UUID        REFERENCES users(id),
  filename    TEXT        NOT NULL,
  payload     JSONB       NOT NULL,
  row_count   INT         NOT NULL DEFAULT 0,
  status      TEXT        NOT NULL DEFAULT 'parsed' CHECK (status IN ('parsed', 'confirmed')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_hr_import_batches_status ON hr_import_batches (status);
