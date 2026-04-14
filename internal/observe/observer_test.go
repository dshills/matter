package observe

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
)

func testObserverCfg() config.ObserveConfig {
	return config.ObserveConfig{
		LogLevel:   "info",
		RecordRuns: false,
		RecordDir:  "",
	}
}

func TestLoggerStructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, LevelInfo)
	logger.SetRunID("run-123")

	logger.Info(1, "planner", "test message", map[string]any{"key": "val"})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if entry.Level != "info" {
		t.Errorf("level = %q, want info", entry.Level)
	}
	if entry.RunID != "run-123" {
		t.Errorf("run_id = %q, want run-123", entry.RunID)
	}
	if entry.Step != 1 {
		t.Errorf("step = %d, want 1", entry.Step)
	}
	if entry.Component != "planner" {
		t.Errorf("component = %q, want planner", entry.Component)
	}
	if entry.Message != "test message" {
		t.Errorf("message = %q, want 'test message'", entry.Message)
	}
	if entry.Fields["key"] != "val" {
		t.Errorf("fields[key] = %v, want val", entry.Fields["key"])
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, LevelWarn)

	logger.Debug(1, "test", "debug msg", nil)
	logger.Info(1, "test", "info msg", nil)
	logger.Warn(1, "test", "warn msg", nil)
	logger.Error(1, "test", "error msg", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines (warn + error), got %d: %s", len(lines), buf.String())
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  LogLevel
	}{
		{"debug", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"error", LevelError},
		{"unknown", LevelInfo},
		{"", LevelInfo},
	}
	for _, tt := range tests {
		if got := ParseLogLevel(tt.input); got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTracerEmitAndEvents(t *testing.T) {
	tracer := NewTracer("run-1")

	tracer.Emit(1, EventPlannerStarted, nil)
	tracer.Emit(1, EventPlannerCompleted, map[string]any{"tokens": 100})
	tracer.Emit(1, EventToolStarted, map[string]any{"tool": "read"})
	tracer.Emit(1, EventToolCompleted, map[string]any{"tool": "read"})

	events := tracer.Events()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Type != EventPlannerStarted {
		t.Errorf("event 0 type = %q, want planner_started", events[0].Type)
	}
	if events[0].RunID != "run-1" {
		t.Errorf("event 0 run_id = %q, want run-1", events[0].RunID)
	}

	// Verify copy.
	events[0].RunID = "mutated"
	if tracer.Events()[0].RunID == "mutated" {
		t.Error("Events() should return a copy")
	}
}

func TestMetricsCounters(t *testing.T) {
	m := NewMetrics()

	m.IncRunsStarted()
	m.IncRunsStarted()
	m.IncRunsCompleted()
	m.IncRunsFailed()
	m.IncToolCalls()
	m.IncToolCalls()
	m.IncToolCalls()
	m.IncToolFailures()
	m.IncLLMCalls()
	m.IncLLMFailures()
	m.AddRunDuration(2 * time.Second)
	m.AddToolDuration(500 * time.Millisecond)
	m.IncStepCount()
	m.IncStepCount()
	m.AddTokens(1000)
	m.AddCost(0.05)

	snap := m.Snapshot()
	if snap.RunsStarted != 2 {
		t.Errorf("RunsStarted = %d, want 2", snap.RunsStarted)
	}
	if snap.RunsCompleted != 1 {
		t.Errorf("RunsCompleted = %d, want 1", snap.RunsCompleted)
	}
	if snap.RunsFailed != 1 {
		t.Errorf("RunsFailed = %d, want 1", snap.RunsFailed)
	}
	if snap.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", snap.ToolCalls)
	}
	if snap.ToolFailures != 1 {
		t.Errorf("ToolFailures = %d, want 1", snap.ToolFailures)
	}
	if snap.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1", snap.LLMCalls)
	}
	if snap.LLMFailures != 1 {
		t.Errorf("LLMFailures = %d, want 1", snap.LLMFailures)
	}
	if snap.RunDuration != 2*time.Second {
		t.Errorf("RunDuration = %v, want 2s", snap.RunDuration)
	}
	if snap.ToolDuration != 500*time.Millisecond {
		t.Errorf("ToolDuration = %v, want 500ms", snap.ToolDuration)
	}
	if snap.StepCount != 2 {
		t.Errorf("StepCount = %d, want 2", snap.StepCount)
	}
	if snap.TotalTokens != 1000 {
		t.Errorf("TotalTokens = %d, want 1000", snap.TotalTokens)
	}
	if snap.TotalCostUSD != 0.05 {
		t.Errorf("TotalCostUSD = %f, want 0.05", snap.TotalCostUSD)
	}
}

func TestObserverStartAndEndRun(t *testing.T) {
	var buf bytes.Buffer
	cfg := testObserverCfg()
	obs := NewObserver(cfg, &buf)

	session := obs.StartRun("run-42", "test task", config.DefaultConfig(), nil)

	// Should have logged the start.
	if !strings.Contains(buf.String(), "run started") {
		t.Error("expected 'run started' log entry")
	}

	session.EndRun(true, "all done", 5, 3*time.Second, 500, 0.02)

	snap := obs.Metrics.Snapshot()
	if snap.RunsStarted != 1 {
		t.Errorf("RunsStarted = %d, want 1", snap.RunsStarted)
	}
	if snap.RunsCompleted != 1 {
		t.Errorf("RunsCompleted = %d, want 1", snap.RunsCompleted)
	}

	events := session.Tracer().Events()
	if len(events) == 0 {
		t.Fatal("expected at least one trace event")
	}
	last := events[len(events)-1]
	if last.Type != EventRunCompleted {
		t.Errorf("last event type = %q, want run_completed", last.Type)
	}
}

func TestObserverPlannerEvents(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)
	session := obs.StartRun("run-1", "test", config.DefaultConfig(), nil)

	session.PlannerStarted(1)
	session.PlannerCompleted(1, 200, 0.01, 500*time.Millisecond)

	snap := obs.Metrics.Snapshot()
	if snap.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1", snap.LLMCalls)
	}
	if snap.TotalTokens != 200 {
		t.Errorf("TotalTokens = %d, want 200", snap.TotalTokens)
	}
}

func TestObserverToolEvents(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)
	session := obs.StartRun("run-1", "test", config.DefaultConfig(), nil)

	session.ToolStarted(1, "read")
	session.ToolCompleted(1, "read", 100*time.Millisecond, "")

	session.ToolStarted(2, "write")
	session.ToolCompleted(2, "write", 50*time.Millisecond, "permission denied")

	snap := obs.Metrics.Snapshot()
	if snap.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", snap.ToolCalls)
	}
	if snap.ToolFailures != 1 {
		t.Errorf("ToolFailures = %d, want 1", snap.ToolFailures)
	}
	if snap.ToolDuration != 150*time.Millisecond {
		t.Errorf("ToolDuration = %v, want 150ms", snap.ToolDuration)
	}
}

func TestConcurrentRunSessions(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	// Start two concurrent sessions from the same observer.
	s1 := obs.StartRun("run-a", "task a", config.DefaultConfig(), nil)
	s2 := obs.StartRun("run-b", "task b", config.DefaultConfig(), nil)

	// Each session has its own tracer.
	s1.PlannerStarted(1)
	s2.PlannerStarted(1)
	s1.PlannerCompleted(1, 100, 0.01, time.Millisecond)
	s2.PlannerCompleted(1, 200, 0.02, time.Millisecond)

	e1 := s1.Tracer().Events()
	e2 := s2.Tracer().Events()
	// 3 events each: run_started + planner_started + planner_completed
	if len(e1) != 3 {
		t.Errorf("session 1 events = %d, want 3", len(e1))
	}
	if len(e2) != 3 {
		t.Errorf("session 2 events = %d, want 3", len(e2))
	}
	if e1[0].RunID != "run-a" {
		t.Errorf("session 1 run_id = %q, want run-a", e1[0].RunID)
	}
	if e2[0].RunID != "run-b" {
		t.Errorf("session 2 run_id = %q, want run-b", e2[0].RunID)
	}

	// Shared metrics should aggregate both sessions.
	snap := obs.Metrics.Snapshot()
	if snap.RunsStarted != 2 {
		t.Errorf("RunsStarted = %d, want 2", snap.RunsStarted)
	}
	if snap.LLMCalls != 2 {
		t.Errorf("LLMCalls = %d, want 2", snap.LLMCalls)
	}
	if snap.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", snap.TotalTokens)
	}
}
