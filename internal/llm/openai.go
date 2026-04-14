package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dshills/matter/internal/errtype"
	"github.com/dshills/matter/pkg/matter"
)

const (
	openaiDefaultBaseURL = "https://api.openai.com/v1"
	openaiDefaultMaxTok  = 4096
	matterUserAgent      = "matter/0.2.0"
	maxResponseBodyBytes = 10 * 1024 * 1024 // 10 MB cap on response body
)

// sharedTransport is reused across all provider client instances to ensure
// proper connection pooling when multiple clients are created. We clone
// http.DefaultTransport to preserve proxy support, dial timeouts, and TLS
// handshake timeout, then tune pool sizes for concurrent agent workloads.
var sharedTransport = func() *http.Transport {
	dt, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Fallback if DefaultTransport was replaced by middleware.
		dt = &http.Transport{}
	}
	t := dt.Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 100
	t.IdleConnTimeout = 90 * time.Second
	return t
}()

// openaiClient implements Client for the OpenAI Chat Completions API.
type openaiClient struct {
	apiKey       string
	model        string
	baseURL      string
	timeout      time.Duration
	extraHeaders map[string]string
	httpClient   *http.Client
}

// newOpenAIClient creates an OpenAI provider from config.
func newOpenAIClient(cfg ProviderConfig) (Client, error) {
	if cfg.APIKey == "" {
		return nil, errtype.NewConfigurationError("OpenAI API key is required", nil)
	}

	base := openaiDefaultBaseURL
	if cfg.BaseURL != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &openaiClient{
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		baseURL:      base,
		timeout:      timeout,
		extraHeaders: cfg.ExtraHeaders,
		httpClient:   &http.Client{Transport: sharedTransport},
	}, nil
}

// openaiRequest is the Chat Completions API request body.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse is the Chat Completions API response body.
type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// openaiErrorResponse is the error response body from OpenAI.
type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete sends a chat completion request to OpenAI and returns the response.
func (c *openaiClient) Complete(ctx context.Context, req Request) (Response, error) {
	// Build request body.
	messages := make([]openaiMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, openaiMessage{
			Role:    mapRoleToOpenAI(m.Role),
			Content: mapContentToOpenAI(m.Role, m.Content),
		})
	}

	model := req.Model
	if model == "" {
		model = c.model
	}

	oaiReq := openaiRequest{
		Model:    model,
		Messages: messages,
	}

	// Set max_tokens: use request value, fall back to spec default (§1.4: 4096).
	// The default is intentionally model-agnostic — callers that need a higher
	// ceiling (e.g. for long-form generation) should set Request.MaxTokens
	// explicitly. 4096 is a safe baseline that works across all OpenAI models.
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = openaiDefaultMaxTok
	}
	oaiReq.MaxTokens = &maxTokens

	// Temperature is always sent explicitly because the planner owns sampling
	// control (spec §1.4). A zero value is intentional — it selects greedy
	// decoding for deterministic, reproducible agent behaviour. Callers that
	// want stochastic output set Request.Temperature > 0.
	temp := req.Temperature
	oaiReq.Temperature = &temp

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to marshal request", err, false)
	}

	// Build HTTP request.
	url := c.baseURL + "/chat/completions"
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to create request", err, false)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("User-Agent", matterUserAgent)
	for k, v := range c.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	// Execute request and measure round-trip latency (time-to-first-byte).
	start := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	latency := time.Since(start)
	if err != nil {
		// Context cancellation is not retriable — the caller aborted.
		if reqCtx.Err() == context.Canceled {
			return Response{}, errtype.NewLLMError("request cancelled", err, false)
		}
		// Network errors and timeouts are retriable.
		return Response{}, errtype.NewLLMError("request failed", err, true)
	}
	defer func() { _ = httpResp.Body.Close() }()

	// Read one byte beyond the limit so we can detect truncation.
	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBodyBytes+1))

	if err != nil {
		return Response{}, errtype.NewLLMError("failed to read response", err, true)
	}
	if int64(len(respBody)) > maxResponseBodyBytes {
		return Response{}, errtype.NewLLMError(
			fmt.Sprintf("response body exceeded %d byte limit", maxResponseBodyBytes), nil, false)
	}

	// Handle error status codes.
	if httpResp.StatusCode != http.StatusOK {
		return Response{}, classifyOpenAIError(httpResp.StatusCode, respBody)
	}

	// Parse response.
	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return Response{}, errtype.NewLLMError("failed to decode response", err, false)
	}

	if len(oaiResp.Choices) == 0 {
		return Response{}, errtype.NewLLMError("no choices in response", nil, false)
	}

	return Response{
		Content:          oaiResp.Choices[0].Message.Content,
		PromptTokens:     oaiResp.Usage.PromptTokens,
		CompletionTokens: oaiResp.Usage.CompletionTokens,
		TotalTokens:      oaiResp.Usage.TotalTokens,
		Provider:         "openai",
		Model:            model,
		Latency:          latency,
	}, nil
}

// mapRoleToOpenAI converts matter message roles to OpenAI API roles.
// Tool results are mapped to "user" role with a "[Tool Result]" prefix per
// spec §1.4 — this provides a provider-agnostic representation that works
// across all OpenAI models without requiring tool_call_id tracking. The
// native "tool" role requires parallel tool-call IDs which the matter
// framework does not currently surface.
func mapRoleToOpenAI(role matter.MessageRole) string {
	switch role {
	case matter.RoleSystem:
		return "system"
	case matter.RoleUser:
		return "user"
	case matter.RolePlanner: // "assistant"
		return "assistant"
	case matter.RoleTool:
		return "user"
	default:
		return "user"
	}
}

// mapContentToOpenAI prefixes tool result messages.
func mapContentToOpenAI(role matter.MessageRole, content string) string {
	if role == matter.RoleTool {
		return "[Tool Result] " + content
	}
	return content
}

// classifyOpenAIError creates an appropriate AgentError based on HTTP status.
func classifyOpenAIError(statusCode int, body []byte) *errtype.AgentError {
	// Try to extract error message from response body.
	msg := fmt.Sprintf("HTTP %d", statusCode)
	if len(body) > 0 {
		var errResp openaiErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
			msg = fmt.Sprintf("HTTP %d: %s", statusCode, errResp.Error.Message)
		}
	}

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return errtype.NewLLMError(msg, nil, false) // terminal
	case statusCode == http.StatusBadRequest:
		return errtype.NewLLMError(msg, nil, false) // terminal
	case statusCode == http.StatusRequestTimeout:
		return errtype.NewLLMError(msg, nil, true) // retriable
	case statusCode == http.StatusTooManyRequests:
		return errtype.NewLLMError(msg, nil, true) // retriable
	case statusCode >= 500:
		return errtype.NewLLMError(msg, nil, true) // retriable
	default:
		return errtype.NewLLMError(msg, nil, false) // terminal by default
	}
}
