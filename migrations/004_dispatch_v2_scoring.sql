-- Migration 004: Dispatch v2 Scoring Engine
-- Adds 11-factor scoring fields to swarm_tasks and creates agent_task_history table.
-- All new columns are NULLable for backward compatibility with existing tasks.

ALTER TABLE swarm_tasks
  ADD COLUMN IF NOT EXISTS risk_score REAL,
  ADD COLUMN IF NOT EXISTS cost_estimate_tokens BIGINT,
  ADD COLUMN IF NOT EXISTS cost_estimate_usd REAL,
  ADD COLUMN IF NOT EXISTS verifiability_score REAL,
  ADD COLUMN IF NOT EXISTS reversibility_score REAL,
  ADD COLUMN IF NOT EXISTS oversight_level TEXT CHECK (oversight_level IS NULL OR oversight_level IN ('autonomous','notify','review','approve','supervise')),
  ADD COLUMN IF NOT EXISTS scoring_factors JSONB,
  ADD COLUMN IF NOT EXISTS scoring_version INTEGER DEFAULT 1,
  ADD COLUMN IF NOT EXISTS complexity_score REAL,
  ADD COLUMN IF NOT EXISTS uncertainty_score REAL,
  ADD COLUMN IF NOT EXISTS duration_class TEXT CHECK (duration_class IS NULL OR duration_class IN ('instant','short','medium','long','extended')),
  ADD COLUMN IF NOT EXISTS contextuality_score REAL,
  ADD COLUMN IF NOT EXISTS subjectivity_score REAL,
  ADD COLUMN IF NOT EXISTS fast_path BOOLEAN DEFAULT false,
  ADD COLUMN IF NOT EXISTS pareto_frontier JSONB,
  ADD COLUMN IF NOT EXISTS alternative_decompositions JSONB;

CREATE TABLE IF NOT EXISTS agent_task_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_slug TEXT NOT NULL,
  task_id UUID NOT NULL REFERENCES swarm_tasks(task_id),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  duration_seconds REAL,
  tokens_used BIGINT,
  cost_usd REAL,
  success BOOLEAN,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_task_history_agent ON agent_task_history(agent_slug);
CREATE INDEX IF NOT EXISTS idx_agent_task_history_task ON agent_task_history(task_id);
