package policy

import "fmt"

// CheckBudget verifies that the run has not exceeded its budget limits.
func CheckBudget(state *RunState) Result {
	if state.StepsUsed >= state.MaxSteps {
		return Result{
			Allowed: false,
			Reason:  fmt.Sprintf("step budget exhausted: %d/%d", state.StepsUsed, state.MaxSteps),
		}
	}
	if state.TotalTokens >= state.MaxTotalTokens {
		return Result{
			Allowed: false,
			Reason:  fmt.Sprintf("token budget exhausted: %d/%d", state.TotalTokens, state.MaxTotalTokens),
		}
	}
	if state.CostUSD >= state.MaxCostUSD {
		return Result{
			Allowed: false,
			Reason:  fmt.Sprintf("cost budget exhausted: $%.4f/$%.2f", state.CostUSD, state.MaxCostUSD),
		}
	}
	return Result{Allowed: true}
}
