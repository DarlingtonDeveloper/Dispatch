-- Stage engine: add stage lifecycle columns to backlog_items and create stage_gates table.

ALTER TABLE backlog_items ADD COLUMN IF NOT EXISTS stage_template TEXT[] DEFAULT '{}';
ALTER TABLE backlog_items ADD COLUMN IF NOT EXISTS current_stage TEXT DEFAULT '';
ALTER TABLE backlog_items ADD COLUMN IF NOT EXISTS stage_index INT DEFAULT 0;

CREATE TABLE IF NOT EXISTS stage_gates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    backlog_item_id UUID NOT NULL REFERENCES backlog_items(id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    criterion TEXT NOT NULL,
    satisfied BOOLEAN DEFAULT FALSE,
    satisfied_at TIMESTAMPTZ,
    satisfied_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(backlog_item_id, stage, criterion)
);

CREATE INDEX IF NOT EXISTS idx_stage_gates_item_stage ON stage_gates(backlog_item_id, stage);
