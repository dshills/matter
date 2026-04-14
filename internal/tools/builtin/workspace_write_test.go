package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceWriteNewFile(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "output.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(dir, "output.txt"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want %q", string(data), "hello")
	}
}

func TestWorkspaceWriteCreatesSubdirectory(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "sub/dir/file.txt",
		"content": "nested",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(dir, "sub", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("file content = %q, want %q", string(data), "nested")
	}
}

func TestWorkspaceWriteExistingFileNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "existing.txt",
		"content": "new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error when overwriting without flag")
	}

	// File should not be modified.
	data, _ := os.ReadFile(filepath.Join(dir, "existing.txt"))
	if string(data) != "old" {
		t.Errorf("file was modified: got %q, want %q", string(data), "old")
	}
}

func TestWorkspaceWriteExistingFileWithOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":      "existing.txt",
		"content":   "new",
		"overwrite": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "existing.txt"))
	if string(data) != "new" {
		t.Errorf("file content = %q, want %q", string(data), "new")
	}
}

func TestWorkspaceWriteHiddenPathRejected(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    ".env",
		"content": "SECRET=x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for hidden path write")
	}
}

func TestWorkspaceWriteHiddenPathBypassAttempt(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory so the path is valid.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "subdir/../.env",
		"content": "SECRET=x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for hidden path bypass via traversal")
	}
}

func TestWorkspaceWriteHiddenPathAllowed(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceWrite(dir, []string{".config/myapp"})
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    ".config/myapp/settings.json",
		"content": `{"key":"val"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".config", "myapp", "settings.json"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != `{"key":"val"}` {
		t.Errorf("file content = %q", string(data))
	}
}

func TestWorkspaceWritePathTraversal(t *testing.T) {
	dir := t.TempDir()

	tool := NewWorkspaceWrite(dir, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "../escape.txt",
		"content": "bad",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for path traversal")
	}
}

func TestWorkspaceWriteSafetyFlags(t *testing.T) {
	tool := NewWorkspaceWrite(t.TempDir(), nil)
	if tool.Safe {
		t.Error("workspace_write should be Safe=false")
	}
	if !tool.SideEffect {
		t.Error("workspace_write should be SideEffect=true")
	}
}
