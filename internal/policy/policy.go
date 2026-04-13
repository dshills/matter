// Package policy provides guardrails for the agent loop: workspace confinement,
// budget enforcement, and tool restrictions.
package policy

import (
	"context"

	"github.com/dshills/matter/pkg/matter"
)

// Result holds the outcome of a policy check.
type Result struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Checker evaluates whether a tool call is permitted given the current run state.
type Checker interface {
	CheckToolCall(ctx context.Context, tool matter.Tool, input map[string]any) Result
}

// RunState holds the current run metrics for budget-based policy checks.
type RunState struct {
	StepsUsed      int
	MaxSteps       int
	TotalTokens    int
	MaxTotalTokens int
	CostUSD        float64
	MaxCostUSD     float64
	WorkspaceRoot  string
	DisabledTools  map[string]bool // tool names explicitly disabled
}

// DefaultChecker composes filesystem and budget checks.
type DefaultChecker struct {
	state *RunState
}

// NewChecker creates a policy checker with the given run state.
// The caller must update the RunState as the run progresses.
func NewChecker(state *RunState) *DefaultChecker {
	return &DefaultChecker{state: state}
}

// CheckToolCall evaluates workspace confinement, budget, and tool restrictions.
func (c *DefaultChecker) CheckToolCall(_ context.Context, tool matter.Tool, input map[string]any) Result {
	// Check tool restrictions.
	if c.state.DisabledTools != nil && c.state.DisabledTools[tool.Name] {
		return Result{Allowed: false, Reason: "tool " + tool.Name + " is disabled by policy"}
	}

	// Check budget.
	if r := CheckBudget(c.state); !r.Allowed {
		return r
	}

	// Check filesystem confinement for tools with path inputs.
	if r := CheckFilesystem(c.state.WorkspaceRoot, input); !r.Allowed {
		return r
	}

	return Result{Allowed: true}
}
