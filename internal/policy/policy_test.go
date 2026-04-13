package policy

import (
	"context"
	"testing"

	"github.com/dshills/matter/pkg/matter"
)

func testTool(name string, safe bool) matter.Tool {
	return matter.Tool{Name: name, Safe: safe}
}

func TestCheckerAllowsSafeTool(t *testing.T) {
	state := &RunState{
		MaxSteps:       10,
		MaxTotalTokens: 1000,
		MaxCostUSD:     1.0,
	}
	checker := NewChecker(state)
	result := checker.CheckToolCall(context.Background(), testTool("read", true), nil)
	if !result.Allowed {
		t.Errorf("safe tool should be allowed: %s", result.Reason)
	}
}

func TestCheckerBlocksDisabledTool(t *testing.T) {
	state := &RunState{
		MaxSteps:       10,
		MaxTotalTokens: 1000,
		MaxCostUSD:     1.0,
		DisabledTools:  map[string]bool{"exec": true},
	}
	checker := NewChecker(state)
	result := checker.CheckToolCall(context.Background(), testTool("exec", false), nil)
	if result.Allowed {
		t.Error("disabled tool should be blocked")
	}
	if result.Reason == "" {
		t.Error("should have a reason")
	}
}

func TestCheckerBlocksExhaustedBudget(t *testing.T) {
	state := &RunState{
		StepsUsed:      10,
		MaxSteps:       10,
		MaxTotalTokens: 1000,
		MaxCostUSD:     1.0,
	}
	checker := NewChecker(state)
	result := checker.CheckToolCall(context.Background(), testTool("write", false), nil)
	if result.Allowed {
		t.Error("should block when step budget exhausted")
	}
}

func TestCheckerBlocksPathTraversal(t *testing.T) {
	state := &RunState{
		MaxSteps:       10,
		MaxTotalTokens: 1000,
		MaxCostUSD:     1.0,
		WorkspaceRoot:  "/tmp/workspace",
	}
	checker := NewChecker(state)
	input := map[string]any{"path": "../../../etc/passwd"}
	result := checker.CheckToolCall(context.Background(), testTool("write", false), input)
	if result.Allowed {
		t.Error("path traversal should be blocked")
	}
}

func TestCheckerAllowsValidPath(t *testing.T) {
	state := &RunState{
		MaxSteps:       10,
		MaxTotalTokens: 1000,
		MaxCostUSD:     1.0,
		WorkspaceRoot:  "/tmp/workspace",
	}
	checker := NewChecker(state)
	input := map[string]any{"path": "src/main.go"}
	result := checker.CheckToolCall(context.Background(), testTool("write", false), input)
	if !result.Allowed {
		t.Errorf("valid relative path should be allowed: %s", result.Reason)
	}
}

func TestBudgetChecks(t *testing.T) {
	tests := []struct {
		name    string
		state   RunState
		allowed bool
	}{
		{"steps ok", RunState{StepsUsed: 5, MaxSteps: 10, MaxTotalTokens: 1000, MaxCostUSD: 1.0}, true},
		{"steps exceeded", RunState{StepsUsed: 10, MaxSteps: 10, MaxTotalTokens: 1000, MaxCostUSD: 1.0}, false},
		{"tokens exceeded", RunState{MaxSteps: 10, TotalTokens: 1000, MaxTotalTokens: 1000, MaxCostUSD: 1.0}, false},
		{"cost exceeded", RunState{MaxSteps: 10, MaxTotalTokens: 1000, CostUSD: 1.0, MaxCostUSD: 1.0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckBudget(&tt.state)
			if result.Allowed != tt.allowed {
				t.Errorf("got allowed=%v, want %v: %s", result.Allowed, tt.allowed, result.Reason)
			}
		})
	}
}

func TestFilesystemCheckVariousKeys(t *testing.T) {
	// Should check all path-like keys.
	for _, key := range []string{"path", "file", "filename", "filepath", "directory", "dir"} {
		input := map[string]any{key: "../../escape"}
		result := CheckFilesystem("/tmp/ws", input)
		if result.Allowed {
			t.Errorf("key %q with traversal should be blocked", key)
		}
	}
}

func TestFilesystemCheckNoWorkspace(t *testing.T) {
	// No workspace root means no filesystem check.
	input := map[string]any{"path": "../../escape"}
	result := CheckFilesystem("", input)
	if !result.Allowed {
		t.Error("should allow anything when workspace is empty")
	}
}
