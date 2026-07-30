-- Store HR documents attached to an employee: the signed scans collected during
-- onboarding (contracts, KTP, ijazah, BPJS, etc.) and any generated letter that
-- HR chooses to keep on file. The generator itself (PKWT/PKWTT/Surat Peringatan/
-- Paklaring) is stateless — it streams a DOCX/PDF back without persisting — so
-- this table only holds what HR deliberately uploads.
--
-- Files live on disk in the same uploads dir as employee photos and the payslip
-- logo; only the stored filename is kept here. Deleting an employee cascades to
-- their documents rows (the files are unlinked by the handler before delete).
CREATE TABLE employee_documents (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id    UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  -- doc_type is a free-ish label used for grouping/filtering in the UI. Common
  -- values: 'pkwt', 'pkwtt', 'surat_peringatan', 'paklaring', 'ktp', 'kk',
  -- 'ijazah', 'npwp', 'bpjs_kesehatan', 'bpjs_ketenagakerjaan', 'foto', 'other'.
  doc_type       TEXT        NOT NULL DEFAULT 'other',
  title          TEXT        NOT NULL,
  file_path      TEXT        NOT NULL,        -- filename inside the uploads dir
  original_name  TEXT,                        -- name as uploaded by the user
  mime_type      TEXT,
  size_bytes     BIGINT,
  -- Whether this is a signed copy. Onboarding uploads default to signed; a blank
  -- template kept for reference can be flagged false.
  is_signed      BOOLEAN     NOT NULL DEFAULT TRUE,
  notes          TEXT,
  uploaded_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_employee_documents_employee ON employee_documents (employee_id);
CREATE INDEX idx_employee_documents_type     ON employee_documents (doc_type);
