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

func TestAnthropicClientSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("unexpected x-api-key header: %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("unexpected anthropic-version: %s", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "matter/") {
			t.Errorf("unexpected user-agent: %s", r.Header.Get("User-Agent"))
		}

		// Verify request body.
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("model = %q, want claude-sonnet-4-20250514", req.Model)
		}
		// System message should be extracted to top-level field.
		if req.System != "You are helpful." {
			t.Errorf("system = %q, want 'You are helpful.'", req.System)
		}
		// Only non-system messages should be in the messages array.
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want user", req.Messages[0].Role)
		}
		if req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %d, want 4096", req.MaxTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "Hello!"}},
			Usage:   anthropicUsage{InputTokens: 10, OutputTokens: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newAnthropicClient(ProviderConfig{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
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
	if resp.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", resp.Provider)
	}
	if resp.Latency <= 0 {
		t.Error("latency should be positive")
	}
}

func TestAnthropicClientSystemMessageExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if req.System != "system prompt" {
			t.Errorf("system = %q, want 'system prompt'", req.System)
		}

		// System message should NOT appear in messages array.
		for _, msg := range req.Messages {
			if msg.Content == "system prompt" {
				t.Error("system message should not be in messages array")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newAnthropicClient(ProviderConfig{
		APIKey: "test-key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "system prompt"},
			{Role: matter.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicClientNoSystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if req.System != "" {
			t.Errorf("system should be empty when no system message, got %q", req.System)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newAnthropicClient(ProviderConfig{
		APIKey: "test-key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicClientToolRoleMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
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
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newAnthropicClient(ProviderConfig{
		APIKey: "test-key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"authentication_error"}}`))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "bad-key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal error"}}`))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError502(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientError503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 100 * time.Millisecond,
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

func TestAnthropicClientJSONDecodeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
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

func TestAnthropicClientCustomBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "proxied"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test with trailing slash in BaseURL.
	client, err := newAnthropicClient(ProviderConfig{
		APIKey:  "key",
		Model:   "claude-sonnet-4-20250514",
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

func TestAnthropicClientExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value" {
			t.Errorf("missing extra header X-Custom")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey:       "key",
		Model:        "claude-sonnet-4-20250514",
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

func TestAnthropicClientMissingAPIKey(t *testing.T) {
	_, err := newAnthropicClient(ProviderConfig{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
		APIKey:   "",
	})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestAnthropicClientDefaultMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %d, want 4096 (default)", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
		// MaxTokens deliberately omitted (0).
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicClientEmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 0},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestAnthropicClientViaProviderFactory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "factory works"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewClient(ProviderConfig{
		Provider: "anthropic",
		APIKey:   "key",
		Model:    "claude-sonnet-4-20250514",
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

func TestAnthropicClientCustomMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.MaxTokens != 8192 {
			t.Errorf("max_tokens = %d, want 8192", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages:  []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
		MaxTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicClientConsecutiveRoleMerging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// User + Tool (both "user") should be merged into one message.
		// Then assistant, then another user — total 3 messages.
		if len(req.Messages) != 3 {
			t.Errorf("expected 3 messages after merging, got %d", len(req.Messages))
			for i, m := range req.Messages {
				t.Logf("  msg[%d]: role=%s content=%q", i, m.Role, m.Content)
			}
		}

		// First message should be user + tool merged.
		if req.Messages[0].Role != "user" {
			t.Errorf("msg[0] role = %q, want user", req.Messages[0].Role)
		}
		if !strings.Contains(req.Messages[0].Content, "hello") ||
			!strings.Contains(req.Messages[0].Content, "[Tool Result]") {
			t.Errorf("msg[0] should contain both user and tool content, got %q", req.Messages[0].Content)
		}

		// Second should be assistant.
		if req.Messages[1].Role != "assistant" {
			t.Errorf("msg[1] role = %q, want assistant", req.Messages[1].Role)
		}

		// Third should be user.
		if req.Messages[2].Role != "user" {
			t.Errorf("msg[2] role = %q, want user", req.Messages[2].Role)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 10, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := newAnthropicClient(ProviderConfig{
		APIKey: "test-key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "system"},
			{Role: matter.RoleUser, Content: "hello"},
			{Role: matter.RoleTool, Content: "file contents"},
			{Role: matter.RolePlanner, Content: "I see"},
			{Role: matter.RoleUser, Content: "thanks"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMergeConsecutiveRoles(t *testing.T) {
	tests := []struct {
		name string
		in   []anthropicMessage
		want []anthropicMessage
	}{
		{
			name: "no merge needed",
			in: []anthropicMessage{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
			},
			want: []anthropicMessage{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
			},
		},
		{
			name: "consecutive user merged",
			in: []anthropicMessage{
				{Role: "user", Content: "a"},
				{Role: "user", Content: "b"},
				{Role: "assistant", Content: "c"},
			},
			want: []anthropicMessage{
				{Role: "user", Content: "a\nb"},
				{Role: "assistant", Content: "c"},
			},
		},
		{
			name: "three consecutive merged",
			in: []anthropicMessage{
				{Role: "user", Content: "a"},
				{Role: "user", Content: "b"},
				{Role: "user", Content: "c"},
			},
			want: []anthropicMessage{
				{Role: "user", Content: "a\nb\nc"},
			},
		},
		{
			name: "empty input",
			in:   []anthropicMessage{},
			want: []anthropicMessage{},
		},
		{
			name: "single message",
			in:   []anthropicMessage{{Role: "user", Content: "a"}},
			want: []anthropicMessage{{Role: "user", Content: "a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConsecutiveRoles(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Role != tt.want[i].Role || got[i].Content != tt.want[i].Content {
					t.Errorf("msg[%d] = {%s, %q}, want {%s, %q}",
						i, got[i].Role, got[i].Content, tt.want[i].Role, tt.want[i].Content)
				}
			}
		})
	}
}

func TestAnthropicClientMultipleContentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{
				{Type: "text", Text: "First part."},
				{Type: "text", Text: "Second part."},
			},
			Usage: anthropicUsage{InputTokens: 10, OutputTokens: 8},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "First part.\nSecond part."
	if resp.Content != want {
		t.Errorf("content = %q, want %q", resp.Content, want)
	}
}

func TestAnthropicClientEmptyMessages(t *testing.T) {
	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: "http://localhost", Timeout: 5 * time.Second,
	})

	// Only system message — results in empty messages array.
	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "system only"},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty messages after system extraction")
	}
}

func TestAnthropicClientMultipleSystemMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// Multiple system messages should be concatenated.
		if !strings.Contains(req.System, "first system") || !strings.Contains(req.System, "second system") {
			t.Errorf("system = %q, want both system messages concatenated", req.System)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := anthropicResponse{
			Content: []anthropicContent{{Type: "text", Text: "ok"}},
			Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := newAnthropicClient(ProviderConfig{
		APIKey: "key", Model: "claude-sonnet-4-20250514", BaseURL: server.URL, Timeout: 5 * time.Second,
	})

	_, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "first system"},
			{Role: matter.RoleSystem, Content: "second system"},
			{Role: matter.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
