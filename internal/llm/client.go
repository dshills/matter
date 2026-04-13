// Package llm provides the LLM client abstraction, retry logic, and cost estimation.
package llm

import (
	"context"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// Request is the normalized LLM request type.
type Request struct {
	Model       string           `json:"model"`
	Messages    []matter.Message `json:"messages"`
	MaxTokens   int              `json:"max_tokens"`
	Temperature float64          `json:"temperature"`
}

// Response is the normalized LLM response type.
type Response struct {
	Content          string        `json:"content"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	EstimatedCostUSD float64       `json:"estimated_cost_usd"`
	Provider         string        `json:"provider"`
	Model            string        `json:"model"`
	Latency          time.Duration `json:"latency"`
}

// Client is the interface for LLM providers.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
