package observe

import (
	"sync"
	"time"
)

// EventType identifies a trace event kind.
type EventType string

const (
	EventRunStarted       EventType = "run_started"
	EventPlannerStarted   EventType = "planner_started"
	EventPlannerCompleted EventType = "planner_completed"
	EventToolStarted      EventType = "tool_started"
	EventToolCompleted    EventType = "tool_completed"
	EventPlannerFailed    EventType = "planner_failed"
	EventRetry            EventType = "retry"
	EventSummaryGenerated EventType = "summary_generated"
	EventLimitExceeded    EventType = "limit_exceeded"
	EventRunCompleted     EventType = "run_completed"
)

// TraceEvent captures a single step-level event.
type TraceEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	RunID     string         `json:"run_id"`
	Step      int            `json:"step"`
	Type      EventType      `json:"type"`
	Data      map[string]any `json:"data,omitempty"`
}

// Tracer collects step-level trace events for a run.
type Tracer struct {
	mu     sync.Mutex
	runID  string
	events []TraceEvent
}

// NewTracer creates a tracer for the given run.
func NewTracer(runID string) *Tracer {
	return &Tracer{runID: runID}
}

// Emit records a trace event.
func (t *Tracer) Emit(step int, eventType EventType, data map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, TraceEvent{
		Timestamp: time.Now(),
		RunID:     t.runID,
		Step:      step,
		Type:      eventType,
		Data:      data,
	})
}

// Events returns a copy of all recorded trace events.
func (t *Tracer) Events() []TraceEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TraceEvent, len(t.events))
	copy(out, t.events)
	return out
}
