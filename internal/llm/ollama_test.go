package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

func newTestOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama uses OpenAI-compatible endpoint.
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Content: "ollama response"}}},
			Usage:   openaiUsage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestOllamaLocalClientSuccess(t *testing.T) {
	server := newTestOllamaServer(t)
	defer server.Close()

	client, err := newOllamaLocalClient(ProviderConfig{
		Model:   "llama3",
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "ollama response" {
		t.Errorf("content = %q, want 'ollama response'", resp.Content)
	}
	if resp.Provider != "ollama" {
		t.Errorf("provider = %q, want 'ollama'", resp.Provider)
	}
	if resp.TotalTokens != 12 {
		t.Errorf("total tokens = %d, want 12", resp.TotalTokens)
	}
}

func TestOllamaLocalNoAPIKeyRequired(t *testing.T) {
	// Ollama local should not require an API key.
	_, err := newOllamaLocalClient(ProviderConfig{
		Model: "llama3",
		// No APIKey, no BaseURL — should use defaults.
	})
	if err != nil {
		t.Fatalf("ollama local should not require API key: %v", err)
	}
}

func TestOllamaRemoteClientSuccess(t *testing.T) {
	server := newTestOllamaServer(t)
	defer server.Close()

	client, err := newOllamaRemoteClient(ProviderConfig{
		Model:   "llama3",
		BaseURL: server.URL,
		APIKey:  "optional-key",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Provider != "ollama-remote" {
		t.Errorf("provider = %q, want 'ollama-remote'", resp.Provider)
	}
}

func TestOllamaRemoteRequiresBaseURL(t *testing.T) {
	_, err := newOllamaRemoteClient(ProviderConfig{
		Model: "llama3",
		// No BaseURL — should fail.
	})
	if err == nil {
		t.Error("expected error when base_url is missing for ollama-remote")
	}
}

func TestOllamaLocalViaProviderFactory(t *testing.T) {
	server := newTestOllamaServer(t)
	defer server.Close()

	client, err := NewClient(ProviderConfig{
		Provider: "ollama",
		Model:    "llama3",
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
	if resp.Provider != "ollama" {
		t.Errorf("provider = %q, want 'ollama'", resp.Provider)
	}
}

func TestOllamaRemoteViaProviderFactory(t *testing.T) {
	server := newTestOllamaServer(t)
	defer server.Close()

	client, err := NewClient(ProviderConfig{
		Provider: "ollama-remote",
		Model:    "llama3",
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
	if resp.Provider != "ollama-remote" {
		t.Errorf("provider = %q, want 'ollama-remote'", resp.Provider)
	}
}

func TestOllamaLocalDefaultTimeout(t *testing.T) {
	// Verify that the default timeout for Ollama is 120s (longer than OpenAI's 30s).
	client, err := newOllamaLocalClient(ProviderConfig{
		Model: "llama3",
	})
	if err != nil {
		t.Fatal(err)
	}

	oc, ok := client.(*ollamaClient)
	if !ok {
		t.Fatal("expected *ollamaClient")
	}
	if oc.inner.timeout != 120*time.Second {
		t.Errorf("timeout = %v, want 120s", oc.inner.timeout)
	}
}
