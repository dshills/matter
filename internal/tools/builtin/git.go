package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dshills/matter/pkg/matter"
)

// GitHelper provides shared git infrastructure for all git tools.
// It lazily discovers the repo root and caches it for subsequent calls.
type GitHelper struct {
	workspaceRoot  string
	maxOutputBytes int

	once     sync.Once
	repoRoot string
	initErr  error
}

// NewGitHelper creates a shared git helper for the workspace.
func NewGitHelper(workspaceRoot string, maxOutputBytes int) *GitHelper {
	return &GitHelper{
		workspaceRoot:  workspaceRoot,
		maxOutputBytes: maxOutputBytes,
	}
}

// GitReadTools returns the read-only git tools (status, diff, log, blame).
func GitReadTools(gh *GitHelper) []matter.Tool {
	return []matter.Tool{
		NewGitStatus(gh),
		NewGitDiff(gh),
		NewGitLog(gh),
		NewGitBlame(gh),
	}
}

// GitWriteTools returns the mutating git tools (add, commit, branch, checkout).
func GitWriteTools(gh *GitHelper) []matter.Tool {
	return []matter.Tool{
		NewGitAdd(gh),
		NewGitCommit(gh),
		NewGitBranch(gh),
		NewGitCheckout(gh),
	}
}

// ensureRepo lazily discovers the git repo root.
func (g *GitHelper) ensureRepo() error {
	g.once.Do(func() {
		// Check git is installed.
		if _, err := exec.LookPath("git"); err != nil {
			g.initErr = fmt.Errorf("git is not installed or not in PATH")
			return
		}

		// Discover repo root.
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = g.workspaceRoot
		out, err := cmd.Output()
		if err != nil {
			g.initErr = fmt.Errorf("workspace is not in a git repository")
			return
		}

		repoRoot := strings.TrimSpace(string(out))

		// Resolve symlinks for comparison.
		absWorkspace, err := filepath.EvalSymlinks(g.workspaceRoot)
		if err != nil {
			g.initErr = fmt.Errorf("resolving workspace path: %w", err)
			return
		}
		absRepo, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			g.initErr = fmt.Errorf("resolving repo root: %w", err)
			return
		}

		if absRepo != absWorkspace {
			g.initErr = fmt.Errorf("git repository root (%s) is outside workspace (%s)", absRepo, absWorkspace)
			return
		}

		g.repoRoot = absRepo
	})
	return g.initErr
}

// runGit executes a git command and returns the output.
func (g *GitHelper) runGit(ctx context.Context, args ...string) (string, error) {
	if err := g.ensureRepo(); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.repoRoot

	// Use a bounded buffer to prevent memory exhaustion from large git output.
	limit := g.maxOutputBytes
	if limit <= 0 {
		limit = 1048576 // 1 MB default
	}
	stdout := &boundedBuffer{limit: limit}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	output := stdout.String()
	if stdout.truncated {
		output += "\n[TRUNCATED]"
	}

	return output, nil
}

// boundedBuffer is a bytes.Buffer that stops accepting data after a limit.
type boundedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil // discard but report success to avoid blocking
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}
