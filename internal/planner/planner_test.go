package planner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

func defaultPlannerConfig() config.PlannerConfig {
	return config.PlannerConfig{
		MaxResponseTokens: 4096,
		Temperature:       0,
	}
}

func mustNewPlanner(t *testing.T, client llm.Client, cfg config.PlannerConfig) *Planner {
	t.Helper()
	p, err := NewPlanner(client, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlannerDecideTool(t *testing.T) {
	resp := llm.Response{
		Content:      `{"type":"tool","reasoning":"need to read file","tool_call":{"name":"read","input":{"path":"main.go"}}}`,
		PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	dec, _, err := p.Decide(context.Background(), "summarize the code", nil, "[]", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != matter.DecisionTypeTool {
		t.Errorf("type = %q, want tool", dec.Type)
	}
	if dec.ToolCall.Name != "read" {
		t.Errorf("tool name = %q, want read", dec.ToolCall.Name)
	}
}

func TestPlannerDecideComplete(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"complete","reasoning":"done","final":{"summary":"the code does X"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	dec, _, err := p.Decide(context.Background(), "summarize", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != matter.DecisionTypeComplete {
		t.Errorf("type = %q, want complete", dec.Type)
	}
	if dec.Final.Summary != "the code does X" {
		t.Errorf("summary = %q", dec.Final.Summary)
	}
}

func TestPlannerDecideFail(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"fail","reasoning":"no tools","final":{"summary":"cannot complete"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	dec, _, err := p.Decide(context.Background(), "do something", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != matter.DecisionTypeFail {
		t.Errorf("type = %q, want fail", dec.Type)
	}
}

func TestPlannerDecideWithRepair(t *testing.T) {
	// LLM returns markdown-fenced JSON — should be repaired by local cleanup.
	resp := llm.Response{
		Content: "```json\n{\"type\":\"complete\",\"reasoning\":\"ok\",\"final\":{\"summary\":\"done\"}}\n```",
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	dec, _, err := p.Decide(context.Background(), "task", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != matter.DecisionTypeComplete {
		t.Errorf("type = %q, want complete", dec.Type)
	}
	// Should only have called LLM once (the main call, not a repair call).
	if mock.CallCount() != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.CallCount())
	}
}

func TestPlannerDecideLLMError(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{{}}, []error{context.DeadlineExceeded})
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	_, _, err := p.Decide(context.Background(), "task", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}

func TestPlannerPromptContainsRequiredSections(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"complete","reasoning":"ok","final":{"summary":"done"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	_, _, err := p.Decide(context.Background(), "my test task", nil, `[{"name":"read"}]`, BudgetInfo{
		StepsUsed: 5, MaxSteps: 20,
		TokensUsed: 1000, MaxTotalTokens: 50000,
		CostUsed: 0.50, MaxCostUSD: 3.0,
		MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	// The system message should contain all required sections.
	systemMsg := reqs[0].Messages[0].Content
	checks := []string{
		"my test task",      // user task
		`[{"name":"read"}]`, // tool schemas
		"5 / 20",            // steps budget
		"1000 / 50000",      // tokens budget
		"$0.5000 / $3.00",   // cost budget
		"Do not invent",     // instruction
		"Do not repeat",     // instruction
		"Complete when",     // instruction
		"Prefer minimal",    // instruction
	}
	for _, check := range checks {
		if !strings.Contains(systemMsg, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestPlannerDecideWithMemoryContext(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"complete","reasoning":"ok","final":{"summary":"done"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	memCtx := []matter.Message{
		{Role: matter.RoleUser, Content: "original task"},
		{Role: matter.RolePlanner, Content: "I'll read the file"},
		{Role: matter.RoleTool, Content: "file contents here"},
	}

	_, _, err := p.Decide(context.Background(), "task", memCtx, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	reqs := mock.Requests()
	// System message + 3 memory context messages = 4 total.
	if len(reqs[0].Messages) != 4 {
		t.Errorf("expected 4 messages (system + 3 context), got %d", len(reqs[0].Messages))
	}
	if reqs[0].Messages[0].Role != matter.RoleSystem {
		t.Errorf("first message should be system, got %q", reqs[0].Messages[0].Role)
	}
}

func TestPlannerMaxResponseTokens(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"complete","reasoning":"ok","final":{"summary":"done"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)

	cfg := defaultPlannerConfig()
	cfg.MaxResponseTokens = 8192
	p := mustNewPlanner(t, mock, cfg)

	_, _, err := p.Decide(context.Background(), "task", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	reqs := mock.Requests()
	if reqs[0].MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", reqs[0].MaxTokens)
	}
}

func TestPlannerTemperature(t *testing.T) {
	resp := llm.Response{
		Content: `{"type":"complete","reasoning":"ok","final":{"summary":"done"}}`,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)

	cfg := defaultPlannerConfig()
	cfg.Temperature = 0.7
	p := mustNewPlanner(t, mock, cfg)

	_, _, err := p.Decide(context.Background(), "task", nil, "", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	reqs := mock.Requests()
	if reqs[0].Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", reqs[0].Temperature)
	}
}
