// Package agent provides the core agent loop and run orchestration.
// Error types are defined in internal/errtype to avoid import cycles.
package agent

import "github.com/dshills/matter/internal/errtype"

// Re-export error types so existing code within the agent package can use them directly.
type (
	ErrorCategory       = errtype.ErrorCategory
	ErrorClassification = errtype.ErrorClassification
	AgentError          = errtype.AgentError
)

// Re-export error category constants.
const (
	ErrCatPlanner         = errtype.ErrCatPlanner
	ErrCatLLM             = errtype.ErrCatLLM
	ErrCatToolValidation  = errtype.ErrCatToolValidation
	ErrCatToolExecution   = errtype.ErrCatToolExecution
	ErrCatTimeout         = errtype.ErrCatTimeout
	ErrCatLimitExceeded   = errtype.ErrCatLimitExceeded
	ErrCatPolicyViolation = errtype.ErrCatPolicyViolation
	ErrCatParse           = errtype.ErrCatParse
	ErrCatReplay          = errtype.ErrCatReplay
	ErrCatSandboxResource = errtype.ErrCatSandboxResource
	ErrCatConfiguration   = errtype.ErrCatConfiguration
)

// Re-export classification constants.
const (
	ClassRetriable   = errtype.ClassRetriable
	ClassRecoverable = errtype.ClassRecoverable
	ClassTerminal    = errtype.ClassTerminal
)

// Re-export sentinel errors.
var (
	ErrPlanner         = errtype.ErrPlanner
	ErrLLM             = errtype.ErrLLM
	ErrToolValidation  = errtype.ErrToolValidation
	ErrToolExecution   = errtype.ErrToolExecution
	ErrTimeout         = errtype.ErrTimeout
	ErrLimitExceeded   = errtype.ErrLimitExceeded
	ErrPolicyViolation = errtype.ErrPolicyViolation
	ErrParse           = errtype.ErrParse
	ErrReplay          = errtype.ErrReplay
	ErrSandboxResource = errtype.ErrSandboxResource
	ErrConfiguration   = errtype.ErrConfiguration
)

// Re-export constructor functions.
var (
	NewPlannerError         = errtype.NewPlannerError
	NewLLMError             = errtype.NewLLMError
	NewToolValidationError  = errtype.NewToolValidationError
	NewToolExecutionError   = errtype.NewToolExecutionError
	NewTimeoutError         = errtype.NewTimeoutError
	NewLimitExceededError   = errtype.NewLimitExceededError
	NewPolicyViolationError = errtype.NewPolicyViolationError
	NewParseError           = errtype.NewParseError
	NewReplayError          = errtype.NewReplayError
	NewSandboxResourceError = errtype.NewSandboxResourceError
	NewConfigurationError   = errtype.NewConfigurationError
)
