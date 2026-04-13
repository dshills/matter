package planner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dshills/matter/internal/agent"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// ParseResult holds the parsed decision and any token usage from repair.
type ParseResult struct {
	Decision      matter.Decision
	RepairTokens  int     // tokens used for LLM correction (0 if not needed)
	RepairCostUSD float64 // cost of LLM correction (0 if not needed)
}

// ParseDecision attempts to parse raw LLM output into a Decision using
// the three-stage repair pipeline:
//  1. Direct JSON parse
//  2. Local cleanup (strip fences, fix commas, close braces)
//  3. LLM correction (at most once)
//
// Returns a planner error if all stages fail.
func ParseDecision(ctx context.Context, client llm.Client, raw string) (ParseResult, error) {
	// Stage 1: Direct parse.
	var dec matter.Decision
	if err := json.Unmarshal([]byte(raw), &dec); err == nil {
		if vErr := ValidateDecision(dec); vErr == nil {
			return ParseResult{Decision: dec}, nil
		}
	}

	// Stage 2: Local cleanup.
	cleaned := localCleanup(raw)
	if err := json.Unmarshal([]byte(cleaned), &dec); err == nil {
		if vErr := ValidateDecision(dec); vErr == nil {
			return ParseResult{Decision: dec}, nil
		}
	}

	// Stage 3: LLM correction (at most once).
	if client != nil {
		corrected, resp, err := llmCorrection(ctx, client, raw)
		if err != nil {
			return ParseResult{}, agent.NewPlannerError(
				fmt.Sprintf("all parse attempts failed, LLM correction error: %v", err), err)
		}

		result := ParseResult{
			RepairTokens:  resp.TotalTokens,
			RepairCostUSD: resp.EstimatedCostUSD,
		}

		// Apply local cleanup to LLM correction output (LLMs often add fences).
		corrected = localCleanup(corrected)

		if err := json.Unmarshal([]byte(corrected), &dec); err == nil {
			if vErr := ValidateDecision(dec); vErr == nil {
				result.Decision = dec
				return result, nil
			}
		}

		// LLM correction produced invalid JSON too — preserve repair usage.
		return result, agent.NewParseError(
			"LLM correction produced invalid JSON", nil)
	}

	return ParseResult{}, agent.NewParseError(
		"all parse attempts failed and no LLM client for correction", nil)
}
