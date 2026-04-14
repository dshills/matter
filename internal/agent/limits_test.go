package agent

import (
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
)

func testAgentConfig() config.AgentConfig {
	return config.AgentConfig{
		MaxSteps:                 20,
		MaxDuration:              2 * time.Minute,
		MaxPromptTokens:          40000,
		MaxCompletionTokens:      10000,
		MaxTotalTokens:           50000,
		MaxCostUSD:               3.00,
		MaxConsecutiveErrors:     3,
		MaxRepeatedToolCalls:     2,
		MaxConsecutiveNoProgress: 3,
	}
}

func TestEvaluateLimitsAllPass(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now()}
	lc := EvaluateLimits(cfg, m)
	if lc.Exceeded {
		t.Errorf("no limits should be exceeded: %s", lc.Message)
	}
}

func TestEvaluateLimitsMaxSteps(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), Steps: 20}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_steps" {
		t.Errorf("expected max_steps exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxDuration(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now().Add(-3 * time.Minute)}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_duration" {
		t.Errorf("expected max_duration exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxPromptTokens(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), PromptTokens: 40000}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_prompt_tokens" {
		t.Errorf("expected max_prompt_tokens exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxCompletionTokens(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), CompletionTokens: 10000}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_completion_tokens" {
		t.Errorf("expected max_completion_tokens exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxTotalTokens(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), TotalTokens: 50000}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_total_tokens" {
		t.Errorf("expected max_total_tokens exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxCost(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), CostUSD: 3.00}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_cost_usd" {
		t.Errorf("expected max_cost_usd exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxConsecutiveErrors(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), ConsecutiveErrors: 3}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_consecutive_errors" {
		t.Errorf("expected max_consecutive_errors exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxRepeatedToolCalls(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), RepeatedToolDetect: true}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_repeated_tool_calls" {
		t.Errorf("expected max_repeated_tool_calls exceeded, got %+v", lc)
	}
}

func TestEvaluateLimitsMaxConsecutiveNoProgress(t *testing.T) {
	cfg := testAgentConfig()
	m := RunMetrics{StartTime: time.Now(), ConsecutiveNoProg: 3}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_consecutive_no_progress" {
		t.Errorf("expected max_consecutive_no_progress exceeded, got %+v", lc)
	}
}

// max_asks is enforced in handleAsk, not EvaluateLimits, so that the agent
// can always process the answer to its last allowed question.
func TestEvaluateLimitsMaxAsksNotCheckedHere(t *testing.T) {
	cfg := testAgentConfig()
	cfg.MaxAsks = 3
	m := RunMetrics{StartTime: time.Now(), AskCount: 100}
	lc := EvaluateLimits(cfg, m)
	if lc.Exceeded && lc.Limit == "max_asks" {
		t.Error("max_asks should not be checked in EvaluateLimits")
	}
}

func TestEvaluateLimitsPausedDurationExcluded(t *testing.T) {
	cfg := testAgentConfig()
	cfg.MaxDuration = 2 * time.Minute
	// Start 3 minutes ago, but 2 minutes were paused → only 1 minute active.
	m := RunMetrics{
		StartTime:      time.Now().Add(-3 * time.Minute),
		PausedDuration: 2 * time.Minute,
	}
	lc := EvaluateLimits(cfg, m)
	if lc.Exceeded && lc.Limit == "max_duration" {
		t.Error("paused duration should be excluded from max_duration check")
	}
}

// TestEvaluateLimitsOrder verifies that when multiple limits are exceeded,
// the first in spec order wins.
func TestEvaluateLimitsOrder(t *testing.T) {
	cfg := testAgentConfig()
	// Exceed both steps and cost.
	m := RunMetrics{
		StartTime: time.Now(),
		Steps:     20,
		CostUSD:   5.0,
	}
	lc := EvaluateLimits(cfg, m)
	if !lc.Exceeded || lc.Limit != "max_steps" {
		t.Errorf("max_steps should win over max_cost_usd, got %s", lc.Limit)
	}
}
