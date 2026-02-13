#!/usr/bin/env bash
set -euo pipefail

API_PORT="${DISPATCH_PORT:-8600}"
METRICS_PORT="${DISPATCH_METRICS_PORT:-8601}"
BASE="http://localhost:${API_PORT}/api/v1"
ADMIN_TOKEN="${DISPATCH_ADMIN_TOKEN:-e2e-token}"
FAIL=0

pass() { echo "  PASS: $1"; }
fail() { echo "  FAIL: $1 — $2"; FAIL=1; }

echo "=== Dispatch E2E Smoke Tests ==="

# 1. Health check (metrics port)
echo "--- Health ---"
HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' "http://localhost:${METRICS_PORT}/health")
if [ "$HTTP" = "200" ]; then
  pass "GET /health (metrics) → 200"
else
  fail "GET /health (metrics)" "expected 200, got $HTTP"
fi

# 2. Create task
echo "--- Task CRUD ---"
HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' \
  -X POST "$BASE/tasks" \
  -H "Content-Type: application/json" \
  -H "X-Agent-ID: e2e-test" \
  -d '{"title":"E2E smoke test task","description":"Created by E2E tests","priority":1}')
if [ "$HTTP" = "201" ]; then
  pass "POST /api/v1/tasks → 201"
else
  fail "POST /api/v1/tasks" "expected 201, got $HTTP (body: $(cat /tmp/e2e_body))"
fi

# Extract task ID
TASK_ID=$(python3 -c "import json,sys; print(json.load(sys.stdin)['task_id'])" < /tmp/e2e_body 2>/dev/null || echo "")
if [ -z "$TASK_ID" ]; then
  TASK_ID=$(jq -r '.task_id' /tmp/e2e_body 2>/dev/null || echo "")
fi

# 3. Get task by ID
if [ -n "$TASK_ID" ]; then
  HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' \
    -H "X-Agent-ID: e2e-test" \
    "$BASE/tasks/$TASK_ID")
  if [ "$HTTP" = "200" ]; then
    pass "GET /api/v1/tasks/$TASK_ID → 200"
  else
    fail "GET /api/v1/tasks/$TASK_ID" "expected 200, got $HTTP"
  fi
else
  fail "Get task" "skipped — no task ID extracted"
fi

# 4. List tasks
HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' \
  -H "X-Agent-ID: e2e-test" \
  "$BASE/tasks")
if [ "$HTTP" = "200" ]; then
  pass "GET /api/v1/tasks → 200"
else
  fail "GET /api/v1/tasks" "expected 200, got $HTTP"
fi

# 5. Complete task
if [ -n "$TASK_ID" ]; then
  HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' \
    -X POST "$BASE/tasks/$TASK_ID/complete" \
    -H "Content-Type: application/json" \
    -H "X-Agent-ID: e2e-test" \
    -d '{"result":{"status":"e2e-done"}}')
  if [ "$HTTP" = "200" ]; then
    pass "POST /api/v1/tasks/$TASK_ID/complete → 200"
  else
    fail "POST /api/v1/tasks/$TASK_ID/complete" "expected 200, got $HTTP (body: $(cat /tmp/e2e_body))"
  fi
fi

# 6. Stats (admin)
echo "--- Admin ---"
HTTP=$(curl -s -o /tmp/e2e_body -w '%{http_code}' \
  -H "X-Agent-ID: e2e-test" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE/stats")
if [ "$HTTP" = "200" ]; then
  pass "GET /api/v1/stats (admin) → 200"
else
  fail "GET /api/v1/stats" "expected 200, got $HTTP"
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "All Dispatch E2E tests passed."
else
  echo "Some Dispatch E2E tests FAILED."
  exit 1
fi
