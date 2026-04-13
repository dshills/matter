package planner

import (
	"fmt"

	"github.com/dshills/matter/pkg/matter"
)

// ValidateDecision checks that a parsed Decision is structurally valid:
//   - Type must be one of the three valid enum values
//   - ToolCall must be present (with non-empty Name) when Type == "tool"
//   - Final must be present when Type == "complete" or "fail"
func ValidateDecision(d matter.Decision) error {
	switch d.Type {
	case matter.DecisionTypeTool:
		if d.ToolCall == nil {
			return fmt.Errorf("decision type is %q but tool_call is missing", d.Type)
		}
		if d.ToolCall.Name == "" {
			return fmt.Errorf("decision type is %q but tool_call.name is empty", d.Type)
		}
	case matter.DecisionTypeComplete:
		if d.Final == nil {
			return fmt.Errorf("decision type is %q but final is missing", d.Type)
		}
	case matter.DecisionTypeFail:
		if d.Final == nil {
			return fmt.Errorf("decision type is %q but final is missing", d.Type)
		}
	default:
		return fmt.Errorf("invalid decision type: %q", d.Type)
	}
	return nil
}
