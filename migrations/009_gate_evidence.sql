-- Gate evidence and autonomy system: add evidence tracking and autonomy config.

-- Add evidence columns to stage_gates
ALTER TABLE stage_gates ADD COLUMN IF NOT EXISTS evidence TEXT;
ALTER TABLE stage_gates ADD COLUMN IF NOT EXISTS evidence_submitted_by TEXT;
ALTER TABLE stage_gates ADD COLUMN IF NOT EXISTS evidence_submitted_at TIMESTAMPTZ;

-- Create autonomy_config table for economy auto-approve tracking
CREATE TABLE IF NOT EXISTS autonomy_config (
    id SERIAL PRIMARY KEY,
    tier TEXT UNIQUE NOT NULL,
    auto_approve BOOLEAN DEFAULT FALSE,
    consecutive_approvals INT DEFAULT 0,
    consecutive_corrections INT DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert initial economy tier config
INSERT INTO autonomy_config (tier) VALUES ('economy') ON CONFLICT (tier) DO NOTHING;

-- Create index for efficient lookups
CREATE INDEX IF NOT EXISTS idx_autonomy_config_tier ON autonomy_config(tier);
