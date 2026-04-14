package llm

import (
	"context"
	"testing"
)

func TestNewClientMock(t *testing.T) {
	client, err := NewClient(ProviderConfig{Provider: "mock"})
	if err != nil {
		t.Fatalf("NewClient(mock) error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient(mock) returned nil")
	}
}

func TestNewClientUnknownProvider(t *testing.T) {
	_, err := NewClient(ProviderConfig{Provider: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewClientMockReturnsWorkingClient(t *testing.T) {
	client, err := NewClient(ProviderConfig{Provider: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	// Mock client with no responses should return an error on Complete
	// (exhausted), confirming it's a real mock.
	_, err = client.Complete(context.Background(), Request{})
	if err == nil {
		t.Error("expected exhausted error from empty mock client")
	}
}

func TestResolveAPIKeyMatterEnvFirst(t *testing.T) {
	t.Setenv("MATTER_LLM_API_KEY", "matter-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")

	key := ResolveAPIKey("openai", "config-key")
	if key != "matter-key" {
		t.Errorf("expected matter-key, got %q", key)
	}
}

func TestResolveAPIKeyProviderEnvSecond(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	key := ResolveAPIKey("openai", "config-key")
	if key != "openai-key" {
		t.Errorf("expected openai-key, got %q", key)
	}
}

func TestResolveAPIKeyAnthropicEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")

	key := ResolveAPIKey("anthropic", "config-key")
	if key != "anthropic-key" {
		t.Errorf("expected anthropic-key, got %q", key)
	}
}

func TestResolveAPIKeyConfigFallback(t *testing.T) {
	// Clear any provider-specific env vars that may be set in the environment.
	t.Setenv("OPENAI_API_KEY", "")

	key := ResolveAPIKey("openai", "config-key")
	if key != "config-key" {
		t.Errorf("expected config-key, got %q", key)
	}
}

func TestResolveAPIKeyEmptyAll(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	key := ResolveAPIKey("openai", "")
	if key != "" {
		t.Errorf("expected empty string, got %q", key)
	}
}

func TestResolveAPIKeyUnknownProviderUsesConfig(t *testing.T) {
	key := ResolveAPIKey("custom", "my-key")
	if key != "my-key" {
		t.Errorf("expected my-key for unknown provider, got %q", key)
	}
}
