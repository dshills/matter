package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/memory"
	"github.com/dshills/matter/internal/policy"
	"github.com/dshills/matter/internal/tools"
	"github.com/dshills/matter/pkg/matter"
)

func TestSnapshotRoundTrip(t *testing.T) {
	snap := AgentSnapshot{
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: "You are an agent.", Timestamp: time.Now().Truncate(time.Millisecond), Step: 0},
			{Role: matter.RoleUser, Content: "Do something.", Timestamp: time.Now().Truncate(time.Millisecond), Step: 0},
			{Role: matter.RolePlanner, Content: `{"type":"complete"}`, Timestamp: time.Now().Truncate(time.Millisecond), Step: 1},
		},
		RunMetrics: RunMetrics{
			Steps:              3,
			StartTime:          time.Now().Truncate(time.Millisecond),
			PromptTokens:       500,
			CompletionTokens:   200,
			TotalTokens:        700,
			CostUSD:            0.05,
			ConsecutiveErrors:  1,
			ConsecutiveNoProg:  0,
			RepeatedToolDetect: false,
			AskCount:           1,
			PausedDuration:     2 * time.Second,
		},
		LoopState: LoopDetectorState{
			ToolCounts:       map[string]int{"read:{}": 2, "write:{}": 1},
			ConsecutiveCount: 1,
			LastToolSig:      "write:{}",
			History: []CallRecord{
				{Name: "read", Input: "{}"},
				{Name: "read", Input: "{}"},
				{Name: "write", Input: "{}"},
			},
			PrevResult: "some output",
		},
		Task:      "test task",
		Workspace: "/tmp/test",
	}

	data, err := MarshalSnapshot(snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	restored, err := UnmarshalSnapshot(data)
	if err != nil {
		t.Fatalf("UnmarshalSnapshot: %v", err)
	}

	// Re-serialize to compare.
	data2, err := MarshalSnapshot(restored)
	if err != nil {
		t.Fatalf("MarshalSnapshot (round 2): %v", err)
	}

	if string(data) != string(data2) {
		t.Errorf("round-trip mismatch:\n  original: %s\n  restored: %s", data, data2)
	}
}

func TestSnapshotSizeWarning(t *testing.T) {
	// Create a snapshot with large messages to exceed 10MB.
	largeContent := strings.Repeat("x", 11*1024*1024) // 11MB
	snap := AgentSnapshot{
		Messages: []matter.Message{
			{Role: matter.RoleUser, Content: largeContent},
		},
		RunMetrics: RunMetrics{},
		LoopState:  LoopDetectorState{ToolCounts: map[string]int{}},
	}

	data, err := MarshalSnapshot(snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	// Verify it's actually large.
	if len(data) < snapshotSizeWarning {
		t.Errorf("snapshot size %d, expected > %d", len(data), snapshotSizeWarning)
	}
}

func TestLoopDetectorStateRoundTrip(t *testing.T) {
	ld := NewLoopDetector(5)
	ld.RecordCall("read", map[string]any{"path": "/tmp/a"})
	ld.RecordCall("read", map[string]any{"path": "/tmp/a"})
	ld.RecordCall("write", map[string]any{"path": "/tmp/b"})
	ld.prevResult = "file contents"

	state := ld.State()

	// Verify state captures history.
	if len(state.History) != 3 {
		t.Fatalf("history length = %d, want 3", len(state.History))
	}
	if state.PrevResult != "file contents" {
		t.Errorf("prevResult = %q, want 'file contents'", state.PrevResult)
	}

	// Serialize and deserialize.
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored LoopDetectorState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Create new detector and restore.
	ld2 := NewLoopDetector(5)
	ld2.RestoreState(restored)

	if len(ld2.history) != 3 {
		t.Fatalf("restored history length = %d, want 3", len(ld2.history))
	}
	if ld2.history[0].Name != "read" {
		t.Errorf("history[0].Name = %q, want 'read'", ld2.history[0].Name)
	}
	if ld2.prevResult != "file contents" {
		t.Errorf("restored prevResult = %q, want 'file contents'", ld2.prevResult)
	}

	// Verify state matches after restore.
	state2 := ld2.State()
	data2, _ := json.Marshal(state2)
	if string(data) != string(data2) {
		t.Errorf("state round-trip mismatch:\n  original: %s\n  restored: %s", data, data2)
	}
}

func TestRunMetricsJSONRoundTrip(t *testing.T) {
	m := RunMetrics{
		Steps:              5,
		StartTime:          time.Now().Truncate(time.Millisecond),
		PromptTokens:       1000,
		CompletionTokens:   500,
		TotalTokens:        1500,
		CostUSD:            0.123,
		ConsecutiveErrors:  2,
		ConsecutiveNoProg:  1,
		RepeatedToolDetect: true,
		AskCount:           3,
		PausedDuration:     5 * time.Second,
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored RunMetrics
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Steps != m.Steps {
		t.Errorf("Steps = %d, want %d", restored.Steps, m.Steps)
	}
	if restored.TotalTokens != m.TotalTokens {
		t.Errorf("TotalTokens = %d, want %d", restored.TotalTokens, m.TotalTokens)
	}
	if restored.CostUSD != m.CostUSD {
		t.Errorf("CostUSD = %f, want %f", restored.CostUSD, m.CostUSD)
	}
	if restored.PausedDuration != m.PausedDuration {
		t.Errorf("PausedDuration = %v, want %v", restored.PausedDuration, m.PausedDuration)
	}
	if !restored.RepeatedToolDetect {
		t.Error("RepeatedToolDetect should be true")
	}
	if !restored.StartTime.Equal(m.StartTime) {
		t.Errorf("StartTime = %v, want %v", restored.StartTime, m.StartTime)
	}
}

func TestAgentSnapshotAndRestore(t *testing.T) {
	cfg := testConfig()
	mockClient := llm.NewMockClient(nil, nil)
	registry := tools.NewRegistry()
	policyState := &policy.RunState{
		MaxSteps:       cfg.Agent.MaxSteps,
		MaxTotalTokens: cfg.Agent.MaxTotalTokens,
		MaxCostUSD:     cfg.Agent.MaxCostUSD,
	}
	checker := policy.NewChecker(policyState)

	ag, err := New(cfg, mockClient, registry, checker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Set up memory with two messages.
	ag.memory = memory.NewManager(cfg.Memory, mockClient)
	_ = ag.memory.Add(context.Background(), matter.Message{
		Role: matter.RoleSystem, Content: "system prompt", Timestamp: time.Now(),
	})
	_ = ag.memory.Add(context.Background(), matter.Message{
		Role: matter.RoleUser, Content: "do the thing", Timestamp: time.Now(),
	})

	ag.metrics = RunMetrics{
		Steps:       3,
		StartTime:   time.Now(),
		TotalTokens: 500,
		CostUSD:     0.02,
		AskCount:    1,
	}
	ag.detector = NewLoopDetector(cfg.Agent.MaxRepeatedToolCalls)
	ag.detector.RecordCall("read", map[string]any{"path": "/tmp"})
	ag.detector.prevResult = "contents"

	// Take snapshot.
	snap := ag.Snapshot()

	if len(snap.Messages) != 2 {
		t.Errorf("snapshot messages = %d, want 2", len(snap.Messages))
	}
	if snap.RunMetrics.Steps != 3 {
		t.Errorf("snapshot steps = %d, want 3", snap.RunMetrics.Steps)
	}

	// Create new agent and restore.
	ag2, err := New(cfg, mockClient, registry, checker)
	if err != nil {
		t.Fatalf("New (restore): %v", err)
	}

	ag2.RestoreFromSnapshot(snap)

	if ag2.memory.MessageCount() != 2 {
		t.Errorf("restored messages = %d, want 2", ag2.memory.MessageCount())
	}
	if ag2.metrics.Steps != 3 {
		t.Errorf("restored steps = %d, want 3", ag2.metrics.Steps)
	}
	if ag2.metrics.AskCount != 1 {
		t.Errorf("restored askCount = %d, want 1", ag2.metrics.AskCount)
	}
	if len(ag2.detector.history) != 1 {
		t.Errorf("restored detector history = %d, want 1", len(ag2.detector.history))
	}
	if ag2.detector.prevResult != "contents" {
		t.Errorf("restored prevResult = %q, want 'contents'", ag2.detector.prevResult)
	}
}
