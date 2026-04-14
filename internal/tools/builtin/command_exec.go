package builtin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// NewCommandExec creates the command_exec tool with the given workspace root,
// timeout, and output size limit.
func NewCommandExec(workspaceRoot string, timeout time.Duration, maxOutputBytes int) matter.Tool {
	return matter.Tool{
		Name:         "command_exec",
		Description:  "Execute a command in the workspace directory. Output is capped. Stdin is closed.",
		InputSchema:  CommandExecSchema,
		Timeout:      timeout,
		Safe:         false,
		SideEffect:   true,
		FatalOnError: false,
		Execute:      commandExecFunc(workspaceRoot, maxOutputBytes),
	}
}

func commandExecFunc(workspaceRoot string, maxOutputBytes int) matter.ToolExecuteFunc {
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

		// Resolve relative command paths against workspace root.
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

		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = workspaceRoot

		// Restrict environment to avoid leaking host credentials.
		cmd.Env = []string{
			"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
			"HOME=" + workspaceRoot,
			"TMPDIR=" + os.TempDir(),
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
