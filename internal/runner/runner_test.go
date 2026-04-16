package runner

import (
	"context"
	"testing"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

func mockClient() llm.Client {
	return llm.NewMockClient(nil, nil)
}

func TestNewWithMockClient(t *testing.T) {
	cfg := config.DefaultConfig()
	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if r == nil {
		t.Fatal("New() returned nil runner")
	}
}

func TestNewNilClientReturnsError(t *testing.T) {
	cfg := config.DefaultConfig()
	_, err := New(cfg, nil)
	if err == nil {
		t.Error("expected error for nil llm client, got nil")
	}
}

func TestNewInvalidConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 0 // invalid
	_, err := New(cfg, mockClient())
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestNewRegistersDefaultTools(t *testing.T) {
	cfg := config.DefaultConfig()
	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	toolList := r.Tools()
	names := make(map[string]bool)
	for _, tool := range toolList {
		names[tool.Name] = true
	}

	if !names["workspace_read"] {
		t.Error("expected workspace_read tool to be registered")
	}
	if !names["workspace_write"] {
		t.Error("expected workspace_write tool to be registered")
	}
	if !names["web_fetch"] {
		t.Error("expected web_fetch tool to be registered")
	}
	if names["command_exec"] {
		t.Error("expected command_exec to NOT be registered by default")
	}
}

func TestNewWithCommandExecEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.EnableCommandExec = true
	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	toolList := r.Tools()
	found := false
	for _, tool := range toolList {
		if tool.Name == "command_exec" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected command_exec tool when enabled in config")
	}
}

func TestNewWithAllToolsDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.EnableWorkspaceRead = false
	cfg.Tools.EnableWorkspaceWrite = false
	cfg.Tools.EnableWebFetch = false
	cfg.Tools.EnableCommandExec = false
	cfg.Tools.EnableWorkspaceFind = false
	cfg.Tools.EnableWorkspaceGrep = false

	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if len(r.Tools()) != 0 {
		t.Errorf("expected 0 tools, got %d", len(r.Tools()))
	}
}

func TestRunWithMockClient(t *testing.T) {
	cfg := config.DefaultConfig()

	// Mock returns a "complete" decision so the run terminates.
	completeResp := llm.Response{
		Content:      `{"type":"complete","reasoning":"done","final":{"summary":"test complete"}}`,
		TotalTokens:  10,
		PromptTokens: 5,
	}
	mock := llm.NewMockClient([]llm.Response{completeResp}, nil)

	r, err := New(cfg, mock)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result := r.Run(context.Background(), matter.RunRequest{
		Task:      "say hello",
		Workspace: t.TempDir(),
	})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.FinalSummary != "test complete" {
		t.Errorf("summary = %q, want %q", result.FinalSummary, "test complete")
	}
}

func TestRunWithCancelledContext(t *testing.T) {
	cfg := config.DefaultConfig()
	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result := r.Run(ctx, matter.RunRequest{
		Task:      "should cancel",
		Workspace: t.TempDir(),
	})

	if result.Success {
		t.Error("expected failure for cancelled context")
	}
}

func TestResumeNotPaused(t *testing.T) {
	cfg := config.DefaultConfig()
	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result := r.Resume(context.Background(), "some answer")
	if result.Error == nil {
		t.Error("expected ErrNotPaused error")
	}
	if result.Error != ErrNotPaused {
		t.Errorf("error = %v, want ErrNotPaused", result.Error)
	}
}

func TestRunWhilePaused(t *testing.T) {
	cfg := config.DefaultConfig()

	// Mock returns an ask decision to pause the run.
	askResp := llm.Response{
		Content:     `{"type":"ask","reasoning":"need info","ask":{"question":"Which one?"}}`,
		TotalTokens: 10,
	}
	mock := llm.NewMockClient([]llm.Response{askResp}, nil)

	r, err := New(cfg, mock)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result := r.Run(context.Background(), matter.RunRequest{
		Task:      "ambiguous",
		Workspace: t.TempDir(),
	})

	if !result.Paused {
		t.Fatal("expected paused run")
	}
	if !r.IsPaused() {
		t.Error("IsPaused() should be true")
	}

	// Try to start a new run while paused.
	result2 := r.Run(context.Background(), matter.RunRequest{
		Task:      "new task",
		Workspace: t.TempDir(),
	})
	if result2.Error == nil || result2.Error != ErrRunWhilePaused {
		t.Errorf("expected ErrRunWhilePaused, got %v", result2.Error)
	}
}

func TestPauseResumeComplete(t *testing.T) {
	cfg := config.DefaultConfig()

	askResp := llm.Response{
		Content:     `{"type":"ask","reasoning":"need info","ask":{"question":"Which one?","options":["A","B"]}}`,
		TotalTokens: 10,
	}
	completeResp := llm.Response{
		Content:     `{"type":"complete","reasoning":"done","final":{"summary":"used A"}}`,
		TotalTokens: 10,
	}
	mock := llm.NewMockClient([]llm.Response{askResp, completeResp}, nil)

	r, err := New(cfg, mock)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result := r.Run(context.Background(), matter.RunRequest{
		Task:      "task",
		Workspace: t.TempDir(),
	})

	if !result.Paused {
		t.Fatal("expected paused")
	}

	// Resume with answer.
	result = r.Resume(context.Background(), "A")

	if result.Paused {
		t.Error("expected completed, not paused")
	}
	if !result.Success {
		t.Errorf("expected success, got: %v", result.Error)
	}
	if result.FinalSummary != "used A" {
		t.Errorf("summary = %q, want 'used A'", result.FinalSummary)
	}
	if r.IsPaused() {
		t.Error("IsPaused() should be false after completion")
	}
}

func TestConfigReturnsEffectiveConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 42

	r, err := New(cfg, mockClient())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if r.Config().Agent.MaxSteps != 42 {
		t.Errorf("config max_steps = %d, want 42", r.Config().Agent.MaxSteps)
	}
}
