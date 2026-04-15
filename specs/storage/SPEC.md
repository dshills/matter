# Persistent Storage Specification

## 1. Overview

Matter currently stores all run state, metrics, and session data in memory. A server restart loses every completed run, every paused conversation, and every accumulated metric. This specification defines a pluggable storage layer that persists run history, enables pause/resume across restarts, and provides durable metrics — while preserving the current zero-dependency single-binary deployment option.

## 2. Goals

1. **Durable run history** — completed, failed, and cancelled runs survive server restarts and are queryable by ID, status, time range, and recency.
2. **Persistent pause/resume** — paused agent runs are recoverable after a server restart. A user can pause a run, restart the server, and resume.
3. **Durable metrics** — aggregate counters (runs started, tokens used, cost) persist across restarts.
4. **Pluggable backends** — a `Store` interface with two concrete implementations: SQLite (default, single-binary) and in-memory (testing, backward compat).
5. **Minimal disruption** — existing packages (`server`, `observe`, `runner`) depend on the new interface, not on a concrete backend. No changes to the agent loop, planner, or tool execution.

## 3. Non-Goals

- Multi-node / distributed storage (Postgres, Redis). Out of scope for v1; the interface should not preclude it.
- Full-text search over run content or step outputs.
- Automatic data migration tooling between backends.
- Storing LLM conversation history beyond what RunRecord already captures.
- Encryption at rest (filesystem-level encryption is the user's responsibility).

## 4. Storage Interface

### 4.1 `Store` Interface

```go
package storage

type Store interface {
    // Run lifecycle
    CreateRun(ctx context.Context, run *RunRow) error
    GetRun(ctx context.Context, runID string) (*RunRow, error)
    UpdateRun(ctx context.Context, run *RunRow) error
    ListRuns(ctx context.Context, filter RunFilter) ([]RunRow, error)
    DeleteRun(ctx context.Context, runID string) error

    // Run steps (append-only)
    AppendStep(ctx context.Context, runID string, step *StepRow) error
    GetSteps(ctx context.Context, runID string) ([]StepRow, error)

    // Run events (progress events for SSE replay)
    AppendEvent(ctx context.Context, runID string, event *EventRow) error
    GetEvents(ctx context.Context, runID string, afterSeq int) ([]EventRow, error)

    // Metrics (global aggregate counters)
    IncrementMetrics(ctx context.Context, delta MetricsDelta) error
    GetMetrics(ctx context.Context) (*MetricsRow, error)

    // Lifecycle
    Close() error
}
```

### 4.2 Data Types

```go
type RunRow struct {
    RunID        string
    Status       string    // running, completed, failed, cancelled, paused
    Task         string
    Workspace    string
    Model        string
    Provider     string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    CompletedAt  *time.Time

    // Result fields (populated on completion)
    Success      *bool
    Summary      string
    ErrorMessage string

    // Aggregate metrics for this run
    Steps        int
    TotalTokens  int
    TotalCostUSD float64
    DurationMS   int64

    // Pause/resume
    Question     string // non-empty when Status == "paused"
    PausedState  []byte // serialized agent state for resume (JSON)
}

type StepRow struct {
    StepID       int64  // auto-increment
    RunID        string
    StepNumber   int
    Timestamp    time.Time
    Decision     string // "tool_call", "complete", "fail", "ask"
    ToolName     string
    ToolInput    string // JSON
    ToolOutput   string
    ToolError    string
    RawResponse  string
    Tokens       int
    CostUSD      float64
}

type EventRow struct {
    Seq       int64  // auto-increment, used for SSE replay cursor
    RunID     string
    Timestamp time.Time
    Type      string // event type from matter.ProgressEvent
    Data      string // JSON-encoded event payload
}

type RunFilter struct {
    Status  string     // optional: filter by status
    Limit   int        // max results (default 50, max 200)
    Offset  int        // pagination offset
    OrderBy string     // "created_at" (default) or "updated_at"
    Before  *time.Time // optional: runs created before this time
    After   *time.Time // optional: runs created after this time
}

type MetricsDelta struct {
    RunsStarted   int
    RunsCompleted int
    RunsFailed    int
    ToolCalls     int
    ToolFailures  int
    LLMCalls      int
    LLMFailures   int
    StepCount     int
    TotalTokens   int
    TotalCostUSD  float64
    DurationMS    int64
}

type MetricsRow struct {
    RunsStarted   int
    RunsCompleted int
    RunsFailed    int
    ToolCalls     int
    ToolFailures  int
    LLMCalls      int
    LLMFailures   int
    StepCount     int
    TotalTokens   int
    TotalCostUSD  float64
    DurationMS    int64
    UpdatedAt     time.Time
}
```

### 4.3 Constraints

- `RunID` is the primary key for runs. It is a UUID string generated by the caller (existing behavior).
- `StepRow` entries are append-only. Steps are never updated or deleted independently.
- `EventRow` entries are append-only. The `Seq` field is a monotonically increasing integer per run, used as a cursor for SSE late-joiners.
- `DeleteRun` cascades to steps and events for that run.
- `ListRuns` returns rows without steps or events (summary only). Use `GetSteps`/`GetEvents` for detail.
- All methods must be safe for concurrent use.

## 5. SQLite Backend

### 5.1 Schema

Three tables: `runs`, `steps`, `events`, plus a singleton `metrics` table.

```sql
CREATE TABLE IF NOT EXISTS runs (
    run_id       TEXT PRIMARY KEY,
    status       TEXT NOT NULL DEFAULT 'running',
    task         TEXT NOT NULL,
    workspace    TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL DEFAULT '',
    provider     TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    success      INTEGER, -- NULL = in progress, 0 = false, 1 = true
    summary      TEXT NOT NULL DEFAULT '',
    error_msg    TEXT NOT NULL DEFAULT '',
    steps        INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    total_cost   REAL NOT NULL DEFAULT 0.0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    question     TEXT NOT NULL DEFAULT '',
    paused_state BLOB
);

CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_runs_created ON runs(created_at);

CREATE TABLE IF NOT EXISTS steps (
    step_id      INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    step_number  INTEGER NOT NULL,
    timestamp    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    decision     TEXT NOT NULL DEFAULT '',
    tool_name    TEXT NOT NULL DEFAULT '',
    tool_input   TEXT NOT NULL DEFAULT '',
    tool_output  TEXT NOT NULL DEFAULT '',
    tool_error   TEXT NOT NULL DEFAULT '',
    raw_response TEXT NOT NULL DEFAULT '',
    tokens       INTEGER NOT NULL DEFAULT 0,
    cost_usd     REAL NOT NULL DEFAULT 0.0
);

CREATE INDEX idx_steps_run ON steps(run_id, step_number);

CREATE TABLE IF NOT EXISTS events (
    seq       INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id    TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    type      TEXT NOT NULL DEFAULT '',
    data      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_events_run ON events(run_id, seq);

CREATE TABLE IF NOT EXISTS metrics (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    runs_started    INTEGER NOT NULL DEFAULT 0,
    runs_completed  INTEGER NOT NULL DEFAULT 0,
    runs_failed     INTEGER NOT NULL DEFAULT 0,
    tool_calls      INTEGER NOT NULL DEFAULT 0,
    tool_failures   INTEGER NOT NULL DEFAULT 0,
    llm_calls       INTEGER NOT NULL DEFAULT 0,
    llm_failures    INTEGER NOT NULL DEFAULT 0,
    step_count      INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    total_cost      REAL NOT NULL DEFAULT 0.0,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed the singleton row.
INSERT OR IGNORE INTO metrics (id) VALUES (1);
```

### 5.2 Connection Management

- Use `database/sql` with `modernc.org/sqlite` (pure-Go, no CGO) to preserve single-binary deployment.
- Single connection with `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout=5000` for safe concurrent reads during writes.
- `PRAGMA foreign_keys=ON` for cascade deletes.
- Database file path configured via `storage.path` in config (default: `~/.matter/matter.db`).

### 5.3 Performance Considerations

- Steps and events are inserted one at a time during run execution. Batch inserts are not required — agent runs produce ~10-100 steps, not millions.
- `ListRuns` uses indexed queries on `status` and `created_at`. Pagination via `LIMIT/OFFSET`.
- `tool_output` and `raw_response` can be large (truncated LLM responses). SQLite handles TEXT columns up to 1GB; no special handling needed.
- `paused_state` is stored as a BLOB. Size depends on conversation history length; typically 10-500KB.

## 6. In-Memory Backend

A `MemoryStore` implementation using Go maps protected by `sync.RWMutex`. This replaces the current ad-hoc `RunStore` in `internal/server/runs.go` and provides the same interface for testing and single-run CLI use.

Behavior matches SQLite semantics: auto-incrementing sequences for steps/events, cascade delete, filter/pagination support.

## 7. Integration Points

### 7.1 Configuration

Add to `config.Config`:

```go
type StorageConfig struct {
    Backend string // "sqlite" (default) or "memory"
    Path    string // SQLite file path (default: ~/.matter/matter.db)
}
```

Config file example:

```yaml
storage:
  backend: sqlite
  path: /var/lib/matter/matter.db
```

Environment variable overrides: `MATTER_STORAGE_BACKEND`, `MATTER_STORAGE_PATH`, `MATTER_STORAGE_RETENTION`, `MATTER_STORAGE_PAUSED_RETENTION`, `MATTER_STORAGE_GC_INTERVAL`.

### 7.2 Server Integration

- `internal/server/` replaces direct `RunStore` map usage with `storage.Store` calls.
- `RunStore` struct is replaced by the store interface. The server holds a `storage.Store` and calls `CreateRun`, `UpdateRun`, `GetRun`, `ListRuns`.
- SSE event streaming uses `AppendEvent`/`GetEvents` for late-joiner replay instead of the in-memory `Events` slice.
- Active run cancellation (`context.CancelFunc`) and subscriber management remain in-memory (these are ephemeral by nature and not persisted).
- Max concurrent/paused run limits are enforced by querying `ListRuns` with status filter, or maintained as in-memory atomic counters (existing pattern) with store as source of truth on startup.

### 7.3 Observer Integration

- `internal/observe/metrics.go` delegates counter increments to `storage.Store.IncrementMetrics`.
- `Metrics.Snapshot()` reads from `storage.Store.GetMetrics`.
- On startup, metrics are loaded from the store, not reset to zero.

### 7.4 Recorder Integration

- `internal/observe/recorder.go` writes steps via `storage.Store.AppendStep` instead of buffering in memory and flushing to a JSON file.
- The existing JSON file recording (`RecordRuns`/`RecordDir`) is retained as an optional secondary output for backward compatibility. It is not the primary storage path.

### 7.5 Runner Integration

- Paused agent state is serialized to JSON and stored in `RunRow.PausedState`.
- On resume, the runner deserializes the agent state from the store instead of holding it in memory.
- This requires `internal/agent.Agent` to support JSON marshaling/unmarshaling of its resumable state (message history, run metrics, loop detector state).

### 7.6 CLI Integration

- `matter run` (single task, no server) uses the in-memory backend by default. No database needed for one-shot CLI use.
- `matter-server` uses SQLite by default.
- `matter replay` can read from the store if `--run-id` is provided, falling back to JSON files for backward compat.

## 8. Agent State Serialization

For pause/resume across restarts, the agent must serialize its resumable state. The serialized state includes:

```go
type AgentSnapshot struct {
    Messages     []matter.Message  `json:"messages"`      // conversation history
    RunMetrics   RunMetrics        `json:"run_metrics"`    // steps, tokens, cost
    LoopState    LoopDetectorState `json:"loop_state"`     // repeated call tracking
    Task         string            `json:"task"`
    Workspace    string            `json:"workspace"`
}
```

Fields NOT serialized (reconstructed on resume):
- LLM client (recreated from config)
- Tool registry and executor (recreated from config)
- Policy checker (recreated from config)
- Observer/session (new session created for resumed run)
- Context and cancel functions (ephemeral)

## 9. Data Retention

- `storage.retention` config controls how long completed/failed/cancelled runs are kept (default: 168h / 7 days).
- A background goroutine in the server calls `DeleteRun` for expired runs on a configurable interval (default: every hour).
- Paused runs have a separate `storage.paused_retention` (default: 24h). Paused runs older than this are cancelled and deleted.
- Metrics are never auto-deleted; they accumulate indefinitely.

## 10. Error Handling

- Storage errors during run execution (e.g., disk full) are logged but do not abort the agent run. The run continues with best-effort persistence.
- Storage errors during API reads (e.g., GetRun) return HTTP 500 with a descriptive error message.
- On startup, if SQLite cannot open the database file, the server fails to start with a clear error message.

## 11. Testing Strategy

- `MemoryStore` is the primary test backend. All server and observer tests use it.
- SQLite backend gets its own test suite using temporary databases (`t.TempDir()`).
- Interface compliance tests: a shared test suite that runs against both backends to verify identical behavior.
- Integration test: start server with SQLite, create run, restart server, verify run is queryable.
