package llm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ModelPricing maps a provider/model pair to per-token costs.
type ModelPricing struct {
	Provider            string  `json:"provider"`
	Model               string  `json:"model"`
	PromptCostPer1K     float64 `json:"prompt_cost_per_1k"`
	CompletionCostPer1K float64 `json:"completion_cost_per_1k"`
}

//go:embed pricing.json
var embeddedPricingJSON []byte

// pricingTable is the parsed pricing data, initialized lazily.
var (
	pricingTable []ModelPricing
	pricingOnce  sync.Once
	pricingErr   error
)

// loadEmbeddedPricing parses the embedded pricing.json on first access.
func loadEmbeddedPricing() ([]ModelPricing, error) {
	pricingOnce.Do(func() {
		pricingErr = json.Unmarshal(embeddedPricingJSON, &pricingTable)
	})
	return pricingTable, pricingErr
}

// LoadPricingFromFile parses a pricing JSON file from disk.
// Returns the parsed entries or an error if the file is unreadable or malformed.
func LoadPricingFromFile(path string) ([]ModelPricing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pricing file: %w", err)
	}
	var entries []ModelPricing
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing pricing file: %w", err)
	}
	return entries, nil
}

// LookupPricing finds pricing for a provider/model pair in the given table.
// If table is nil, the embedded pricing table is used.
// Returns an error if the model is not found.
func LookupPricing(provider, model string) (ModelPricing, error) {
	return LookupPricingIn(nil, provider, model)
}

// LookupPricingIn finds pricing for a provider/model pair in the given table.
// If table is nil, the embedded pricing table is used.
func LookupPricingIn(table []ModelPricing, provider, model string) (ModelPricing, error) {
	if table == nil {
		var err error
		table, err = loadEmbeddedPricing()
		if err != nil {
			return ModelPricing{}, fmt.Errorf("loading embedded pricing: %w", err)
		}
	}
	for _, p := range table {
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

// FallbackPricing returns a ModelPricing using the given fallback cost per 1K
// for both prompt and completion. Used when a model is not in the pricing table.
func FallbackPricing(provider, model string, costPer1K float64) ModelPricing {
	return ModelPricing{
		Provider:            provider,
		Model:               model,
		PromptCostPer1K:     costPer1K,
		CompletionCostPer1K: costPer1K,
	}
}
