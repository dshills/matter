package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestAgentErrorMessage(t *testing.T) {
	err := NewPlannerError("invalid decision", nil)
	want := "planner_error: invalid decision"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAgentErrorMessageWithCause(t *testing.T) {
	cause := fmt.Errorf("json parse failed")
	err := NewParseError("bad output", cause)
	want := "parse_error: bad output: json parse failed"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAgentErrorUnwrap(t *testing.T) {
	cause := fmt.Errorf("underlying")
	err := NewLLMError("request failed", cause, true)
	if !errors.Is(err, cause) {
		t.Error("Unwrap should expose the cause")
	}
}

func TestAgentErrorIsMatching(t *testing.T) {
	err := NewToolValidationError("bad input", nil)
	if !errors.Is(err, ErrToolValidation) {
		t.Error("should match sentinel ErrToolValidation")
	}
	if errors.Is(err, ErrPlanner) {
		t.Error("should not match sentinel ErrPlanner")
	}
}

func TestErrorClassifications(t *testing.T) {
	tests := []struct {
		name  string
		err   *AgentError
		class ErrorClassification
	}{
		{"planner recoverable", NewPlannerError("x", nil), ClassRecoverable},
		{"llm retriable", NewLLMError("x", nil, true), ClassRetriable},
		{"llm terminal", NewLLMError("x", nil, false), ClassTerminal},
		{"tool validation recoverable", NewToolValidationError("x", nil), ClassRecoverable},
		{"tool exec recoverable", NewToolExecutionError("x", nil, false), ClassRecoverable},
		{"tool exec fatal", NewToolExecutionError("x", nil, true), ClassTerminal},
		{"timeout tool recoverable", NewTimeoutError("x", nil, false), ClassRecoverable},
		{"timeout run terminal", NewTimeoutError("x", nil, true), ClassTerminal},
		{"limit exceeded terminal", NewLimitExceededError("x"), ClassTerminal},
		{"policy violation terminal", NewPolicyViolationError("x"), ClassTerminal},
		{"parse recoverable", NewParseError("x", nil), ClassRecoverable},
		{"replay terminal", NewReplayError("x", nil), ClassTerminal},
		{"sandbox recoverable", NewSandboxResourceError("x", nil), ClassRecoverable},
		{"config terminal", NewConfigurationError("x", nil), ClassTerminal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Classification != tt.class {
				t.Errorf("got classification %d, want %d", tt.err.Classification, tt.class)
			}
		})
	}
}
