package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/memory"
	"github.com/dshills/matter/internal/observe"
	"github.com/dshills/matter/internal/planner"
	"github.com/dshills/matter/internal/policy"
	"github.com/dshills/matter/internal/tools"
	"github.com/dshills/matter/pkg/matter"
)

// Agent orchestrates a single run: plan → policy → execute → store → check limits.
type Agent struct {
	cfg        config.Config
	llmClient  llm.Client
	planner    *planner.Planner
	executor   *tools.Executor
	registry   *tools.Registry
	memory     *memory.Manager
	policy     policy.Checker
	observer   *observe.Observer
	session    *observe.RunSession
	metrics    RunMetrics
	detector   *LoopDetector
	progressFn matter.ProgressFunc
}

// New creates an agent with the given configuration and dependencies.
func New(
	cfg config.Config,
	llmClient llm.Client,
	registry *tools.Registry,
	policyChecker policy.Checker,
) (*Agent, error) {
	p, err := planner.NewPlanner(llmClient, cfg.Planner)
	if err != nil {
		return nil, fmt.Errorf("creating planner: %w", err)
	}
	return &Agent{
		cfg:       cfg,
		planner:   p,
		executor:  tools.NewExecutor(registry),
		registry:  registry,
		llmClient: llmClient,
		policy:    policyChecker,
	}, nil
}

// Run executes the agent loop for the given request.
// Each call resets internal state (memory, metrics, loop detector) so the
// agent can be reused across runs.
func (a *Agent) Run(ctx context.Context, req matter.RunRequest) matter.RunResult {
	// Reset per-run state.
	a.memory = memory.NewManager(a.cfg.Memory, a.llmClient)
	a.detector = NewLoopDetector(a.cfg.Agent.MaxRepeatedToolCalls)
	a.metrics = RunMetrics{StartTime: time.Now()}

	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	if a.observer != nil {
		a.session = a.observer.StartRun(runID, req.Task, a.cfg, a.progressFn)
	}

	// Seed memory with the system prompt and user task.
	sysMsg := matter.Message{
		Role:      matter.RoleSystem,
		Content:   "You are an autonomous agent. Complete the user's task.",
		Timestamp: time.Now(),
	}
	if err := a.memory.Add(ctx, sysMsg); err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to seed system message: %w", err)}
	}

	taskMsg := matter.Message{
		Role:      matter.RoleUser,
		Content:   req.Task,
		Timestamp: time.Now(),
	}
	if err := a.memory.Add(ctx, taskMsg); err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to seed task message: %w", err)}
	}

	return a.finalizeRun(a.loop(ctx, req))
}

// SetObserver attaches an observer for logging, tracing, metrics, and recording.
func (a *Agent) SetObserver(obs *observe.Observer) {
	a.observer = obs
}

// SetProgressFunc registers a progress callback for real-time events.
// Must be called before Run.
func (a *Agent) SetProgressFunc(fn matter.ProgressFunc) {
	a.progressFn = fn
}

// ResumeWithAnswer adds the user's answer to memory and re-enters the agent loop.
// Called by the runner after a paused run is resumed.
func (a *Agent) ResumeWithAnswer(ctx context.Context, req matter.RunRequest, answer string, pausedDuration time.Duration) matter.RunResult {
	// Account for time spent paused.
	a.metrics.PausedDuration += pausedDuration

	// Add the user's answer to memory.
	answerMsg := matter.Message{
		Role:      matter.RoleUser,
		Content:   answer,
		Timestamp: time.Now(),
		Step:      a.metrics.Steps,
	}
	if err := a.memory.Add(ctx, answerMsg); err != nil {
		return a.finalizeRun(matter.RunResult{
			Error: fmt.Errorf("failed to store answer message: %w", err),
		})
	}

	return a.finalizeRun(a.loop(ctx, req))
}

// finalizeRun populates metrics on the result and ends the observer session
// (unless the run is paused and will be resumed later).
func (a *Agent) finalizeRun(result matter.RunResult) matter.RunResult {
	result.Steps = a.metrics.Steps
	result.TotalTokens = a.metrics.TotalTokens
	result.TotalCostUSD = a.metrics.CostUSD

	if a.session != nil && !result.Paused {
		duration := time.Since(a.metrics.StartTime) - a.metrics.PausedDuration
		a.session.EndRun(result.Success, result.FinalSummary, a.metrics.Steps, duration, a.metrics.TotalTokens, a.metrics.CostUSD)
	}

	return result
}

// Metrics returns the current run metrics.
func (a *Agent) Metrics() RunMetrics {
	return a.metrics
}
