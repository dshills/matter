package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
)

func TestPromptDefaultIdenticalToV1(t *testing.T) {
	mock := llm.NewMockClient([]llm.Response{
		{Content: `{"type":"complete","reasoning":"ok","final":{"summary":"done"}}`},
	}, nil)
	p := mustNewPlanner(t, mock, defaultPlannerConfig())

	prompt := p.buildPrompt("test task", "[tools]", BudgetInfo{
		MaxSteps: 20, MaxTotalTokens: 50000, MaxCostUSD: 3.0, MaxDuration: 2 * time.Minute,
	})

	// Must contain the default persona.
	if !strings.Contains(prompt, "You are an autonomous agent") {
		t.Error("default prompt missing persona")
	}
	// Must contain instructions.
	if !strings.Contains(prompt, "Do not invent tools") {
		t.Error("default prompt missing instructions")
	}
	// Must contain output format.
	if !strings.Contains(prompt, "## Output Format") {
		t.Error("default prompt missing output format")
	}
	// Must contain task.
	if !strings.Contains(prompt, "test task") {
		t.Error("default prompt missing task")
	}
}

func TestPromptPrefix(t *testing.T) {
	cfg := defaultPlannerConfig()
	cfg.PromptPrefix = "CUSTOM PREFIX HERE"
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("task", "", BudgetInfo{MaxDuration: time.Minute})

	if !strings.Contains(prompt, "CUSTOM PREFIX HERE") {
		t.Error("prompt should contain prefix")
	}
	// Prefix should come before the persona.
	prefixIdx := strings.Index(prompt, "CUSTOM PREFIX HERE")
	personaIdx := strings.Index(prompt, "You are an autonomous agent")
	if prefixIdx >= personaIdx {
		t.Error("prefix should appear before persona")
	}
	// Default instructions should still be present.
	if !strings.Contains(prompt, "Do not invent tools") {
		t.Error("instructions missing with prefix set")
	}
}

func TestPromptSuffix(t *testing.T) {
	cfg := defaultPlannerConfig()
	cfg.PromptSuffix = "CUSTOM SUFFIX HERE"
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("task", "", BudgetInfo{MaxDuration: time.Minute})

	if !strings.Contains(prompt, "CUSTOM SUFFIX HERE") {
		t.Error("prompt should contain suffix")
	}
	// Suffix should come after instructions but before output format.
	suffixIdx := strings.Index(prompt, "CUSTOM SUFFIX HERE")
	instructIdx := strings.Index(prompt, "Do not invent tools")
	formatIdx := strings.Index(prompt, "## Output Format")
	if suffixIdx <= instructIdx {
		t.Error("suffix should appear after instructions")
	}
	if suffixIdx >= formatIdx {
		t.Error("suffix should appear before output format")
	}
}

func TestPromptSystemPromptOverride(t *testing.T) {
	cfg := defaultPlannerConfig()
	cfg.SystemPrompt = "You are a code review expert."
	cfg.PromptPrefix = "SHOULD BE IGNORED"
	cfg.PromptSuffix = "SHOULD BE IGNORED TOO"
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("review code", "[tools]", BudgetInfo{MaxDuration: time.Minute})

	// Custom prompt should be present.
	if !strings.Contains(prompt, "You are a code review expert.") {
		t.Error("custom system prompt missing")
	}
	// Default persona should NOT be present.
	if strings.Contains(prompt, "You are an autonomous agent") {
		t.Error("default persona should be replaced by system_prompt")
	}
	// Prefix/suffix should be ignored.
	if strings.Contains(prompt, "SHOULD BE IGNORED") {
		t.Error("prefix/suffix should be ignored when system_prompt is set")
	}
	// Default instructions should NOT be present.
	if strings.Contains(prompt, "Do not invent tools") {
		t.Error("default instructions should be replaced by system_prompt")
	}
	// Structural sections must still be present.
	if !strings.Contains(prompt, "## Available Tools") {
		t.Error("tools section missing with system_prompt override")
	}
	if !strings.Contains(prompt, "## Budget") {
		t.Error("budget section missing with system_prompt override")
	}
	if !strings.Contains(prompt, "## Output Format") {
		t.Error("output format missing with system_prompt override")
	}
	if !strings.Contains(prompt, "## Task") {
		t.Error("task section missing with system_prompt override")
	}
}

func TestPromptSystemPromptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("You are a data analyst."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultPlannerConfig()
	cfg.SystemPromptFile = path
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("analyze data", "", BudgetInfo{MaxDuration: time.Minute})

	if !strings.Contains(prompt, "You are a data analyst.") {
		t.Error("file-based prompt missing")
	}
	if strings.Contains(prompt, "You are an autonomous agent") {
		t.Error("default persona should be replaced by file prompt")
	}
	// Structural sections must still be present.
	if !strings.Contains(prompt, "## Output Format") {
		t.Error("output format missing with file prompt")
	}
}

func TestPromptSystemPromptTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("FROM FILE"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultPlannerConfig()
	cfg.SystemPrompt = "FROM INLINE"
	cfg.SystemPromptFile = path
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("task", "", BudgetInfo{MaxDuration: time.Minute})

	if !strings.Contains(prompt, "FROM INLINE") {
		t.Error("inline system_prompt should take precedence")
	}
	if strings.Contains(prompt, "FROM FILE") {
		t.Error("file prompt should be ignored when inline is set")
	}
}

func TestPromptMissingFile(t *testing.T) {
	cfg := defaultPlannerConfig()
	cfg.SystemPromptFile = "/nonexistent/prompt.txt"
	mock := llm.NewMockClient(nil, nil)

	_, err := NewPlanner(mock, cfg)
	if err == nil {
		t.Error("expected error for missing prompt file")
	}
}

func TestPromptEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("   \n  \t  "), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultPlannerConfig()
	cfg.SystemPromptFile = path
	mock := llm.NewMockClient(nil, nil)

	_, err := NewPlanner(mock, cfg)
	if err == nil {
		t.Error("expected error for empty prompt file")
	}
}

func TestPromptOutputFormatAlwaysPresent(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.PlannerConfig
	}{
		{"default", defaultPlannerConfig()},
		{"with prefix", config.PlannerConfig{PromptPrefix: "PREFIX", MaxResponseTokens: 4096}},
		{"with suffix", config.PlannerConfig{PromptSuffix: "SUFFIX", MaxResponseTokens: 4096}},
		{"with system_prompt", config.PlannerConfig{SystemPrompt: "Custom agent.", MaxResponseTokens: 4096}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := llm.NewMockClient(nil, nil)
			p := mustNewPlanner(t, mock, tt.cfg)
			prompt := p.buildPrompt("task", "", BudgetInfo{MaxDuration: time.Minute})
			if !strings.Contains(prompt, "## Output Format") {
				t.Error("output format section missing")
			}
			if !strings.Contains(prompt, "Return ONLY the JSON object") {
				t.Error("JSON instruction missing")
			}
		})
	}
}

func TestPromptPrefixAndSuffix(t *testing.T) {
	cfg := defaultPlannerConfig()
	cfg.PromptPrefix = "PREFIX_TEXT"
	cfg.PromptSuffix = "SUFFIX_TEXT"
	mock := llm.NewMockClient(nil, nil)
	p := mustNewPlanner(t, mock, cfg)

	prompt := p.buildPrompt("task", "", BudgetInfo{MaxDuration: time.Minute})

	prefixIdx := strings.Index(prompt, "PREFIX_TEXT")
	personaIdx := strings.Index(prompt, "You are an autonomous agent")
	suffixIdx := strings.Index(prompt, "SUFFIX_TEXT")
	formatIdx := strings.Index(prompt, "## Output Format")

	if prefixIdx == -1 || personaIdx == -1 || suffixIdx == -1 || formatIdx == -1 {
		t.Fatal("missing expected content in prompt")
	}
	if prefixIdx >= personaIdx {
		t.Error("prefix should come before persona")
	}
	if suffixIdx >= formatIdx {
		t.Error("suffix should come before output format")
	}
}
