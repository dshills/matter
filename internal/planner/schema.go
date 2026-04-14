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
		// ToolCalls takes precedence over ToolCall when both are present.
		if len(d.ToolCalls) > 0 {
			for i, tc := range d.ToolCalls {
				if tc.Name == "" {
					return fmt.Errorf("decision type is %q but tool_calls[%d].name is empty", d.Type, i)
				}
			}
		} else if d.ToolCall != nil {
			if d.ToolCall.Name == "" {
				return fmt.Errorf("decision type is %q but tool_call.name is empty", d.Type)
			}
		} else {
			return fmt.Errorf("decision type is %q but neither tool_call nor tool_calls is set", d.Type)
		}
	case matter.DecisionTypeComplete:
		if d.Final == nil {
			return fmt.Errorf("decision type is %q but final is missing", d.Type)
		}
	case matter.DecisionTypeFail:
		if d.Final == nil {
			return fmt.Errorf("decision type is %q but final is missing", d.Type)
		}
	case matter.DecisionTypeAsk:
		if d.Ask == nil {
			return fmt.Errorf("decision type is %q but ask is missing", d.Type)
		}
		if d.Ask.Question == "" {
			return fmt.Errorf("decision type is %q but ask.question is empty", d.Type)
		}
	default:
		return fmt.Errorf("invalid decision type: %q", d.Type)
	}
	return nil
}
