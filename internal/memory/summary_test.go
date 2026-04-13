package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

func TestSummarize(t *testing.T) {
	messages := []matter.Message{
		{Role: matter.RoleUser, Content: "What is the weather?"},
		{Role: matter.RolePlanner, Content: "I'll check using the weather tool."},
		{Role: matter.RoleTool, Content: "Temperature: 72F, Sunny"},
	}

	resp := llm.Response{
		Content:          "User asked about weather. Tool returned 72F, Sunny.",
		TotalTokens:      150,
		EstimatedCostUSD: 0.002,
	}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)

	summary, usage, err := Summarize(context.Background(), mock, "test-model", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Summary should be a system message with [Context Summary] prefix.
	if summary.Role != matter.RoleSystem {
		t.Errorf("summary role = %q, want %q", summary.Role, matter.RoleSystem)
	}
	if !strings.HasPrefix(summary.Content, "[Context Summary]") {
		t.Errorf("summary should start with [Context Summary], got %q", summary.Content)
	}
	if !strings.Contains(summary.Content, resp.Content) {
		t.Error("summary should contain the LLM response content")
	}

	// Usage should be populated.
	if usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", usage.TotalTokens)
	}
	if usage.CostUSD != 0.002 {
		t.Errorf("CostUSD = %f, want 0.002", usage.CostUSD)
	}

	// Verify the request sent to the LLM.
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Model != "test-model" {
		t.Errorf("model = %q, want %q", req.Model, "test-model")
	}
	if len(req.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != matter.RoleSystem {
		t.Error("first request message should be system (the summarization prompt)")
	}
	// The user message should contain all three original messages.
	userContent := req.Messages[1].Content
	if !strings.Contains(userContent, "weather") {
		t.Error("request should contain original message content")
	}
	if !strings.Contains(userContent, "72F") {
		t.Error("request should contain tool output")
	}
}

func TestSummarizeLLMError(t *testing.T) {
	mock := llm.NewMockClient(
		[]llm.Response{{}},
		[]error{context.DeadlineExceeded},
	)

	_, _, err := Summarize(context.Background(), mock, "test-model", []matter.Message{
		{Role: matter.RoleUser, Content: "test"},
	})
	if err == nil {
		t.Error("expected error when LLM fails")
	}
	if !strings.Contains(err.Error(), "summarization") {
		t.Errorf("error should mention summarization: %v", err)
	}
}

func TestSummarizeMessageFormatting(t *testing.T) {
	messages := []matter.Message{
		{Role: matter.RoleUser, Content: "hello"},
		{Role: matter.RolePlanner, Content: "world"},
	}

	resp := llm.Response{Content: "summary"}
	mock := llm.NewMockClient([]llm.Response{resp}, nil)

	if _, _, err := Summarize(context.Background(), mock, "model", messages); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqs := mock.Requests()
	userContent := reqs[0].Messages[1].Content

	// Each message should be formatted as [role] content.
	if !strings.Contains(userContent, "[user] hello") {
		t.Errorf("should format as [role] content, got %q", userContent)
	}
	if !strings.Contains(userContent, "[assistant] world") {
		t.Errorf("should format as [role] content, got %q", userContent)
	}
}
