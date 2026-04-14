// Package matter provides the public API and shared types for the matter agent framework.
package matter

import (
	"context"
	"time"
)

// MessageRole identifies the sender of a message.
type MessageRole string

const (
	RoleUser    MessageRole = "user"
	RoleSystem  MessageRole = "system"
	RolePlanner MessageRole = "assistant" // maps to LLM provider's assistant role
	RoleTool    MessageRole = "tool"
)

// Message is the canonical message type shared across all modules.
type Message struct {
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	Timestamp time.Time   `json:"timestamp"`
	Step      int         `json:"step"`
}

// DecisionType classifies the planner's decision.
type DecisionType string

const (
	DecisionTypeTool     DecisionType = "tool"
	DecisionTypeComplete DecisionType = "complete"
	DecisionTypeFail     DecisionType = "fail"
	DecisionTypeAsk      DecisionType = "ask"
)

// Decision represents a parsed planner output.
type Decision struct {
	Type      DecisionType `json:"type"`
	Reasoning string       `json:"reasoning"`
	ToolCall  *ToolCall    `json:"tool_call,omitempty"`
	Final     *FinalAnswer `json:"final,omitempty"`
	Ask       *AskRequest  `json:"ask,omitempty"`
}

// AskRequest represents a question the agent needs answered before proceeding.
type AskRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// ToolCall represents a request to invoke a registered tool.
type ToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// FinalAnswer represents the agent's concluding response.
type FinalAnswer struct {
	Summary string `json:"summary"`
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ToolExecuteFunc is the function signature for tool execution.
type ToolExecuteFunc func(ctx context.Context, input map[string]any) (ToolResult, error)

// Tool defines a registered tool available to the agent.
type Tool struct {
	Name         string
	Description  string
	InputSchema  []byte // Must be a valid JSON Schema document
	Timeout      time.Duration
	Safe         bool
	SideEffect   bool
	FatalOnError bool // If true, execution errors terminate the run
	Execute      ToolExecuteFunc
}

// RunRequest is the input for a matter run.
type RunRequest struct {
	Task      string
	Workspace string
}

// RunResult is the output of a matter run.
type RunResult struct {
	FinalSummary string
	Steps        int
	TotalTokens  int
	TotalCostUSD float64
	Success      bool
	Error        error
	Paused       bool        // true if the run is waiting for user input
	Question     *AskRequest // set when Paused is true
}

// ProgressEvent describes a step-level event during a run.
type ProgressEvent struct {
	RunID     string         `json:"run_id"`
	Step      int            `json:"step"`
	Event     string         `json:"event"` // "run_started", "planner_started", "planner_completed", "planner_failed", "tool_started", "tool_completed", "limit_exceeded", "run_completed"
	Data      map[string]any `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
}

// ProgressFunc is called synchronously for each step-level event during a run.
// The agent loop is intentionally suspended until the callback returns (per
// spec §4.2) so consumers see events in strict order. Implementations should
// return within 500ms; callbacks exceeding this are not terminated but will
// trigger a warning log. Panics are recovered and logged without affecting
// the run. Asynchronous dispatch is the caller's responsibility if needed.
type ProgressFunc func(event ProgressEvent)
