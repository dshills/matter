# Persistent Storage — Implementation Plan

## Phase 1: Storage Interface and In-Memory Backend

### Goals
Define the `storage.Store` interface and implement `MemoryStore` as the first backend. This phase produces a working, tested abstraction with no external dependencies and no changes to existing packages.

### Files to Create
- `internal/storage/store.go` — `Store` interface and all data types (`RunRow`, `StepRow`, `EventRow`, `RunFilter`, `MetricsDelta`, `MetricsRow`)
- `internal/storage/memory.go` — `MemoryStore` implementation using maps + `sync.RWMutex`
- `internal/storage/memory_test.go` — Full test suite for `MemoryStore`
- `internal/storage/compliance_test.go` — Shared compliance test suite that validates any `Store` implementation

### Key Decisions
- `Store` interface methods match the spec exactly (Section 4.1), plus `DeleteExpiredRuns(ctx context.Context, completedBefore, pausedBefore time.Time) (int, error)` included from the start to avoid breaking the interface in a later phase.
- `MemoryStore` uses `map[string]*RunRow` for runs, `map[string][]StepRow` for steps, `map[string][]EventRow` for events, and a single `MetricsRow` for metrics.
- Auto-incrementing `StepRow.StepID` and `EventRow.Seq` are implemented with atomic counters.
- `DeleteRun` cascades to steps and events.
- `ListRuns` supports `RunFilter` with status, limit, offset, time range, and ordering.
- All methods are safe for concurrent use via `sync.RWMutex`.
- Compliance test suite is parameterized: it takes a `func() Store` factory and runs all CRUD, filter, cascade, retention, and concurrency tests against any backend. The concurrency test spawns 20 goroutines each calling `CreateRun`/`AppendStep`/`IncrementMetrics` simultaneously and verifies final state consistency. The suite is run with `go test -race`.

### Acceptance Criteria
- `Store` interface compiles and is documented.
- `MemoryStore` passes compliance tests for all CRUD operations: create/get/update/list/delete runs, append/get steps, append/get events, increment/get metrics, delete expired runs.
- Pagination, filtering, and cascade delete are tested.
- `go build ./...`, `go test ./internal/storage/...`, `golangci-lint run ./...` all pass.

---

## Phase 2: SQLite Backend

### Goals
Implement `SQLiteStore` using `modernc.org/sqlite` (pure-Go, no CGO). This is the production default for the server.

### Files to Create
- `internal/storage/sqlite.go` — `SQLiteStore` implementation
- `internal/storage/sqlite_test.go` — SQLite-specific tests + shared compliance tests via the factory pattern from Phase 1

### Files to Modify
- `go.mod` / `go.sum` — add `modernc.org/sqlite` dependency

### Key Decisions
- Schema matches spec Section 5.1 exactly.
- Connection opened with `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`.
- Schema is auto-created on `NewSQLiteStore` via `CREATE TABLE IF NOT EXISTS`.
- Database path is passed as a constructor argument. Default path (`~/.matter/matter.db`) is resolved by the caller (config layer), not by the store.
- Parent directory is created automatically if it doesn't exist.
- `Close()` closes the `*sql.DB`.
- `DeleteExpiredRuns` uses `DELETE FROM runs WHERE status IN ('completed','failed','cancelled') AND completed_at < ?` for terminal runs, and `WHERE status = 'paused' AND updated_at < ?` for paused runs (paused runs lack a `completed_at`). Cascade to steps/events via foreign key `ON DELETE CASCADE`.

### Acceptance Criteria
- SQLite store passes the shared compliance test suite from Phase 1.
- Database file is created on first use.
- WAL mode is active (verified by test querying `PRAGMA journal_mode`).
- Cascade delete works (delete run → steps and events deleted).
- `ListRuns` returns correct results for all filter combinations (status, time range, pagination).
- Build remains single-binary (no CGO).

---

## Phase 3: Configuration

### Goals
Add `StorageConfig` to the config system so the backend and path are configurable via YAML, env vars, and defaults.

### Files to Modify
- `internal/config/config.go` — Add `StorageConfig` struct to `Config`, add defaults, add env var resolution
- `internal/config/config_test.go` — Test storage config loading, defaults, env overrides

### Key Decisions
- `StorageConfig` includes all fields upfront: `Backend string`, `Path string`, `Retention time.Duration`, `PausedRetention time.Duration`, `GCInterval time.Duration`.
- Defaults: `Backend: "sqlite"`, `Path: "~/.matter/matter.db"`, `Retention: 168h`, `PausedRetention: 24h`, `GCInterval: 1h`.
- The config struct default is `"sqlite"`. The CLI entrypoint (`cmd/matter/main.go`) overrides to `"memory"` when running single tasks without a server. The server entrypoint uses the config default.
- Env vars: `MATTER_STORAGE_BACKEND`, `MATTER_STORAGE_PATH`, `MATTER_STORAGE_RETENTION`, `MATTER_STORAGE_PAUSED_RETENTION`, `MATTER_STORAGE_GC_INTERVAL`.
- Config validation: backend must be `"sqlite"` or `"memory"`. Path is validated only when backend is `"sqlite"`.

### Files to Create
- `internal/storage/factory.go` — `NewStore(cfg StorageConfig) (Store, error)` factory

### Acceptance Criteria
- Default config loads with `backend: sqlite` and sensible path.
- Env vars override YAML config.
- `NewStore` returns `MemoryStore` or `SQLiteStore` based on config.
- Invalid backend name returns a clear error.
- CLI entrypoint defaults to `"memory"` backend for single-task mode.

---

## Phase 4: Server Integration

### Goals
Replace the in-memory `RunStore` in `internal/server/` with `storage.Store`. Runs, steps, and events are persisted through the store interface. Active run tracking (cancel functions, SSE subscribers) remains in-memory.

### Files to Modify
- `internal/server/server.go` — Accept `storage.Store` in `New()`, replace `RunStore` usage with store calls. Add startup reconciliation: on init, query `store.ListRuns(status=running)` and transition each to `failed` with `ErrorMessage: "server restarted"` via `store.UpdateRun()`. Initialize paused count from `store.ListRuns(status=paused)`.
- `internal/server/runs.go` — Refactor to `internal/server/active_runs.go`: a thin in-memory layer for ephemeral state only (cancel funcs, SSE subscribers, running/paused atomic counters). Remove persistent data fields (`Result`, `Events` slice). The `ActiveRun` struct retains: `RunID`, `Cancel context.CancelFunc`, subscribers, `Created time.Time`.
- `internal/server/handlers.go` (or equivalent handler files) — Update run creation, status queries, and deletion to use `storage.Store`
- `internal/server/sse.go` — Use `store.AppendEvent`/`store.GetEvents` for late-joiner replay instead of in-memory `Events` slice
- `internal/server/server_test.go` — Update tests to inject `MemoryStore`

### Files to Create
- `internal/server/integration_test.go` — Integration test: start server with SQLiteStore (temp file), create run, close server, start new server with same DB file, verify `GET /runs/{id}` returns the completed run.

### Key Decisions
- `Server` struct gets a `store storage.Store` field. The old `*RunStore` is replaced by a lightweight `*ActiveRunTracker` (renamed from `RunStore`) that holds only ephemeral state.
- On run creation: `store.CreateRun()` for persistence + `ActiveRunTracker.Add()` for cancel/subscribe.
- On run completion: `store.UpdateRun()` with result fields + remove from `ActiveRunTracker`.
- `GET /runs/{id}` reads from `store.GetRun()`. If not found in the store, returns 404.
- `GET /runs/{id}/events` uses `store.GetEvents(afterSeq)` for historical replay + live subscriber for real-time events.
- `DELETE /runs/{id}` cancels via `ActiveRunTracker` (if running) + `store.UpdateRun()` with cancelled status.
- Max concurrent/paused limits: running count from `ActiveRunTracker` (in-memory, O(1)). Paused count initialized from store on startup, then tracked in-memory.
- Startup reconciliation: all previously-`running` runs are marked `failed` with `"server restarted"` error. This prevents ghost runs from exhausting the concurrency limit.
- GC goroutine calls `store.DeleteExpiredRuns()` using configured retention values. Before deleting paused runs, GC removes them from the `ActiveRunTracker` if present.

### Acceptance Criteria
- Server starts with `MemoryStore` and all existing tests pass.
- Run lifecycle (create → status → complete) persists through the store.
- SSE late-joiner gets events from store, not just in-memory buffer.
- Cancellation works for both in-memory and persisted state.
- GC deletes expired runs from the store via `DeleteExpiredRuns`.
- `GET /runs/{id}` returns data for completed runs even after in-memory tracking is cleared.
- Startup reconciliation: runs in `running` state in the store are transitioned to `failed` on startup. Concurrency counter reflects only truly active runs.
- Integration test passes: create run → restart server → run is queryable.

---

## Phase 5: Observer and Recorder Integration

### Goals
Wire metrics and run recording through `storage.Store`. Metrics persist across restarts. Steps are written to the store as they happen (not buffered and flushed).

### Files to Modify
- `internal/observe/metrics.go` — Add optional `storage.Store` backing. When set, `Inc*` methods accumulate deltas in-memory. Deltas are flushed to `store.IncrementMetrics()` on run completion and on a 60-second background ticker. `Snapshot()` reads from `store.GetMetrics()` when store is available.
- `internal/observe/recorder.go` — When a store is available, `RecordStep()` calls `store.AppendStep()`. `Flush()` still writes JSON file if `RecordRuns` is enabled (backward compat). The store is the primary path.
- `internal/observe/observer.go` — Accept `storage.Store` in constructor or via setter. Pass it to `Metrics` and `Recorder`.

### Key Decisions
- Metrics flush strategy: in-memory counters accumulate deltas. Flushed to store on two triggers: (1) run completion/failure/cancellation, and (2) a 60-second background ticker owned by `Metrics`. Acceptable data-loss window on crash: up to 60 seconds of metric increments.
- The background ticker starts when a store is set and stops on `Close()`.
- Recorder writes steps to store incrementally (one `AppendStep` per step). JSON file flush is retained as optional secondary output.
- If store write fails, log the error at `warn` level and continue. Never abort a run due to storage failure.
- On startup, `Metrics` loads initial values from `store.GetMetrics()`.

### Acceptance Criteria
- Metrics survive a simulated restart: write metrics → create new `Metrics` with same store → verify counts are preserved.
- Steps appear in store immediately after each agent step, not just at run end.
- JSON file recording still works when enabled alongside store.
- Storage errors are logged but don't affect run execution.
- Metric flush happens on run completion (tested) and on ticker (tested with short interval).

---

## Phase 6: Agent State Serialization for Pause/Resume

### Goals
Enable pause/resume across server restarts by serializing agent state to the store.

### Files to Create
- `internal/agent/snapshot.go` — `AgentSnapshot` type with JSON tags, `Snapshot() AgentSnapshot` method on `Agent`, `RestoreFromSnapshot(snap AgentSnapshot)` method on `Agent`
- `internal/agent/snapshot_test.go` — Round-trip serialization tests, snapshot size warning test

### Files to Modify
- `internal/agent/agent.go` — Add `Messages() []matter.Message` accessor that delegates to `memory.Manager`. Add `RestoreMetrics(m RunMetrics)` setter.
- `internal/memory/manager.go` — Add `Messages() []matter.Message` and `RestoreMessages(msgs []matter.Message)` methods if not already present
- `internal/agent/loop_detector.go` — Add `LoopDetectorState` struct (exported, JSON-serializable: contains the map of tool call signatures and counts, and the consecutive-repeat counter). Add `State() LoopDetectorState` and `RestoreState(s LoopDetectorState)` methods. All fields must be JSON-serializable (no channels, no function values).
- `internal/runner/runner.go` — On pause, serialize agent snapshot via `agent.Snapshot()`, store as `RunRow.PausedState` via `store.UpdateRun()`. On resume, deserialize `AgentSnapshot` from `RunRow.PausedState`, create a new `Agent` from config, call `RestoreFromSnapshot()`, then call the existing `ResumeWithAnswer()` method (which already exists on `Agent` — signature: `ResumeWithAnswer(ctx context.Context, req matter.RunRequest, answer string, pausedDuration time.Duration) matter.RunResult`).
- `internal/server/handlers.go` — Resume handler loads `RunRow.PausedState` from store via `store.GetRun()`, deserializes snapshot, creates a new runner, and resumes. No longer requires the original in-memory runner to be alive.

### Key Decisions
- `AgentSnapshot` struct definition:
  ```go
  type AgentSnapshot struct {
      Messages   []matter.Message   `json:"messages"`
      RunMetrics RunMetrics         `json:"run_metrics"`
      LoopState  LoopDetectorState  `json:"loop_state"`
      Task       string             `json:"task"`
      Workspace  string             `json:"workspace"`
  }
  ```
- `LoopDetectorState` struct definition (derived from existing `LoopDetector` fields):
  ```go
  type LoopDetectorState struct {
      ToolCounts       map[string]int `json:"tool_counts"`
      ConsecutiveCount int            `json:"consecutive_count"`
      LastToolSig      string         `json:"last_tool_sig"`
  }
  ```
- `ResumeWithAnswer()` already exists on `Agent`. No new method needed — only `RestoreFromSnapshot()` is new.
- Non-serializable components (LLM client, tool registry, policy checker, observer) are reconstructed from config on resume — they are stateless singletons.
- Size limit: if serialized snapshot exceeds 10MB, log a warning at `warn` level but still store it.
- Security note: `PausedState` contains full conversation history which may include sensitive tool outputs. Filesystem-level encryption is recommended for production deployments. Document this in README.

### Acceptance Criteria
- Agent can be snapshotted and restored: run 3 steps → pause → snapshot → create new agent → restore → resume → verify step count continues from 3.
- Paused run survives simulated restart: pause → store snapshot in SQLite → create new runner → resume from store → run completes.
- Snapshot includes all conversation history so the LLM has full context on resume.
- Round-trip test: snapshot → JSON → restore → snapshot again → JSON matches.
- `matter.Message` round-trips through JSON without data loss (it is a plain struct with exported fields: `Role`, `Content`, `Timestamp`, `Step` — all JSON-serializable). Verified by comparing serialized output before and after restore.
- `LoopDetectorState` round-trips correctly (tested independently).
- Snapshot size warning is logged when exceeding 10MB (tested with artificially large messages).

---

## Phase 7: Data Retention and Cleanup

### Goals
Wire the retention config and GC goroutine to use `store.DeleteExpiredRuns`. The interface method and implementations already exist from Phase 1/2.

### Files to Modify
- `internal/server/server.go` — Update GC goroutine to call `store.DeleteExpiredRuns()` using `StorageConfig.Retention` and `StorageConfig.PausedRetention`. GC interval: 60s. Before deleting paused runs, GC calls `ActiveRunTracker.Remove()` to clean up in-memory state and decrement the paused counter. Log deleted count at `debug` level.
- `internal/server/server_test.go` — Test retention-based cleanup

### Key Decisions
- GC interval: 1h (default, matching spec Section 9). The retention *period* (7 days / 24h) controls how long data lives; the GC *interval* controls how often cleanup runs. The interval is configurable via `StorageConfig.GCInterval` (default: `1h`). The cleanup query is fast (indexed on `updated_at`) so shorter intervals are safe if needed.
- `DeleteExpiredRuns` transitions paused runs to `cancelled` before deleting. The GC goroutine first removes them from the in-memory `ActiveRunTracker` to prevent stale resume attempts.
- Metrics are never auto-deleted.
- A startup INFO log is emitted when the SQLite database is first created: `"persistent storage initialized at <path>"`.

### Acceptance Criteria
- Completed runs older than retention period are deleted by GC.
- Paused runs older than paused retention are removed from both store and `ActiveRunTracker`.
- Retention values are configurable via YAML and env vars.
- GC logs the number of runs cleaned up at debug level.
- No interface changes needed (method exists from Phase 1).

---

## Dependency Order

```
Phase 1 (interface + memory) 
  → Phase 2 (SQLite) 
  → Phase 3 (config)
  → Phase 4 (server integration) 
  → Phase 5 (observer integration)
  → Phase 6 (agent serialization)
  → Phase 7 (retention)
```

Phases 4 and 5 can be done in parallel after Phase 3. Phase 6 depends on Phase 4 (server needs store for pause state). Phase 7 depends on Phase 4.

## Risks

1. **`modernc.org/sqlite` binary size** — Pure-Go SQLite adds ~5-10MB to the binary. Acceptable for a production system; can be trimmed with build tags if needed.
2. **Agent snapshot size** — Long conversations with large tool outputs could produce large snapshots. The memory manager's summarization should keep this bounded, but a size warning at 10MB is prudent.
3. **SQLite write contention** — WAL mode handles concurrent reads well, but concurrent writes serialize. With the expected throughput (tens of runs, not thousands), this is not a concern.
4. **Backward compatibility** — JSON file recording is retained alongside store recording. The `matter replay` command should work with both sources.
5. **Sensitive data at rest** — `paused_state`, `tool_output`, and `raw_response` columns may contain sensitive data. Document that filesystem-level encryption is recommended for production.
