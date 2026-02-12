# Dispatch Update Complete

## Changes Made

### 1. Task Model — Owner + Submitter
- Added `owner` (UUID string) and `submitter` (string) fields to `store.Task`
- Updated migration SQL with both columns + owner index
- Ran ALTER TABLE on Supabase to add columns to existing table
- All postgres queries (create, get, list, update, scan) updated for new fields
- `TaskFilter` supports `Owner` field for filtering

### 2. Owner-Scoped Assignment
- New `internal/alexandria` package with `Client` interface and `HTTPClient`
- Queries `GET http://localhost:8500/api/v1/devices` with `X-Agent-ID: dispatch`
- Broker filters candidates to only agents owned by the task's owner
- If no owned agents match capability, task stays PENDING with "unmatched" event published to NATS

### 3. Always-On Priority Scoring
- New `PolicyMultiplier()` function in broker/matcher
- `warren.AgentState` now includes `Policy` field (always-on/on-demand)
- Scoring: always-on+ready → ×1.0, on-demand+awake → ×0.9, on-demand+sleeping → ×0.6
- Applied as additional multiplier in `ScoreCandidate()`

### 4. NATS-Only Events (No Gateway Delivery)
- Dispatch publishes full task payloads to NATS subjects:
  - `swarm.task.{id}.assigned` — full task object including assignee
  - `swarm.task.{id}.timeout` — with retry count
  - `swarm.task.{id}.reassigned` — on agent stop
  - `swarm.task.{id}.unmatched` — no capable agents found
- Dispatch subscribes to incoming NATS events for bookkeeping:
  - `swarm.task.request` — creates tasks
  - `swarm.task.*.completed` — marks completed in Supabase
  - `swarm.task.*.failed` — marks failed
  - `swarm.task.*.progress` — transitions assigned→running, logs events
- All subscriptions set up via `broker.SetupSubscriptions()`

### 5. Capability Matching via PromptForge
- Forge client queries `GET /api/v1/prompts`, parses `capabilities` section
- 60-second TTL cache on persona list to avoid hammering PromptForge
- Comma-separated capability tags matched against task `scope`

### 6. Tests
- All existing tests updated for new `Broker.New()` signature (added alexandria param)
- `TestScoreCandidate` — updated with policy-aware scoring
- `TestPolicyMultiplier` — new test for always-on/on-demand multipliers
- `TestOwnerScopedFiltering` — verifies only owner's agents get assigned
- `TestOwnerScopedUnmatched` — verifies unmatched event when no owned agents match
- `TestCreateTask` — verifies owner and submitter fields
- `TestTaskFields` — store-level field test

## Verification
```
go build ./...  ✅
go vet ./...    ✅
go test ./...   ✅ (all packages pass)
```

## Config
New config fields:
- `alexandria.url` (default: `http://localhost:8500`)
- Env: `DISPATCH_ALEXANDRIA_URL`
