// Package storage defines the persistent storage interface for matter runs,
// steps, events, and metrics.
package storage

import (
	"context"
	"time"
)

// Store is the persistent storage interface for matter.
// All methods must be safe for concurrent use.
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
	GetEvents(ctx context.Context, runID string, afterSeq int64) ([]EventRow, error)

	// Metrics (global aggregate counters)
	IncrementMetrics(ctx context.Context, delta MetricsDelta) error
	GetMetrics(ctx context.Context) (*MetricsRow, error)

	// Retention
	DeleteExpiredRuns(ctx context.Context, completedBefore, pausedBefore time.Time) (int, error)

	// Lifecycle
	Close() error
}

// RunRow represents a persisted run.
type RunRow struct {
	RunID       string
	Status      string // running, completed, failed, cancelled, paused
	Task        string
	Workspace   string
	Model       string
	Provider    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time

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
	Question    string // non-empty when Status == "paused"
	PausedState []byte // serialized agent state for resume (JSON)
}

// StepRow represents a single step within a run.
type StepRow struct {
	StepID      int64 // auto-increment
	RunID       string
	StepNumber  int
	Timestamp   time.Time
	Decision    string // "tool_call", "complete", "fail", "ask"
	ToolName    string
	ToolInput   string // JSON
	ToolOutput  string
	ToolError   string
	RawResponse string
	Tokens      int
	CostUSD     float64
}

// EventRow represents a progress event for SSE replay.
type EventRow struct {
	Seq       int64 // auto-increment, used for SSE replay cursor
	RunID     string
	Timestamp time.Time
	Type      string // event type from matter.ProgressEvent
	Data      string // JSON-encoded event payload
}

// RunFilter controls ListRuns query parameters.
type RunFilter struct {
	Status  string     // optional: filter by status
	Limit   int        // max results (default 50, max 200)
	Offset  int        // pagination offset
	OrderBy string     // "created_at" (default) or "updated_at"
	Before  *time.Time // optional: runs created before this time
	After   *time.Time // optional: runs created after this time
}

// MetricsDelta holds incremental metric updates.
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

// MetricsRow holds the global aggregate metrics.
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

// ErrNotFound is returned when a requested resource does not exist.
type ErrNotFound struct {
	Resource string
	ID       string
}

func (e *ErrNotFound) Error() string {
	return e.Resource + " not found: " + e.ID
}

// ErrConflict is returned when a resource already exists.
type ErrConflict struct {
	Resource string
	ID       string
}

func (e *ErrConflict) Error() string {
	return e.Resource + " already exists: " + e.ID
}

// normalizeFilter applies defaults to a RunFilter.
func normalizeFilter(f RunFilter) RunFilter {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.OrderBy != "updated_at" {
		f.OrderBy = "created_at"
	}
	return f
}

// deepCopyRunRow returns a deep copy of a RunRow, cloning pointer and slice fields.
func deepCopyRunRow(run *RunRow) RunRow {
	cp := *run
	if run.CompletedAt != nil {
		t := *run.CompletedAt
		cp.CompletedAt = &t
	}
	if run.Success != nil {
		b := *run.Success
		cp.Success = &b
	}
	if run.PausedState != nil {
		cp.PausedState = make([]byte, len(run.PausedState))
		copy(cp.PausedState, run.PausedState)
	}
	return cp
}
