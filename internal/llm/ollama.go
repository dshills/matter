package llm

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dshills/matter/internal/errtype"
)

const (
	ollamaDefaultBaseURL = "http://localhost:11434/v1"
)

// ollamaClient wraps openaiClient since Ollama exposes an OpenAI-compatible
// chat completions API. The only differences are defaults: no API key
// required for local, different base URL, and provider name in responses.
type ollamaClient struct {
	inner    *openaiClient
	provider string // "ollama" or "ollama-remote"
}

// newOllamaLocalClient creates an Ollama provider for a local instance.
// No API key is required; base URL defaults to http://localhost:11434/v1.
func newOllamaLocalClient(cfg ProviderConfig) (Client, error) {
	base := ollamaDefaultBaseURL
	if cfg.BaseURL != "" {
		base = normalizeOllamaBaseURL(cfg.BaseURL)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second // Ollama local models can be slow to load
	}

	inner := &openaiClient{
		apiKey:       cfg.APIKey, // may be empty for local
		model:        cfg.Model,
		baseURL:      base,
		timeout:      timeout,
		extraHeaders: cfg.ExtraHeaders,
		httpClient:   &http.Client{Transport: sharedTransport},
	}

	return &ollamaClient{inner: inner, provider: "ollama"}, nil
}

// newOllamaRemoteClient creates an Ollama provider for a network instance.
// BaseURL is required; API key is optional (depends on remote configuration).
func newOllamaRemoteClient(cfg ProviderConfig) (Client, error) {
	if cfg.BaseURL == "" {
		return nil, errtype.NewConfigurationError(
			"base_url is required for ollama-remote (e.g., http://gpu-server:11434/v1)", nil)
	}

	base := normalizeOllamaBaseURL(cfg.BaseURL)

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	inner := &openaiClient{
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		baseURL:      base,
		timeout:      timeout,
		extraHeaders: cfg.ExtraHeaders,
		httpClient:   &http.Client{Transport: sharedTransport},
	}

	return &ollamaClient{inner: inner, provider: "ollama-remote"}, nil
}

// Complete delegates to the inner OpenAI-compatible client and overrides
// the provider name in the response.
func (c *ollamaClient) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := c.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	resp.Provider = c.provider
	return resp, nil
}

// normalizeOllamaBaseURL ensures the base URL ends with /v1 for
// OpenAI-compatible API routing. Users commonly provide just the host
// (e.g., http://localhost:11434) without the /v1 suffix.
func normalizeOllamaBaseURL(rawURL string) string {
	base := strings.TrimRight(rawURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}
