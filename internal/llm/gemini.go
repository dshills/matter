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
	geminiDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	geminiDefaultMaxTok  = 4096
)

// geminiClient implements Client for the Google Gemini API.
type geminiClient struct {
	apiKey       string
	model        string
	baseURL      string
	timeout      time.Duration
	extraHeaders map[string]string
	httpClient   *http.Client
}

// newGeminiClient creates a Gemini provider from config.
func newGeminiClient(cfg ProviderConfig) (Client, error) {
	if cfg.APIKey == "" {
		return nil, errtype.NewConfigurationError("Gemini API key is required", nil)
	}

	base := geminiDefaultBaseURL
	if cfg.BaseURL != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &geminiClient{
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		baseURL:      base,
		timeout:      timeout,
		extraHeaders: cfg.ExtraHeaders,
		httpClient:   &http.Client{Transport: sharedTransport},
	}, nil
}

// Gemini API request types.

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	SystemInstruct   *geminiContent         `json:"systemInstruction,omitempty"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

// Gemini API response types.

type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// Complete sends a generateContent request to Gemini and returns the response.
func (c *geminiClient) Complete(ctx context.Context, req Request) (Response, error) {
	// Extract system messages and build contents array.
	var systemParts []string
	contents := make([]geminiContent, 0, len(req.Messages))

	for _, m := range req.Messages {
		if m.Role == matter.RoleSystem {
			systemParts = append(systemParts, m.Content)
			continue
		}
		contents = append(contents, geminiContent{
			Role:  mapRoleToGemini(m.Role),
			Parts: []geminiPart{{Text: mapContentToGemini(m.Role, m.Content)}},
		})
	}

	// Gemini requires at least one content entry.
	if len(contents) == 0 {
		return Response{}, errtype.NewLLMError("at least one non-system message is required", nil, false)
	}

	// Gemini requires the first message to have the "user" role.
	// Drop any leading "model" messages that can appear from planner history.
	for len(contents) > 0 && contents[0].Role == "model" {
		contents = contents[1:]
	}
	if len(contents) == 0 {
		return Response{}, errtype.NewLLMError("at least one user message is required", nil, false)
	}

	// Merge consecutive same-role messages (Gemini requires alternating roles).
	contents = mergeGeminiContents(contents)

	model := req.Model
	if model == "" {
		model = c.model
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = geminiDefaultMaxTok
	}

	// Temperature is always sent explicitly because the planner owns sampling
	// control. A zero value is intentional — it selects greedy decoding for
	// deterministic, reproducible agent behaviour.
	temp := req.Temperature
	gemReq := geminiRequest{
		Contents: contents,
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: maxTokens,
			Temperature:     &temp,
		},
	}

	if len(systemParts) > 0 {
		gemReq.SystemInstruct = &geminiContent{
			Parts: []geminiPart{{Text: strings.Join(systemParts, "\n")}},
		}
	}

	body, err := json.Marshal(gemReq)
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to marshal request", err, false)
	}

	// Build HTTP request. API key is sent via header to avoid URL logging exposure.
	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, model)
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to create request", err, false)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
	httpReq.Header.Set("User-Agent", matterUserAgent)
	for k, v := range c.extraHeaders {
		httpReq.Header.Set(k, v)
	}

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

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return Response{}, errtype.NewLLMError("failed to read response", err, true)
	}
	if int64(len(respBody)) > maxResponseBodyBytes {
		return Response{}, errtype.NewLLMError(
			fmt.Sprintf("response body exceeded %d byte limit", maxResponseBodyBytes), nil, false)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, classifyGeminiError(httpResp.StatusCode, respBody)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return Response{}, errtype.NewLLMError("failed to decode response", err, false)
	}

	if len(gemResp.Candidates) == 0 {
		return Response{}, errtype.NewLLMError("no candidates in response", nil, false)
	}

	// Gemini may return HTTP 200 but block output via safety filters.
	// Only STOP and MAX_TOKENS indicate usable output.
	reason := gemResp.Candidates[0].FinishReason
	if reason != "" && reason != "STOP" && reason != "MAX_TOKENS" {
		return Response{}, errtype.NewLLMError(
			fmt.Sprintf("generation blocked: finishReason=%s", reason), nil, false)
	}

	// Concatenate all text parts from the first candidate.
	var textParts []string
	for _, part := range gemResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	if len(textParts) == 0 {
		return Response{}, errtype.NewLLMError("no text content in response", nil, false)
	}
	content := strings.Join(textParts, "\n")

	return Response{
		Content:          content,
		PromptTokens:     gemResp.UsageMetadata.PromptTokenCount,
		CompletionTokens: gemResp.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      gemResp.UsageMetadata.TotalTokenCount,
		Provider:         "gemini",
		Model:            model,
		Latency:          latency,
	}, nil
}

// mapRoleToGemini converts matter message roles to Gemini API roles.
func mapRoleToGemini(role matter.MessageRole) string {
	switch role {
	case matter.RoleUser:
		return "user"
	case matter.RolePlanner:
		return "model"
	case matter.RoleTool:
		return "user"
	default:
		return "user"
	}
}

// mapContentToGemini prefixes tool result messages.
func mapContentToGemini(role matter.MessageRole, content string) string {
	if role == matter.RoleTool {
		return "[Tool Result] " + content
	}
	return content
}

// mergeGeminiContents merges consecutive same-role contents.
// Gemini requires alternating user/model turns.
func mergeGeminiContents(contents []geminiContent) []geminiContent {
	if len(contents) == 0 {
		return contents
	}
	merged := make([]geminiContent, 0, len(contents))
	merged = append(merged, contents[0])
	for i := 1; i < len(contents); i++ {
		last := &merged[len(merged)-1]
		if contents[i].Role == last.Role {
			last.Parts = append(last.Parts, contents[i].Parts...)
		} else {
			merged = append(merged, contents[i])
		}
	}
	return merged
}

// classifyGeminiError creates an appropriate AgentError based on HTTP status.
func classifyGeminiError(statusCode int, body []byte) *errtype.AgentError {
	msg := fmt.Sprintf("HTTP %d", statusCode)
	if len(body) > 0 {
		var errResp geminiErrorResponse
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
