package storage

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryStore is an in-memory implementation of Store.
// It is suitable for testing and single-run CLI use.
type MemoryStore struct {
	mu      sync.RWMutex
	runs    map[string]*RunRow
	steps   map[string][]StepRow
	events  map[string][]EventRow
	metrics MetricsRow

	stepSeq  atomic.Int64
	eventSeq atomic.Int64
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:   make(map[string]*RunRow),
		steps:  make(map[string][]StepRow),
		events: make(map[string][]EventRow),
	}
}

func (m *MemoryStore) CreateRun(_ context.Context, run *RunRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.RunID]; exists {
		return &ErrConflict{Resource: "run", ID: run.RunID}
	}

	// Store a deep copy to prevent caller mutations of pointer/slice fields.
	cp := deepCopyRunRow(run)
	m.runs[run.RunID] = &cp
	return nil
}

func (m *MemoryStore) GetRun(_ context.Context, runID string) (*RunRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	run, ok := m.runs[runID]
	if !ok {
		return nil, &ErrNotFound{Resource: "run", ID: runID}
	}
	cp := deepCopyRunRow(run)
	return &cp, nil
}

func (m *MemoryStore) UpdateRun(_ context.Context, run *RunRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.RunID]; !exists {
		return &ErrNotFound{Resource: "run", ID: run.RunID}
	}

	cp := deepCopyRunRow(run)
	m.runs[run.RunID] = &cp
	return nil
}

func (m *MemoryStore) ListRuns(_ context.Context, filter RunFilter) ([]RunRow, error) {
	filter = normalizeFilter(filter)

	// Collect pointers under read lock, then sort/copy outside to minimize lock duration.
	m.mu.RLock()
	var ptrs []*RunRow
	for _, run := range m.runs {
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		if filter.Before != nil && !run.CreatedAt.Before(*filter.Before) {
			continue
		}
		if filter.After != nil && !run.CreatedAt.After(*filter.After) {
			continue
		}
		ptrs = append(ptrs, run)
	}
	m.mu.RUnlock()

	// Sort pointers (cheaper than sorting full structs).
	sort.Slice(ptrs, func(i, j int) bool {
		if filter.OrderBy == "updated_at" {
			return ptrs[i].UpdatedAt.Before(ptrs[j].UpdatedAt)
		}
		return ptrs[i].CreatedAt.Before(ptrs[j].CreatedAt)
	})

	// Apply offset and limit.
	if filter.Offset >= len(ptrs) {
		return []RunRow{}, nil
	}
	ptrs = ptrs[filter.Offset:]
	if len(ptrs) > filter.Limit {
		ptrs = ptrs[:filter.Limit]
	}

	// Deep-copy only the page that will be returned.
	result := make([]RunRow, len(ptrs))
	for i, p := range ptrs {
		result[i] = deepCopyRunRow(p)
	}
	return result, nil
}

func (m *MemoryStore) DeleteRun(_ context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[runID]; !exists {
		return &ErrNotFound{Resource: "run", ID: runID}
	}

	delete(m.runs, runID)
	delete(m.steps, runID)
	delete(m.events, runID)
	return nil
}

func (m *MemoryStore) AppendStep(_ context.Context, runID string, step *StepRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[runID]; !exists {
		return &ErrNotFound{Resource: "run", ID: runID}
	}

	cp := *step
	cp.StepID = m.stepSeq.Add(1)
	cp.RunID = runID
	m.steps[runID] = append(m.steps[runID], cp)
	return nil
}

func (m *MemoryStore) GetSteps(_ context.Context, runID string) ([]StepRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.runs[runID]; !exists {
		return nil, &ErrNotFound{Resource: "run", ID: runID}
	}

	steps := m.steps[runID]
	out := make([]StepRow, len(steps))
	copy(out, steps)
	return out, nil
}

func (m *MemoryStore) AppendEvent(_ context.Context, runID string, event *EventRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[runID]; !exists {
		return &ErrNotFound{Resource: "run", ID: runID}
	}

	cp := *event
	cp.Seq = m.eventSeq.Add(1)
	cp.RunID = runID
	m.events[runID] = append(m.events[runID], cp)
	return nil
}

func (m *MemoryStore) GetEvents(_ context.Context, runID string, afterSeq int64) ([]EventRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.runs[runID]; !exists {
		return nil, &ErrNotFound{Resource: "run", ID: runID}
	}

	var out []EventRow
	for _, ev := range m.events[runID] {
		if ev.Seq > afterSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *MemoryStore) IncrementMetrics(_ context.Context, delta MetricsDelta) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics.RunsStarted += delta.RunsStarted
	m.metrics.RunsCompleted += delta.RunsCompleted
	m.metrics.RunsFailed += delta.RunsFailed
	m.metrics.ToolCalls += delta.ToolCalls
	m.metrics.ToolFailures += delta.ToolFailures
	m.metrics.LLMCalls += delta.LLMCalls
	m.metrics.LLMFailures += delta.LLMFailures
	m.metrics.StepCount += delta.StepCount
	m.metrics.TotalTokens += delta.TotalTokens
	m.metrics.TotalCostUSD += delta.TotalCostUSD
	m.metrics.DurationMS += delta.DurationMS
	m.metrics.ToolDurationMS += delta.ToolDurationMS
	m.metrics.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStore) GetMetrics(_ context.Context) (*MetricsRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cp := m.metrics
	return &cp, nil
}

func (m *MemoryStore) DeleteExpiredRuns(_ context.Context, completedBefore, pausedBefore time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var deleted int
	for id, run := range m.runs {
		switch run.Status {
		case "completed", "failed", "cancelled":
			if run.CompletedAt != nil && run.CompletedAt.Before(completedBefore) {
				delete(m.runs, id)
				delete(m.steps, id)
				delete(m.events, id)
				deleted++
			}
		case "paused":
			if run.UpdatedAt.Before(pausedBefore) {
				delete(m.runs, id)
				delete(m.steps, id)
				delete(m.events, id)
				deleted++
			}
		}
	}
	return deleted, nil
}

// Close is a no-op for the in-memory store.
func (m *MemoryStore) Close() error {
	return nil
}
