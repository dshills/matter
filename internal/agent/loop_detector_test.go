package agent

import (
	"fmt"
	"testing"

	"github.com/dshills/matter/pkg/matter"
)

func TestLoopDetectorNoRepeatInitially(t *testing.T) {
	ld := NewLoopDetector(2)
	if ld.IsRepeated() {
		t.Error("should not detect repetition with no history")
	}
}

func TestLoopDetectorSingleCallNotRepeated(t *testing.T) {
	ld := NewLoopDetector(2)
	ld.RecordCall("read", map[string]any{"path": "a.txt"})
	if ld.IsRepeated() {
		t.Error("single call should not be repeated")
	}
}

func TestLoopDetectorRepeatedCalls(t *testing.T) {
	ld := NewLoopDetector(2)
	input := map[string]any{"path": "a.txt"}
	ld.RecordCall("read", input)
	ld.RecordCall("read", input)
	if !ld.IsRepeated() {
		t.Error("should detect 2 identical calls with threshold=2")
	}
}

func TestLoopDetectorDifferentInputsNotRepeated(t *testing.T) {
	ld := NewLoopDetector(2)
	ld.RecordCall("read", map[string]any{"path": "a.txt"})
	ld.RecordCall("read", map[string]any{"path": "b.txt"})
	if ld.IsRepeated() {
		t.Error("different inputs should not be counted as repeated")
	}
}

func TestLoopDetectorSlidingWindow(t *testing.T) {
	ld := NewLoopDetector(2)
	// Window = 2*2 = 4 steps.
	input := map[string]any{"path": "a.txt"}
	ld.RecordCall("read", input)
	// Add different calls to push the first one out of window.
	for i := range 3 {
		ld.RecordCall("write", map[string]any{"data": fmt.Sprintf("%d", i)})
	}
	// Now add the same read again — only 1 in the window of 4.
	ld.RecordCall("read", input)
	if ld.IsRepeated() {
		t.Error("only 1 match in window, should not be repeated")
	}
}

func TestLoopDetectorZeroThreshold(t *testing.T) {
	ld := NewLoopDetector(0)
	ld.RecordCall("read", map[string]any{"path": "a.txt"})
	ld.RecordCall("read", map[string]any{"path": "a.txt"})
	if ld.IsRepeated() {
		t.Error("zero threshold should never detect repetition")
	}
}

func TestCheckProgressCompleteIsProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeComplete}
	if !ld.CheckProgress(decision, nil, nil) {
		t.Error("complete decision should be progress")
	}
}

func TestCheckProgressFailIsProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeFail}
	if !ld.CheckProgress(decision, nil, nil) {
		t.Error("fail decision should be progress")
	}
}

func TestCheckProgressErrorIsNotProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeTool}
	if ld.CheckProgress(decision, nil, fmt.Errorf("some error")) {
		t.Error("error should not be progress")
	}
}

func TestCheckProgressToolErrorResultNotProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeTool}
	result := &matter.ToolResult{Error: "tool failed"}
	if ld.CheckProgress(decision, result, nil) {
		t.Error("tool error result should not be progress")
	}
}

func TestCheckProgressTruncatedIsProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeTool}
	result := &matter.ToolResult{Output: "data...\n[TRUNCATED]"}
	if !ld.CheckProgress(decision, result, nil) {
		t.Error("truncated result should be progress")
	}
}

func TestCheckProgressNewResultIsProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeTool}
	result1 := &matter.ToolResult{Output: "output1"}
	result2 := &matter.ToolResult{Output: "output2"}

	if !ld.CheckProgress(decision, result1, nil) {
		t.Error("first result should be progress")
	}
	if !ld.CheckProgress(decision, result2, nil) {
		t.Error("different result should be progress")
	}
}

func TestCheckProgressSameResultNoProgress(t *testing.T) {
	ld := NewLoopDetector(2)
	decision := matter.Decision{Type: matter.DecisionTypeTool}
	result := &matter.ToolResult{Output: "same output"}

	ld.CheckProgress(decision, result, nil) // first time — progress
	if ld.CheckProgress(decision, result, nil) {
		t.Error("same result should not be progress")
	}
}
