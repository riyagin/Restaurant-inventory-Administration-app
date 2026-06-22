CREATE TABLE overtime_requests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    date        DATE NOT NULL,
    hours       NUMERIC(5,2) NOT NULL CHECK (hours > 0),
    reason      TEXT,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON overtime_requests(employee_id, date);
