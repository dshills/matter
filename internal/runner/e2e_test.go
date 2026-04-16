package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// e2e tests run against real LLM providers.
// They are skipped when the required API key is not set.
//
// Run all e2e tests:
//   OPENAI_API_KEY=sk-... ANTHROPIC_API_KEY=sk-ant-... GEMINI_API_KEY=AI... go test ./internal/runner/ -run TestE2E -v -timeout 120s
//
// Run a single provider:
//   OPENAI_API_KEY=sk-... go test ./internal/runner/ -run TestE2E/OpenAI -v -timeout 120s

// providerSpec defines a provider to test.
type providerSpec struct {
	name     string // test name
	envVar   string // env var that must be set
	provider string
	model    string
	timeout  time.Duration
}

// providers returns the list of providers to test.
// Each is skipped at runtime if the required env var is not set.
func providers() []providerSpec {
	return []providerSpec{
		{
			name:     "OpenAI",
			envVar:   "OPENAI_API_KEY",
			provider: "openai",
			model:    "gpt-4o-mini",
			timeout:  30 * time.Second,
		},
		{
			name:     "Anthropic",
			envVar:   "ANTHROPIC_API_KEY",
			provider: "anthropic",
			model:    "claude-haiku-4-5-20251001",
			timeout:  30 * time.Second,
		},
		{
			name:     "Gemini",
			envVar:   "GEMINI_API_KEY",
			provider: "gemini",
			model:    "gemini-2.0-flash",
			timeout:  30 * time.Second,
		},
	}
}

// skipIfNoKey skips the test if the required env var is not set.
func skipIfNoKey(t *testing.T, envVar string) string {
	t.Helper()
	key := os.Getenv(envVar)
	if key == "" {
		t.Skipf("skipping: %s not set", envVar)
	}
	return key
}

// newE2ERunner creates a runner with a real LLM client for the given provider.
func newE2ERunner(t *testing.T, spec providerSpec, apiKey string) *Runner {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.LLM.Provider = spec.provider
	cfg.LLM.Model = spec.model
	cfg.LLM.Timeout = spec.timeout
	cfg.LLM.MaxRetries = 2
	cfg.Agent.MaxSteps = 10
	cfg.Agent.MaxDuration = 90 * time.Second
	cfg.Agent.MaxCostUSD = 1.00
	cfg.Agent.MaxAsks = 0
	cfg.Tools.EnableWorkspaceRead = true
	cfg.Tools.EnableWorkspaceWrite = false
	cfg.Tools.EnableWebFetch = false
	cfg.Tools.EnableCommandExec = false
	cfg.Observe.LogLevel = "warn"
	cfg.Observe.RecordRuns = false

	client, err := llm.NewClient(llm.ProviderConfig{
		Provider: spec.provider,
		Model:    spec.model,
		APIKey:   apiKey,
		Timeout:  spec.timeout,
	})
	if err != nil {
		t.Fatalf("NewClient(%s): %v", spec.provider, err)
	}

	r, err := New(cfg, client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return r
}

// setupWorkspace creates a temp directory with test files.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "README.md", "# Test Project\n\nA small test project for e2e testing.\n")
	writeFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}
`)
	writeFile(t, dir, "util.go", `package main

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// Multiply returns the product of two integers.
func Multiply(a, b int) int {
	return a * b
}
`)

	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// Test: Simple completion (no tools)
// The agent should answer a direct question without needing tools.
// ---------------------------------------------------------------------------

func TestE2E_SimpleCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result := r.Run(ctx, matter.RunRequest{
				Task:      "What is 2 + 2? Answer with just the number.",
				Workspace: t.TempDir(),
			})

			if result.Error != nil {
				t.Fatalf("run failed: %v", result.Error)
			}
			if !result.Success {
				t.Fatal("expected success")
			}
			if result.FinalSummary == "" {
				t.Error("expected non-empty summary")
			}
			if result.Steps < 1 {
				t.Errorf("steps = %d, want >= 1", result.Steps)
			}
			if result.TotalTokens < 1 {
				t.Errorf("tokens = %d, want >= 1", result.TotalTokens)
			}

			t.Logf("summary=%q steps=%d tokens=%d cost=$%.4f",
				result.FinalSummary, result.Steps, result.TotalTokens, result.TotalCostUSD)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: File reading with workspace_read tool
// The agent should use workspace_read to read a file and answer about it.
// ---------------------------------------------------------------------------

func TestE2E_ReadFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)
			workspace := setupWorkspace(t)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result := r.Run(ctx, matter.RunRequest{
				Task:      "Read the file main.go and tell me what the main function prints. Answer with just the printed string.",
				Workspace: workspace,
			})

			if result.Error != nil {
				t.Fatalf("run failed: %v", result.Error)
			}
			if !result.Success {
				t.Fatal("expected success")
			}

			// The agent should mention "hello world" in its summary.
			lower := strings.ToLower(result.FinalSummary)
			if !strings.Contains(lower, "hello world") {
				t.Errorf("summary %q does not contain 'hello world'", result.FinalSummary)
			}
			if result.Steps < 2 {
				t.Errorf("steps = %d, want >= 2 (plan + tool call)", result.Steps)
			}

			t.Logf("summary=%q steps=%d tokens=%d cost=$%.4f",
				result.FinalSummary, result.Steps, result.TotalTokens, result.TotalCostUSD)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Multi-file analysis
// The agent should read multiple files and synthesize information.
// ---------------------------------------------------------------------------

func TestE2E_MultiFileAnalysis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)
			workspace := setupWorkspace(t)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result := r.Run(ctx, matter.RunRequest{
				Task:      "Read util.go and list the names of all functions defined in it. Answer with just the function names separated by commas.",
				Workspace: workspace,
			})

			if result.Error != nil {
				t.Fatalf("run failed: %v", result.Error)
			}
			if !result.Success {
				t.Fatal("expected success")
			}

			lower := strings.ToLower(result.FinalSummary)
			if !strings.Contains(lower, "add") {
				t.Errorf("summary %q does not mention 'Add'", result.FinalSummary)
			}
			if !strings.Contains(lower, "multiply") {
				t.Errorf("summary %q does not mention 'Multiply'", result.FinalSummary)
			}

			t.Logf("summary=%q steps=%d tokens=%d cost=$%.4f",
				result.FinalSummary, result.Steps, result.TotalTokens, result.TotalCostUSD)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Token and cost tracking
// Verify that metrics are populated after a real run.
// ---------------------------------------------------------------------------

func TestE2E_MetricsTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result := r.Run(ctx, matter.RunRequest{
				Task:      "Say 'ok'.",
				Workspace: t.TempDir(),
			})

			if result.Error != nil {
				t.Fatalf("run failed: %v", result.Error)
			}

			if result.TotalTokens <= 0 {
				t.Errorf("TotalTokens = %d, want > 0", result.TotalTokens)
			}
			if result.Steps <= 0 {
				t.Errorf("Steps = %d, want > 0", result.Steps)
			}
			// Cost may be 0 for models not in the pricing table,
			// but tokens should always be tracked.

			t.Logf("steps=%d tokens=%d cost=$%.6f",
				result.Steps, result.TotalTokens, result.TotalCostUSD)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Budget enforcement
// Set a very low step limit and verify the run terminates.
// ---------------------------------------------------------------------------

func TestE2E_BudgetEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Only test with one provider — budget enforcement is provider-independent.
	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)

			cfg := config.DefaultConfig()
			cfg.LLM.Provider = spec.provider
			cfg.LLM.Model = spec.model
			cfg.LLM.Timeout = spec.timeout
			cfg.LLM.MaxRetries = 1
			cfg.Agent.MaxSteps = 2 // very low limit
			cfg.Agent.MaxDuration = 90 * time.Second
			cfg.Agent.MaxCostUSD = 1.00
			cfg.Agent.MaxAsks = 0
			cfg.Tools.EnableWorkspaceRead = true
			cfg.Tools.EnableWorkspaceWrite = false
			cfg.Tools.EnableWebFetch = false
			cfg.Tools.EnableCommandExec = false
			cfg.Observe.LogLevel = "warn"
			cfg.Observe.RecordRuns = false

			client, err := llm.NewClient(llm.ProviderConfig{
				Provider: spec.provider,
				Model:    spec.model,
				APIKey:   apiKey,
				Timeout:  spec.timeout,
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			r, err := New(cfg, client)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			workspace := setupWorkspace(t)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			// Give a task complex enough to need more than 2 steps.
			result := r.Run(ctx, matter.RunRequest{
				Task:      "Read every file in the workspace, then write a detailed summary of each one.",
				Workspace: workspace,
			})

			// The run should terminate (either success within 2 steps or
			// failure from hitting the step limit). Either way, it should
			// not hang or exceed the step budget.
			if result.Steps > 2 {
				t.Errorf("steps = %d, exceeded max_steps=2", result.Steps)
			}

			t.Logf("success=%v steps=%d error=%v", result.Success, result.Steps, result.Error)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Context cancellation
// Verify the run terminates promptly when the context is cancelled.
// ---------------------------------------------------------------------------

func TestE2E_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Use only one provider — cancellation is provider-independent.
	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)
			workspace := setupWorkspace(t)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Give a task that would take many steps.
			start := time.Now()
			result := r.Run(ctx, matter.RunRequest{
				Task:      "Read every file in the workspace one by one. After reading each file, write a 500-word analysis of it.",
				Workspace: workspace,
			})
			elapsed := time.Since(start)

			// Should have stopped due to context timeout, not completed naturally.
			if elapsed > 30*time.Second {
				t.Errorf("run took %s, expected to stop within ~5s from context timeout", elapsed)
			}

			t.Logf("success=%v steps=%d elapsed=%s error=%v",
				result.Success, result.Steps, elapsed.Round(time.Millisecond), result.Error)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Progress callback
// Verify that progress events are emitted during a real run.
// ---------------------------------------------------------------------------

func TestE2E_ProgressEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	for _, spec := range providers() {
		t.Run(spec.name, func(t *testing.T) {
			apiKey := skipIfNoKey(t, spec.envVar)
			r := newE2ERunner(t, spec, apiKey)

			var events []matter.ProgressEvent
			r.SetProgressFunc(func(event matter.ProgressEvent) {
				events = append(events, event)
			})

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result := r.Run(ctx, matter.RunRequest{
				Task:      "Say 'hello'.",
				Workspace: t.TempDir(),
			})

			if result.Error != nil {
				t.Fatalf("run failed: %v", result.Error)
			}

			if len(events) == 0 {
				t.Fatal("expected progress events, got none")
			}

			// Should have at least run_started and run_completed.
			eventTypes := make(map[string]bool)
			for _, e := range events {
				eventTypes[e.Event] = true
			}

			if !eventTypes["run_started"] {
				t.Error("missing run_started event")
			}
			if !eventTypes["run_completed"] {
				t.Error("missing run_completed event")
			}

			t.Logf("received %d events: %v", len(events), eventTypeList(eventTypes))
		})
	}
}

func eventTypeList(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
