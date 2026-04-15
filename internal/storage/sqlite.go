package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver (no CGO).
)

const sqliteSchema = `
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
    success      INTEGER,
    summary      TEXT NOT NULL DEFAULT '',
    error_msg    TEXT NOT NULL DEFAULT '',
    steps        INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    total_cost   REAL NOT NULL DEFAULT 0.0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    question     TEXT NOT NULL DEFAULT '',
    paused_state BLOB
);

CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_created ON runs(created_at);

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

CREATE INDEX IF NOT EXISTS idx_steps_run ON steps(run_id, step_number);

CREATE TABLE IF NOT EXISTS events (
    seq       INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id    TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    type      TEXT NOT NULL DEFAULT '',
    data      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_events_run ON events(run_id, seq);

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
    tool_duration_ms INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO metrics (id) VALUES (1);
`

// SQLiteStore implements Store using a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path
// and initializes the schema. The parent directory is created if needed.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite only supports one writer at a time. Limiting to a single
	// connection serializes all database access and avoids SQLITE_BUSY errors
	// from concurrent writes through database/sql's connection pool.
	db.SetMaxOpenConns(1)

	// Configure SQLite for safe concurrent use.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	// Create tables and indexes.
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for testing.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run *RunRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (run_id, status, task, workspace, model, provider,
			created_at, updated_at, completed_at, success, summary, error_msg,
			steps, total_tokens, total_cost, duration_ms, question, paused_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.Status, run.Task, run.Workspace, run.Model, run.Provider,
		run.CreatedAt.UTC(), run.UpdatedAt.UTC(), nullableTime(run.CompletedAt),
		nullableBool(run.Success), run.Summary, run.ErrorMessage,
		run.Steps, run.TotalTokens, run.TotalCostUSD, run.DurationMS,
		run.Question, run.PausedState,
	)
	if err != nil {
		if isConstraintError(err) {
			return &ErrConflict{Resource: "run", ID: run.RunID}
		}
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (*RunRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, status, task, workspace, model, provider,
			created_at, updated_at, completed_at, success, summary, error_msg,
			steps, total_tokens, total_cost, duration_ms, question, paused_state
		FROM runs WHERE run_id = ?`, runID)

	run, err := scanRunRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &ErrNotFound{Resource: "run", ID: runID}
		}
		return nil, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, run *RunRow) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status=?, task=?, workspace=?, model=?, provider=?,
			updated_at=?, completed_at=?, success=?, summary=?, error_msg=?,
			steps=?, total_tokens=?, total_cost=?, duration_ms=?,
			question=?, paused_state=?
		WHERE run_id=?`,
		run.Status, run.Task, run.Workspace, run.Model, run.Provider,
		run.UpdatedAt.UTC(), nullableTime(run.CompletedAt),
		nullableBool(run.Success), run.Summary, run.ErrorMessage,
		run.Steps, run.TotalTokens, run.TotalCostUSD, run.DurationMS,
		run.Question, run.PausedState,
		run.RunID,
	)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return &ErrNotFound{Resource: "run", ID: run.RunID}
	}
	return nil
}

// orderByWhitelist maps allowed OrderBy values to SQL column names.
var orderByWhitelist = map[string]string{
	"created_at": "created_at",
	"updated_at": "updated_at",
}

func (s *SQLiteStore) ListRuns(ctx context.Context, filter RunFilter) ([]RunRow, error) {
	filter = normalizeFilter(filter)

	query := "SELECT run_id, status, task, workspace, model, provider, " +
		"created_at, updated_at, completed_at, success, summary, error_msg, " +
		"steps, total_tokens, total_cost, duration_ms, question, paused_state " +
		"FROM runs WHERE 1=1"
	var args []any

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Before != nil {
		query += " AND created_at < ?"
		args = append(args, filter.Before.UTC())
	}
	if filter.After != nil {
		query += " AND created_at > ?"
		args = append(args, filter.After.UTC())
	}

	// Use whitelisted column name to prevent SQL injection.
	orderCol := orderByWhitelist[filter.OrderBy]
	if orderCol == "" {
		orderCol = "created_at"
	}
	query += " ORDER BY " + orderCol
	query += " LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []RunRow
	for rows.Next() {
		run, err := scanRunRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		result = append(result, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}

	if result == nil {
		return []RunRow{}, nil
	}
	return result, nil
}

func (s *SQLiteStore) DeleteRun(ctx context.Context, runID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM runs WHERE run_id = ?", runID)
	if err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return &ErrNotFound{Resource: "run", ID: runID}
	}
	// Steps and events are cascade-deleted by foreign key.
	return nil
}

func (s *SQLiteStore) AppendStep(ctx context.Context, runID string, step *StepRow) error {
	ts := step.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO steps (run_id, step_number, timestamp, decision, tool_name,
			tool_input, tool_output, tool_error, raw_response, tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, step.StepNumber, ts.UTC(), step.Decision, step.ToolName,
		step.ToolInput, step.ToolOutput, step.ToolError, step.RawResponse,
		step.Tokens, step.CostUSD,
	)
	if err != nil {
		if isForeignKeyError(err) {
			return &ErrNotFound{Resource: "run", ID: runID}
		}
		return fmt.Errorf("insert step: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSteps(ctx context.Context, runID string) ([]StepRow, error) {
	// Verify run exists (needed for empty-result vs not-found distinction).
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM runs WHERE run_id = ?", runID).Scan(&exists)
	if err != nil {
		return nil, &ErrNotFound{Resource: "run", ID: runID}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT step_id, run_id, step_number, timestamp, decision, tool_name,
			tool_input, tool_output, tool_error, raw_response, tokens, cost_usd
		FROM steps WHERE run_id = ? ORDER BY step_number`, runID)
	if err != nil {
		return nil, fmt.Errorf("get steps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []StepRow
	for rows.Next() {
		var step StepRow
		if err := rows.Scan(&step.StepID, &step.RunID, &step.StepNumber,
			&step.Timestamp, &step.Decision, &step.ToolName,
			&step.ToolInput, &step.ToolOutput, &step.ToolError,
			&step.RawResponse, &step.Tokens, &step.CostUSD); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		result = append(result, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate steps: %w", err)
	}

	if result == nil {
		return []StepRow{}, nil
	}
	return result, nil
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, runID string, event *EventRow) error {
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (run_id, timestamp, type, data)
		VALUES (?, ?, ?, ?)`,
		runID, ts.UTC(), event.Type, event.Data,
	)
	if err != nil {
		if isForeignKeyError(err) {
			return &ErrNotFound{Resource: "run", ID: runID}
		}
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetEvents(ctx context.Context, runID string, afterSeq int64) ([]EventRow, error) {
	// Verify run exists (needed for empty-result vs not-found distinction).
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM runs WHERE run_id = ?", runID).Scan(&exists)
	if err != nil {
		return nil, &ErrNotFound{Resource: "run", ID: runID}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, run_id, timestamp, type, data
		FROM events WHERE run_id = ? AND seq > ? ORDER BY seq`, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []EventRow
	for rows.Next() {
		var ev EventRow
		if err := rows.Scan(&ev.Seq, &ev.RunID, &ev.Timestamp, &ev.Type, &ev.Data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		result = append(result, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	if result == nil {
		return []EventRow{}, nil
	}
	return result, nil
}

func (s *SQLiteStore) IncrementMetrics(ctx context.Context, delta MetricsDelta) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE metrics SET
			runs_started = runs_started + ?,
			runs_completed = runs_completed + ?,
			runs_failed = runs_failed + ?,
			tool_calls = tool_calls + ?,
			tool_failures = tool_failures + ?,
			llm_calls = llm_calls + ?,
			llm_failures = llm_failures + ?,
			step_count = step_count + ?,
			total_tokens = total_tokens + ?,
			total_cost = total_cost + ?,
			duration_ms = duration_ms + ?,
			tool_duration_ms = tool_duration_ms + ?,
			updated_at = ?
		WHERE id = 1`,
		delta.RunsStarted, delta.RunsCompleted, delta.RunsFailed,
		delta.ToolCalls, delta.ToolFailures,
		delta.LLMCalls, delta.LLMFailures,
		delta.StepCount, delta.TotalTokens, delta.TotalCostUSD,
		delta.DurationMS, delta.ToolDurationMS, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("increment metrics: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetMetrics(ctx context.Context) (*MetricsRow, error) {
	var m MetricsRow
	err := s.db.QueryRowContext(ctx, `
		SELECT runs_started, runs_completed, runs_failed,
			tool_calls, tool_failures, llm_calls, llm_failures,
			step_count, total_tokens, total_cost, duration_ms, tool_duration_ms, updated_at
		FROM metrics WHERE id = 1`).Scan(
		&m.RunsStarted, &m.RunsCompleted, &m.RunsFailed,
		&m.ToolCalls, &m.ToolFailures, &m.LLMCalls, &m.LLMFailures,
		&m.StepCount, &m.TotalTokens, &m.TotalCostUSD, &m.DurationMS,
		&m.ToolDurationMS, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get metrics: %w", err)
	}
	return &m, nil
}

func (s *SQLiteStore) DeleteExpiredRuns(ctx context.Context, completedBefore, pausedBefore time.Time) (int, error) {
	// Delete terminal runs past retention.
	r1, err := s.db.ExecContext(ctx, `
		DELETE FROM runs
		WHERE status IN ('completed', 'failed', 'cancelled')
		AND completed_at < ?`, completedBefore.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired completed runs: %w", err)
	}
	n1, _ := r1.RowsAffected()

	// Delete stale paused runs.
	r2, err := s.db.ExecContext(ctx, `
		DELETE FROM runs
		WHERE status = 'paused'
		AND updated_at < ?`, pausedBefore.UTC())
	if err != nil {
		return int(n1), fmt.Errorf("delete expired paused runs: %w", err)
	}
	n2, _ := r2.RowsAffected()

	return int(n1 + n2), nil
}

// --- helpers ---

// scanner is the common interface between *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRun(sc scanner) (*RunRow, error) {
	var run RunRow
	var completedAt sql.NullTime
	var success sql.NullInt64

	if err := sc.Scan(
		&run.RunID, &run.Status, &run.Task, &run.Workspace, &run.Model, &run.Provider,
		&run.CreatedAt, &run.UpdatedAt, &completedAt, &success,
		&run.Summary, &run.ErrorMessage,
		&run.Steps, &run.TotalTokens, &run.TotalCostUSD, &run.DurationMS,
		&run.Question, &run.PausedState,
	); err != nil {
		return nil, err
	}

	if completedAt.Valid {
		t := completedAt.Time
		run.CompletedAt = &t
	}
	if success.Valid {
		b := success.Int64 != 0
		run.Success = &b
	}

	return &run, nil
}

func scanRunRow(row *sql.Row) (*RunRow, error) {
	return scanRun(row)
}

func scanRunRows(rows *sql.Rows) (*RunRow, error) {
	return scanRun(rows)
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func nullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	if *b {
		return 1
	}
	return 0
}

// isConstraintError checks if a SQLite error is a PRIMARY KEY constraint violation.
func isConstraintError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isForeignKeyError checks if a SQLite error is a FOREIGN KEY constraint violation.
func isForeignKeyError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
