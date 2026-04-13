package planner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dshills/matter/internal/agent"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// BudgetInfo holds the current run budget state for prompt construction.
type BudgetInfo struct {
	StepsUsed      int
	MaxSteps       int
	TokensUsed     int
	MaxTotalTokens int
	CostUsed       float64
	MaxCostUSD     float64
	TimeElapsed    time.Duration
	MaxDuration    time.Duration
}

// Planner produces typed decisions by delegating to an LLM client.
type Planner struct {
	client llm.Client
}

// NewPlanner creates a planner backed by the given LLM client.
func NewPlanner(client llm.Client) *Planner {
	return &Planner{client: client}
}

// Decide calls the LLM with a constructed prompt and parses the response
// into a typed Decision. Returns the decision, the LLM response metadata,
// and any error.
func (p *Planner) Decide(
	ctx context.Context,
	task string,
	memoryContext []matter.Message,
	toolSchemas string,
	budget BudgetInfo,
) (matter.Decision, llm.Response, error) {
	prompt := buildPrompt(task, toolSchemas, budget)

	messages := make([]matter.Message, 0, len(memoryContext)+1)
	messages = append(messages, matter.Message{
		Role:    matter.RoleSystem,
		Content: prompt,
	})
	messages = append(messages, memoryContext...)

	req := llm.Request{
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0,
	}

	resp, err := p.client.Complete(ctx, req)
	if err != nil {
		return matter.Decision{}, resp, agent.NewLLMError("planner LLM call failed", err, true)
	}

	result, parseErr := ParseDecision(ctx, p.client, resp.Content)

	// Always account for repair token usage, even on parse failure.
	resp.TotalTokens += result.RepairTokens
	resp.EstimatedCostUSD += result.RepairCostUSD

	if parseErr != nil {
		return matter.Decision{}, resp, parseErr
	}

	return result.Decision, resp, nil
}

// buildPrompt constructs the system prompt per spec Section 8.6.
func buildPrompt(task, toolSchemas string, budget BudgetInfo) string {
	var b strings.Builder

	b.WriteString("You are an autonomous agent. Your job is to complete the user's task by choosing tools or providing a final answer.\n\n")

	// 1. User task
	b.WriteString("## Task\n")
	b.WriteString(task)
	b.WriteString("\n\n")

	// 2. Available tools
	b.WriteString("## Available Tools\n")
	if toolSchemas != "" {
		b.WriteString(toolSchemas)
	} else {
		b.WriteString("No tools available.")
	}
	b.WriteString("\n\n")

	// 3. Budget and limits
	b.WriteString("## Budget\n")
	fmt.Fprintf(&b, "- Steps: %d / %d\n", budget.StepsUsed, budget.MaxSteps)
	fmt.Fprintf(&b, "- Tokens: %d / %d\n", budget.TokensUsed, budget.MaxTotalTokens)
	fmt.Fprintf(&b, "- Cost: $%.4f / $%.2f\n", budget.CostUsed, budget.MaxCostUSD)
	fmt.Fprintf(&b, "- Time: %s / %s\n", budget.TimeElapsed.Round(time.Second), budget.MaxDuration.Round(time.Second))
	b.WriteString("\n")

	// 4. Instructions
	b.WriteString("## Instructions\n")
	b.WriteString("- Do not invent tools that are not in the Available Tools list.\n")
	b.WriteString("- Do not repeat failed steps blindly.\n")
	b.WriteString("- Complete when enough information is available.\n")
	b.WriteString("- Prefer minimal tool usage needed to finish the task.\n\n")

	// 5. Output format
	b.WriteString("## Output Format\n")
	b.WriteString("Respond with a single JSON object. Choose one:\n\n")
	b.WriteString(`Tool call: {"type":"tool","reasoning":"...","tool_call":{"name":"...","input":{...}}}`)
	b.WriteString("\n")
	b.WriteString(`Complete: {"type":"complete","reasoning":"...","final":{"summary":"..."}}`)
	b.WriteString("\n")
	b.WriteString(`Fail: {"type":"fail","reasoning":"...","final":{"summary":"..."}}`)
	b.WriteString("\n\n")
	b.WriteString("Return ONLY the JSON object with no markdown fences or explanation.\n")

	return b.String()
}
