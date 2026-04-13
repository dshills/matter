package agent

import (
	"errors"
	"fmt"
)

// ErrorCategory identifies the type of error.
type ErrorCategory string

const (
	ErrCatPlanner         ErrorCategory = "planner_error"
	ErrCatLLM             ErrorCategory = "llm_error"
	ErrCatToolValidation  ErrorCategory = "tool_validation_error"
	ErrCatToolExecution   ErrorCategory = "tool_execution_error"
	ErrCatTimeout         ErrorCategory = "timeout_error"
	ErrCatLimitExceeded   ErrorCategory = "limit_exceeded_error"
	ErrCatPolicyViolation ErrorCategory = "policy_violation_error"
	ErrCatParse           ErrorCategory = "parse_error"
	ErrCatReplay          ErrorCategory = "replay_error"
	ErrCatSandboxResource ErrorCategory = "sandbox_resource_error"
	ErrCatConfiguration   ErrorCategory = "configuration_error"
)

// ErrorClassification determines how the agent loop handles an error.
type ErrorClassification int

const (
	// ClassRetriable indicates transient failures that should be retried with backoff.
	ClassRetriable ErrorClassification = iota
	// ClassRecoverable indicates non-transient failures returned to the agent loop for replanning.
	ClassRecoverable
	// ClassTerminal indicates fatal failures that immediately end the run.
	ClassTerminal
)

// AgentError is a typed error with category and classification.
type AgentError struct {
	Category       ErrorCategory
	Classification ErrorClassification
	Message        string
	Cause          error
}

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Category, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Category, e.Message)
}

func (e *AgentError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by category.
func (e *AgentError) Is(target error) bool {
	if t, ok := errors.AsType[*AgentError](target); ok {
		return e.Category == t.Category
	}
	return false
}

// Sentinel errors for errors.Is matching.
var (
	ErrPlanner         = &AgentError{Category: ErrCatPlanner}
	ErrLLM             = &AgentError{Category: ErrCatLLM}
	ErrToolValidation  = &AgentError{Category: ErrCatToolValidation}
	ErrToolExecution   = &AgentError{Category: ErrCatToolExecution}
	ErrTimeout         = &AgentError{Category: ErrCatTimeout}
	ErrLimitExceeded   = &AgentError{Category: ErrCatLimitExceeded}
	ErrPolicyViolation = &AgentError{Category: ErrCatPolicyViolation}
	ErrParse           = &AgentError{Category: ErrCatParse}
	ErrReplay          = &AgentError{Category: ErrCatReplay}
	ErrSandboxResource = &AgentError{Category: ErrCatSandboxResource}
	ErrConfiguration   = &AgentError{Category: ErrCatConfiguration}
)

// Constructor functions for each error category.

func NewPlannerError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatPlanner, Classification: ClassRecoverable, Message: msg, Cause: cause}
}

func NewLLMError(msg string, cause error, retriable bool) *AgentError {
	cls := ClassTerminal
	if retriable {
		cls = ClassRetriable
	}
	return &AgentError{Category: ErrCatLLM, Classification: cls, Message: msg, Cause: cause}
}

func NewToolValidationError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatToolValidation, Classification: ClassRecoverable, Message: msg, Cause: cause}
}

func NewToolExecutionError(msg string, cause error, fatal bool) *AgentError {
	cls := ClassRecoverable
	if fatal {
		cls = ClassTerminal
	}
	return &AgentError{Category: ErrCatToolExecution, Classification: cls, Message: msg, Cause: cause}
}

func NewTimeoutError(msg string, cause error, isRunTimeout bool) *AgentError {
	cls := ClassRecoverable
	if isRunTimeout {
		cls = ClassTerminal
	}
	return &AgentError{Category: ErrCatTimeout, Classification: cls, Message: msg, Cause: cause}
}

func NewLimitExceededError(msg string) *AgentError {
	return &AgentError{Category: ErrCatLimitExceeded, Classification: ClassTerminal, Message: msg}
}

func NewPolicyViolationError(msg string) *AgentError {
	return &AgentError{Category: ErrCatPolicyViolation, Classification: ClassTerminal, Message: msg}
}

func NewParseError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatParse, Classification: ClassRecoverable, Message: msg, Cause: cause}
}

func NewReplayError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatReplay, Classification: ClassTerminal, Message: msg, Cause: cause}
}

func NewSandboxResourceError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatSandboxResource, Classification: ClassRecoverable, Message: msg, Cause: cause}
}

func NewConfigurationError(msg string, cause error) *AgentError {
	return &AgentError{Category: ErrCatConfiguration, Classification: ClassTerminal, Message: msg, Cause: cause}
}
