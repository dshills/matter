package llm

import (
	"math"
	"os"
	"path/filepath"
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

func TestLookupPricingInCustomTable(t *testing.T) {
	table := []ModelPricing{
		{Provider: "custom", Model: "my-model", PromptCostPer1K: 0.01, CompletionCostPer1K: 0.02},
	}

	p, err := LookupPricingIn(table, "custom", "my-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PromptCostPer1K != 0.01 {
		t.Errorf("prompt cost = %f, want 0.01", p.PromptCostPer1K)
	}

	_, err = LookupPricingIn(table, "custom", "other")
	if err == nil {
		t.Error("expected error for model not in custom table")
	}
}

func TestLookupPricingInNilUsesEmbedded(t *testing.T) {
	p, err := LookupPricingIn(nil, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PromptCostPer1K != 0.0025 {
		t.Errorf("prompt cost = %f, want 0.0025", p.PromptCostPer1K)
	}
}

func TestLoadPricingFromFile(t *testing.T) {
	content := `[{"provider":"test","model":"m1","prompt_cost_per_1k":0.05,"completion_cost_per_1k":0.10}]`
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	table, err := LoadPricingFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(table) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(table))
	}
	if table[0].Provider != "test" || table[0].Model != "m1" {
		t.Errorf("unexpected entry: %+v", table[0])
	}
	if table[0].PromptCostPer1K != 0.05 {
		t.Errorf("prompt cost = %f, want 0.05", table[0].PromptCostPer1K)
	}
}

func TestLoadPricingFromFileMissing(t *testing.T) {
	_, err := LoadPricingFromFile("/nonexistent/pricing.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadPricingFromFileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPricingFromFile(path)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestFallbackPricing(t *testing.T) {
	p := FallbackPricing("custom", "model-x", 0.005)
	if p.Provider != "custom" {
		t.Errorf("provider = %q, want custom", p.Provider)
	}
	if p.Model != "model-x" {
		t.Errorf("model = %q, want model-x", p.Model)
	}
	if p.PromptCostPer1K != 0.005 {
		t.Errorf("prompt cost = %f, want 0.005", p.PromptCostPer1K)
	}
	if p.CompletionCostPer1K != 0.005 {
		t.Errorf("completion cost = %f, want 0.005", p.CompletionCostPer1K)
	}

	cost := EstimateCost(p, 1000, 1000)
	want := 0.01 // 0.005 + 0.005
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}
