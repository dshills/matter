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

// ActiveRun tracks the state of an in-flight or completed run.
type ActiveRun struct {
	mu      sync.Mutex
	RunID   string
	Status  RunStatus
	Result  *matter.RunResult
	Cancel  context.CancelFunc
	Events  []matter.ProgressEvent
	Created time.Time

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

// RunStore is a thread-safe store for active, paused, and completed runs.
// Running and paused counts are tracked with atomic counters to avoid
// O(N) iteration on every new run or pause check.
type RunStore struct {
	mu   sync.Mutex
	runs map[string]*ActiveRun

	runningCount atomic.Int32
	pausedCount  atomic.Int32

	maxConcurrent int
	maxPaused     int
	retention     time.Duration
}

// NewRunStore creates a run store with the given limits.
func NewRunStore(maxConcurrent, maxPaused int, retention time.Duration) *RunStore {
	return &RunStore{
		runs:          make(map[string]*ActiveRun),
		maxConcurrent: maxConcurrent,
		maxPaused:     maxPaused,
		retention:     retention,
	}
}

// Add registers a new run. Returns an error if the concurrent run limit
// is reached. Uses an atomic counter for O(1) concurrency checks.
func (s *RunStore) Add(run *ActiveRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if int(s.runningCount.Load()) >= s.maxConcurrent {
		return fmt.Errorf("concurrent run limit reached (%d)", s.maxConcurrent)
	}

	s.runs[run.RunID] = run
	s.runningCount.Add(1)
	return nil
}

// Get returns the run with the given ID, or nil if not found.
func (s *RunStore) Get(runID string) *ActiveRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[runID]
}

// CountPaused returns the number of paused runs via atomic counter (O(1)).
func (s *RunStore) CountPaused() int {
	return int(s.pausedCount.Load())
}

// TransitionStatus atomically updates a run's status and adjusts the
// running/paused counters accordingly. Caller must hold run.mu.
func (s *RunStore) TransitionStatus(run *ActiveRun, newStatus RunStatus) {
	s.transitionLocked(run, newStatus)
}

// TryPause atomically checks the paused run limit and transitions the
// run to StatusPaused if under the limit. Returns false if the limit
// would be exceeded. Caller must hold run.mu.
func (s *RunStore) TryPause(run *ActiveRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if int(s.pausedCount.Load()) >= s.maxPaused {
		return false
	}
	s.transitionLocked(run, StatusPaused)
	return true
}

// TryResume atomically checks the concurrent run limit and transitions
// the run from StatusPaused to StatusRunning. Returns false if the limit
// would be exceeded. Caller must hold run.mu.
func (s *RunStore) TryResume(run *ActiveRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if int(s.runningCount.Load()) >= s.maxConcurrent {
		return false
	}
	s.transitionLocked(run, StatusRunning)
	return true
}

func (s *RunStore) transitionLocked(run *ActiveRun, newStatus RunStatus) {
	oldStatus := run.Status
	if oldStatus == newStatus {
		return
	}
	run.Status = newStatus

	// Decrement old counter.
	switch oldStatus {
	case StatusRunning:
		s.runningCount.Add(-1)
	case StatusPaused:
		s.pausedCount.Add(-1)
	}

	// Increment new counter.
	switch newStatus {
	case StatusRunning:
		s.runningCount.Add(1)
	case StatusPaused:
		s.pausedCount.Add(1)
	}
}

// AllRunning returns all runs with status "running".
func (s *RunStore) AllRunning() []*ActiveRun {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []*ActiveRun
	for _, r := range s.runs {
		r.mu.Lock()
		if r.Status == StatusRunning {
			result = append(result, r)
		}
		r.mu.Unlock()
	}
	return result
}

// gcCandidate holds the info needed to decide whether to remove a run.
type gcCandidate struct {
	id      string
	run     *ActiveRun
	status  RunStatus
	created time.Time
}

// GC removes completed, failed, and cancelled runs older than the retention
// period. Paused runs are cancelled and removed if they've been paused longer
// than the retention period. The store lock is held only briefly to snapshot
// candidates, avoiding contention with request handlers.
func (s *RunStore) GC(now time.Time) int {
	// Phase 1: snapshot candidates under the store lock.
	s.mu.Lock()
	candidates := make([]gcCandidate, 0, len(s.runs))
	for id, r := range s.runs {
		r.mu.Lock()
		candidates = append(candidates, gcCandidate{
			id:      id,
			run:     r,
			status:  r.Status,
			created: r.Created,
		})
		r.mu.Unlock()
	}
	s.mu.Unlock()

	// Phase 2: identify which runs to remove (no locks held).
	var toRemove []gcCandidate
	for _, c := range candidates {
		switch c.status {
		case StatusCompleted, StatusFailed, StatusCancelled:
			if now.Sub(c.created) > s.retention {
				toRemove = append(toRemove, c)
			}
		case StatusPaused:
			if now.Sub(c.created) > s.retention {
				toRemove = append(toRemove, c)
			}
		}
	}

	if len(toRemove) == 0 {
		return 0
	}

	// Phase 3: remove under store lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for _, c := range toRemove {
		r := c.run
		r.mu.Lock()
		// Re-check status — it may have changed between phases.
		switch r.Status {
		case StatusCompleted, StatusFailed, StatusCancelled:
			if now.Sub(r.Created) > s.retention {
				delete(s.runs, c.id)
				removed++
			}
		case StatusPaused:
			if now.Sub(r.Created) > s.retention {
				if r.Cancel != nil {
					r.Cancel()
				}
				s.transitionLocked(r, StatusCancelled)
				delete(s.runs, c.id)
				removed++
			}
		}
		r.mu.Unlock()
	}
	return removed
}

// CancelAll cancels all running and paused runs. Used during graceful shutdown.
func (s *RunStore) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.runs {
		r.mu.Lock()
		if r.Status == StatusRunning || r.Status == StatusPaused {
			if r.Cancel != nil {
				r.Cancel()
			}
			s.TransitionStatus(r, StatusCancelled)
		}
		r.mu.Unlock()
	}
}
