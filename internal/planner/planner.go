package planner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/errtype"
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
	AsksUsed       int
	MaxAsks        int
}

// Planner produces typed decisions by delegating to an LLM client.
type Planner struct {
	client         llm.Client
	cfg            config.PlannerConfig
	resolvedPrompt string // loaded from file at construction time
}

// NewPlanner creates a planner backed by the given LLM client.
// The prompt file (if configured) is read once at construction time.
func NewPlanner(client llm.Client, cfg config.PlannerConfig) (*Planner, error) {
	p := &Planner{client: client, cfg: cfg}

	// Resolve system_prompt_file at construction time.
	if cfg.SystemPromptFile != "" && cfg.SystemPrompt == "" {
		data, err := os.ReadFile(cfg.SystemPromptFile)
		if err != nil {
			return nil, errtype.NewConfigurationError(
				fmt.Sprintf("failed to read system_prompt_file %q", cfg.SystemPromptFile), err)
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			return nil, errtype.NewConfigurationError(
				fmt.Sprintf("system_prompt_file %q is empty", cfg.SystemPromptFile), nil)
		}
		p.resolvedPrompt = content
	}

	return p, nil
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
	prompt := p.buildPrompt(task, toolSchemas, budget)

	messages := make([]matter.Message, 0, len(memoryContext)+1)
	messages = append(messages, matter.Message{
		Role:    matter.RoleSystem,
		Content: prompt,
	})
	messages = append(messages, memoryContext...)

	req := llm.Request{
		Messages:    messages,
		MaxTokens:   p.cfg.MaxResponseTokens,
		Temperature: p.cfg.Temperature,
	}

	resp, err := p.client.Complete(ctx, req)
	if err != nil {
		return matter.Decision{}, resp, errtype.NewLLMError("planner LLM call failed", err, true)
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

// defaultPersona is the v1 default persona and instructions section.
const defaultPersona = "You are an autonomous agent. Your job is to complete the user's task by choosing tools or providing a final answer."

// defaultInstructions is the v1 default instruction set.
const defaultInstructions = `## Instructions
- Do not invent tools that are not in the Available Tools list.
- Do not repeat failed steps blindly.
- Complete when enough information is available.
- Prefer minimal tool usage needed to finish the task.`

// MaxPlanSteps returns the configured max_plan_steps for prompt construction.
func (p *Planner) MaxPlanSteps() int {
	return p.cfg.MaxPlanSteps
}

// buildPrompt constructs the system prompt per spec Section 2.3.
// Prompt precedence: system_prompt > system_prompt_file > default.
// When system_prompt is set, prefix/suffix are ignored.
// Structural sections (Tools, Budget, Output Format) are always appended.
func (p *Planner) buildPrompt(task, toolSchemas string, budget BudgetInfo) string {
	var b strings.Builder

	// Determine the persona/instructions section.
	switch {
	case p.cfg.SystemPrompt != "":
		// Full override — prefix/suffix ignored per spec §2.2.
		b.WriteString(p.cfg.SystemPrompt)
		b.WriteString("\n\n")
	case p.resolvedPrompt != "":
		// File-based override — prefix/suffix ignored per spec §2.2.
		b.WriteString(p.resolvedPrompt)
		b.WriteString("\n\n")
	default:
		// Default prompt with optional prefix/suffix.
		if p.cfg.PromptPrefix != "" {
			b.WriteString(p.cfg.PromptPrefix)
			b.WriteString("\n\n")
		}
		b.WriteString(defaultPersona)
		b.WriteString("\n\n")
	}

	// Task section.
	b.WriteString("## Task\n")
	b.WriteString(task)
	b.WriteString("\n\n")

	// Available tools section.
	b.WriteString("## Available Tools\n")
	if toolSchemas != "" {
		b.WriteString(toolSchemas)
	} else {
		b.WriteString("No tools available.")
	}
	b.WriteString("\n\n")

	// Budget section — only display limits that are configured (non-zero).
	b.WriteString("## Budget\n")
	if budget.MaxSteps > 0 {
		fmt.Fprintf(&b, "- Steps: %d / %d\n", budget.StepsUsed, budget.MaxSteps)
	}
	if budget.MaxTotalTokens > 0 {
		fmt.Fprintf(&b, "- Tokens: %d / %d\n", budget.TokensUsed, budget.MaxTotalTokens)
	}
	if budget.MaxCostUSD > 0 {
		fmt.Fprintf(&b, "- Cost: $%.4f / $%.2f\n", budget.CostUsed, budget.MaxCostUSD)
	}
	if budget.MaxDuration > 0 {
		fmt.Fprintf(&b, "- Time: %s / %s\n", budget.TimeElapsed.Round(time.Second), budget.MaxDuration.Round(time.Second))
	}
	if budget.MaxAsks > 0 {
		fmt.Fprintf(&b, "- Asks: %d / %d\n", budget.AsksUsed, budget.MaxAsks)
	}
	b.WriteString("\n")

	// Instructions section (only for default prompt path).
	if p.cfg.SystemPrompt == "" && p.resolvedPrompt == "" {
		b.WriteString(defaultInstructions)
		if budget.MaxAsks > 0 {
			b.WriteString("\n")
			b.WriteString("- Ask the user only when the task is genuinely ambiguous. Do not ask for confirmation of routine actions.\n")
			b.WriteString("- Use options when the question has a small number of likely answers.\n")
		}
		if p.cfg.MaxPlanSteps > 1 {
			b.WriteString("\n")
			b.WriteString("- When multiple tool calls are needed and their inputs don't depend on each other's outputs, return them as a tool_calls array.\n")
			b.WriteString("- Each tool in the sequence is executed in order. If one fails, the rest are skipped and you will be asked to replan.\n")
			b.WriteString("- Limit sequences to straightforward operations. Do not chain calls where later inputs depend on earlier outputs.\n")
			fmt.Fprintf(&b, "- Each tool call in a sequence counts as a separate step. Maximum sequence length: %d.\n", p.cfg.MaxPlanSteps)
		}
		b.WriteString("\n")
		if p.cfg.PromptSuffix != "" {
			b.WriteString("\n")
			b.WriteString(p.cfg.PromptSuffix)
		}
		b.WriteString("\n\n")
	}

	// Output format section — always appended, cannot be overridden.
	b.WriteString("## Output Format\n")
	b.WriteString("Respond with a single JSON object. Choose one:\n\n")
	b.WriteString(`Tool call: {"type":"tool","reasoning":"...","tool_call":{"name":"...","input":{...}}}`)
	b.WriteString("\n")
	if p.cfg.MaxPlanSteps > 1 {
		b.WriteString(`Multi-step plan: {"type":"tool","reasoning":"...","tool_calls":[{"name":"...","input":{...}},{"name":"...","input":{...}}]}`)
		b.WriteString("\n")
	}
	b.WriteString(`Complete: {"type":"complete","reasoning":"...","final":{"summary":"..."}}`)
	b.WriteString("\n")
	b.WriteString(`Fail: {"type":"fail","reasoning":"...","final":{"summary":"..."}}`)
	b.WriteString("\n")
	if budget.MaxAsks > 0 {
		b.WriteString(`Ask: {"type":"ask","reasoning":"...","ask":{"question":"...","options":["A","B"]}}`)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString("Return ONLY the JSON object with no markdown fences or explanation.\n")

	return b.String()
}
