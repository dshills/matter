package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/errtype"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/policy"
	"github.com/dshills/matter/internal/tools"
	"github.com/dshills/matter/pkg/matter"
)

func testConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 10
	cfg.Agent.MaxDuration = 5 * time.Second
	cfg.Agent.MaxTotalTokens = 100000
	cfg.Agent.MaxCostUSD = 10.0
	cfg.Agent.MaxConsecutiveErrors = 3
	cfg.Agent.MaxRepeatedToolCalls = 2
	cfg.Agent.MaxConsecutiveNoProgress = 3
	cfg.Memory.SummarizeAfterMessages = 100 // high to avoid triggering
	cfg.Memory.SummarizeAfterTokens = 100000
	return cfg
}

func completeDecision(summary string) string {
	d := matter.Decision{
		Type:      matter.DecisionTypeComplete,
		Reasoning: "done",
		Final:     &matter.FinalAnswer{Summary: summary},
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func failDecision(summary string) string {
	d := matter.Decision{
		Type:      matter.DecisionTypeFail,
		Reasoning: "cannot proceed",
		Final:     &matter.FinalAnswer{Summary: summary},
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func toolDecision(name string, input map[string]any) string {
	d := matter.Decision{
		Type:      matter.DecisionTypeTool,
		Reasoning: "need to call tool",
		ToolCall:  &matter.ToolCall{Name: name, Input: input},
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func echoTool() matter.Tool {
	return matter.Tool{
		Name:        "echo",
		Description: "echoes input",
		InputSchema: []byte(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		Timeout:     5 * time.Second,
		Safe:        true,
		Execute: func(_ context.Context, input map[string]any) (matter.ToolResult, error) {
			msg, _ := input["msg"].(string)
			return matter.ToolResult{Output: "echo: " + msg}, nil
		},
	}
}

func failingTool(fatal bool) matter.Tool {
	return matter.Tool{
		Name:         "fail_tool",
		Description:  "always fails",
		InputSchema:  []byte(`{"type":"object"}`),
		Timeout:      5 * time.Second,
		Safe:         true,
		FatalOnError: fatal,
		Execute: func(_ context.Context, _ map[string]any) (matter.ToolResult, error) {
			return matter.ToolResult{Error: "tool broke"}, errors.New("tool broke")
		},
	}
}

func setupAgent(cfg config.Config, mockClient *llm.MockClient, toolList ...matter.Tool) *Agent {
	reg := tools.NewRegistry()
	for _, t := range toolList {
		if err := reg.Register(t); err != nil {
			panic(err)
		}
	}
	policyState := &policy.RunState{
		MaxSteps:       cfg.Agent.MaxSteps,
		MaxTotalTokens: cfg.Agent.MaxTotalTokens,
		MaxCostUSD:     cfg.Agent.MaxCostUSD,
	}
	checker := policy.NewChecker(policyState)
	return New(cfg, mockClient, reg, checker)
}

// TestAgentCompletes verifies the agent terminates on a complete decision.
func TestAgentCompletes(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: completeDecision("task done"), TotalTokens: 100},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "say hello"})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.FinalSummary != "task done" {
		t.Errorf("summary = %q, want %q", result.FinalSummary, "task done")
	}
	if result.Steps != 1 {
		t.Errorf("steps = %d, want 1", result.Steps)
	}
}

// TestAgentFails verifies the agent terminates on a fail decision.
func TestAgentFails(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: failDecision("not enough info"), TotalTokens: 50},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "impossible task"})

	if result.Success {
		t.Error("expected failure")
	}
	if result.FinalSummary != "not enough info" {
		t.Errorf("summary = %q, want %q", result.FinalSummary, "not enough info")
	}
}

// TestAgentToolCallThenComplete verifies a multi-step run.
func TestAgentToolCallThenComplete(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: toolDecision("echo", map[string]any{"msg": "hello"}), TotalTokens: 100},
		{Content: completeDecision("echoed hello"), TotalTokens: 80},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "echo hello"})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.Steps != 2 {
		t.Errorf("steps = %d, want 2", result.Steps)
	}
	if result.TotalTokens != 180 {
		t.Errorf("total tokens = %d, want 180", result.TotalTokens)
	}
}

// TestAgentStepLimitExceeded verifies max_steps enforcement.
func TestAgentStepLimitExceeded(t *testing.T) {
	cfg := testConfig()
	cfg.Agent.MaxSteps = 2

	// Return tool calls forever.
	resps := make([]llm.Response, 5)
	for i := range resps {
		resps[i] = llm.Response{Content: toolDecision("echo", map[string]any{"msg": "hi"}), TotalTokens: 10}
	}
	mock := llm.NewMockClient(resps, nil)

	a := setupAgent(cfg, mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "loop"})

	if result.Success {
		t.Error("expected failure due to step limit")
	}
	if !errors.Is(result.Error, errtype.ErrLimitExceeded) {
		t.Errorf("expected limit_exceeded_error, got %v", result.Error)
	}
}

// TestAgentRecoverableToolError verifies that non-fatal tool errors are returned
// to memory for replanning.
func TestAgentRecoverableToolError(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: toolDecision("fail_tool", map[string]any{}), TotalTokens: 50},
		{Content: completeDecision("recovered"), TotalTokens: 50},
	}, nil)

	a := setupAgent(testConfig(), mock, failingTool(false))
	result := a.Run(context.Background(), matter.RunRequest{Task: "try and recover"})

	if !result.Success {
		t.Errorf("expected recovery, got error: %v", result.Error)
	}
	if result.Steps != 2 {
		t.Errorf("steps = %d, want 2", result.Steps)
	}
}

// TestAgentFatalToolError verifies that fatal tool errors terminate the run.
func TestAgentFatalToolError(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: toolDecision("fail_tool", map[string]any{}), TotalTokens: 50},
	}, nil)

	a := setupAgent(testConfig(), mock, failingTool(true))
	result := a.Run(context.Background(), matter.RunRequest{Task: "will crash"})

	if result.Success {
		t.Error("expected failure from fatal tool error")
	}
	if !errors.Is(result.Error, errtype.ErrToolExecution) {
		t.Errorf("expected tool_execution_error, got %v", result.Error)
	}
}

// TestAgentContextCancellation verifies the agent stops on context cancellation.
func TestAgentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mock := llm.NewMockClient([]llm.Response{
		{Content: completeDecision("done")},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(ctx, matter.RunRequest{Task: "cancelled"})

	if result.Success {
		t.Error("expected failure from cancelled context")
	}
}

// TestAgentUnknownTool verifies that calling an unknown tool is recoverable.
func TestAgentUnknownTool(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: toolDecision("nonexistent", map[string]any{}), TotalTokens: 50},
		{Content: completeDecision("recovered"), TotalTokens: 50},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "try unknown"})

	if !result.Success {
		t.Errorf("expected recovery from unknown tool, got error: %v", result.Error)
	}
}

// TestAgentTokenTracking verifies cumulative token accounting.
func TestAgentTokenTracking(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: toolDecision("echo", map[string]any{"msg": "a"}), PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, EstimatedCostUSD: 0.01},
		{Content: completeDecision("done"), PromptTokens: 80, CompletionTokens: 30, TotalTokens: 110, EstimatedCostUSD: 0.005},
	}, nil)

	a := setupAgent(testConfig(), mock, echoTool())
	result := a.Run(context.Background(), matter.RunRequest{Task: "track tokens"})

	if !result.Success {
		t.Fatalf("expected success: %v", result.Error)
	}
	if result.TotalTokens != 260 {
		t.Errorf("total tokens = %d, want 260", result.TotalTokens)
	}
	if result.TotalCostUSD != 0.015 {
		t.Errorf("total cost = %f, want 0.015", result.TotalCostUSD)
	}
}
