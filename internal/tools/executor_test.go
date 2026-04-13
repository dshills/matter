package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

func TestExecutorSuccess(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(matter.Tool{
		Name:        "echo",
		Description: "echoes input",
		InputSchema: []byte(`{"type":"object"}`),
		Timeout:     5 * time.Second,
		Execute: func(_ context.Context, input map[string]any) (matter.ToolResult, error) {
			return matter.ToolResult{Output: "hello"}, nil
		},
	})

	e := NewExecutor(r)
	rec := e.Execute(context.Background(), 1, "echo", map[string]any{})

	if rec.Error != "" {
		t.Errorf("unexpected error: %s", rec.Error)
	}
	if rec.Result.Output != "hello" {
		t.Errorf("output = %q, want hello", rec.Result.Output)
	}
	if rec.StepID != 1 {
		t.Errorf("step ID = %d, want 1", rec.StepID)
	}
	if rec.CallID != 1 {
		t.Errorf("call ID = %d, want 1", rec.CallID)
	}
	if rec.StartTime.IsZero() || rec.EndTime.IsZero() {
		t.Error("start/end times should be set")
	}
	if rec.Duration <= 0 {
		t.Error("duration should be positive")
	}
	if rec.ToolName != "echo" {
		t.Errorf("tool name = %q, want echo", rec.ToolName)
	}
}

func TestExecutorCallIDIncrement(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(matter.Tool{
		Name:        "noop",
		InputSchema: []byte(`{"type":"object"}`),
		Timeout:     time.Second,
		Execute: func(_ context.Context, _ map[string]any) (matter.ToolResult, error) {
			return matter.ToolResult{}, nil
		},
	})

	e := NewExecutor(r)
	rec1 := e.Execute(context.Background(), 1, "noop", map[string]any{})
	rec2 := e.Execute(context.Background(), 2, "noop", map[string]any{})

	if rec2.CallID != rec1.CallID+1 {
		t.Errorf("call IDs should increment: got %d and %d", rec1.CallID, rec2.CallID)
	}
}

func TestExecutorToolNotFound(t *testing.T) {
	r := NewRegistry()
	e := NewExecutor(r)
	rec := e.Execute(context.Background(), 1, "missing", map[string]any{})

	if rec.Error == "" {
		t.Error("expected error for missing tool")
	}
	if rec.Result.Error == "" {
		t.Error("result error should be set")
	}
}

func TestExecutorValidationFailure(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(matter.Tool{
		Name:        "strict",
		InputSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		Timeout:     time.Second,
		Execute: func(_ context.Context, _ map[string]any) (matter.ToolResult, error) {
			t.Error("execute should not be called on validation failure")
			return matter.ToolResult{}, nil
		},
	})

	e := NewExecutor(r)
	rec := e.Execute(context.Background(), 1, "strict", map[string]any{})

	if rec.Error == "" {
		t.Error("expected validation error")
	}
}

func TestExecutorTimeout(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(matter.Tool{
		Name:        "slow",
		InputSchema: []byte(`{"type":"object"}`),
		Timeout:     50 * time.Millisecond,
		Execute: func(ctx context.Context, _ map[string]any) (matter.ToolResult, error) {
			select {
			case <-ctx.Done():
				return matter.ToolResult{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return matter.ToolResult{Output: "should not reach"}, nil
			}
		},
	})

	e := NewExecutor(r)
	rec := e.Execute(context.Background(), 1, "slow", map[string]any{})

	if rec.Error == "" {
		t.Error("expected timeout error")
	}
	if rec.Duration > time.Second {
		t.Errorf("execution took too long: %v", rec.Duration)
	}
}

func TestExecutorToolError(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(matter.Tool{
		Name:        "failing",
		InputSchema: []byte(`{"type":"object"}`),
		Timeout:     time.Second,
		Execute: func(_ context.Context, _ map[string]any) (matter.ToolResult, error) {
			return matter.ToolResult{Output: "partial"}, errors.New("tool broke")
		},
	})

	e := NewExecutor(r)
	rec := e.Execute(context.Background(), 1, "failing", map[string]any{})

	if rec.Error != "tool broke" {
		t.Errorf("error = %q, want 'tool broke'", rec.Error)
	}
	if rec.Result.Output != "partial" {
		t.Errorf("output should preserve partial result: %q", rec.Result.Output)
	}
	if rec.Result.Error != "tool broke" {
		t.Errorf("result error = %q, want 'tool broke'", rec.Result.Error)
	}
}
