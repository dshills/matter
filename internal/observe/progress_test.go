package observe

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/pkg/matter"
)

func TestProgressCallbackInvokedForAllEvents(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	var mu sync.Mutex
	var events []string

	fn := func(e matter.ProgressEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e.Event)
	}

	session := obs.StartRun("run-progress", "test task", config.DefaultConfig(), fn)

	// StartRun emits run_started internally, so it's already captured.
	session.PlannerStarted(1)
	session.PlannerCompleted(1, 100, 0.01, 500*time.Millisecond)
	session.ToolStarted(1, "read")
	session.ToolCompleted(1, "read", 100*time.Millisecond, "")
	session.LimitExceeded(1, "max_steps", "step limit reached")
	session.EndRun(true, "done", 1, time.Second, 100, 0.01)

	mu.Lock()
	defer mu.Unlock()

	expected := []string{
		"run_started",
		"planner_started",
		"planner_completed",
		"tool_started",
		"tool_completed",
		"limit_exceeded",
		"run_completed",
	}

	if len(events) != len(expected) {
		t.Fatalf("got %d events, want %d: %v", len(events), len(expected), events)
	}
	for i, want := range expected {
		if events[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i], want)
		}
	}
}

func TestProgressCallbackReceivesCorrectData(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	var captured []matter.ProgressEvent

	fn := func(e matter.ProgressEvent) {
		captured = append(captured, e)
	}

	session := obs.StartRun("run-data", "my task", config.DefaultConfig(), fn)
	session.ToolStarted(3, "workspace_read")
	session.ToolCompleted(3, "workspace_read", 200*time.Millisecond, "some error")

	// Check run_started event.
	if captured[0].RunID != "run-data" {
		t.Errorf("run_started RunID = %q, want run-data", captured[0].RunID)
	}
	if captured[0].Data["task"] != "my task" {
		t.Errorf("run_started task = %v, want 'my task'", captured[0].Data["task"])
	}
	if captured[0].Timestamp.IsZero() {
		t.Error("run_started timestamp should not be zero")
	}

	// Check tool_started event.
	toolStarted := captured[1]
	if toolStarted.Step != 3 {
		t.Errorf("tool_started step = %d, want 3", toolStarted.Step)
	}
	if toolStarted.Data["tool"] != "workspace_read" {
		t.Errorf("tool_started tool = %v, want workspace_read", toolStarted.Data["tool"])
	}

	// Check tool_completed event.
	toolCompleted := captured[2]
	if toolCompleted.Data["error"] != "some error" {
		t.Errorf("tool_completed error = %v, want 'some error'", toolCompleted.Data["error"])
	}
	if toolCompleted.Data["duration"] != "200ms" {
		t.Errorf("tool_completed duration = %v, want '200ms'", toolCompleted.Data["duration"])
	}
}

func TestProgressCallbackNilSafe(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	// Nil callback should not panic or cause issues.
	session := obs.StartRun("run-nil", "test task", config.DefaultConfig(), nil)
	session.PlannerStarted(1)
	session.PlannerCompleted(1, 100, 0.01, time.Millisecond)
	session.ToolStarted(1, "read")
	session.ToolCompleted(1, "read", time.Millisecond, "")
	session.LimitExceeded(1, "max_steps", "done")
	session.EndRun(true, "done", 1, time.Second, 100, 0.01)

	// If we got here without panicking, the test passes.
}

func TestProgressCallbackPanicRecovery(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	callCount := 0
	fn := func(e matter.ProgressEvent) {
		callCount++
		if e.Event == "planner_started" {
			panic("callback panic!")
		}
	}

	session := obs.StartRun("run-panic", "test", config.DefaultConfig(), fn)
	// run_started callback should have fired (callCount=1).
	session.PlannerStarted(1) // This panics but should be recovered.
	session.PlannerCompleted(1, 100, 0.01, time.Millisecond)

	// All three callbacks should have been invoked despite the panic.
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3 (panic should not prevent subsequent callbacks)", callCount)
	}

	// Panic should be logged.
	if !strings.Contains(buf.String(), "callback panicked") {
		t.Error("expected panic to be logged")
	}
}

func TestProgressCallbackErrorDoesNotAffectRun(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	fn := func(e matter.ProgressEvent) {
		panic("every callback panics")
	}

	session := obs.StartRun("run-error", "test", config.DefaultConfig(), fn)
	session.PlannerStarted(1)
	session.PlannerCompleted(1, 100, 0.01, time.Millisecond)
	session.ToolStarted(1, "read")
	session.ToolCompleted(1, "read", time.Millisecond, "")
	session.EndRun(true, "done", 1, time.Second, 100, 0.01)

	// Verify metrics are still correct despite panicking callbacks.
	snap := obs.Metrics.Snapshot()
	if snap.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1", snap.LLMCalls)
	}
	if snap.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", snap.ToolCalls)
	}
	if snap.RunsCompleted != 1 {
		t.Errorf("RunsCompleted = %d, want 1", snap.RunsCompleted)
	}
}

func TestProgressEventsMatchTracerEvents(t *testing.T) {
	var buf bytes.Buffer
	obs := NewObserver(testObserverCfg(), &buf)

	var progressEvents []string
	fn := func(e matter.ProgressEvent) {
		progressEvents = append(progressEvents, e.Event)
	}

	session := obs.StartRun("run-match", "test", config.DefaultConfig(), fn)
	session.PlannerStarted(1)
	session.PlannerCompleted(1, 100, 0.01, time.Millisecond)
	session.ToolStarted(1, "read")
	session.ToolCompleted(1, "read", time.Millisecond, "")
	session.EndRun(true, "done", 1, time.Second, 100, 0.01)

	traceEvents := session.Tracer().Events()

	// Progress events should match tracer events one-to-one.
	if len(progressEvents) != len(traceEvents) {
		t.Fatalf("progress events (%d) != trace events (%d)", len(progressEvents), len(traceEvents))
	}

	for i, pe := range progressEvents {
		te := string(traceEvents[i].Type)
		if pe != te {
			t.Errorf("event[%d]: progress=%q, tracer=%q", i, pe, te)
		}
	}
}
