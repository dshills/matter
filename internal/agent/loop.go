package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/planner"
	"github.com/dshills/matter/pkg/matter"
)

// loop runs the core agent step loop until termination.
func (a *Agent) loop(ctx context.Context, req matter.RunRequest) matter.RunResult {
	for {
		// Check context cancellation.
		if err := ctx.Err(); err != nil {
			return matter.RunResult{
				Error: NewTimeoutError("context cancelled", err, true),
			}
		}

		// Evaluate limits before each step.
		if lc := EvaluateLimits(a.cfg.Agent, a.metrics); lc.Exceeded {
			return matter.RunResult{
				Error: NewLimitExceededError(lc.Message),
			}
		}

		result, done := a.step(ctx, req)
		if done {
			return result
		}
	}
}

// step executes a single agent step: plan → policy → execute → store → update metrics.
// Returns (result, true) if the run should terminate.
func (a *Agent) step(ctx context.Context, req matter.RunRequest) (matter.RunResult, bool) {
	a.metrics.Steps++

	// Build tool schemas for the prompt.
	toolSchemas, err := a.registry.Schemas()
	if err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to build tool schemas: %w", err)}, true
	}

	// Build budget info for the prompt.
	budget := planner.BudgetInfo{
		StepsUsed:      a.metrics.Steps,
		MaxSteps:       a.cfg.Agent.MaxSteps,
		TokensUsed:     a.metrics.TotalTokens,
		MaxTotalTokens: a.cfg.Agent.MaxTotalTokens,
		CostUsed:       a.metrics.CostUSD,
		MaxCostUSD:     a.cfg.Agent.MaxCostUSD,
		TimeElapsed:    time.Since(a.metrics.StartTime),
		MaxDuration:    a.cfg.Agent.MaxDuration,
	}

	// Call planner.
	memCtx := a.memory.Context()
	decision, resp, planErr := a.planner.Decide(ctx, req.Task, memCtx, string(toolSchemas), budget)

	// Update token metrics from the LLM response.
	a.metrics.PromptTokens += resp.PromptTokens
	a.metrics.CompletionTokens += resp.CompletionTokens
	a.metrics.TotalTokens += resp.TotalTokens
	a.metrics.CostUSD += resp.EstimatedCostUSD

	if planErr != nil {
		return a.handleError(ctx, planErr, nil)
	}

	// Handle terminal decisions.
	switch decision.Type {
	case matter.DecisionTypeComplete:
		a.detector.CheckProgress(decision, nil, nil)
		summary := ""
		if decision.Final != nil {
			summary = decision.Final.Summary
		}
		return matter.RunResult{
			FinalSummary: summary,
			Success:      true,
		}, true

	case matter.DecisionTypeFail:
		a.detector.CheckProgress(decision, nil, nil)
		summary := ""
		if decision.Final != nil {
			summary = decision.Final.Summary
		}
		return matter.RunResult{
			FinalSummary: summary,
			Error:        fmt.Errorf("agent failed: %s", summary),
		}, true

	case matter.DecisionTypeTool:
		return a.executeTool(ctx, decision)
	}

	return matter.RunResult{
		Error: fmt.Errorf("unknown decision type: %s", decision.Type),
	}, true
}

// executeTool handles a tool call decision: policy check → execute → store result.
func (a *Agent) executeTool(ctx context.Context, decision matter.Decision) (matter.RunResult, bool) {
	tc := decision.ToolCall
	if tc == nil {
		return a.handleError(ctx, NewPlannerError("tool decision missing tool_call", nil), nil)
	}

	// Look up the tool for policy checks.
	tool, ok := a.registry.Get(tc.Name)
	if !ok {
		toolErr := NewToolValidationError(fmt.Sprintf("tool %q not found", tc.Name), nil)
		return a.handleError(ctx, toolErr, nil)
	}

	// Policy check for unsafe tools.
	if !tool.Safe && a.policy != nil {
		pr := a.policy.CheckToolCall(ctx, tool, tc.Input)
		if !pr.Allowed {
			return matter.RunResult{
				Error: NewPolicyViolationError(pr.Reason),
			}, true
		}
	}

	// Record call for loop detection.
	a.detector.RecordCall(tc.Name, tc.Input)

	// Execute the tool.
	rec := a.executor.Execute(ctx, a.metrics.Steps, tc.Name, tc.Input)

	// Store the planner decision as an assistant message.
	plannerMsg := matter.Message{
		Role:      matter.RolePlanner,
		Content:   fmt.Sprintf(`{"type":"tool","tool_call":{"name":"%s"}}`, tc.Name),
		Timestamp: time.Now(),
		Step:      a.metrics.Steps,
	}
	if err := a.memory.Add(ctx, plannerMsg); err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to store planner message: %w", err)}, true
	}

	// Store the tool result.
	content := rec.Result.Output
	if rec.Result.Error != "" {
		content = fmt.Sprintf("Error: %s", rec.Result.Error)
	}
	toolMsg := matter.Message{
		Role:      matter.RoleTool,
		Content:   content,
		Timestamp: time.Now(),
		Step:      a.metrics.Steps,
	}
	if err := a.memory.Add(ctx, toolMsg); err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to store tool result: %w", err)}, true
	}

	// Check for tool execution errors.
	if rec.Error != "" {
		toolErr := NewToolExecutionError(rec.Error, nil, tool.FatalOnError)
		if tool.FatalOnError {
			return matter.RunResult{Error: toolErr}, true
		}
		// Recoverable: update progress tracking and continue.
		a.updateProgress(decision, &rec.Result, toolErr)
		return matter.RunResult{}, false
	}

	// Check repeated tool calls.
	a.metrics.RepeatedToolDetect = a.detector.IsRepeated()

	// Update progress tracking.
	a.updateProgress(decision, &rec.Result, nil)

	return matter.RunResult{}, false
}

// handleError processes an error from the step, updating consecutive error
// counts and returning to the loop or terminating.
func (a *Agent) handleError(ctx context.Context, stepErr error, result *matter.ToolResult) (matter.RunResult, bool) {
	var agentErr *AgentError
	if errors.As(stepErr, &agentErr) {
		if agentErr.Classification == ClassTerminal {
			return matter.RunResult{Error: stepErr}, true
		}
	}

	// Non-terminal error: increment consecutive errors, store in memory, continue.
	a.metrics.ConsecutiveErrors++
	a.updateProgress(matter.Decision{}, result, stepErr)

	errMsg := matter.Message{
		Role:      matter.RoleTool,
		Content:   fmt.Sprintf("Error: %s", stepErr.Error()),
		Timestamp: time.Now(),
		Step:      a.metrics.Steps,
	}
	if err := a.memory.Add(ctx, errMsg); err != nil {
		return matter.RunResult{Error: fmt.Errorf("failed to store error message: %w", err)}, true
	}

	return matter.RunResult{}, false
}

// updateProgress updates the loop detector and consecutive no-progress counter.
func (a *Agent) updateProgress(decision matter.Decision, result *matter.ToolResult, stepErr error) {
	if a.detector.CheckProgress(decision, result, stepErr) {
		a.metrics.ConsecutiveErrors = 0
		a.metrics.ConsecutiveNoProg = 0
	} else {
		a.metrics.ConsecutiveNoProg++
	}
}
