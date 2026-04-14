package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// mockRunner implements RunnerIface for testing.
type mockRunner struct {
	mu         sync.Mutex
	runResult  matter.RunResult
	runDelay   time.Duration
	progressFn matter.ProgressFunc
	paused     bool
	resumed    bool
	answer     string
	tools      []matter.Tool
}

func (m *mockRunner) Run(_ context.Context, _ matter.RunRequest) matter.RunResult {
	if m.runDelay > 0 {
		time.Sleep(m.runDelay)
	}
	if m.progressFn != nil {
		m.progressFn(matter.ProgressEvent{
			Event:     "run_started",
			Step:      0,
			Timestamp: time.Now(),
		})
	}
	m.mu.Lock()
	result := m.runResult
	m.mu.Unlock()
	if m.progressFn != nil && !result.Paused {
		event := "run_completed"
		if result.Error != nil {
			event = "run_failed"
		}
		m.progressFn(matter.ProgressEvent{
			Event:     event,
			Step:      result.Steps,
			Timestamp: time.Now(),
		})
	}
	return result
}

func (m *mockRunner) Resume(_ context.Context, answer string) matter.RunResult {
	m.mu.Lock()
	m.resumed = true
	m.answer = answer
	result := m.runResult
	m.mu.Unlock()
	if m.progressFn != nil && !result.Paused {
		m.progressFn(matter.ProgressEvent{
			Event:     "run_completed",
			Step:      result.Steps,
			Timestamp: time.Now(),
		})
	}
	return result
}

func (m *mockRunner) IsPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

func (m *mockRunner) SetProgressFunc(fn matter.ProgressFunc) {
	m.progressFn = fn
}

func (m *mockRunner) Tools() []matter.Tool {
	return m.tools
}

func newTestConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Server.MaxConcurrentRuns = 2
	cfg.Server.MaxPausedRuns = 2
	cfg.Server.RunRetention = 1 * time.Hour
	return cfg
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := newTestConfig()
	client := llm.NewMockClient(nil, nil)
	srv := New(cfg, client)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestHealthEndpoint(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if body["version"] != Version {
		t.Errorf("version = %q, want %q", body["version"], Version)
	}
}

func TestAuthRequired(t *testing.T) {
	cfg := newTestConfig()
	cfg.Server.AuthToken = "secret-token"
	client := llm.NewMockClient(nil, nil)
	srv := New(cfg, client)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// No auth header — should get 401.
	resp, err := http.Get(ts.URL + "/api/v1/tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// Wrong token — should get 401.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tools", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// Correct token — should get 200.
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tools", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Health should work without auth.
	resp, err = http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestCreateRunAndGetStatus(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"task":"test task","workspace":"."}`
	resp, err := http.Post(ts.URL+"/api/v1/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}

	var createResp createRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if createResp.RunID == "" {
		t.Error("expected non-empty run_id")
	}
	if createResp.Status != "running" {
		t.Errorf("status = %q, want running", createResp.Status)
	}

	// Wait a moment for the run to complete (mock is fast).
	time.Sleep(200 * time.Millisecond)

	// Get the run status.
	resp2, err := http.Get(ts.URL + "/api/v1/runs/" + createResp.RunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get status = %d, want 200", resp2.StatusCode)
	}
}

func TestCreateRunMissingTask(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"workspace":"."}`
	resp, err := http.Post(ts.URL+"/api/v1/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetRunNotFound(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/runs/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCancelRun(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a run that will stay "running" by using a mock runner directly.
	ctx, cancel := context.WithCancel(context.Background())
	run := &ActiveRun{
		RunID:   "test-cancel",
		Status:  StatusRunning,
		Cancel:  cancel,
		Created: time.Now(),
	}
	_ = srv.store.Add(run)
	_ = ctx // keep context alive

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/runs/test-cancel", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	run.mu.Lock()
	if run.Status != StatusCancelled {
		t.Errorf("status = %q, want cancelled", run.Status)
	}
	run.mu.Unlock()
}

func TestCancelCompletedRun(t *testing.T) {
	srv, ts := newTestServer(t)

	run := &ActiveRun{
		RunID:   "test-done",
		Status:  StatusCompleted,
		Created: time.Now(),
	}
	_ = srv.store.Add(run)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/runs/test-done", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestListTools(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := body["tools"]; !ok {
		t.Error("expected 'tools' key in response")
	}
}

func TestConcurrentRunLimit(t *testing.T) {
	srv, ts := newTestServer(t)

	// Fill up the concurrent run slots.
	for i := range srv.store.maxConcurrent {
		run := &ActiveRun{
			RunID:   fmt.Sprintf("fill-%d", i),
			Status:  StatusRunning,
			Created: time.Now(),
		}
		if err := srv.store.Add(run); err != nil {
			t.Fatalf("failed to add run: %v", err)
		}
	}

	// Next run should be rejected with 429.
	body := `{"task":"should fail","workspace":"."}`
	resp, err := http.Post(ts.URL+"/api/v1/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
}

func TestGarbageCollection(t *testing.T) {
	store := NewRunStore(10, 10, 1*time.Second)

	// Add a completed run that's old enough to be GC'd.
	run := &ActiveRun{
		RunID:   "old-run",
		Status:  StatusCompleted,
		Created: time.Now().Add(-2 * time.Second),
	}
	_ = store.Add(run)

	removed := store.GC(time.Now())
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	if store.Get("old-run") != nil {
		t.Error("expected old run to be removed")
	}
}

func TestGarbageCollectionKeepsRecent(t *testing.T) {
	store := NewRunStore(10, 10, 1*time.Hour)

	run := &ActiveRun{
		RunID:   "recent-run",
		Status:  StatusCompleted,
		Created: time.Now(),
	}
	_ = store.Add(run)

	removed := store.GC(time.Now())
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}

	if store.Get("recent-run") == nil {
		t.Error("expected recent run to still exist")
	}
}

func TestSSEEvents(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a completed run with stored events directly (avoids slow mock runner).
	run := &ActiveRun{
		RunID:   "sse-test",
		Status:  StatusCompleted,
		Created: time.Now(),
		Events: []matter.ProgressEvent{
			{RunID: "sse-test", Event: "run_started", Step: 0, Timestamp: time.Now()},
			{RunID: "sse-test", Event: "run_completed", Step: 1, Timestamp: time.Now()},
		},
		Result: &matter.RunResult{Success: true},
	}
	_ = srv.store.Add(run)

	// Connect to SSE — should get stored events then close (run is completed).
	resp, err := http.Get(ts.URL + "/api/v1/runs/sse-test/events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	data, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(data, []byte("event: run_started")) {
		t.Error("expected run_started event in SSE stream")
	}
	if !bytes.Contains(data, []byte("event: run_completed")) {
		t.Error("expected run_completed event in SSE stream")
	}
}

func TestAnswerPausedRun(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a paused run with a mock runner.
	mock := &mockRunner{
		runResult: matter.RunResult{
			Success: true,
			Steps:   3,
		},
	}

	_, cancel := context.WithCancel(context.Background())
	run := &ActiveRun{
		RunID:  "paused-run",
		Status: StatusPaused,
		Cancel: cancel,
		Result: &matter.RunResult{
			Paused:   true,
			Question: &matter.AskRequest{Question: "What color?"},
		},
		Created: time.Now(),
		Runner:  mock,
	}
	_ = srv.store.Add(run)

	body := `{"answer":"blue"}`
	resp, err := http.Post(ts.URL+"/api/v1/runs/paused-run/answer", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}

	// Wait for resume to complete.
	time.Sleep(200 * time.Millisecond)

	if !mock.resumed {
		t.Error("expected runner to be resumed")
	}
	if mock.answer != "blue" {
		t.Errorf("answer = %q, want blue", mock.answer)
	}
}

func TestAnswerNotPaused(t *testing.T) {
	srv, ts := newTestServer(t)

	run := &ActiveRun{
		RunID:   "running-run",
		Status:  StatusRunning,
		Created: time.Now(),
	}
	_ = srv.store.Add(run)

	body := `{"answer":"hello"}`
	resp, err := http.Post(ts.URL+"/api/v1/runs/running-run/answer", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestRunStatusPaused(t *testing.T) {
	srv, ts := newTestServer(t)

	run := &ActiveRun{
		RunID:  "paused-status",
		Status: StatusPaused,
		Result: &matter.RunResult{
			Paused:   true,
			Question: &matter.AskRequest{Question: "Pick one"},
			Steps:    2,
		},
		Created: time.Now(),
	}
	_ = srv.store.Add(run)

	resp, err := http.Get(ts.URL + "/api/v1/runs/paused-status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var status runStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if status.Status != "paused" {
		t.Errorf("status = %q, want paused", status.Status)
	}
	if status.Question != "Pick one" {
		t.Errorf("question = %q, want 'Pick one'", status.Question)
	}
}

func TestBroadcastTerminalClosesSubscribers(t *testing.T) {
	run := &ActiveRun{
		RunID:   "broadcast-test",
		Status:  StatusRunning,
		Created: time.Now(),
	}

	ch := run.Subscribe()

	// Send a terminal event.
	run.broadcast(matter.ProgressEvent{Event: "run_completed"})

	// Channel should be closed after terminal event.
	select {
	case _, ok := <-ch:
		if ok {
			// Got the event — read one more to confirm close.
			_, ok2 := <-ch
			if ok2 {
				t.Error("expected channel to be closed after terminal event")
			}
		}
		// Channel closed as expected.
	case <-time.After(time.Second):
		t.Error("timed out waiting for channel close")
	}
}
