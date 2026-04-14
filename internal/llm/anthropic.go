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
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicDefaultMaxTok  = 4096
	anthropicAPIVersion     = "2023-06-01"
)

// anthropicClient implements Client for the Anthropic Messages API.
type anthropicClient struct {
	apiKey       string
	model        string
	baseURL      string
	timeout      time.Duration
	extraHeaders map[string]string
	httpClient   *http.Client
}

// newAnthropicClient creates an Anthropic provider from config.
func newAnthropicClient(cfg ProviderConfig) (Client, error) {
	if cfg.APIKey == "" {
		return nil, errtype.NewConfigurationError("Anthropic API key is required", nil)
	}

	base := anthropicDefaultBaseURL
	if cfg.BaseURL != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &anthropicClient{
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		baseURL:      base,
		timeout:      timeout,
		extraHeaders: cfg.ExtraHeaders,
		httpClient:   &http.Client{Transport: sharedTransport},
	}, nil
}

// anthropicRequest is the Anthropic Messages API request body.
type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the Anthropic Messages API response body.
type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Usage   anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anthropicErrorResponse is the error response body from Anthropic.
type anthropicErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete sends a Messages API request to Anthropic and returns the response.
func (c *anthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	// Extract system messages from the array — Anthropic requires the system
	// prompt as a top-level field, not in the messages array. Multiple system
	// messages are concatenated to preserve all instructions.
	var systemParts []string
	mapped := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == matter.RoleSystem {
			systemParts = append(systemParts, m.Content)
			continue
		}
		mapped = append(mapped, anthropicMessage{
			Role:    mapRoleToAnthropic(m.Role),
			Content: mapContentToAnthropic(m.Role, m.Content),
		})
	}
	systemMsg := strings.Join(systemParts, "\n")

	// Anthropic requires strictly alternating user/assistant roles. Since
	// both RoleUser and RoleTool map to "user", consecutive same-role
	// messages must be merged to avoid a 400 validation error.
	messages := mergeConsecutiveRoles(mapped)

	// Anthropic requires at least one message and the first must be "user".
	if len(messages) == 0 {
		return Response{}, errtype.NewLLMError("at least one non-system message is required", nil, false)
	}
	if messages[0].Role != "user" {
		return Response{}, errtype.NewLLMError("first message must have user role for Anthropic API", nil, false)
	}

	model := req.Model
	if model == "" {
		model = c.model
	}

	// Anthropic requires max_tokens — default to 4096 per spec §1.5.
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = anthropicDefaultMaxTok
	}

	antReq := anthropicRequest{
		Model:     model,
		System:    systemMsg,
		Messages:  messages,
		MaxTokens: maxTokens,
	}

	// Temperature is always sent explicitly because the planner owns sampling
	// control (spec §1.5). A zero value is intentional — it selects greedy
	// decoding for deterministic, reproducible agent behaviour. Callers that
	// want stochastic output set Request.Temperature > 0.
	temp := req.Temperature
	antReq.Temperature = &temp

	body, err := json.Marshal(antReq)
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to marshal request", err, false)
	}

	// Build HTTP request.
	url := c.baseURL + "/v1/messages"
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to create request", err, false)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("User-Agent", matterUserAgent)
	for k, v := range c.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	// Execute request and measure round-trip latency (time-to-first-byte).
	start := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	latency := time.Since(start)
	if err != nil {
		if reqCtx.Err() == context.Canceled {
			return Response{}, errtype.NewLLMError("request cancelled", err, false)
		}
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
		return Response{}, classifyAnthropicError(httpResp.StatusCode, respBody)
	}

	// Parse response.
	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		return Response{}, errtype.NewLLMError("failed to decode response", err, false)
	}

	if len(antResp.Content) == 0 {
		return Response{}, errtype.NewLLMError("no content blocks in response", nil, false)
	}

	// Concatenate all text content blocks. Anthropic may return multiple
	// text blocks in a single response; we join them to avoid data loss.
	var contentParts []string
	for _, block := range antResp.Content {
		if block.Type == "text" && block.Text != "" {
			contentParts = append(contentParts, block.Text)
		}
	}
	content := strings.Join(contentParts, "\n")

	return Response{
		Content:          content,
		PromptTokens:     antResp.Usage.InputTokens,
		CompletionTokens: antResp.Usage.OutputTokens,
		TotalTokens:      antResp.Usage.InputTokens + antResp.Usage.OutputTokens,
		Provider:         "anthropic",
		Model:            model,
		Latency:          latency,
	}, nil
}

// mapRoleToAnthropic converts matter message roles to Anthropic API roles.
// Tool results are mapped to "user" role with a "[Tool Result]" prefix per
// spec §1.5 — this provides a provider-agnostic representation that works
// without requiring tool_use_id tracking.
func mapRoleToAnthropic(role matter.MessageRole) string {
	switch role {
	case matter.RoleUser:
		return "user"
	case matter.RolePlanner:
		return "assistant"
	case matter.RoleTool:
		return "user"
	default:
		return "user"
	}
}

// mapContentToAnthropic prefixes tool result messages.
func mapContentToAnthropic(role matter.MessageRole, content string) string {
	if role == matter.RoleTool {
		return "[Tool Result] " + content
	}
	return content
}

// mergeConsecutiveRoles combines adjacent messages with the same role by
// joining their content with newlines. Anthropic requires strictly alternating
// user/assistant turns, so this is necessary when consecutive user or tool
// messages are mapped to the same "user" role.
func mergeConsecutiveRoles(msgs []anthropicMessage) []anthropicMessage {
	if len(msgs) == 0 {
		return msgs
	}
	merged := make([]anthropicMessage, 0, len(msgs))
	merged = append(merged, msgs[0])
	for i := 1; i < len(msgs); i++ {
		last := &merged[len(merged)-1]
		if msgs[i].Role == last.Role {
			last.Content += "\n" + msgs[i].Content
		} else {
			merged = append(merged, msgs[i])
		}
	}
	return merged
}

// classifyAnthropicError creates an appropriate AgentError based on HTTP status.
// Error classification matches the OpenAI provider per spec §1.5.
func classifyAnthropicError(statusCode int, body []byte) *errtype.AgentError {
	msg := fmt.Sprintf("HTTP %d", statusCode)
	if len(body) > 0 {
		var errResp anthropicErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
			msg = fmt.Sprintf("HTTP %d: %s", statusCode, errResp.Error.Message)
		}
	}

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return errtype.NewLLMError(msg, nil, false)
	case statusCode == http.StatusBadRequest:
		return errtype.NewLLMError(msg, nil, false)
	case statusCode == http.StatusRequestTimeout:
		return errtype.NewLLMError(msg, nil, true)
	case statusCode == http.StatusTooManyRequests:
		return errtype.NewLLMError(msg, nil, true)
	case statusCode >= 500:
		return errtype.NewLLMError(msg, nil, true)
	default:
		return errtype.NewLLMError(msg, nil, false)
	}
}
