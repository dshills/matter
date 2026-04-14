package builtin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// CommandExecSchema is the JSON Schema for the command_exec tool.
var CommandExecSchema = []byte(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "The command to execute"
		},
		"args": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Arguments to pass to the command"
		}
	},
	"required": ["command"],
	"additionalProperties": false
}`)

// restrictedPATH is the sandboxed PATH used by command_exec per spec §3.2.
// Intentionally Unix-only: matter targets Linux/macOS; Windows is out of scope for v1.
// Uses colon separator and standard Unix bin directories.
const restrictedPATH = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// NewCommandExec creates the command_exec tool with the given workspace root,
// timeout, output size limit, and optional command allowlist.
// An empty allowlist permits all commands (v1 backward compatibility).
func NewCommandExec(workspaceRoot string, timeout time.Duration, maxOutputBytes int, allowlist []string) matter.Tool {
	return matter.Tool{
		Name:         "command_exec",
		Description:  "Execute a command in the workspace directory. Output is capped. Stdin is closed.",
		InputSchema:  CommandExecSchema,
		Timeout:      timeout,
		Safe:         false,
		SideEffect:   true,
		FatalOnError: false,
		Execute:      commandExecFunc(workspaceRoot, maxOutputBytes, allowlist),
	}
}

// isCommandAllowed checks if a command is permitted by the allowlist.
// It searches restricted PATH directories directly (not exec.LookPath)
// to find the binary, then verifies the base name is in the allowlist. Workspace-local binaries
// (relative or absolute paths outside the restricted PATH) are intentionally
// rejected per spec §3.2 — this is a security boundary preventing path
// manipulation bypasses. Returns the resolved command path (or original
// if no allowlist) and an error message if rejected.
func isCommandAllowed(command string, allowlist []string) (string, string) {
	if len(allowlist) == 0 {
		return command, ""
	}

	// Reject commands containing path separators (relative or absolute paths)
	// per spec §3.2 — only bare command names are allowed with an allowlist.
	if strings.ContainsAny(command, `/\`) || filepath.IsAbs(command) {
		return "", fmt.Sprintf("command %q contains a path component; only bare command names are allowed with an allowlist", command)
	}

	// Resolve the command by searching only the restricted PATH directories,
	// not the host's PATH. This ensures consistent resolution regardless of
	// the host environment.
	absResolved := ""
	for _, dir := range strings.Split(restrictedPATH, ":") {
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate)
		// Unix executable-bit check; Windows is out of scope for v1 (see restrictedPATH).
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			absResolved = candidate
			break
		}
	}
	if absResolved == "" {
		return "", fmt.Sprintf("command %q not found in restricted PATH", command)
	}

	// Check if the base name is in the allowlist. Allowlist entries should be
	// bare command names per spec §3.2, but we defensively apply filepath.Base
	// to handle entries like "/usr/bin/echo" gracefully.
	// Base-name comparison is safe here because absResolved was found exclusively
	// within restrictedPATH directories above — no untrusted paths can match.
	baseName := filepath.Base(absResolved)
	// Linux and other Unix-like systems (FreeBSD, etc.) are case-sensitive;
	// macOS (darwin) and Windows are case-insensitive.
	caseSensitive := runtime.GOOS != "windows" && runtime.GOOS != "darwin"

	for _, allowed := range allowlist {
		allowedBase := filepath.Base(allowed)
		if caseSensitive {
			if baseName == allowedBase {
				return absResolved, ""
			}
		} else {
			if strings.EqualFold(baseName, allowedBase) {
				return absResolved, ""
			}
		}
	}

	return "", fmt.Sprintf("command %q is not in the allowlist", command)
}

func commandExecFunc(workspaceRoot string, maxOutputBytes int, allowlist []string) matter.ToolExecuteFunc {
	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		command, ok := input["command"].(string)
		if !ok || command == "" {
			return matter.ToolResult{Error: "command is required and must be a string"}, nil
		}

		var args []string
		if rawArgs, ok := input["args"].([]any); ok {
			for _, a := range rawArgs {
				if s, ok := a.(string); ok {
					args = append(args, s)
				}
			}
		}

		// Enforce allowlist before any command execution.
		// With an allowlist, commands are resolved exclusively against restrictedPATH
		// directories via isCommandAllowed (no host PATH involved).
		if len(allowlist) > 0 {
			resolvedCmd, errMsg := isCommandAllowed(command, allowlist)
			if errMsg != "" {
				return matter.ToolResult{Error: errMsg}, nil
			}
			command = resolvedCmd
		} else {
			// No allowlist — v1 backward compatibility. Bare commands are resolved
			// by exec.CommandContext using the host PATH. The child process still
			// runs with restrictedPATH in its environment. Users who need stricter
			// resolution should configure an allowlist.
			if !filepath.IsAbs(command) && (strings.HasPrefix(command, "./") || strings.HasPrefix(command, "../") || strings.ContainsAny(command, `/\`)) {
				joined := filepath.Clean(filepath.Join(workspaceRoot, command))
				absJoined, err := filepath.Abs(joined)
				if err != nil {
					return matter.ToolResult{Error: fmt.Sprintf("failed to resolve command path: %s", err)}, nil
				}
				absRoot, err := filepath.Abs(workspaceRoot)
				if err != nil {
					return matter.ToolResult{Error: fmt.Sprintf("failed to resolve workspace root: %s", err)}, nil
				}
				rel, err := filepath.Rel(absRoot, absJoined)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return matter.ToolResult{Error: "command path escapes workspace"}, nil
				}
				command = absJoined
			}
		}

		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = workspaceRoot

		// Restrict environment to avoid leaking host credentials.
		// TMPDIR is scoped to the workspace to prevent host path leakage.
		sandboxTmp := filepath.Join(workspaceRoot, ".tmp")
		if mkErr := os.MkdirAll(sandboxTmp, 0o700); mkErr != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to create sandbox temp dir: %s", mkErr)}, nil
		}
		cmd.Env = []string{
			"PATH=" + restrictedPATH,
			"HOME=" + workspaceRoot,
			"TMPDIR=" + sandboxTmp,
		}

		// Close stdin immediately — no interactive input.
		cmd.Stdin = nil

		// Capture stdout and stderr together, with size limiting.
		var combined bytes.Buffer
		limitedWriter := &limitWriter{w: &combined, remaining: maxOutputBytes}
		cmd.Stdout = limitedWriter
		cmd.Stderr = limitedWriter

		err := cmd.Run()

		output := combined.String()
		truncated := limitedWriter.truncated

		if truncated {
			output += "\n[OUTPUT TRUNCATED]"
		}

		// If the process was killed by context (timeout), report with partial output.
		if ctx.Err() != nil {
			return matter.ToolResult{
				Output: output,
				Error:  fmt.Sprintf("command timed out: %s", ctx.Err()),
			}, nil
		}

		// If the command exited with a non-zero code, include the exit error.
		// Truncation alone is not an error, but a failed command is reported
		// even when output was truncated.
		if err != nil {
			return matter.ToolResult{
				Output: output,
				Error:  fmt.Sprintf("command failed: %s", err),
			}, nil
		}

		return matter.ToolResult{Output: output}, nil
	}
}

// limitWriter wraps a writer and stops writing after a byte limit is reached.
// It is safe for concurrent use (os/exec writes stdout and stderr concurrently).
type limitWriter struct {
	mu        sync.Mutex
	w         io.Writer
	remaining int
	truncated bool
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	if lw.remaining <= 0 {
		lw.truncated = true
		return len(p), nil // consume but discard
	}

	if len(p) > lw.remaining {
		n, err := lw.w.Write(p[:lw.remaining])
		lw.remaining = 0
		lw.truncated = true
		if err != nil {
			return n, err
		}
		return len(p), nil // report all bytes consumed so the process doesn't block
	}

	n, err := lw.w.Write(p)
	lw.remaining -= n
	return n, err
}
