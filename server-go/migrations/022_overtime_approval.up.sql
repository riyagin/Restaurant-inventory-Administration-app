-- Add a manager approval workflow to overtime, mirroring leave_requests
-- (status pending/approved/rejected/cancelled + decision metadata). Payroll now
-- only counts APPROVED overtime hours (see hr_overtime.sql).

ALTER TABLE overtime_requests
  ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
  ADD COLUMN decided_by    UUID REFERENCES users(id),
  ADD COLUMN decided_at    TIMESTAMPTZ,
  ADD COLUMN decision_note TEXT;

-- Existing rows predate the workflow and were already summed into payroll; mark
-- them approved so overtime pay is unchanged for historical / open periods.
UPDATE overtime_requests SET status = 'approved', decided_at = created_at;

CREATE INDEX idx_overtime_requests_status ON overtime_requests (status);
