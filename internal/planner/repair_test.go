package planner

import (
	"context"
	"testing"

	"github.com/dshills/matter/internal/llm"
)

func TestLocalCleanupStripFences(t *testing.T) {
	input := "```json\n{\"type\":\"complete\"}\n```"
	got := localCleanup(input)
	want := `{"type":"complete"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupStripFencesNoLang(t *testing.T) {
	input := "```\n{\"type\":\"tool\"}\n```"
	got := localCleanup(input)
	want := `{"type":"tool"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupTrailingCommas(t *testing.T) {
	input := `{"type":"tool","reasoning":"test",}`
	got := localCleanup(input)
	want := `{"type":"tool","reasoning":"test"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupTrailingCommaInArray(t *testing.T) {
	input := `[1, 2, 3,]`
	got := localCleanup(input)
	want := `[1, 2, 3]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupMissingBraces(t *testing.T) {
	input := `{"type":"tool","tool_call":{"name":"read"`
	got := localCleanup(input)
	want := `{"type":"tool","tool_call":{"name":"read"}}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupMissingBrackets(t *testing.T) {
	input := `[{"type":"tool"}`
	got := localCleanup(input)
	want := `[{"type":"tool"}]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupTruncatedTrailingComma(t *testing.T) {
	// Truncated input with trailing comma: close first, then fix comma.
	input := `{"key": "val",`
	got := localCleanup(input)
	want := `{"key": "val"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupTrimWhitespace(t *testing.T) {
	input := "  \n  {\"type\":\"complete\"}  \n  "
	got := localCleanup(input)
	want := `{"type":"complete"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupCombined(t *testing.T) {
	input := "```json\n{\"type\":\"tool\",\"reasoning\":\"test\",\"tool_call\":{\"name\":\"read\",}\n```"
	got := localCleanup(input)
	want := `{"type":"tool","reasoning":"test","tool_call":{"name":"read"}}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocalCleanupValidJSON(t *testing.T) {
	input := `{"type":"complete","final":{"summary":"done"}}`
	got := localCleanup(input)
	if got != input {
		t.Errorf("valid JSON should pass through unchanged: got %q", got)
	}
}

func TestFixTrailingCommasRespectsStrings(t *testing.T) {
	// Comma before ] inside a string should NOT be removed.
	input := `{"data":"a,]b","list":[1,2,]}`
	got := fixTrailingCommas(input)
	want := `{"data":"a,]b","list":[1,2]}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloseDelimitersRespectStrings(t *testing.T) {
	// Braces inside strings should not be counted.
	input := `{"content":"value with { brace"` // 1 unmatched {
	got := closeDelimiters(input)
	want := `{"content":"value with { brace"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloseDelimitersLIFOOrder(t *testing.T) {
	input := `[{"name":"read"` // need } then ]
	got := closeDelimiters(input)
	want := `[{"name":"read"}]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloseDelimitersUnterminatedString(t *testing.T) {
	input := `{"key":"val` // unterminated string + unclosed brace
	got := closeDelimiters(input)
	want := `{"key":"val"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLLMCorrection(t *testing.T) {
	corrected := `{"type":"complete","final":{"summary":"done"}}`
	mock := llm.NewMockClient([]llm.Response{
		{Content: corrected, PromptTokens: 50, CompletionTokens: 20},
	}, nil)

	result, resp, err := llmCorrection(context.Background(), mock, "bad json")
	if err != nil {
		t.Fatal(err)
	}
	if result != corrected {
		t.Errorf("got %q, want %q", result, corrected)
	}
	if resp.PromptTokens != 50 {
		t.Errorf("prompt tokens = %d, want 50", resp.PromptTokens)
	}
	if mock.CallCount() != 1 {
		t.Errorf("should call LLM exactly once, got %d", mock.CallCount())
	}
}

func TestLLMCorrectionError(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{{}}, []error{context.DeadlineExceeded})

	_, _, err := llmCorrection(context.Background(), mock, "bad json")
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}
