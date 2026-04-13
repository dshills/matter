package planner

import (
	"context"
	"errors"
	"testing"

	"github.com/dshills/matter/internal/agent"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

func TestParseDecisionToolDirect(t *testing.T) {
	raw := `{"type":"tool","reasoning":"need to read","tool_call":{"name":"read","input":{"path":"file.txt"}}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeTool {
		t.Errorf("type = %q, want tool", result.Decision.Type)
	}
	if result.Decision.ToolCall.Name != "read" {
		t.Errorf("tool name = %q, want read", result.Decision.ToolCall.Name)
	}
	if result.RepairTokens != 0 {
		t.Errorf("repair tokens should be 0 for direct parse, got %d", result.RepairTokens)
	}
}

func TestParseDecisionCompleteDirect(t *testing.T) {
	raw := `{"type":"complete","reasoning":"done","final":{"summary":"task finished"}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeComplete {
		t.Errorf("type = %q, want complete", result.Decision.Type)
	}
	if result.Decision.Final.Summary != "task finished" {
		t.Errorf("summary = %q, want 'task finished'", result.Decision.Final.Summary)
	}
}

func TestParseDecisionFailDirect(t *testing.T) {
	raw := `{"type":"fail","reasoning":"cannot proceed","final":{"summary":"not enough info"}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeFail {
		t.Errorf("type = %q, want fail", result.Decision.Type)
	}
}

func TestParseDecisionMarkdownFences(t *testing.T) {
	raw := "```json\n{\"type\":\"complete\",\"reasoning\":\"ok\",\"final\":{\"summary\":\"done\"}}\n```"
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeComplete {
		t.Errorf("type = %q, want complete", result.Decision.Type)
	}
}

func TestParseDecisionTrailingComma(t *testing.T) {
	raw := `{"type":"complete","reasoning":"ok","final":{"summary":"done",},}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeComplete {
		t.Errorf("type = %q, want complete", result.Decision.Type)
	}
}

func TestParseDecisionUnclosedBraces(t *testing.T) {
	raw := `{"type":"tool","reasoning":"go","tool_call":{"name":"read","input":{"path":"x.txt"}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeTool {
		t.Errorf("type = %q, want tool", result.Decision.Type)
	}
}

func TestParseDecisionLLMCorrection(t *testing.T) {
	// Malformed JSON that local cleanup can't fix.
	raw := `{type: tool, reasoning: need file`

	corrected := `{"type":"tool","reasoning":"need file","tool_call":{"name":"read","input":{"path":"a.txt"}}}`
	mock := llm.NewMockClient([]llm.Response{
		{Content: corrected, TotalTokens: 100, EstimatedCostUSD: 0.001},
	}, nil)

	result, err := ParseDecision(context.Background(), mock, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeTool {
		t.Errorf("type = %q, want tool", result.Decision.Type)
	}
	if result.RepairTokens != 100 {
		t.Errorf("repair tokens = %d, want 100", result.RepairTokens)
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM should be called exactly once, got %d", mock.CallCount())
	}
}

func TestParseDecisionLLMCorrectionFails(t *testing.T) {
	raw := `{completely broken`
	mock := llm.NewMockClient([]llm.Response{{}}, []error{context.DeadlineExceeded})

	_, err := ParseDecision(context.Background(), mock, raw)
	if err == nil {
		t.Error("expected error when LLM correction fails")
	}
	if !errors.Is(err, agent.ErrPlanner) {
		t.Errorf("expected planner error, got %T", err)
	}
}

func TestParseDecisionLLMCorrectionInvalidJSON(t *testing.T) {
	raw := `{broken`
	mock := llm.NewMockClient([]llm.Response{
		{Content: "still broken json"},
	}, nil)

	_, err := ParseDecision(context.Background(), mock, raw)
	if err == nil {
		t.Error("expected error when LLM correction returns bad JSON")
	}
	if !errors.Is(err, agent.ErrParse) {
		t.Errorf("expected parse error, got %T", err)
	}
}

func TestParseDecisionNoLLMClient(t *testing.T) {
	raw := `{completely broken`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error with no LLM client for correction")
	}
}

func TestParseDecisionInvalidType(t *testing.T) {
	raw := `{"type":"unknown","reasoning":"test"}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		// Direct parse succeeds but validation fails, falls through to cleanup
		// which also fails validation, then falls to LLM which is nil → error.
		// This is expected.
		return
	}
	t.Error("invalid type should fail validation")
}

func TestParseDecisionMissingToolCall(t *testing.T) {
	raw := `{"type":"tool","reasoning":"test"}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error when tool_call is missing for type=tool")
	}
}

func TestParseDecisionMissingFinal(t *testing.T) {
	raw := `{"type":"complete","reasoning":"test"}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error when final is missing for type=complete")
	}
}
