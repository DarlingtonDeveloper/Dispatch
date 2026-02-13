-- 003_create_swarm_tasks_fresh.sql
-- Creates swarm_tasks and swarm_task_events from scratch.
-- Use this on a fresh database where 001/002 were never run.

BEGIN;

CREATE TABLE IF NOT EXISTS swarm_tasks (
  task_id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title                TEXT NOT NULL,
  description          TEXT,
  owner                TEXT NOT NULL DEFAULT 'system',
  required_capabilities TEXT[] DEFAULT '{}',

  status               TEXT NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'assigned', 'in_progress', 'completed', 'failed', 'timed_out')),
  assigned_agent       TEXT,

  created_at           TIMESTAMPTZ DEFAULT now(),
  assigned_at          TIMESTAMPTZ,
  started_at           TIMESTAMPTZ,
  completed_at         TIMESTAMPTZ,
  updated_at           TIMESTAMPTZ DEFAULT now(),

  result               JSONB,
  error                TEXT,

  retry_count          INTEGER DEFAULT 0,
  max_retries          INTEGER DEFAULT 3,
  retry_eligible       BOOLEAN DEFAULT true,

  timeout_seconds      INTEGER DEFAULT 300,

  priority             INTEGER DEFAULT 0 CHECK (priority >= 0 AND priority <= 10),
  source               TEXT DEFAULT 'manual',
  parent_task_id       UUID REFERENCES swarm_tasks(task_id),
  metadata             JSONB DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS swarm_task_events (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id    UUID NOT NULL REFERENCES swarm_tasks(task_id),
  event      TEXT NOT NULL,
  agent_id   TEXT,
  payload    JSONB,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_tasks_status   ON swarm_tasks (status);
CREATE INDEX IF NOT EXISTS idx_tasks_agent    ON swarm_tasks (assigned_agent);
CREATE INDEX IF NOT EXISTS idx_tasks_owner    ON swarm_tasks (owner);
CREATE INDEX IF NOT EXISTS idx_tasks_created  ON swarm_tasks (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_parent   ON swarm_tasks (parent_task_id);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON swarm_tasks (priority DESC, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_tasks_assigned_timeout
  ON swarm_tasks (assigned_at)
  WHERE status = 'assigned';

CREATE INDEX IF NOT EXISTS idx_tasks_started_timeout
  ON swarm_tasks (started_at)
  WHERE status = 'in_progress';

CREATE INDEX IF NOT EXISTS idx_task_events_task    ON swarm_task_events (task_id);
CREATE INDEX IF NOT EXISTS idx_task_events_created ON swarm_task_events (created_at);

-- Auto-update updated_at
CREATE OR REPLACE FUNCTION update_swarm_tasks_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER swarm_tasks_updated_at
  BEFORE UPDATE ON swarm_tasks
  FOR EACH ROW
  EXECUTE FUNCTION update_swarm_tasks_updated_at();

COMMIT;
