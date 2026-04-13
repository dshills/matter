package llm

import "fmt"

// ModelPricing maps a provider/model pair to per-token costs.
type ModelPricing struct {
	Provider            string
	Model               string
	PromptCostPer1K     float64 // cost per 1,000 prompt tokens in USD
	CompletionCostPer1K float64 // cost per 1,000 completion tokens in USD
}

// PricingTable contains pricing for all v1-supported models.
var PricingTable = []ModelPricing{
	{Provider: "openai", Model: "gpt-4o", PromptCostPer1K: 0.0025, CompletionCostPer1K: 0.0100},
	{Provider: "openai", Model: "gpt-4o-mini", PromptCostPer1K: 0.000150, CompletionCostPer1K: 0.000600},
	{Provider: "anthropic", Model: "claude-sonnet-4-20250514", PromptCostPer1K: 0.003, CompletionCostPer1K: 0.015},
}

// LookupPricing finds pricing for a provider/model pair.
// Returns an error if the model is not in the pricing table.
func LookupPricing(provider, model string) (ModelPricing, error) {
	for _, p := range PricingTable {
		if p.Provider == provider && p.Model == model {
			return p, nil
		}
	}
	return ModelPricing{}, fmt.Errorf("model %s/%s not found in pricing table", provider, model)
}

// EstimateCost calculates the estimated cost in USD for a given token usage.
func EstimateCost(pricing ModelPricing, promptTokens, completionTokens int) float64 {
	return (float64(promptTokens) * pricing.PromptCostPer1K / 1000) +
		(float64(completionTokens) * pricing.CompletionCostPer1K / 1000)
}
