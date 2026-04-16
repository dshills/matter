package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- git_add tests ---

func TestGitAdd_StageFile(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitAdd(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"paths": []any{"new.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	// Verify staged via git status.
	statusTool := NewGitStatus(gh)
	status, _ := statusTool.Execute(context.Background(), nil)
	if !strings.Contains(status.Output, "A  new.txt") {
		t.Errorf("expected file staged: %s", status.Output)
	}
}

func TestGitAdd_StageAll(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitAdd(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"paths": []any{"."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Staged") {
		t.Errorf("expected confirmation: %s", result.Output)
	}
}

// --- git_commit tests ---

func TestGitCommit_WithMessage(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)

	// Stage the file.
	addTool := NewGitAdd(gh)
	if result, _ := addTool.Execute(context.Background(), map[string]any{"paths": []any{"new.txt"}}); result.Error != "" {
		t.Fatalf("add error: %s", result.Error)
	}

	// Commit.
	commitTool := NewGitCommit(gh)
	result, err := commitTool.Execute(context.Background(), map[string]any{
		"message": "add new file",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("commit error: %s", result.Error)
	}

	// Verify via git log.
	logTool := NewGitLog(gh)
	logResult, _ := logTool.Execute(context.Background(), map[string]any{})
	if !strings.Contains(logResult.Output, "add new file") {
		t.Errorf("expected commit in log: %s", logResult.Output)
	}
}

func TestGitCommit_NothingStaged(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)

	tool := NewGitCommit(gh)
	result, err := tool.Execute(context.Background(), map[string]any{
		"message": "empty commit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Error("expected error when nothing is staged")
	}
}

// --- git_branch tests ---

func TestGitBranch_List(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBranch(gh)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "main") {
		t.Errorf("expected 'main' in branch list: %s", result.Output)
	}
}

func TestGitBranch_Create(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBranch(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "feature/new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	// Verify branch exists.
	listResult, _ := tool.Execute(context.Background(), map[string]any{})
	if !strings.Contains(listResult.Output, "feature/new") {
		t.Errorf("expected 'feature/new' in branch list: %s", listResult.Output)
	}
}

func TestGitBranch_CreateAndCheckout(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBranch(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":     "feature/checkout",
		"checkout": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	// Verify HEAD is on the new branch.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if strings.TrimSpace(string(out)) != "feature/checkout" {
		t.Errorf("expected HEAD on feature/checkout, got %s", string(out))
	}
}

func TestGitBranch_InvalidName(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBranch(gh)

	tests := []string{
		"bad name",      // spaces
		"-leading-dash", // leading dash
		"has..dots",     // double dots
	}

	for _, name := range tests {
		result, err := tool.Execute(context.Background(), map[string]any{
			"name": name,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Error == "" {
			t.Errorf("expected error for branch name %q", name)
		}
	}
}

// --- git_checkout tests ---

func TestGitCheckout_SwitchBranch(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)

	// Create a branch first.
	branchTool := NewGitBranch(gh)
	if result, _ := branchTool.Execute(context.Background(), map[string]any{"name": "other"}); result.Error != "" {
		t.Fatalf("branch error: %s", result.Error)
	}

	// Switch to it.
	checkoutTool := NewGitCheckout(gh)
	result, err := checkoutTool.Execute(context.Background(), map[string]any{
		"branch": "other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("checkout error: %s", result.Error)
	}

	// Verify HEAD.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if strings.TrimSpace(string(out)) != "other" {
		t.Errorf("expected HEAD on 'other', got %s", string(out))
	}
}

func TestGitCheckout_RestoreFile(t *testing.T) {
	dir := setupGitRepo(t)

	// Modify a committed file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitCheckout(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "main.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("checkout error: %s", result.Error)
	}

	// File should be restored to committed state.
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(content) == "modified" {
		t.Error("file should have been restored to committed state")
	}
}

func TestGitCheckout_BothSpecified(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitCheckout(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"branch": "main",
		"path":   "main.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "not both") {
		t.Errorf("expected mutual exclusivity error, got: %s", result.Error)
	}
}
