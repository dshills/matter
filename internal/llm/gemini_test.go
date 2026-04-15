package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/errtype"
	"github.com/dshills/matter/pkg/matter"
)

func TestGeminiClientSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "gemini-2.5-flash:generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("missing or wrong API key header: %s", r.Header.Get("x-goog-api-key"))
		}

		var req geminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if len(req.Contents) == 0 {
			t.Fatal("expected at least one content entry")
		}
		if req.SystemInstruct == nil {
			t.Fatal("expected system instruction")
		}
		if req.SystemInstruct.Parts[0].Text != "Be helpful." {
			t.Errorf("system = %q, want 'Be helpful.'", req.SystemInstruct.Parts[0].Text)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Hello from Gemini!"}},
				},
			}},
			UsageMetadata: geminiUsageMetadata{
				PromptTokenCount:     12,
				CandidatesTokenCount: 8,
				TotalTokenCount:      20,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newGeminiClient(ProviderConfig{
		Provider: "gemini",
		Model:    "gemini-2.5-flash",
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "Be helpful."},
			{Role: matter.RoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "Hello from Gemini!" {
		t.Errorf("content = %q, want 'Hello from Gemini!'", resp.Content)
	}
	if resp.PromptTokens != 12 {
		t.Errorf("prompt tokens = %d, want 12", resp.PromptTokens)
	}
	if resp.CompletionTokens != 8 {
		t.Errorf("completion tokens = %d, want 8", resp.CompletionTokens)
	}
	if resp.TotalTokens != 20 {
		t.Errorf("total tokens = %d, want 20", resp.TotalTokens)
	}
	if resp.Provider != "gemini" {
		t.Errorf("provider = %q, want gemini", resp.Provider)
	}
	if resp.Latency <= 0 {
		t.Error("latency should be positive")
	}
}

func TestGeminiClientToolRoleMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// Check that tool results appear as user role with prefix.
		for _, c := range req.Contents {
			for _, p := range c.Parts {
				if strings.HasPrefix(p.Text, "[Tool Result]") && c.Role != "user" {
					t.Errorf("tool message role = %q, want user", c.Role)
				}
			}
		}

		// Check that planner messages map to "model".
		for _, c := range req.Contents {
			for _, p := range c.Parts {
				if p.Text == "I'll help" && c.Role != "model" {
					t.Errorf("planner message role = %q, want model", c.Role)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		resp := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{Parts: []geminiPart{{Text: "ok"}}},
			}},
			UsageMetadata: geminiUsageMetadata{TotalTokenCount: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "key", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: "hello"},
			{Role: matter.RolePlanner, Content: "I'll help"},
			{Role: matter.RoleTool, Content: "file contents"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeminiClientError401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","status":"UNAUTHENTICATED","code":401}}`))
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "bad", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassTerminal {
		t.Errorf("expected ClassTerminal, got %d", agentErr.Classification)
	}
}

func TestGeminiClientError429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","code":429}}`))
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "key", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable, got %d", agentErr.Classification)
	}
}

func TestGeminiClientError500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "key", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable, got %d", agentErr.Classification)
	}
}

func TestGeminiClientMissingAPIKey(t *testing.T) {
	_, err := newGeminiClient(ProviderConfig{
		Provider: "gemini",
		Model:    "gemini-2.5-flash",
		APIKey:   "",
	})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestGeminiClientEmptyCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := geminiResponse{
			Candidates:    []geminiCandidate{},
			UsageMetadata: geminiUsageMetadata{TotalTokenCount: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "key", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

func TestGeminiClientViaProviderFactory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{Parts: []geminiPart{{Text: "factory ok"}}},
			}},
			UsageMetadata: geminiUsageMetadata{TotalTokenCount: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ProviderConfig{
		Provider: "gemini",
		APIKey:   "key",
		Model:    "gemini-2.5-flash",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "factory ok" {
		t.Errorf("content = %q, want 'factory ok'", resp.Content)
	}
}

func TestGeminiMergeConsecutiveRoles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// Two consecutive user messages + tool result should be merged.
		// Input: user, user, model, user(tool) — should become: user(merged), model, user(tool)
		if len(req.Contents) != 3 {
			t.Errorf("expected 3 contents after merge, got %d", len(req.Contents))
		}

		w.Header().Set("Content-Type", "application/json")
		resp := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{Parts: []geminiPart{{Text: "ok"}}},
			}},
			UsageMetadata: geminiUsageMetadata{TotalTokenCount: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newGeminiClient(ProviderConfig{
		APIKey: "key", Model: "gemini-2.5-flash", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: "first"},
			{Role: matter.RoleUser, Content: "second"},
			{Role: matter.RolePlanner, Content: "planning"},
			{Role: matter.RoleTool, Content: "result"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
