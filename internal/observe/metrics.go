package observe

import (
	"sync"
	"time"
)

// Metrics tracks in-memory counters for all spec-required metrics.
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
}

// NewMetrics creates a new metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// IncRunsStarted increments runs_started_total.
func (m *Metrics) IncRunsStarted() {
	m.mu.Lock()
	m.RunsStarted++
	m.mu.Unlock()
}

// IncRunsCompleted increments runs_completed_total.
func (m *Metrics) IncRunsCompleted() {
	m.mu.Lock()
	m.RunsCompleted++
	m.mu.Unlock()
}

// IncRunsFailed increments runs_failed_total.
func (m *Metrics) IncRunsFailed() {
	m.mu.Lock()
	m.RunsFailed++
	m.mu.Unlock()
}

// IncToolCalls increments tool_calls_total.
func (m *Metrics) IncToolCalls() {
	m.mu.Lock()
	m.ToolCalls++
	m.mu.Unlock()
}

// IncToolFailures increments tool_failures_total.
func (m *Metrics) IncToolFailures() {
	m.mu.Lock()
	m.ToolFailures++
	m.mu.Unlock()
}

// IncLLMCalls increments llm_calls_total.
func (m *Metrics) IncLLMCalls() {
	m.mu.Lock()
	m.LLMCalls++
	m.mu.Unlock()
}

// IncLLMFailures increments llm_failures_total.
func (m *Metrics) IncLLMFailures() {
	m.mu.Lock()
	m.LLMFailures++
	m.mu.Unlock()
}

// AddRunDuration adds to run_duration_seconds.
func (m *Metrics) AddRunDuration(d time.Duration) {
	m.mu.Lock()
	m.RunDuration += d
	m.mu.Unlock()
}

// AddToolDuration adds to tool_duration_seconds.
func (m *Metrics) AddToolDuration(d time.Duration) {
	m.mu.Lock()
	m.ToolDuration += d
	m.mu.Unlock()
}

// IncStepCount increments step_count.
func (m *Metrics) IncStepCount() {
	m.mu.Lock()
	m.StepCount++
	m.mu.Unlock()
}

// AddTokens adds to total_tokens.
func (m *Metrics) AddTokens(n int) {
	m.mu.Lock()
	m.TotalTokens += n
	m.mu.Unlock()
}

// AddCost adds to total_cost_usd.
func (m *Metrics) AddCost(c float64) {
	m.mu.Lock()
	m.TotalCostUSD += c
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
