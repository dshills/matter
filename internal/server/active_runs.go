// Package server implements the HTTP API server for matter.
package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// RunStatus represents the current state of a run.
type RunStatus string

const (
	StatusRunning   RunStatus = "running"
	StatusCompleted RunStatus = "completed"
	StatusFailed    RunStatus = "failed"
	StatusCancelled RunStatus = "cancelled"
	StatusPaused    RunStatus = "paused"
)

// ActiveRun tracks the ephemeral state of an in-flight run.
// Persistent data (result, events, steps) is stored via storage.Store.
type ActiveRun struct {
	mu     sync.Mutex
	RunID  string
	Status RunStatus
	Cancel context.CancelFunc

	// Runner is kept alive for paused runs so Resume can use it.
	// Set to nil after the run completes/fails/is cancelled.
	Runner RunnerIface

	// SSE subscriber channels — written by progress callback, read by SSE handler.
	subscribers map[chan matter.ProgressEvent]struct{}
	subMu       sync.Mutex
}

// RunnerIface abstracts runner.Runner for testing.
type RunnerIface interface {
	Run(ctx context.Context, req matter.RunRequest) matter.RunResult
	Resume(ctx context.Context, answer string) matter.RunResult
	IsPaused() bool
	SetProgressFunc(fn matter.ProgressFunc)
	Tools() []matter.Tool
}

// Subscribe creates a new SSE subscriber channel. The caller must call
// Unsubscribe when done to prevent goroutine leaks.
func (r *ActiveRun) Subscribe() chan matter.ProgressEvent {
	ch := make(chan matter.ProgressEvent, 100)
	r.subMu.Lock()
	if r.subscribers == nil {
		r.subscribers = make(map[chan matter.ProgressEvent]struct{})
	}
	r.subscribers[ch] = struct{}{}
	r.subMu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (r *ActiveRun) Unsubscribe(ch chan matter.ProgressEvent) {
	r.subMu.Lock()
	delete(r.subscribers, ch)
	r.subMu.Unlock()
}

// broadcast sends an event to all subscribers. If a subscriber's buffer is
// full, intermediate events are dropped (slow consumer). For terminal events,
// the mutex is released before attempting delivery so that slow subscribers
// cannot block other operations. After terminal delivery, all subscriber
// channels are closed.
func (r *ActiveRun) broadcast(event matter.ProgressEvent) {
	r.subMu.Lock()

	terminal := isTerminalEvent(event.Event)

	if !terminal {
		// Non-blocking send; drop if buffer full (slow consumer).
		for ch := range r.subscribers {
			select {
			case ch <- event:
			default:
			}
		}
		r.subMu.Unlock()
		return
	}

	// For terminal events, snapshot subscribers and release lock before
	// attempting delivery to avoid holding the mutex during timeouts.
	subs := make([]chan matter.ProgressEvent, 0, len(r.subscribers))
	for ch := range r.subscribers {
		subs = append(subs, ch)
	}
	// Clear the map while we still hold the lock so no new subscribers
	// see a stale set.
	for ch := range r.subscribers {
		delete(r.subscribers, ch)
	}
	r.subMu.Unlock()

	// Deliver terminal event with timeout, then close each channel.
	for _, ch := range subs {
		select {
		case ch <- event:
		case <-time.After(5 * time.Second):
			// Subscriber too slow — skip.
		}
		close(ch)
	}
}

// isTerminalEvent returns true for events that end a run's lifecycle.
// "run_paused" is not terminal — the SSE connection stays open so the
// client can receive further events after answering the question.
func isTerminalEvent(event string) bool {
	return event == "run_completed" || event == "run_failed"
}

// ActiveRunTracker is a thread-safe tracker for in-flight and paused runs.
// It manages only ephemeral state: cancel functions, SSE subscribers, and
// running/paused counters. Persistent run data lives in storage.Store.
type ActiveRunTracker struct {
	mu   sync.Mutex
	runs map[string]*ActiveRun

	runningCount atomic.Int32
	pausedCount  atomic.Int32

	maxConcurrent int
	maxPaused     int
}

// NewActiveRunTracker creates a tracker with the given concurrency limits.
func NewActiveRunTracker(maxConcurrent, maxPaused int) *ActiveRunTracker {
	return &ActiveRunTracker{
		runs:          make(map[string]*ActiveRun),
		maxConcurrent: maxConcurrent,
		maxPaused:     maxPaused,
	}
}

// Add registers a new active run. Returns an error if the concurrent run
// limit is reached. Uses an atomic counter for O(1) concurrency checks.
func (t *ActiveRunTracker) Add(run *ActiveRun) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(t.runningCount.Load()) >= t.maxConcurrent {
		return fmt.Errorf("concurrent run limit reached (%d)", t.maxConcurrent)
	}

	t.runs[run.RunID] = run
	t.runningCount.Add(1)
	return nil
}

// Get returns the active run with the given ID, or nil if not tracked.
func (t *ActiveRunTracker) Get(runID string) *ActiveRun {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs[runID]
}

// Remove removes a run from the tracker. Used when a run reaches a terminal
// state and its ephemeral resources should be released.
func (t *ActiveRunTracker) Remove(runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	run, ok := t.runs[runID]
	if !ok {
		return
	}

	// Adjust counters before removing.
	run.mu.Lock()
	switch run.Status {
	case StatusRunning:
		t.runningCount.Add(-1)
	case StatusPaused:
		t.pausedCount.Add(-1)
	}
	run.mu.Unlock()

	delete(t.runs, runID)
}

// CountPaused returns the number of paused runs via atomic counter (O(1)).
func (t *ActiveRunTracker) CountPaused() int {
	return int(t.pausedCount.Load())
}

// TransitionStatus atomically updates a run's status and adjusts the
// running/paused counters accordingly. Caller must hold run.mu.
func (t *ActiveRunTracker) TransitionStatus(run *ActiveRun, newStatus RunStatus) {
	t.transitionLocked(run, newStatus)
}

// TryPause atomically checks the paused run limit and transitions the
// run to StatusPaused if under the limit. Returns false if the limit
// would be exceeded. Caller must hold run.mu.
func (t *ActiveRunTracker) TryPause(run *ActiveRun) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(t.pausedCount.Load()) >= t.maxPaused {
		return false
	}
	t.transitionLocked(run, StatusPaused)
	return true
}

// TryResume atomically checks the concurrent run limit and transitions
// the run from StatusPaused to StatusRunning. Returns false if the limit
// would be exceeded. Caller must hold run.mu.
func (t *ActiveRunTracker) TryResume(run *ActiveRun) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(t.runningCount.Load()) >= t.maxConcurrent {
		return false
	}
	t.transitionLocked(run, StatusRunning)
	return true
}

func (t *ActiveRunTracker) transitionLocked(run *ActiveRun, newStatus RunStatus) {
	oldStatus := run.Status
	if oldStatus == newStatus {
		return
	}
	run.Status = newStatus

	// Decrement old counter.
	switch oldStatus {
	case StatusRunning:
		t.runningCount.Add(-1)
	case StatusPaused:
		t.pausedCount.Add(-1)
	}

	// Increment new counter.
	switch newStatus {
	case StatusRunning:
		t.runningCount.Add(1)
	case StatusPaused:
		t.pausedCount.Add(1)
	}
}

// CancelAll cancels all running and paused runs. Used during graceful shutdown.
func (t *ActiveRunTracker) CancelAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, r := range t.runs {
		r.mu.Lock()
		if r.Status == StatusRunning || r.Status == StatusPaused {
			if r.Cancel != nil {
				r.Cancel()
			}
			t.TransitionStatus(r, StatusCancelled)
		}
		r.mu.Unlock()
	}
}

// SetPausedCount sets the initial paused count (used for startup reconciliation
// when loading paused run count from the store).
func (t *ActiveRunTracker) SetPausedCount(n int) {
	t.pausedCount.Store(int32(n))
}
