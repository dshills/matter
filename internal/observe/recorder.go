package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/storage"
)

// StepRecord captures a single step in the run recording.
type StepRecord struct {
	Step         int            `json:"step"`
	Timestamp    time.Time      `json:"timestamp"`
	PlannerInput string         `json:"planner_input,omitempty"`
	RawResponse  string         `json:"raw_response,omitempty"`
	Decision     string         `json:"decision,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	ToolInput    map[string]any `json:"tool_input,omitempty"`
	ToolOutput   string         `json:"tool_output,omitempty"`
	ToolError    string         `json:"tool_error,omitempty"`
	Error        string         `json:"error,omitempty"`
	Tokens       int            `json:"tokens,omitempty"`
	CostUSD      float64        `json:"cost_usd,omitempty"`
}

// SafeConfig is a sanitized copy of config.Config for recording.
// It omits fields that could contain sensitive data.
type SafeConfig struct {
	Agent   config.AgentConfig   `json:"agent"`
	Memory  config.MemoryConfig  `json:"memory"`
	Tools   config.ToolsConfig   `json:"tools"`
	Sandbox config.SandboxConfig `json:"sandbox"`
	Observe config.ObserveConfig `json:"observe"`
	LLM     SafeLLMConfig        `json:"llm"`
}

// SafeLLMConfig records LLM settings without sensitive credentials.
type SafeLLMConfig struct {
	Provider   string        `json:"provider"`
	Model      string        `json:"model"`
	Timeout    time.Duration `json:"timeout"`
	MaxRetries int           `json:"max_retries"`
}

// sanitizeConfig creates a SafeConfig from a full config.Config.
func sanitizeConfig(cfg config.Config) SafeConfig {
	return SafeConfig{
		Agent:   cfg.Agent,
		Memory:  cfg.Memory,
		Tools:   cfg.Tools,
		Sandbox: cfg.Sandbox,
		Observe: cfg.Observe,
		LLM: SafeLLMConfig{
			Provider:   cfg.LLM.Provider,
			Model:      cfg.LLM.Model,
			Timeout:    cfg.LLM.Timeout,
			MaxRetries: cfg.LLM.MaxRetries,
		},
	}
}

// RunRecord is the complete recording of a single agent run.
type RunRecord struct {
	RunID       string        `json:"run_id"`
	Task        string        `json:"task"`
	Config      SafeConfig    `json:"config"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time,omitempty"`
	Steps       []StepRecord  `json:"steps"`
	Outcome     OutcomeRecord `json:"outcome"`
	TraceEvents []TraceEvent  `json:"trace_events,omitempty"`
}

// OutcomeRecord captures the final run outcome.
type OutcomeRecord struct {
	Success     bool          `json:"success"`
	Summary     string        `json:"summary,omitempty"`
	TotalSteps  int           `json:"total_steps"`
	Duration    time.Duration `json:"duration"`
	TotalTokens int           `json:"total_tokens"`
	TotalCost   float64       `json:"total_cost_usd"`
}

// Recorder writes run records to disk as JSON files and optionally
// persists steps to a storage.Store.
type Recorder struct {
	mu        sync.Mutex
	record    RunRecord
	recordDir string
	store     storage.Store
	runID     string
}

// NewRecorder creates a recorder for the given run.
func NewRecorder(runID, task string, cfg config.Config, recordDir string) *Recorder {
	return &Recorder{
		record: RunRecord{
			RunID:     runID,
			Task:      task,
			Config:    sanitizeConfig(cfg),
			StartTime: time.Now(),
		},
		recordDir: recordDir,
		runID:     runID,
	}
}

// SetStore configures a persistent store for step recording.
// When set, RecordStep writes each step to the store incrementally.
func (r *Recorder) SetStore(store storage.Store) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = store
}

// RecordStep adds a step to the run record and optionally writes it
// to the store. Store errors are logged but do not affect the run.
func (r *Recorder) RecordStep(step StepRecord) {
	r.mu.Lock()
	r.record.Steps = append(r.record.Steps, step)
	store := r.store
	runID := r.runID
	r.mu.Unlock()

	if store != nil {
		toolInputJSON := ""
		if step.ToolInput != nil {
			data, err := json.Marshal(step.ToolInput)
			if err != nil {
				log.Printf("recorder: failed to marshal tool input for step %d run %s: %v", step.Step, runID, err)
			} else {
				toolInputJSON = string(data)
			}
		}
		row := &storage.StepRow{
			StepNumber:  step.Step,
			Timestamp:   step.Timestamp,
			Decision:    step.Decision,
			ToolName:    step.ToolName,
			ToolInput:   toolInputJSON,
			ToolOutput:  step.ToolOutput,
			ToolError:   step.ToolError,
			RawResponse: step.RawResponse,
			Tokens:      step.Tokens,
			CostUSD:     step.CostUSD,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.AppendStep(ctx, runID, row); err != nil {
			log.Printf("recorder: failed to persist step %d for run %s: %v", step.Step, runID, err)
		}
	}
}

// SetOutcome sets the final outcome of the run and updates the store
// if one is configured.
func (r *Recorder) SetOutcome(success bool, summary string, steps int, duration time.Duration, tokens int, cost float64) {
	r.mu.Lock()
	now := time.Now()
	r.record.EndTime = now
	r.record.Outcome = OutcomeRecord{
		Success:     success,
		Summary:     summary,
		TotalSteps:  steps,
		Duration:    duration,
		TotalTokens: tokens,
		TotalCost:   cost,
	}
	store := r.store
	runID := r.runID
	r.mu.Unlock()

	if store != nil {
		status := "completed"
		if !success {
			status = "failed"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := store.UpdateRun(ctx, &storage.RunRow{
			RunID:        runID,
			Status:       status,
			Summary:      summary,
			Success:      &success,
			CompletedAt:  &now,
			Steps:        steps,
			TotalTokens:  tokens,
			TotalCostUSD: cost,
			DurationMS:   duration.Milliseconds(),
			UpdatedAt:    now,
		})
		if err != nil {
			log.Printf("recorder: failed to update run %s outcome in store: %v", runID, err)
		}
	}
}

// SetTraceEvents attaches trace events to the record.
func (r *Recorder) SetTraceEvents(events []TraceEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record.TraceEvents = events
}

// Flush writes the run record to disk as a JSON file.
// This is retained for backward compatibility alongside store persistence.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	rec := r.record
	r.mu.Unlock()

	if err := os.MkdirAll(r.recordDir, 0o755); err != nil {
		return fmt.Errorf("failed to create record directory: %w", err)
	}

	filename := fmt.Sprintf("%s.json", rec.RunID)
	path := filepath.Join(r.recordDir, filename)

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run record: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write run record: %w", err)
	}

	return nil
}

// Record returns the current run record (for testing).
func (r *Recorder) Record() RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.record
}
