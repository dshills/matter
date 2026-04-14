package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandExecSuccess(t *testing.T) {
	dir := t.TempDir()

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo",
		"args":    []any{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) != "hello world" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(result.Output), "hello world")
	}
}

func TestCommandExecWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("found"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "cat",
		"args":    []any{"marker.txt"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if result.Output != "found" {
		t.Errorf("output = %q, want %q", result.Output, "found")
	}
}

func TestCommandExecTimeout(t *testing.T) {
	dir := t.TempDir()

	// Use a very short timeout context.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(ctx, map[string]any{
		"command": "sleep",
		"args":    []any{"10"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected timeout error")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("error = %q, want mention of timeout", result.Error)
	}
}

func TestCommandExecOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	maxBytes := 100

	tool := NewCommandExec(dir, 10*time.Second, maxBytes)
	// Generate output larger than maxBytes.
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "sh",
		"args":    []any{"-c", "dd if=/dev/zero bs=200 count=1 2>/dev/null | tr '\\0' 'x'"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Truncation should not set error.
	if result.Error != "" {
		t.Errorf("truncation should not set error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "[OUTPUT TRUNCATED]") {
		t.Error("expected truncation notice in output")
	}
}

func TestCommandExecNonZeroExit(t *testing.T) {
	dir := t.TempDir()

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "sh",
		"args":    []any{"-c", "exit 1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for non-zero exit")
	}
	if !strings.Contains(result.Error, "command failed") {
		t.Errorf("error = %q, want mention of command failed", result.Error)
	}
}

func TestCommandExecStderrCaptured(t *testing.T) {
	dir := t.TempDir()

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "sh",
		"args":    []any{"-c", "echo errout >&2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "errout") {
		t.Errorf("stderr not captured in output: %q", result.Output)
	}
}

func TestCommandExecMissingCommand(t *testing.T) {
	dir := t.TempDir()

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for missing command")
	}
}

func TestCommandExecSafetyFlags(t *testing.T) {
	tool := NewCommandExec(t.TempDir(), 10*time.Second, 1024)
	if tool.Safe {
		t.Error("command_exec should be Safe=false")
	}
	if !tool.SideEffect {
		t.Error("command_exec should be SideEffect=true")
	}
	if tool.FatalOnError {
		t.Error("command_exec should be FatalOnError=false")
	}
}
