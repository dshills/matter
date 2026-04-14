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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, maxBytes, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
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

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for missing command")
	}
}

func TestCommandExecSafetyFlags(t *testing.T) {
	tool := NewCommandExec(t.TempDir(), 10*time.Second, 1024, nil)
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

func TestCommandExecAllowlistAllowed(t *testing.T) {
	dir := t.TempDir()
	allowlist := []string{"echo", "cat"}

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, allowlist)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo",
		"args":    []any{"allowed"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("expected echo to be allowed, got error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) != "allowed" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(result.Output), "allowed")
	}
}

func TestCommandExecAllowlistRejected(t *testing.T) {
	dir := t.TempDir()
	allowlist := []string{"echo", "cat"}

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, allowlist)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "rm",
		"args":    []any{"-rf", "/"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected tool error for rejected command")
	}
	if !strings.Contains(result.Error, "not in the allowlist") {
		t.Errorf("error = %q, want mention of allowlist", result.Error)
	}
}

func TestCommandExecAllowlistEmptyAllowsAll(t *testing.T) {
	dir := t.TempDir()

	// Empty allowlist = v1 behavior, all commands allowed.
	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo",
		"args":    []any{"anything"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("empty allowlist should allow all commands, got error: %s", result.Error)
	}
}

func TestCommandExecAllowlistAbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	allowlist := []string{"echo"}

	// Even if the base name matches, an absolute path outside restricted PATH
	// should be rejected. Use a non-existent path to test the rejection.
	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, allowlist)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "/tmp/echo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for absolute path outside restricted PATH")
	}
}

func TestCommandExecAllowlistRelativePathRejected(t *testing.T) {
	dir := t.TempDir()
	allowlist := []string{"echo"}

	// Relative paths are rejected outright with an allowlist, even if the
	// base name matches an allowed command.
	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, allowlist)
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "./echo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for relative path with allowlist")
	}
}

func TestCommandExecAllowlistRejectionIsRecoverable(t *testing.T) {
	dir := t.TempDir()
	allowlist := []string{"echo"}

	tool := NewCommandExec(dir, 10*time.Second, 1024*1024, allowlist)
	// Rejected command returns a tool result error, not a Go error.
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "rm",
	})
	// err should be nil — this is a recoverable tool error, not fatal.
	if err != nil {
		t.Fatalf("rejection should not return Go error, got: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected tool error for rejected command")
	}
}

func TestIsCommandAllowed(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		allowlist []string
		wantOK    bool
	}{
		{"allowed command", "echo", []string{"echo", "cat"}, true},
		{"rejected command", "rm", []string{"echo", "cat"}, false},
		{"empty allowlist allows all", "rm", nil, true},
		{"nonexistent command", "nonexistent_cmd_xyz", []string{"echo"}, false},
		{"absolute path outside PATH", "/tmp/echo", []string{"echo"}, false},
		{"relative path rejected", "./echo", []string{"echo"}, false},
		{"path with slash rejected", "subdir/echo", []string{"echo"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, errMsg := isCommandAllowed(tt.command, tt.allowlist)
			if tt.wantOK {
				if errMsg != "" {
					t.Errorf("expected allowed, got error: %s", errMsg)
				}
				if resolved == "" {
					t.Error("expected non-empty resolved path")
				}
			} else {
				if errMsg == "" {
					t.Error("expected rejection error message")
				}
			}
		})
	}
}
