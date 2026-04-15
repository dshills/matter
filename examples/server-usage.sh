#!/usr/bin/env bash
# matter-server HTTP API usage examples
# These examples demonstrate the REST API using curl.
# Start the server first, then run these in another terminal.

set -euo pipefail

BASE_URL="${MATTER_SERVER_URL:-http://localhost:8080}"
TOKEN="${MATTER_SERVER_AUTH_TOKEN:-}"

# Build auth header args as an array to avoid word-splitting issues.
AUTH_ARGS=()
if [ -n "$TOKEN" ]; then
  AUTH_ARGS=(-H "Authorization: Bearer $TOKEN")
fi

# ============================================================================
# Starting the server
# ============================================================================

# Start with defaults (listens on :8080, SQLite storage)
# matter-server

# Start with custom config
# matter-server --config examples/configs/server-production.yaml

# Start with custom port
# matter-server --listen :9090

# Start with auth token (recommended)
# export MATTER_SERVER_AUTH_TOKEN=my-secret-token
# matter-server

# ============================================================================
# Health check (no auth required)
# ============================================================================

echo "=== Health Check ==="
curl -s "$BASE_URL/api/v1/health" | jq .
# Response: {"status":"ok","version":"0.2.0"}

# ============================================================================
# List available tools
# ============================================================================

echo -e "\n=== List Tools ==="
curl -sf "${AUTH_ARGS[@]}" "$BASE_URL/api/v1/tools" | jq .
# Response: {"tools":[{"name":"workspace_read","description":"...","safe":true,"side_effect":false}, ...]}

# ============================================================================
# Create a run
# ============================================================================

echo -e "\n=== Create Run ==="
RUN_RESPONSE=$(curl -sf -X POST "${AUTH_ARGS[@]}" \
  -H "Content-Type: application/json" \
  -d '{"task": "List all Go files and count them", "workspace": "."}' \
  "$BASE_URL/api/v1/runs")

echo "$RUN_RESPONSE" | jq .
# Response: {"run_id":"abc123...","status":"running"}

RUN_ID=$(echo "$RUN_RESPONSE" | jq -r '.run_id')
echo "Run ID: $RUN_ID"

# ============================================================================
# Check run status
# ============================================================================

echo -e "\n=== Check Status ==="
# Poll until the run completes
for i in $(seq 1 30); do
  STATUS=$(curl -sf "${AUTH_ARGS[@]}" "$BASE_URL/api/v1/runs/$RUN_ID")
  CURRENT=$(echo "$STATUS" | jq -r '.status')
  echo "[$i] Status: $CURRENT"

  if [ "$CURRENT" = "completed" ] || [ "$CURRENT" = "failed" ]; then
    echo "$STATUS" | jq .
    break
  fi
  sleep 1
done

# Response when completed:
# {
#   "run_id": "abc123...",
#   "status": "completed",
#   "success": true,
#   "final_summary": "Found 42 Go files in the workspace.",
#   "steps": 3,
#   "total_tokens": 1500,
#   "total_cost_usd": 0.0045,
#   "duration": "4.2s"
# }

# ============================================================================
# Stream events via SSE
# ============================================================================

echo -e "\n=== SSE Event Streaming ==="
# Start a new run and stream its events in real time.
# Use -N to disable curl buffering for SSE.

RUN_RESPONSE=$(curl -sf -X POST "${AUTH_ARGS[@]}" \
  -H "Content-Type: application/json" \
  -d '{"task": "Read the README file", "workspace": "."}' \
  "$BASE_URL/api/v1/runs")
RUN_ID=$(echo "$RUN_RESPONSE" | jq -r '.run_id')

# Stream events (this will block until the run completes):
echo "Streaming events for run $RUN_ID..."
curl -N -s "${AUTH_ARGS[@]}" "$BASE_URL/api/v1/runs/$RUN_ID/events"

# SSE output looks like:
# event: run_started
# data: {"run_id":"abc123","step":0,"event":"run_started","timestamp":"..."}
#
# event: planner_started
# data: {"run_id":"abc123","step":1,"event":"planner_started","timestamp":"..."}
#
# event: tool_started
# data: {"run_id":"abc123","step":1,"event":"tool_started","data":{"tool":"workspace_read"},"timestamp":"..."}
#
# event: run_completed
# data: {"run_id":"abc123","step":2,"event":"run_completed","timestamp":"..."}

# ============================================================================
# Cancel a run
# ============================================================================

echo -e "\n=== Cancel Run ==="
# Start a long-running task
RUN_RESPONSE=$(curl -sf -X POST "${AUTH_ARGS[@]}" \
  -H "Content-Type: application/json" \
  -d '{"task": "Analyze every file in detail", "workspace": "."}' \
  "$BASE_URL/api/v1/runs")
RUN_ID=$(echo "$RUN_RESPONSE" | jq -r '.run_id')

sleep 2  # let it start

# Cancel it
curl -sf -X DELETE "${AUTH_ARGS[@]}" "$BASE_URL/api/v1/runs/$RUN_ID" | jq .
# Response: {"run_id":"abc123","status":"cancelled"}

# ============================================================================
# Conversation mode (pause/resume)
# ============================================================================

echo -e "\n=== Conversation Mode ==="
# When the agent asks a question, the run pauses.

# 1. Check status -- if paused, it includes the question:
# {
#   "run_id": "abc123",
#   "status": "paused",
#   "question": "Which database should I configure?",
#   "steps": 3
# }

# 2. Resume with an answer:
# curl -sf -X POST "${AUTH_ARGS[@]}" \
#   -H "Content-Type: application/json" \
#   -d '{"answer": "PostgreSQL"}' \
#   "$BASE_URL/api/v1/runs/$RUN_ID/answer" | jq .
# Response: {"run_id":"abc123","status":"running"}

# The run continues from where it left off.
# This works even after a server restart (agent state is persisted in SQLite).

echo "Done."
