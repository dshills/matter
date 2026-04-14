package observe

import (
	"fmt"
	"io"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/pkg/matter"
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
// progressFn is optional — pass nil to disable progress callbacks.
func (o *Observer) StartRun(runID, task string, cfgSnapshot config.Config, progressFn matter.ProgressFunc) *RunSession {
	// SetRunID mutates the shared logger, which is safe because Runner
	// executes runs sequentially (one at a time per Runner instance).
	// Concurrent run support would require per-session loggers — deferred
	// to a future version as a separate concern from progress callbacks.
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

	s := &RunSession{
		runID:      runID,
		logger:     o.Logger,
		tracer:     NewTracer(runID),
		metrics:    o.Metrics,
		rec:        rec,
		progressFn: progressFn,
	}

	// Emit run_started event and invoke progress callback.
	s.tracer.Emit(0, EventRunStarted, map[string]any{
		"task": task,
	})
	s.invokeProgress(matter.ProgressEvent{
		RunID:     runID,
		Step:      0,
		Event:     string(EventRunStarted),
		Data:      map[string]any{"task": task},
		Timestamp: time.Now(),
	})

	return s
}

// RunSession is a per-run observability handle. It owns a dedicated Tracer
// and Recorder while sharing the Observer's Logger and Metrics.
// All methods are safe for concurrent use.
type RunSession struct {
	runID      string
	logger     *Logger
	tracer     *Tracer
	metrics    *Metrics
	rec        *Recorder
	progressFn matter.ProgressFunc
}

// Tracer returns the session's tracer for direct access in tests.
func (s *RunSession) Tracer() *Tracer {
	return s.tracer
}

// slowCallbackThreshold is the duration after which a progress callback
// triggers a warning log. Per spec §4.2, callbacks are never terminated.
const slowCallbackThreshold = 500 * time.Millisecond

// copyData returns a shallow copy of the map to prevent callbacks from
// mutating internal tracer/logger state. All values in event Data maps
// are primitives (string, int, float64, bool) — deep copy is unnecessary.
func copyData(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// invokeProgress safely calls the progress callback with panic recovery
// and slow-callback warning logging. The Data map is copied to prevent
// callbacks from mutating internal tracer state.
func (s *RunSession) invokeProgress(event matter.ProgressEvent) {
	if s.progressFn == nil {
		return
	}

	// Isolate callback from internal state.
	event.Data = copyData(event.Data)

	start := time.Now()
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error(event.Step, "progress", "callback panicked", map[string]any{
					"event": event.Event,
					"panic": fmt.Sprintf("%v", r),
				})
			}
		}()
		s.progressFn(event)
	}()

	if elapsed := time.Since(start); elapsed > slowCallbackThreshold {
		s.logger.Warn(event.Step, "progress", "slow callback", map[string]any{
			"event":   event.Event,
			"elapsed": elapsed.String(),
		})
	}
}

// EndRun finalizes the run: logs completion, writes run record if enabled.
func (s *RunSession) EndRun(success bool, summary string, steps int, duration time.Duration, tokens int, cost float64) {
	s.metrics.AddRunDuration(duration)
	if success {
		s.metrics.IncRunsCompleted()
	} else {
		s.metrics.IncRunsFailed()
	}

	data := map[string]any{
		"success": success,
		"summary": summary,
	}

	s.tracer.Emit(steps, EventRunCompleted, data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      steps,
		Event:     string(EventRunCompleted),
		Data:      data,
		Timestamp: time.Now(),
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

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventPlannerStarted),
		Data:      nil,
		Timestamp: time.Now(),
	})
}

// PlannerCompleted records a planner response event.
func (s *RunSession) PlannerCompleted(step int, tokens int, cost float64, latency time.Duration) {
	s.metrics.AddTokens(tokens)
	s.metrics.AddCost(cost)

	data := map[string]any{
		"tokens":  tokens,
		"cost":    cost,
		"latency": latency.String(),
	}

	s.tracer.Emit(step, EventPlannerCompleted, data)
	s.logger.Info(step, "planner", "planner response received", data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventPlannerCompleted),
		Data:      data,
		Timestamp: time.Now(),
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

	data := map[string]any{"error": err.Error()}

	s.tracer.Emit(step, EventPlannerFailed, data)
	s.logger.Error(step, "planner", "planner call failed", data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventPlannerFailed),
		Data:      data,
		Timestamp: time.Now(),
	})
}

// ToolStarted records a tool call start event.
func (s *RunSession) ToolStarted(step int, toolName string) {
	s.metrics.IncToolCalls()

	data := map[string]any{"tool": toolName}

	s.tracer.Emit(step, EventToolStarted, data)
	s.logger.Debug(step, "tool", "tool call started", data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventToolStarted),
		Data:      data,
		Timestamp: time.Now(),
	})
}

// ToolCompleted records a tool call completion event.
func (s *RunSession) ToolCompleted(step int, toolName string, duration time.Duration, errMsg string) {
	s.metrics.AddToolDuration(duration)
	if errMsg != "" {
		s.metrics.IncToolFailures()
	}

	data := map[string]any{
		"tool":     toolName,
		"duration": duration.String(),
		"error":    errMsg,
	}

	s.tracer.Emit(step, EventToolCompleted, data)

	level := LevelInfo
	if errMsg != "" {
		level = LevelWarn
	}
	s.logger.Log(level, step, "tool", "tool call completed", data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventToolCompleted),
		Data:      data,
		Timestamp: time.Now(),
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
	data := map[string]any{
		"limit":   limit,
		"message": message,
	}

	s.tracer.Emit(step, EventLimitExceeded, data)
	s.logger.Warn(step, "agent", "limit exceeded", data)

	s.invokeProgress(matter.ProgressEvent{
		RunID:     s.runID,
		Step:      step,
		Event:     string(EventLimitExceeded),
		Data:      data,
		Timestamp: time.Now(),
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
