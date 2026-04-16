package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupFindWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create files
	for _, f := range []string{
		"main.go",
		"util.go",
		"README.md",
		"internal/handler.go",
		"internal/handler_test.go",
		"internal/models/user.go",
		"cmd/app/main.go",
		"docs/guide.md",
	} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("// "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Hidden directory
	hiddenDir := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// node_modules (should be skipped)
	nmDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, "index.js"), []byte("module"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorkspaceFind_GlobAllGo(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 6 {
		t.Errorf("got %d matches, want 6: %v", len(lines), lines)
	}
	// Should be sorted
	for i := 1; i < len(lines); i++ {
		if lines[i] < lines[i-1] {
			t.Errorf("results not sorted: %q before %q", lines[i-1], lines[i])
		}
	}
}

func TestWorkspaceFind_SkipsHiddenDirs(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*",
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Output, ".hidden") {
		t.Error("hidden directory should be skipped")
	}
}

func TestWorkspaceFind_AllowedHiddenPaths(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, []string{".hidden"})

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, ".hidden/secret.txt") {
		t.Error("allowed hidden path should be included")
	}
}

func TestWorkspaceFind_SkipsNodeModules(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.js",
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Output, "node_modules") {
		t.Error("node_modules should be skipped")
	}
}

func TestWorkspaceFind_MaxResults(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "**/*",
		"max_results": float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "[TRUNCATED:") {
		t.Error("expected truncation notice")
	}
}

func TestWorkspaceFind_InvalidPattern(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "[invalid",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Error == "" {
		t.Error("expected error for invalid pattern")
	}
}

func TestWorkspaceFind_NoMatches(t *testing.T) {
	dir := setupFindWorkspace(t)
	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.rs",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "No files matched") {
		t.Errorf("expected 'No files matched', got %q", result.Output)
	}
}

func TestWorkspaceFind_GitignoreRespected(t *testing.T) {
	dir := setupFindWorkspace(t)
	// Create .gitignore
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceFind(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	// docs/guide.md should be excluded, README.md should remain
	if strings.Contains(result.Output, "docs/guide.md") {
		t.Error("gitignored file should be excluded")
	}
	if !strings.Contains(result.Output, "README.md") {
		t.Error("non-ignored file should be included")
	}
}
