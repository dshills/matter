package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupEditWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}

func helper() string {
	return "original"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorkspaceEdit_UniqueMatch(t *testing.T) {
	dir := setupEditWorkspace(t)
	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "main.go",
		"old_text": `return "original"`,
		"new_text": `return "modified"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	// Verify file was modified.
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(content), `"modified"`) {
		t.Error("file should contain 'modified'")
	}
	if strings.Contains(string(content), `"original"`) {
		t.Error("file should not contain 'original'")
	}

	// Check confirmation message.
	if !strings.Contains(result.Output, "Edited main.go") {
		t.Errorf("expected 'Edited main.go' in output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "line") {
		t.Errorf("expected line number in output: %s", result.Output)
	}
}

func TestWorkspaceEdit_NotFound(t *testing.T) {
	dir := setupEditWorkspace(t)
	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "main.go",
		"old_text": "this text does not exist",
		"new_text": "replacement",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' error, got: %s", result.Error)
	}
}

func TestWorkspaceEdit_AmbiguousMatch(t *testing.T) {
	dir := setupEditWorkspace(t)

	// Write a file with duplicate text.
	content := "line one\nduplicate\nline three\nduplicate\nline five\n"
	if err := os.WriteFile(filepath.Join(dir, "dup.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "dup.txt",
		"old_text": "duplicate",
		"new_text": "unique",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "ambiguous") {
		t.Errorf("expected 'ambiguous' error, got: %s", result.Error)
	}
	if !strings.Contains(result.Error, "2 occurrences") {
		t.Errorf("expected '2 occurrences' in error, got: %s", result.Error)
	}

	// File should be unchanged.
	read, _ := os.ReadFile(filepath.Join(dir, "dup.txt"))
	if string(read) != content {
		t.Error("file should not be modified on ambiguous match")
	}
}

func TestWorkspaceEdit_IdenticalText(t *testing.T) {
	dir := setupEditWorkspace(t)
	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "main.go",
		"old_text": "hello",
		"new_text": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "identical") {
		t.Errorf("expected 'identical' error, got: %s", result.Error)
	}
}

func TestWorkspaceEdit_FileNotFound(t *testing.T) {
	dir := setupEditWorkspace(t)
	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "nonexistent.go",
		"old_text": "old",
		"new_text": "new",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' error, got: %s", result.Error)
	}
}

func TestWorkspaceEdit_FileTooLarge(t *testing.T) {
	dir := setupEditWorkspace(t)
	// Create a file over 2MB.
	large := strings.Repeat("x", maxEditFileSize+1)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(large), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "large.txt",
		"old_text": "x",
		"new_text": "y",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "too large") {
		t.Errorf("expected 'too large' error, got: %s", result.Error)
	}
}

func TestWorkspaceEdit_HiddenPathBlocked(t *testing.T) {
	dir := setupEditWorkspace(t)
	hiddenDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "settings.json"), []byte(`{"key": "value"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     ".config/settings.json",
		"old_text": `"value"`,
		"new_text": `"new_value"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Error, "rejected") {
		t.Errorf("expected path rejected error, got: %s", result.Error)
	}
}

func TestWorkspaceEdit_HiddenPathAllowed(t *testing.T) {
	dir := setupEditWorkspace(t)
	hiddenDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "settings.json"), []byte(`{"key": "value"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceEdit(dir, []string{".config"})

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     ".config/settings.json",
		"old_text": `"value"`,
		"new_text": `"new_value"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	content, _ := os.ReadFile(filepath.Join(hiddenDir, "settings.json"))
	if !strings.Contains(string(content), "new_value") {
		t.Error("file should be modified when hidden path is allowed")
	}
}

func TestWorkspaceEdit_LineNumberInConfirmation(t *testing.T) {
	dir := setupEditWorkspace(t)
	tool := NewWorkspaceEdit(dir, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":     "main.go",
		"old_text": `return "original"`,
		"new_text": `return "changed"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	// "return original" is on line 10.
	if !strings.Contains(result.Output, "line 10") {
		t.Errorf("expected 'line 10' in output: %s", result.Output)
	}
}
