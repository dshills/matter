package llm

import (
	"math"
	"testing"
)

func TestLookupPricingFound(t *testing.T) {
	tests := []struct {
		provider string
		model    string
	}{
		{"openai", "gpt-4o"},
		{"openai", "gpt-4o-mini"},
		{"anthropic", "claude-sonnet-4-20250514"},
	}
	for _, tt := range tests {
		p, err := LookupPricing(tt.provider, tt.model)
		if err != nil {
			t.Errorf("LookupPricing(%q, %q) returned error: %v", tt.provider, tt.model, err)
		}
		if p.PromptCostPer1K <= 0 || p.CompletionCostPer1K <= 0 {
			t.Errorf("pricing for %s/%s should have positive costs", tt.provider, tt.model)
		}
	}
}

func TestLookupPricingNotFound(t *testing.T) {
	_, err := LookupPricing("openai", "nonexistent-model")
	if err == nil {
		t.Error("expected error for unknown model")
	}
}

func TestEstimateCostGPT4o(t *testing.T) {
	pricing, _ := LookupPricing("openai", "gpt-4o")
	cost := EstimateCost(pricing, 1000, 1000)
	// 1000 * 0.0025/1000 + 1000 * 0.0100/1000 = 0.0025 + 0.0100 = 0.0125
	want := 0.0125
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestEstimateCostGPT4oMini(t *testing.T) {
	pricing, _ := LookupPricing("openai", "gpt-4o-mini")
	cost := EstimateCost(pricing, 10000, 5000)
	// 10000 * 0.000150/1000 + 5000 * 0.000600/1000 = 0.0015 + 0.003 = 0.0045
	want := 0.0045
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestEstimateCostClaude(t *testing.T) {
	pricing, _ := LookupPricing("anthropic", "claude-sonnet-4-20250514")
	cost := EstimateCost(pricing, 2000, 500)
	// 2000 * 0.003/1000 + 500 * 0.015/1000 = 0.006 + 0.0075 = 0.0135
	want := 0.0135
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestEstimateCostZeroTokens(t *testing.T) {
	pricing, _ := LookupPricing("openai", "gpt-4o")
	cost := EstimateCost(pricing, 0, 0)
	if cost != 0 {
		t.Errorf("cost should be 0 for 0 tokens, got %f", cost)
	}
}
