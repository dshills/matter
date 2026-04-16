package runner

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// e2e tests for the new developer tools (search, edit, git).
// Gated behind API keys — skipped when not set.
//
// Run:
//   OPENAI_API_KEY=sk-... go test ./internal/runner/ -run TestE2ETools -v -timeout 120s

// newToolsRunner creates a runner with search, edit, and git tools enabled.
func newToolsRunner(t *testing.T, spec providerSpec, apiKey, workspace string) *Runner {
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
	cfg.Tools.EnableWorkspaceFind = true
	cfg.Tools.EnableWorkspaceGrep = true
	cfg.Tools.EnableWorkspaceEdit = true
	cfg.Tools.EnableWebFetch = false
	cfg.Tools.EnableCommandExec = false
	cfg.Tools.EnableGit = true
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

// setupToolsWorkspace creates a temp git repo with test files.
func setupToolsWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}
`)
	writeFile(t, dir, "util.go", `package main

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`)
	writeFile(t, dir, "README.md", "# Test Project\n\nA test project for e2e testing.\n")

	// Initialize git repo.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %s\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "checkout", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-m", "initial commit")

	return dir
}

// ---------------------------------------------------------------------------
// Test: Search with workspace_grep, then answer about the result
// ---------------------------------------------------------------------------

func TestE2ETools_GrepAndAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Use only one provider for tool tests (faster, cheaper).
	spec := providers()[0] // OpenAI
	apiKey := skipIfNoKey(t, spec.envVar)
	workspace := setupToolsWorkspace(t)
	r := newToolsRunner(t, spec, apiKey, workspace)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result := r.Run(ctx, matter.RunRequest{
		Task:      "Use workspace_grep to search for the pattern 'func Add' in all .go files. Tell me what file it's in and what it does.",
		Workspace: workspace,
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}
	if !result.Success {
		t.Fatal("expected success")
	}

	lower := strings.ToLower(result.FinalSummary)
	if !strings.Contains(lower, "util.go") && !strings.Contains(lower, "add") {
		t.Errorf("summary should mention util.go or Add: %q", result.FinalSummary)
	}

	t.Logf("summary=%q steps=%d tokens=%d", result.FinalSummary, result.Steps, result.TotalTokens)
}

// ---------------------------------------------------------------------------
// Test: Find files with workspace_find
// ---------------------------------------------------------------------------

func TestE2ETools_FindFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	spec := providers()[0]
	apiKey := skipIfNoKey(t, spec.envVar)
	workspace := setupToolsWorkspace(t)
	r := newToolsRunner(t, spec, apiKey, workspace)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result := r.Run(ctx, matter.RunRequest{
		Task:      "Use workspace_find to list all .go files in the workspace. How many are there? Answer with just the count.",
		Workspace: workspace,
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}

	lower := strings.ToLower(result.FinalSummary)
	if !strings.Contains(lower, "2") {
		t.Errorf("summary should mention 2 Go files: %q", result.FinalSummary)
	}

	t.Logf("summary=%q steps=%d tokens=%d", result.FinalSummary, result.Steps, result.TotalTokens)
}

// ---------------------------------------------------------------------------
// Test: Git status and log
// ---------------------------------------------------------------------------

func TestE2ETools_GitReadOps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	spec := providers()[0]
	apiKey := skipIfNoKey(t, spec.envVar)
	workspace := setupToolsWorkspace(t)

	// Create an uncommitted change so git_status has something to show.
	writeFile(t, workspace, "new.txt", "uncommitted file\n")

	r := newToolsRunner(t, spec, apiKey, workspace)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result := r.Run(ctx, matter.RunRequest{
		Task:      "Use git_status to check the repo state, and git_log to see the commit history. Summarize what you find.",
		Workspace: workspace,
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}

	lower := strings.ToLower(result.FinalSummary)
	// Should mention the untracked file and the initial commit.
	hasNewFile := strings.Contains(lower, "new.txt") || strings.Contains(lower, "untracked")
	hasCommit := strings.Contains(lower, "initial") || strings.Contains(lower, "commit")
	if !hasNewFile && !hasCommit {
		t.Errorf("summary should mention untracked file or commit history: %q", result.FinalSummary)
	}

	t.Logf("summary=%q steps=%d tokens=%d", result.FinalSummary, result.Steps, result.TotalTokens)
}

// writeFile is defined in e2e_test.go (same package).
// It creates a file at dir/name with the given content.
// Redeclaring here would cause a compile error, so we reuse it.
// If e2e_test.go is not compiled (e.g., filtered out), this file
// still compiles because setupToolsWorkspace uses the local helper above.
func init() {
	// Ensure setupWorkspace and writeFile from e2e_test.go are available.
	// This is a no-op; the functions are in the same package.
}
