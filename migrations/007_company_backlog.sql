-- 007_company_backlog.sql
-- Company backlog: work planning layer (separate from swarm_tasks agent assignment)

BEGIN;

-- backlog_items: company-level work items with lifecycle and scoring
CREATE TABLE IF NOT EXISTS backlog_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           TEXT NOT NULL,
    description     TEXT,
    item_type       TEXT NOT NULL DEFAULT 'task',         -- epic, story, task, spike, bug
    status          TEXT NOT NULL DEFAULT 'backlog',      -- backlog, ready, in_discovery, planned, in_progress, review, blocked, done, cancelled
    domain          TEXT,                                 -- infrastructure, product, operations, research, etc.
    assigned_to     TEXT,                                 -- agent slug or human identifier
    parent_id       UUID REFERENCES backlog_items(id) ON DELETE SET NULL,

    -- Scoring inputs
    impact          DOUBLE PRECISION,                    -- 0.0-1.0 business impact
    urgency         DOUBLE PRECISION,                    -- 0.0-1.0 time pressure
    estimated_tokens BIGINT,                             -- estimated token cost
    effort_estimate TEXT,                                -- xs, s, m, l, xl

    -- Scoring outputs
    priority_score  DOUBLE PRECISION,                    -- computed 4-factor score
    scores_source   TEXT DEFAULT 'manual',               -- manual, discovery, system

    -- Model routing hints
    model_tier      TEXT,                                -- economy, standard, premium
    labels          TEXT[],
    one_way_door    BOOLEAN DEFAULT FALSE,

    -- Discovery
    discovery_assessment JSONB,                          -- full assessment from discovery phase

    -- Metadata
    source          TEXT DEFAULT 'manual',               -- manual, seed, agent, nats
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- backlog_dependencies: junction table for item blocking relationships
CREATE TABLE IF NOT EXISTS backlog_dependencies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    blocker_id  UUID NOT NULL REFERENCES backlog_items(id) ON DELETE CASCADE,
    blocked_id  UUID NOT NULL REFERENCES backlog_items(id) ON DELETE CASCADE,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);

-- dispatch_overrides: human override audit trail
CREATE TABLE IF NOT EXISTS dispatch_overrides (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    backlog_item_id UUID REFERENCES backlog_items(id) ON DELETE SET NULL,
    task_id         UUID REFERENCES swarm_tasks(task_id) ON DELETE SET NULL,
    override_type   TEXT NOT NULL,                       -- priority, assignment, status, model_tier
    previous_value  TEXT,
    new_value       TEXT NOT NULL,
    reason          TEXT,
    overridden_by   TEXT NOT NULL,                       -- who made the override
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- autonomy_events: track autonomous vs overridden decisions
CREATE TABLE IF NOT EXISTS autonomy_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    backlog_item_id UUID REFERENCES backlog_items(id) ON DELETE SET NULL,
    task_id         UUID REFERENCES swarm_tasks(task_id) ON DELETE SET NULL,
    event_type      TEXT NOT NULL,                       -- auto_assigned, auto_scored, overridden, escalated
    was_autonomous  BOOLEAN NOT NULL DEFAULT TRUE,
    details         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_backlog_items_status ON backlog_items(status);
CREATE INDEX IF NOT EXISTS idx_backlog_items_domain ON backlog_items(domain);
CREATE INDEX IF NOT EXISTS idx_backlog_items_assigned_to ON backlog_items(assigned_to);
CREATE INDEX IF NOT EXISTS idx_backlog_items_parent_id ON backlog_items(parent_id);
CREATE INDEX IF NOT EXISTS idx_backlog_items_priority_score ON backlog_items(priority_score DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_backlog_items_status_priority ON backlog_items(status, priority_score DESC NULLS LAST, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_backlog_dependencies_blocker ON backlog_dependencies(blocker_id);
CREATE INDEX IF NOT EXISTS idx_backlog_dependencies_blocked ON backlog_dependencies(blocked_id);
CREATE INDEX IF NOT EXISTS idx_backlog_dependencies_unresolved ON backlog_dependencies(blocked_id) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dispatch_overrides_item ON dispatch_overrides(backlog_item_id);
CREATE INDEX IF NOT EXISTS idx_dispatch_overrides_task ON dispatch_overrides(task_id);
CREATE INDEX IF NOT EXISTS idx_autonomy_events_item ON autonomy_events(backlog_item_id);
CREATE INDEX IF NOT EXISTS idx_autonomy_events_created ON autonomy_events(created_at);

-- updated_at trigger (following swarm_tasks pattern from migration 003)
CREATE OR REPLACE FUNCTION update_backlog_items_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_backlog_items_updated_at ON backlog_items;
CREATE TRIGGER trigger_backlog_items_updated_at
    BEFORE UPDATE ON backlog_items
    FOR EACH ROW
    EXECUTE FUNCTION update_backlog_items_updated_at();

-- autonomy_rate view: daily autonomous vs overridden ratio
CREATE OR REPLACE VIEW autonomy_rate AS
SELECT
    DATE(created_at) AS day,
    COUNT(*) AS total_events,
    COUNT(*) FILTER (WHERE was_autonomous = TRUE) AS autonomous_count,
    COUNT(*) FILTER (WHERE was_autonomous = FALSE) AS overridden_count,
    CASE
        WHEN COUNT(*) > 0
        THEN ROUND(COUNT(*) FILTER (WHERE was_autonomous = TRUE)::NUMERIC / COUNT(*)::NUMERIC, 4)
        ELSE 0
    END AS autonomy_ratio
FROM autonomy_events
GROUP BY DATE(created_at)
ORDER BY day DESC;

COMMIT;
