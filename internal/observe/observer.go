package observe

import (
	"io"
	"time"

	"github.com/dshills/matter/internal/config"
)

// Observer is a shared factory that holds global state (logger, metrics, config).
// Call StartRun to get a per-run RunSession that is safe for concurrent use.
type Observer struct {
	Logger  *Logger
	Metrics *Metrics
	cfg     config.ObserveConfig
}

// NewObserver creates an observer with shared subsystems initialized.
func NewObserver(cfg config.ObserveConfig, logOut io.Writer) *Observer {
	level := ParseLogLevel(cfg.LogLevel)
	return &Observer{
		Logger:  NewLogger(logOut, level),
		Metrics: NewMetrics(),
		cfg:     cfg,
	}
}

// StartRun creates a new per-run session with its own tracer and optional recorder.
// The session shares the observer's logger and metrics (both are thread-safe).
func (o *Observer) StartRun(runID, task string, cfgSnapshot config.Config) *RunSession {
	o.Logger.SetRunID(runID)
	o.Metrics.IncRunsStarted()

	var rec *Recorder
	if o.cfg.RecordRuns {
		rec = NewRecorder(runID, task, cfgSnapshot, o.cfg.RecordDir)
	}

	o.Logger.Info(0, "agent", "run started", map[string]any{
		"run_id": runID,
		"task":   task,
	})

	return &RunSession{
		logger:  o.Logger,
		tracer:  NewTracer(runID),
		metrics: o.Metrics,
		rec:     rec,
	}
}

// RunSession is a per-run observability handle. It owns a dedicated Tracer
// and Recorder while sharing the Observer's Logger and Metrics.
// All methods are safe for concurrent use.
type RunSession struct {
	logger  *Logger
	tracer  *Tracer
	metrics *Metrics
	rec     *Recorder
}

// Tracer returns the session's tracer for direct access in tests.
func (s *RunSession) Tracer() *Tracer {
	return s.tracer
}

// EndRun finalizes the run: logs completion, writes run record if enabled.
func (s *RunSession) EndRun(success bool, summary string, steps int, duration time.Duration, tokens int, cost float64) {
	s.metrics.AddRunDuration(duration)
	if success {
		s.metrics.IncRunsCompleted()
	} else {
		s.metrics.IncRunsFailed()
	}

	s.tracer.Emit(steps, EventRunCompleted, map[string]any{
		"success": success,
		"summary": summary,
	})

	s.logger.Info(steps, "agent", "run completed", map[string]any{
		"success":  success,
		"steps":    steps,
		"duration": duration.String(),
		"tokens":   tokens,
		"cost":     cost,
	})

	if s.rec != nil {
		s.rec.SetOutcome(success, summary, steps, duration, tokens, cost)
		s.rec.SetTraceEvents(s.tracer.Events())
		if err := s.rec.Flush(); err != nil {
			s.logger.Error(steps, "recorder", "failed to write run record", map[string]any{
				"error": err.Error(),
			})
		}
	}
}

// PlannerStarted records a planner request event.
func (s *RunSession) PlannerStarted(step int) {
	s.metrics.IncLLMCalls()
	s.tracer.Emit(step, EventPlannerStarted, nil)
	s.logger.Debug(step, "planner", "planner request started", nil)
}

// PlannerCompleted records a planner response event.
func (s *RunSession) PlannerCompleted(step int, tokens int, cost float64, latency time.Duration) {
	s.metrics.AddTokens(tokens)
	s.metrics.AddCost(cost)
	s.tracer.Emit(step, EventPlannerCompleted, map[string]any{
		"tokens":  tokens,
		"cost":    cost,
		"latency": latency.String(),
	})
	s.logger.Info(step, "planner", "planner response received", map[string]any{
		"tokens":  tokens,
		"cost":    cost,
		"latency": latency.String(),
	})

	if s.rec != nil {
		s.rec.RecordStep(StepRecord{
			Step:      step,
			Timestamp: time.Now(),
			Decision:  "planner_completed",
			Tokens:    tokens,
			CostUSD:   cost,
		})
	}
}

// PlannerFailed records a planner failure.
func (s *RunSession) PlannerFailed(step int, err error) {
	s.metrics.IncLLMFailures()
	s.logger.Error(step, "planner", "planner call failed", map[string]any{
		"error": err.Error(),
	})
}

// ToolStarted records a tool call start event.
func (s *RunSession) ToolStarted(step int, toolName string) {
	s.metrics.IncToolCalls()
	s.tracer.Emit(step, EventToolStarted, map[string]any{"tool": toolName})
	s.logger.Debug(step, "tool", "tool call started", map[string]any{"tool": toolName})
}

// ToolCompleted records a tool call completion event.
func (s *RunSession) ToolCompleted(step int, toolName string, duration time.Duration, errMsg string) {
	s.metrics.AddToolDuration(duration)
	if errMsg != "" {
		s.metrics.IncToolFailures()
	}

	s.tracer.Emit(step, EventToolCompleted, map[string]any{
		"tool":     toolName,
		"duration": duration.String(),
		"error":    errMsg,
	})

	level := LevelInfo
	if errMsg != "" {
		level = LevelWarn
	}
	s.logger.Log(level, step, "tool", "tool call completed", map[string]any{
		"tool":     toolName,
		"duration": duration.String(),
		"error":    errMsg,
	})

	if s.rec != nil {
		s.rec.RecordStep(StepRecord{
			Step:      step,
			Timestamp: time.Now(),
			ToolName:  toolName,
			ToolError: errMsg,
		})
	}
}

// StepCompleted records a step completion.
func (s *RunSession) StepCompleted(step int) {
	s.metrics.IncStepCount()
}

// LimitExceeded records a limit exceeded event.
func (s *RunSession) LimitExceeded(step int, limit, message string) {
	s.tracer.Emit(step, EventLimitExceeded, map[string]any{
		"limit":   limit,
		"message": message,
	})
	s.logger.Warn(step, "agent", "limit exceeded", map[string]any{
		"limit":   limit,
		"message": message,
	})
}

// SummaryGenerated records a summarization event.
func (s *RunSession) SummaryGenerated(step int, tokens int) {
	s.tracer.Emit(step, EventSummaryGenerated, map[string]any{
		"tokens": tokens,
	})
	s.logger.Debug(step, "memory", "summary generated", map[string]any{
		"tokens": tokens,
	})
}

// RetryPerformed records a retry event.
func (s *RunSession) RetryPerformed(step int, attempt int, err error) {
	s.tracer.Emit(step, EventRetry, map[string]any{
		"attempt": attempt,
		"error":   err.Error(),
	})
	s.logger.Warn(step, "llm", "retry performed", map[string]any{
		"attempt": attempt,
		"error":   err.Error(),
	})
}
