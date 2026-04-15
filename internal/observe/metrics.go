package observe

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dshills/matter/internal/storage"
)

// Metrics tracks in-memory counters for all spec-required metrics.
// When a storage.Store is configured, deltas are periodically flushed
// to persistent storage and Snapshot reads from the store.
type Metrics struct {
	mu sync.Mutex

	RunsStarted   int
	RunsCompleted int
	RunsFailed    int

	ToolCalls    int
	ToolFailures int

	LLMCalls    int
	LLMFailures int

	RunDuration  time.Duration
	ToolDuration time.Duration

	StepCount    int
	TotalTokens  int
	TotalCostUSD float64

	// Store-backed persistence.
	store     storage.Store
	delta     storage.MetricsDelta // unflushed increments
	storeOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
}

// NewMetrics creates a new metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{
		stop: make(chan struct{}),
	}
}

// storeOpTimeout is the timeout for individual store operations.
const storeOpTimeout = 5 * time.Second

// SetStore configures a persistent store for metrics. On startup, it loads
// existing metric values from the store. It starts a background ticker that
// flushes accumulated deltas every 60 seconds.
// SetStore is idempotent — only the first call takes effect.
func (m *Metrics) SetStore(store storage.Store) {
	m.storeOnce.Do(func() {
		m.mu.Lock()
		m.store = store
		m.mu.Unlock()

		// Load existing metrics from store.
		ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
		defer cancel()
		existing, err := store.GetMetrics(ctx)
		if err != nil {
			log.Printf("metrics: failed to load from store: %v", err)
		} else {
			m.mu.Lock()
			m.RunsStarted += existing.RunsStarted
			m.RunsCompleted += existing.RunsCompleted
			m.RunsFailed += existing.RunsFailed
			m.ToolCalls += existing.ToolCalls
			m.ToolFailures += existing.ToolFailures
			m.LLMCalls += existing.LLMCalls
			m.LLMFailures += existing.LLMFailures
			m.StepCount += existing.StepCount
			m.TotalTokens += existing.TotalTokens
			m.TotalCostUSD += existing.TotalCostUSD
			m.RunDuration += time.Duration(existing.DurationMS) * time.Millisecond
			m.ToolDuration += time.Duration(existing.ToolDurationMS) * time.Millisecond
			m.mu.Unlock()
		}

		// Start background flush ticker.
		go m.flushLoop()
	})
}

// flushLoop periodically flushes accumulated deltas to the store.
func (m *Metrics) flushLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.FlushToStore()
		}
	}
}

// Close stops the background flush ticker and flushes any remaining deltas.
func (m *Metrics) Close() {
	m.stopOnce.Do(func() {
		close(m.stop)
	})
	m.FlushToStore()
}

// FlushToStore writes accumulated deltas to the store and resets the delta
// tracker. This is called on run completion, on the periodic ticker, and
// on Close. It is safe for concurrent use.
func (m *Metrics) FlushToStore() {
	m.mu.Lock()
	if m.store == nil {
		m.mu.Unlock()
		return
	}
	// Snapshot and reset the delta.
	delta := m.delta
	m.delta = storage.MetricsDelta{}
	store := m.store
	m.mu.Unlock()

	// Skip flush if nothing changed.
	if delta == (storage.MetricsDelta{}) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := store.IncrementMetrics(ctx, delta); err != nil {
		log.Printf("metrics: failed to flush to store: %v", err)
		// Put the delta back so it's not lost.
		m.mu.Lock()
		m.delta.RunsStarted += delta.RunsStarted
		m.delta.RunsCompleted += delta.RunsCompleted
		m.delta.RunsFailed += delta.RunsFailed
		m.delta.ToolCalls += delta.ToolCalls
		m.delta.ToolFailures += delta.ToolFailures
		m.delta.LLMCalls += delta.LLMCalls
		m.delta.LLMFailures += delta.LLMFailures
		m.delta.StepCount += delta.StepCount
		m.delta.TotalTokens += delta.TotalTokens
		m.delta.TotalCostUSD += delta.TotalCostUSD
		m.delta.DurationMS += delta.DurationMS
		m.delta.ToolDurationMS += delta.ToolDurationMS
		m.mu.Unlock()
	}
}

// IncRunsStarted increments runs_started_total.
func (m *Metrics) IncRunsStarted() {
	m.mu.Lock()
	m.RunsStarted++
	m.delta.RunsStarted++
	m.mu.Unlock()
}

// IncRunsCompleted increments runs_completed_total.
func (m *Metrics) IncRunsCompleted() {
	m.mu.Lock()
	m.RunsCompleted++
	m.delta.RunsCompleted++
	m.mu.Unlock()
}

// IncRunsFailed increments runs_failed_total.
func (m *Metrics) IncRunsFailed() {
	m.mu.Lock()
	m.RunsFailed++
	m.delta.RunsFailed++
	m.mu.Unlock()
}

// IncToolCalls increments tool_calls_total.
func (m *Metrics) IncToolCalls() {
	m.mu.Lock()
	m.ToolCalls++
	m.delta.ToolCalls++
	m.mu.Unlock()
}

// IncToolFailures increments tool_failures_total.
func (m *Metrics) IncToolFailures() {
	m.mu.Lock()
	m.ToolFailures++
	m.delta.ToolFailures++
	m.mu.Unlock()
}

// IncLLMCalls increments llm_calls_total.
func (m *Metrics) IncLLMCalls() {
	m.mu.Lock()
	m.LLMCalls++
	m.delta.LLMCalls++
	m.mu.Unlock()
}

// IncLLMFailures increments llm_failures_total.
func (m *Metrics) IncLLMFailures() {
	m.mu.Lock()
	m.LLMFailures++
	m.delta.LLMFailures++
	m.mu.Unlock()
}

// AddRunDuration adds to run_duration_seconds.
func (m *Metrics) AddRunDuration(d time.Duration) {
	m.mu.Lock()
	m.RunDuration += d
	m.delta.DurationMS += d.Milliseconds()
	m.mu.Unlock()
}

// AddToolDuration adds to tool_duration_seconds.
func (m *Metrics) AddToolDuration(d time.Duration) {
	m.mu.Lock()
	m.ToolDuration += d
	m.delta.ToolDurationMS += d.Milliseconds()
	m.mu.Unlock()
}

// IncStepCount increments step_count.
func (m *Metrics) IncStepCount() {
	m.mu.Lock()
	m.StepCount++
	m.delta.StepCount++
	m.mu.Unlock()
}

// AddTokens adds to total_tokens.
func (m *Metrics) AddTokens(n int) {
	m.mu.Lock()
	m.TotalTokens += n
	m.delta.TotalTokens += n
	m.mu.Unlock()
}

// AddCost adds to total_cost_usd.
func (m *Metrics) AddCost(c float64) {
	m.mu.Lock()
	m.TotalCostUSD += c
	m.delta.TotalCostUSD += c
	m.mu.Unlock()
}

// Snapshot returns a copy of the current metrics.
func (m *Metrics) Snapshot() Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Metrics{
		RunsStarted:   m.RunsStarted,
		RunsCompleted: m.RunsCompleted,
		RunsFailed:    m.RunsFailed,
		ToolCalls:     m.ToolCalls,
		ToolFailures:  m.ToolFailures,
		LLMCalls:      m.LLMCalls,
		LLMFailures:   m.LLMFailures,
		RunDuration:   m.RunDuration,
		ToolDuration:  m.ToolDuration,
		StepCount:     m.StepCount,
		TotalTokens:   m.TotalTokens,
		TotalCostUSD:  m.TotalCostUSD,
	}
}
