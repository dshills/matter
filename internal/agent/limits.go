package agent

import (
	"fmt"
	"time"

	"github.com/dshills/matter/internal/config"
)

// RunMetrics tracks cumulative metrics for limit evaluation.
type RunMetrics struct {
	Steps              int
	StartTime          time.Time
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	CostUSD            float64
	ConsecutiveErrors  int
	ConsecutiveNoProg  int
	RepeatedToolDetect bool // set by loop detector
}

// LimitCheck identifies which limit was exceeded.
type LimitCheck struct {
	Exceeded bool
	Limit    string
	Message  string
}

// EvaluateLimits checks all 9 hard limits in spec order.
// Returns the first exceeded limit, or a non-exceeded result if all pass.
func EvaluateLimits(cfg config.AgentConfig, m RunMetrics) LimitCheck {
	// 1. max_steps
	if m.Steps >= cfg.MaxSteps {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_steps",
			Message:  fmt.Sprintf("step limit exceeded: %d/%d", m.Steps, cfg.MaxSteps),
		}
	}

	// 2. max_duration
	elapsed := time.Since(m.StartTime)
	if elapsed >= cfg.MaxDuration {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_duration",
			Message:  fmt.Sprintf("duration limit exceeded: %s/%s", elapsed.Round(time.Second), cfg.MaxDuration),
		}
	}

	// 3. max_prompt_tokens
	if m.PromptTokens >= cfg.MaxPromptTokens {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_prompt_tokens",
			Message:  fmt.Sprintf("prompt token limit exceeded: %d/%d", m.PromptTokens, cfg.MaxPromptTokens),
		}
	}

	// 4. max_completion_tokens
	if m.CompletionTokens >= cfg.MaxCompletionTokens {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_completion_tokens",
			Message:  fmt.Sprintf("completion token limit exceeded: %d/%d", m.CompletionTokens, cfg.MaxCompletionTokens),
		}
	}

	// 5. max_total_tokens
	if m.TotalTokens >= cfg.MaxTotalTokens {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_total_tokens",
			Message:  fmt.Sprintf("total token limit exceeded: %d/%d", m.TotalTokens, cfg.MaxTotalTokens),
		}
	}

	// 6. max_cost_usd
	if m.CostUSD >= cfg.MaxCostUSD {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_cost_usd",
			Message:  fmt.Sprintf("cost limit exceeded: $%.4f/$%.2f", m.CostUSD, cfg.MaxCostUSD),
		}
	}

	// 7. max_consecutive_errors
	if m.ConsecutiveErrors >= cfg.MaxConsecutiveErrors {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_consecutive_errors",
			Message:  fmt.Sprintf("consecutive error limit exceeded: %d/%d", m.ConsecutiveErrors, cfg.MaxConsecutiveErrors),
		}
	}

	// 8. max_repeated_tool_calls
	if m.RepeatedToolDetect {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_repeated_tool_calls",
			Message:  "repeated tool call detected",
		}
	}

	// 9. max_consecutive_no_progress
	if m.ConsecutiveNoProg >= cfg.MaxConsecutiveNoProgress {
		return LimitCheck{
			Exceeded: true,
			Limit:    "max_consecutive_no_progress",
			Message:  fmt.Sprintf("no-progress limit exceeded: %d/%d", m.ConsecutiveNoProg, cfg.MaxConsecutiveNoProgress),
		}
	}

	return LimitCheck{Exceeded: false}
}
