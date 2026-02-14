-- Migration 005: Agent trust scores
-- Stores per-agent trust scores by category/severity combo.
-- trust_score is 0.0-1.0; 0.0 = untrusted, 1.0 = fully trusted.

CREATE TABLE IF NOT EXISTS agent_trust (
  agent_slug TEXT NOT NULL,
  category   TEXT NOT NULL DEFAULT '',
  severity   TEXT NOT NULL DEFAULT '',
  trust_score REAL NOT NULL DEFAULT 0.0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (agent_slug, category, severity)
);
