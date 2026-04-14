package planner

import (
	"context"
	"errors"
	"testing"

	"github.com/dshills/matter/internal/errtype"
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
	if !errors.Is(err, errtype.ErrParse) {
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

func TestParseDecisionAskDirect(t *testing.T) {
	raw := `{"type":"ask","reasoning":"need clarification","ask":{"question":"Which file?","options":["a.txt","b.txt"]}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeAsk {
		t.Errorf("type = %q, want ask", result.Decision.Type)
	}
	if result.Decision.Ask == nil {
		t.Fatal("ask is nil")
	}
	if result.Decision.Ask.Question != "Which file?" {
		t.Errorf("question = %q, want 'Which file?'", result.Decision.Ask.Question)
	}
	if len(result.Decision.Ask.Options) != 2 {
		t.Errorf("options count = %d, want 2", len(result.Decision.Ask.Options))
	}
}

func TestParseDecisionAskWithoutOptions(t *testing.T) {
	raw := `{"type":"ask","reasoning":"ambiguous","ask":{"question":"What do you mean?"}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeAsk {
		t.Errorf("type = %q, want ask", result.Decision.Type)
	}
	if len(result.Decision.Ask.Options) != 0 {
		t.Errorf("options should be empty, got %v", result.Decision.Ask.Options)
	}
}

func TestParseDecisionAskMissingAskField(t *testing.T) {
	raw := `{"type":"ask","reasoning":"test"}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error when ask field is missing for type=ask")
	}
}

func TestParseDecisionAskEmptyQuestion(t *testing.T) {
	raw := `{"type":"ask","reasoning":"test","ask":{"question":""}}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error when ask.question is empty")
	}
}

func TestParseDecisionMultiStepToolCalls(t *testing.T) {
	raw := `{"type":"tool","reasoning":"read files","tool_calls":[{"name":"read","input":{"path":"a.txt"}},{"name":"read","input":{"path":"b.txt"}}]}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Type != matter.DecisionTypeTool {
		t.Errorf("type = %q, want tool", result.Decision.Type)
	}
	if len(result.Decision.ToolCalls) != 2 {
		t.Fatalf("tool_calls count = %d, want 2", len(result.Decision.ToolCalls))
	}
	if result.Decision.ToolCalls[0].Name != "read" {
		t.Errorf("tool_calls[0].name = %q, want read", result.Decision.ToolCalls[0].Name)
	}
	if result.Decision.ToolCalls[1].Name != "read" {
		t.Errorf("tool_calls[1].name = %q, want read", result.Decision.ToolCalls[1].Name)
	}
}

func TestParseDecisionMultiStepEmptyName(t *testing.T) {
	raw := `{"type":"tool","reasoning":"test","tool_calls":[{"name":"read","input":{}},{"name":"","input":{}}]}`
	_, err := ParseDecision(context.Background(), nil, raw)
	if err == nil {
		t.Error("expected error when tool_calls contains empty name")
	}
}

func TestParseDecisionMultiStepTakesPrecedence(t *testing.T) {
	// When both tool_call and tool_calls are set, tool_calls wins.
	raw := `{"type":"tool","reasoning":"test","tool_call":{"name":"ignored","input":{}},"tool_calls":[{"name":"used","input":{}}]}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Decision.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(result.Decision.ToolCalls))
	}
	if result.Decision.ToolCalls[0].Name != "used" {
		t.Errorf("tool_calls[0].name = %q, want used", result.Decision.ToolCalls[0].Name)
	}
}

func TestParseDecisionSingleToolCallStillWorks(t *testing.T) {
	// v1 format with just tool_call should continue to work.
	raw := `{"type":"tool","reasoning":"test","tool_call":{"name":"read","input":{"path":"file.txt"}}}`
	result, err := ParseDecision(context.Background(), nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.ToolCall == nil {
		t.Fatal("expected tool_call to be set")
	}
	if result.Decision.ToolCall.Name != "read" {
		t.Errorf("tool_call.name = %q, want read", result.Decision.ToolCall.Name)
	}
	if len(result.Decision.ToolCalls) != 0 {
		t.Errorf("tool_calls should be empty for v1 format, got %d", len(result.Decision.ToolCalls))
	}
}
