package tools

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// ExecutionRecord captures metadata about a single tool execution.
type ExecutionRecord struct {
	StepID    int               `json:"step_id"`
	CallID    int64             `json:"call_id"`
	ToolName  string            `json:"tool_name"`
	Input     map[string]any    `json:"input"`
	Result    matter.ToolResult `json:"result"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Duration  time.Duration     `json:"duration"`
	Error     string            `json:"error,omitempty"`
}

// Executor runs tools with validation, timeout, and metadata tracking.
type Executor struct {
	registry    *Registry
	schemaCache *SchemaCache
	callSeq     atomic.Int64
}

// NewExecutor creates a tool executor backed by the given registry.
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry:    registry,
		schemaCache: NewSchemaCache(),
	}
}

// Execute runs a tool by name with the given input. It validates the input
// against the tool's schema, enforces the tool's timeout, and captures
// execution metadata.
func (e *Executor) Execute(ctx context.Context, stepID int, name string, input map[string]any) ExecutionRecord {
	callID := e.callSeq.Add(1)
	rec := ExecutionRecord{
		StepID:   stepID,
		CallID:   callID,
		ToolName: name,
		Input:    input,
	}

	tool, ok := e.registry.Get(name)
	if !ok {
		rec.Error = fmt.Sprintf("tool %q not found", name)
		rec.Result = matter.ToolResult{Error: rec.Error}
		return rec
	}

	if err := ValidateInputCached(e.schemaCache, name, tool.InputSchema, input); err != nil {
		rec.Error = err.Error()
		rec.Result = matter.ToolResult{Error: rec.Error}
		return rec
	}

	timeout := tool.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rec.StartTime = time.Now()
	result, err := tool.Execute(execCtx, input)
	rec.EndTime = time.Now()
	rec.Duration = rec.EndTime.Sub(rec.StartTime)
	rec.Result = result

	if err != nil {
		rec.Error = err.Error()
		if rec.Result.Error == "" {
			rec.Result.Error = err.Error()
		}
	}

	return rec
}
