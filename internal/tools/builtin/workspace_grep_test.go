package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupGrepWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}
`,
		"handler.go": `package main

func HandleRequest() error {
	return nil
}

func HandleResponse() error {
	return fmt.Errorf("not implemented")
}
`,
		"README.md": "# My Project\n\nThis is a test project.\n",
		"data.bin":  string([]byte{0x00, 0x01, 0x02, 0xFF}), // binary
	}

	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Large file (>1MB) - should be skipped
	largeContent := strings.Repeat("x", maxGrepFileSize+1)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(largeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestWorkspaceGrep_BasicSearch(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "func Handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	if !strings.Contains(result.Output, "handler.go:3:") {
		t.Errorf("expected handler.go:3 match, got:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "HandleRequest") {
		t.Error("expected HandleRequest in output")
	}
	if !strings.Contains(result.Output, "HandleResponse") {
		t.Error("expected HandleResponse in output")
	}
}

func TestWorkspaceGrep_GlobFilter(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"glob":    "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "main.go") {
		t.Error("expected match in main.go")
	}
}

func TestWorkspaceGrep_ContextLines(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":       "HandleRequest",
		"glob":          "*.go",
		"context_lines": float64(1),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should have context lines with - prefix
	if !strings.Contains(result.Output, "-") {
		t.Errorf("expected context lines with - prefix, got:\n%s", result.Output)
	}
}

func TestWorkspaceGrep_SkipsBinaryFiles(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": ".*",
		"glob":    "*.bin",
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Output, "data.bin") {
		t.Error("binary file should be skipped")
	}
}

func TestWorkspaceGrep_SkipsLargeFiles(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "x",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "files skipped (>1MB)") {
		t.Errorf("expected large file skip notice, got:\n%s", result.Output)
	}
}

func TestWorkspaceGrep_InvalidRegex(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "[invalid",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Error == "" {
		t.Error("expected error for invalid regex")
	}
}

func TestWorkspaceGrep_PathFilter(t *testing.T) {
	dir := setupGrepWorkspace(t)
	// Create a subdirectory with a file
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.go"), []byte("func Nested() {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "func",
		"path":    "sub",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "Nested") {
		t.Error("expected Nested in path-filtered results")
	}
	if strings.Contains(result.Output, "main.go") {
		t.Error("main.go should not appear when searching sub/ only")
	}
}

func TestWorkspaceGrep_NoMatches(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "zzzznonexistent",
		"glob":    "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "No matches found") {
		t.Errorf("expected 'No matches found', got %q", result.Output)
	}
}

func TestWorkspaceGrep_MaxResults(t *testing.T) {
	dir := setupGrepWorkspace(t)
	tool := NewWorkspaceGrep(dir, 1048576, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     ".",
		"glob":        "*.go",
		"max_results": float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "[TRUNCATED:") {
		t.Error("expected truncation notice")
	}
}
