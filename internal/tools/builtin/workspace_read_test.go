package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceReadSuccess(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if result.Output != content {
		t.Errorf("output = %q, want %q", result.Output, content)
	}
}

func TestWorkspaceReadSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "data.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "sub/data.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "nested" {
		t.Errorf("output = %q, want %q", result.Output, "nested")
	}
}

func TestWorkspaceReadPathTraversal(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "../etc/passwd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for path traversal, got none")
	}
}

func TestWorkspaceReadAbsolutePath(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "/etc/passwd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for absolute path, got none")
	}
}

func TestWorkspaceReadMissingFile(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "nonexistent.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for missing file, got none")
	}
}

func TestWorkspaceReadEmptyPath(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for empty path, got none")
	}
}

func TestWorkspaceReadHiddenPathRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceRead(dir, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": ".env"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for hidden path read")
	}
}

func TestWorkspaceReadHiddenPathAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".config", "app.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceRead(dir, 1024*1024, ".config")
	result, err := tool.Execute(context.Background(), map[string]any{"path": ".config/app.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if result.Output != "{}" {
		t.Errorf("output = %q, want %q", result.Output, "{}")
	}
}

func TestWorkspaceReadTruncation(t *testing.T) {
	dir := t.TempDir()
	largeContent := strings.Repeat("x", 2000)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(largeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceRead(dir, 1024)
	result, err := tool.Execute(context.Background(), map[string]any{"path": "large.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("truncation should not set error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "[TRUNCATED at") {
		t.Error("expected truncation notice in output")
	}
	if !strings.HasPrefix(result.Output, strings.Repeat("x", 1024)) {
		t.Error("output should start with maxBytes of original content")
	}
}

func TestWorkspaceReadSafeFlag(t *testing.T) {
	tool := NewWorkspaceRead(t.TempDir(), 1024*1024)
	if !tool.Safe {
		t.Error("workspace_read should be Safe=true")
	}
	if tool.SideEffect {
		t.Error("workspace_read should be SideEffect=false")
	}
}
