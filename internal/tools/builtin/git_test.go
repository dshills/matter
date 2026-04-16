package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepo creates a temp directory, initializes a git repo, and commits a file.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

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

	// Init repo.
	run("git", "init")
	run("git", "checkout", "-b", "main")

	// Create and commit a file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "main.go")
	run("git", "commit", "-m", "initial commit")

	return dir
}

// --- git_status tests ---

func TestGitStatus_CleanRepo(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitStatus(gh)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "clean") {
		t.Errorf("expected 'clean' in output: %s", result.Output)
	}
}

func TestGitStatus_UntrackedFile(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitStatus(gh)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "?? new.txt") {
		t.Errorf("expected untracked file in output: %s", result.Output)
	}
}

func TestGitStatus_NotARepo(t *testing.T) {
	dir := t.TempDir()
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitStatus(gh)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "not in a git repository") {
		t.Errorf("expected repo error, got: %s", result.Error)
	}
}

// --- git_diff tests ---

func TestGitDiff_UnstagedChanges(t *testing.T) {
	dir := setupGitRepo(t)
	// Modify committed file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { /* modified */ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitDiff(gh)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "modified") {
		t.Errorf("expected diff with 'modified': %s", result.Output)
	}
}

func TestGitDiff_StagedChanges(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { /* staged */ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "main.go")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s\n%s", err, out)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitDiff(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"staged": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "staged") {
		t.Errorf("expected staged diff: %s", result.Output)
	}
}

func TestGitDiff_PathFilter(t *testing.T) {
	dir := setupGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitDiff(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "main.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "changed") {
		t.Errorf("expected diff for main.go: %s", result.Output)
	}
}

// --- git_log tests ---

func TestGitLog_Oneline(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitLog(gh)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "initial commit") {
		t.Errorf("expected 'initial commit' in log: %s", result.Output)
	}
	// Oneline format: hash + message on one line.
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line (oneline format), got %d", len(lines))
	}
}

func TestGitLog_MediumFormat(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitLog(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"oneline": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Medium format has multiple lines per commit (commit, Author, Date, message).
	if !strings.Contains(result.Output, "Author:") {
		t.Errorf("expected 'Author:' in medium format: %s", result.Output)
	}
}

func TestGitLog_MaxCount(t *testing.T) {
	dir := setupGitRepo(t)

	// Add a second commit.
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "second commit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s\n%s", err, out)
	}

	gh := NewGitHelper(dir, 1048576)
	tool := NewGitLog(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"max_count": float64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 commit, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(result.Output, "second commit") {
		t.Errorf("expected most recent commit: %s", result.Output)
	}
}

// --- git_blame tests ---

func TestGitBlame_TrackedFile(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBlame(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "main.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}
	// Blame should contain line content.
	if !strings.Contains(result.Output, "package main") {
		t.Errorf("expected 'package main' in blame: %s", result.Output)
	}
}

func TestGitBlame_NonexistentFile(t *testing.T) {
	dir := setupGitRepo(t)
	gh := NewGitHelper(dir, 1048576)
	tool := NewGitBlame(gh)

	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "nonexistent.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Error("expected error for nonexistent file")
	}
}

// --- workspace confinement ---

func TestGit_RepoRootOutsideWorkspace(t *testing.T) {
	dir := setupGitRepo(t)

	// Create a subdirectory within the repo.
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use subdir as workspace — repo root is parent, should fail.
	gh := NewGitHelper(subDir, 1048576)
	tool := NewGitStatus(gh)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "outside workspace") {
		t.Errorf("expected 'outside workspace' error, got: %s", result.Error)
	}
}
