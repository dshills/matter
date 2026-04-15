package llm

import (
	"fmt"
	"os"
	"time"
)

// ProviderConfig holds provider-specific configuration extracted from the
// top-level config at construction time.
type ProviderConfig struct {
	Provider     string
	Model        string
	Timeout      time.Duration
	APIKey       string            // resolved from credential chain
	BaseURL      string            // optional override for proxies/self-hosted
	ExtraHeaders map[string]string // optional custom headers
}

// ProviderFactory creates an LLM client from config.
// Returns an error if required credentials are missing or the provider is unknown.
type ProviderFactory func(cfg ProviderConfig) (Client, error)

// providers maps provider names to factories.
var providers = map[string]ProviderFactory{
	"mock":          newMockClientFromConfig,
	"openai":        newOpenAIClient,
	"anthropic":     newAnthropicClient,
	"gemini":        newGeminiClient,
	"ollama":        newOllamaLocalClient,
	"ollama-remote": newOllamaRemoteClient,
}

// RegisterProvider adds a provider factory to the registry.
// This is intended for use during init or setup, not at runtime.
func RegisterProvider(name string, factory ProviderFactory) {
	providers[name] = factory
}

// NewClient creates an LLM client for the configured provider.
func NewClient(cfg ProviderConfig) (Client, error) {
	factory, ok := providers[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
	}
	return factory(cfg)
}

// ResolveAPIKey resolves the API key using the credential chain:
// 1. MATTER_LLM_API_KEY environment variable
// 2. Provider-specific environment variable (OPENAI_API_KEY or ANTHROPIC_API_KEY)
// 3. Config file value (passed as configKey)
func ResolveAPIKey(provider, configKey string) string {
	// 1. MATTER_LLM_API_KEY
	if key := os.Getenv("MATTER_LLM_API_KEY"); key != "" {
		return key
	}

	// 2. Provider-specific env var
	providerEnvVars := map[string]string{
		"openai":        "OPENAI_API_KEY",
		"anthropic":     "ANTHROPIC_API_KEY",
		"gemini":        "GEMINI_API_KEY",
		"ollama":        "OLLAMA_API_KEY",
		"ollama-remote": "OLLAMA_API_KEY",
	}
	if envVar, ok := providerEnvVars[provider]; ok {
		if key := os.Getenv(envVar); key != "" {
			return key
		}
	}

	// 3. Config file value
	return configKey
}

// newMockClientFromConfig creates a mock client from a ProviderConfig.
// The mock client is empty (no predefined responses) and suitable for
// testing tool registration and other non-LLM functionality.
func newMockClientFromConfig(_ ProviderConfig) (Client, error) {
	return NewMockClient(nil, nil), nil
}
