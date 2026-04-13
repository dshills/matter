package agent

import (
	"encoding/json"
	"strings"

	"github.com/dshills/matter/pkg/matter"
)

// callRecord captures a tool call for repetition detection.
type callRecord struct {
	Name  string
	Input string // JSON-serialized input for comparison
}

// LoopDetector tracks progress and detects repeated tool calls.
type LoopDetector struct {
	maxRepeated int // max_repeated_tool_calls threshold
	history     []callRecord
	prevResult  string // previous tool result for progress comparison
}

// NewLoopDetector creates a loop detector with the given threshold.
func NewLoopDetector(maxRepeated int) *LoopDetector {
	return &LoopDetector{maxRepeated: maxRepeated}
}

// RecordCall adds a tool call to the history for repetition detection.
func (ld *LoopDetector) RecordCall(name string, input map[string]any) {
	inputJSON, _ := json.Marshal(input)
	ld.history = append(ld.history, callRecord{
		Name:  name,
		Input: string(inputJSON),
	})
}

// IsRepeated checks if the most recent tool call has been repeated
// at least maxRepeated times within a sliding window of 2N steps.
func (ld *LoopDetector) IsRepeated() bool {
	if ld.maxRepeated <= 0 || len(ld.history) == 0 {
		return false
	}

	latest := ld.history[len(ld.history)-1]
	windowSize := 2 * ld.maxRepeated
	start := len(ld.history) - windowSize
	if start < 0 {
		start = 0
	}

	count := 0
	for i := start; i < len(ld.history); i++ {
		if ld.history[i].Name == latest.Name && ld.history[i].Input == latest.Input {
			count++
		}
	}

	return count >= ld.maxRepeated
}

// CheckProgress determines if a step made progress per spec Section 14.3.
func (ld *LoopDetector) CheckProgress(decision matter.Decision, result *matter.ToolResult, stepErr error) bool {
	// Complete or fail decisions always count as progress.
	if decision.Type == matter.DecisionTypeComplete || decision.Type == matter.DecisionTypeFail {
		return true
	}

	// Errors do not constitute progress.
	if stepErr != nil {
		return false
	}

	// Tool call with error result is not progress.
	if result != nil && result.Error != "" {
		return false
	}

	// Truncated results always count as progress.
	if result != nil && strings.Contains(result.Output, "[TRUNCATED]") {
		return true
	}

	// Non-error result different from previous counts as progress.
	if result != nil {
		if result.Output != ld.prevResult {
			ld.prevResult = result.Output
			return true
		}
		// Same result as before — no progress.
		return false
	}

	// Tool decision with different call from previous step counts as progress.
	if decision.Type == matter.DecisionTypeTool && len(ld.history) >= 2 {
		curr := ld.history[len(ld.history)-1]
		prev := ld.history[len(ld.history)-2]
		if curr.Name != prev.Name || curr.Input != prev.Input {
			return true
		}
		return false
	}

	return true
}
