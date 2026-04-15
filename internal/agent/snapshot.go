package agent

import (
	"encoding/json"
	"log"

	"github.com/dshills/matter/internal/memory"
	"github.com/dshills/matter/pkg/matter"
)

// snapshotSizeWarning is the threshold (10 MB) above which a warning is logged.
const snapshotSizeWarning = 10 * 1024 * 1024

// AgentSnapshot holds the serializable state of an agent for pause/resume.
// Non-serializable components (LLM client, tool registry, policy checker,
// observer) are reconstructed from config on resume.
type AgentSnapshot struct {
	Messages   []matter.Message  `json:"messages"`
	RunMetrics RunMetrics        `json:"run_metrics"`
	LoopState  LoopDetectorState `json:"loop_state"`
	Task       string            `json:"task"`
	Workspace  string            `json:"workspace"`
}

// LoopDetectorState is the serializable state of a LoopDetector.
type LoopDetectorState struct {
	ToolCounts       map[string]int `json:"tool_counts"`
	ConsecutiveCount int            `json:"consecutive_count"`
	LastToolSig      string         `json:"last_tool_sig"`
	History          []CallRecord   `json:"history"`
	PrevResult       string         `json:"prev_result"`
}

// CallRecord is the exported form of callRecord for serialization.
type CallRecord struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

// State returns the serializable state of the loop detector.
func (ld *LoopDetector) State() LoopDetectorState {
	// Build tool call counts from history.
	counts := make(map[string]int, len(ld.history))
	var lastSig string
	consecutiveCount := 0

	for i, rec := range ld.history {
		sig := rec.Name + ":" + rec.Input
		counts[sig]++
		if i == len(ld.history)-1 {
			lastSig = sig
		}
	}

	// Count consecutive identical calls from the end.
	if len(ld.history) > 0 {
		last := ld.history[len(ld.history)-1]
		for i := len(ld.history) - 1; i >= 0; i-- {
			if ld.history[i].Name == last.Name && ld.history[i].Input == last.Input {
				consecutiveCount++
			} else {
				break
			}
		}
	}

	history := make([]CallRecord, len(ld.history))
	for i, rec := range ld.history {
		history[i] = CallRecord(rec)
	}

	return LoopDetectorState{
		ToolCounts:       counts,
		ConsecutiveCount: consecutiveCount,
		LastToolSig:      lastSig,
		History:          history,
		PrevResult:       ld.prevResult,
	}
}

// RestoreState restores the loop detector from a serialized state.
// Only history and prevResult are restored because those are the only
// mutable fields on LoopDetector. The ToolCounts, ConsecutiveCount, and
// LastToolSig fields in LoopDetectorState are derived from history by
// State() and are not stored in the detector — they exist in the state
// struct solely for external inspection and round-trip verification.
func (ld *LoopDetector) RestoreState(s LoopDetectorState) {
	ld.history = make([]callRecord, len(s.History))
	for i, rec := range s.History {
		ld.history[i] = callRecord(rec)
	}
	ld.prevResult = s.PrevResult
}

// Snapshot captures the agent's serializable state for pause/resume.
func (a *Agent) Snapshot() AgentSnapshot {
	return AgentSnapshot{
		Messages:   a.memory.Messages(),
		RunMetrics: a.metrics,
		LoopState:  a.detector.State(),
	}
}

// RestoreFromSnapshot restores agent state from a deserialized snapshot.
// The agent must already be constructed with config and dependencies via New().
// Memory, metrics, and loop detector are restored from the snapshot.
func (a *Agent) RestoreFromSnapshot(snap AgentSnapshot) {
	a.memory = memory.NewManager(a.cfg.Memory, a.llmClient)
	a.memory.RestoreMessages(snap.Messages)
	a.metrics = snap.RunMetrics
	a.detector = NewLoopDetector(a.cfg.Agent.MaxRepeatedToolCalls)
	a.detector.RestoreState(snap.LoopState)
}

// MarshalSnapshot serializes an AgentSnapshot to JSON.
// Logs a warning if the serialized size exceeds 10 MB.
func MarshalSnapshot(snap AgentSnapshot) ([]byte, error) {
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	if len(data) > snapshotSizeWarning {
		log.Printf("agent: snapshot size %d bytes exceeds 10MB warning threshold", len(data))
	}
	return data, nil
}

// UnmarshalSnapshot deserializes an AgentSnapshot from JSON.
func UnmarshalSnapshot(data []byte) (AgentSnapshot, error) {
	var snap AgentSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return AgentSnapshot{}, err
	}
	return snap, nil
}
