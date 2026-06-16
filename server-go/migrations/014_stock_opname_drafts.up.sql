CREATE TABLE IF NOT EXISTS stock_opname_drafts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    pic_name     TEXT,
    operator_name TEXT,
    notes        TEXT,
    items        JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by   UUID REFERENCES users(id),
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);
