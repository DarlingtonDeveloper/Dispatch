# Dispatch — Task Broker & Work Scheduler

Dispatch is the task broker and work scheduler for the OpenClaw agent swarm. It receives work requests, matches them to the best available worker based on capability and availability, assigns tasks, tracks them through completion or failure, and handles retries and escalation.

## Architecture

```
┌──────────┐     ┌──────────┐     ┌───────────┐
│  Agents  │────▶│ Dispatch │────▶│  Workers  │
│(requesters)    │  :8600   │     │ (agents)  │
└──────────┘     └────┬─────┘     └───────────┘
                      │
         ┌────────────┼────────────┐
         │            │            │
    ┌────▼───┐  ┌─────▼────┐  ┌───▼────┐
    │Supabase│  │  Hermes  │  │ Warren │
    │  (DB)  │  │  (NATS)  │  │(agents)│
    └────────┘  └──────────┘  └────────┘
```

- **Language:** Go
- **Ports:** 8600 (API), 8601 (metrics/health)
- **Storage:** Supabase (PostgreSQL)
- **Events:** NATS via Hermes
- **Dependencies:** Warren (availability/wake), PromptForge (capabilities)

## API Reference

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (filter: `status`, `requester`, `assignee`, `scope`) |
| `GET` | `/api/v1/tasks/:id` | Get task detail |
| `PATCH` | `/api/v1/tasks/:id` | Update task (cancel, add context) |
| `POST` | `/api/v1/tasks/:id/complete` | Worker reports completion |
| `POST` | `/api/v1/tasks/:id/fail` | Worker reports failure |
| `POST` | `/api/v1/tasks/:id/progress` | Worker reports progress |

### Admin (requires `Authorization: Bearer <token>`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/stats` | Queue depth, avg completion time |
| `GET` | `/api/v1/agents` | Capability map (PromptForge + Warren) |
| `POST` | `/api/v1/agents/:id/drain` | Stop assigning to agent |

### Infrastructure

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (port 8601) |
| `GET` | `/metrics` | Prometheus metrics (port 8601) |

### Auth

All `/api/v1/*` requests require `X-Agent-ID` header. Admin endpoints additionally require `Authorization: Bearer <token>`.

## Task Lifecycle

```
REQUEST → PENDING → ASSIGNED → RUNNING → COMPLETED
                                       → FAILED → (retry?) → PENDING
                                       → TIMEOUT → (retry?) → PENDING
                       → CANCELLED
```

## Assignment Algorithm

1. Query PromptForge for agents with matching capability tags
2. **Owner filtering** (if `owner_filter_enabled: true`): query Alexandria for devices owned by the task's owner and restrict candidates to those agents. When disabled, any capable agent can receive work regardless of ownership.
3. Query Warren for each candidate's availability
4. Score: `capability_match × availability_multiplier × priority_weight`
   - Ready: ×1.0 | Sleeping: ×0.8 | Busy (under limit): ×0.5 | Degraded: ×0
5. Assign to highest-scoring candidate (wake if sleeping)
6. Start timeout timer

## Configuration

```yaml
server:
  port: 8600
  metrics_port: 8601
  admin_token: "dispatch-admin-xxxxx"

database:
  url: "postgresql://..."

hermes:
  url: "nats://localhost:4222"

warren:
  url: "http://localhost:9090"
  token: "warren-admin-token"

promptforge:
  url: "http://localhost:8083"

assignment:
  tick_interval_ms: 5000
  wake_timeout_ms: 30000
  default_timeout_ms: 300000
  max_concurrent_per_agent: 3
  owner_filter_enabled: true    # set false to allow cross-owner task assignment

logging:
  level: "info"
  format: "json"
```

### Environment Variable Overrides

| Variable | Config Path |
|----------|-------------|
| `DISPATCH_PORT` | `server.port` |
| `DISPATCH_METRICS_PORT` | `server.metrics_port` |
| `DISPATCH_ADMIN_TOKEN` | `server.admin_token` |
| `DISPATCH_DATABASE_URL` | `database.url` |
| `DISPATCH_HERMES_URL` | `hermes.url` |
| `DISPATCH_WARREN_URL` | `warren.url` |
| `DISPATCH_WARREN_TOKEN` | `warren.token` |
| `DISPATCH_FORGE_URL` | `promptforge.url` |
| `DISPATCH_TICK_INTERVAL_MS` | `assignment.tick_interval_ms` |
| `DISPATCH_OWNER_FILTER_ENABLED` | `assignment.owner_filter_enabled` |
| `DISPATCH_LOG_LEVEL` | `logging.level` |

## Deployment

```bash
# Build
docker build -t dispatch:latest .

# Run
docker run -p 8600:8600 -p 8601:8601 \
  -e DISPATCH_DATABASE_URL="postgresql://..." \
  -e DISPATCH_ADMIN_TOKEN="secret" \
  dispatch:latest
```

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

## License

MIT
