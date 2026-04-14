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

func TestOpenAIClientSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "matter/") {
			t.Errorf("unexpected user-agent: %s", r.Header.Get("User-Agent"))
		}

		// Verify request body.
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("first message role = %q, want system", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "user" {
			t.Errorf("second message role = %q, want user", req.Messages[1].Role)
		}
		if req.MaxTokens == nil || *req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %v, want 4096", req.MaxTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "Hello!"}}},
			Usage:   openaiUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newOpenAIClient(ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "You are helpful."},
			{Role: matter.RoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "Hello!" {
		t.Errorf("content = %q, want Hello!", resp.Content)
	}
	if resp.PromptTokens != 10 {
		t.Errorf("prompt tokens = %d, want 10", resp.PromptTokens)
	}
	if resp.CompletionTokens != 5 {
		t.Errorf("completion tokens = %d, want 5", resp.CompletionTokens)
	}
	if resp.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", resp.TotalTokens)
	}
	if resp.Provider != "openai" {
		t.Errorf("provider = %q, want openai", resp.Provider)
	}
	if resp.Latency <= 0 {
		t.Error("latency should be positive")
	}
}

func TestOpenAIClientToolRoleMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// Tool messages should be mapped to "user" with "[Tool Result] " prefix.
		for _, msg := range req.Messages {
			if strings.HasPrefix(msg.Content, "[Tool Result]") {
				if msg.Role != "user" {
					t.Errorf("tool message role = %q, want user", msg.Role)
				}
			}
		}

		// Assistant (planner) messages should map to "assistant".
		for _, msg := range req.Messages {
			if msg.Content == "I'll help" && msg.Role != "assistant" {
				t.Errorf("assistant message role = %q, want assistant", msg.Role)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "ok"}}},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newOpenAIClient(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "system prompt"},
			{Role: matter.RoleUser, Content: "hello"},
			{Role: matter.RolePlanner, Content: "I'll help"},
			{Role: matter.RoleTool, Content: "file contents here"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIClientError401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "bad-key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassTerminal {
		t.Errorf("expected ClassTerminal, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientError403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassTerminal {
		t.Errorf("expected ClassTerminal for 403, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientError429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable for 429, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientError500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal error"}}`))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable for 500, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientError400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassTerminal {
		t.Errorf("expected ClassTerminal for 400, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 100 * time.Millisecond,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable for timeout, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientJSONDecodeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassTerminal {
		t.Errorf("expected ClassTerminal for decode failure, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientCustomBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "proxied"}}},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test with trailing slash in BaseURL.
	client, err := newOpenAIClient(ProviderConfig{
		APIKey:  "key",
		Model:   "gpt-4o",
		BaseURL: server.URL + "/",
		Timeout: 5 * time.Second,
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
	if resp.Content != "proxied" {
		t.Errorf("content = %q, want proxied", resp.Content)
	}
}

func TestOpenAIClientExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value" {
			t.Errorf("missing extra header X-Custom")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "ok"}}},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey:       "key",
		Model:        "gpt-4o",
		BaseURL:      server.URL,
		Timeout:      5 * time.Second,
		ExtraHeaders: map[string]string{"X-Custom": "value"},
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIClientMissingAPIKey(t *testing.T) {
	_, err := newOpenAIClient(ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		APIKey:   "",
	})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestOpenAIClientDefaultMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.MaxTokens == nil || *req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %v, want 4096 (default)", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "ok"}}},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
		// MaxTokens deliberately omitted (0).
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIClientEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIClientViaProviderFactory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "factory works"}}},
			Usage:   openaiUsage{TotalTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ProviderConfig{
		Provider: "openai",
		APIKey:   "key",
		Model:    "gpt-4o",
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
	if resp.Content != "factory works" {
		t.Errorf("content = %q, want 'factory works'", resp.Content)
	}
}

func TestOpenAIClientError502(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable for 502, got %d", agentErr.Classification)
	}
}

func TestOpenAIClientError503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, _ := newOpenAIClient(ProviderConfig{
		APIKey: "key", Model: "gpt-4o", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})

	var agentErr *errtype.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if agentErr.Classification != errtype.ClassRetriable {
		t.Errorf("expected ClassRetriable for 503, got %d", agentErr.Classification)
	}
}
