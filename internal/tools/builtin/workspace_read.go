// Package builtin provides the four required built-in tools for the matter agent.
package builtin

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// WorkspaceReadSchema is the JSON Schema for the workspace_read tool.
var WorkspaceReadSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Relative path to the file to read within the workspace"
		}
	},
	"required": ["path"],
	"additionalProperties": false
}`)

// NewWorkspaceRead creates the workspace_read tool for the given workspace root.
// maxBytes limits the output size; content beyond the limit is truncated with a notice.
// allowedHiddenPaths lists hidden paths that are permitted for reading.
func NewWorkspaceRead(workspaceRoot string, maxBytes int, allowedHiddenPaths ...string) matter.Tool {
	return matter.Tool{
		Name:        "workspace_read",
		Description: "Read a file from the workspace. Path must be relative to the workspace root. Large files are truncated.",
		InputSchema: WorkspaceReadSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute:     workspaceReadFunc(workspaceRoot, maxBytes, allowedHiddenPaths),
	}
}

func workspaceReadFunc(workspaceRoot string, maxBytes int, allowedHiddenPaths []string) matter.ToolExecuteFunc {
	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		path, ok := input["path"].(string)
		if !ok || path == "" {
			return matter.ToolResult{Error: "path is required and must be a string"}, nil
		}

		resolved, err := workspace.ResolvePath(workspaceRoot, path)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}

		// Check hidden path restrictions using the cleaned relative path.
		cleanPath := filepath.Clean(path)
		if err := workspace.CheckHiddenPath(cleanPath, allowedHiddenPaths); err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}

		f, err := os.Open(resolved)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to read file: %s", err)}, nil
		}
		defer func() { _ = f.Close() }()

		// Read up to maxBytes + 1 to detect truncation.
		limit := maxBytes + 1
		data, err := io.ReadAll(io.LimitReader(f, int64(limit)))
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to read file: %s", err)}, nil
		}

		truncated := len(data) > maxBytes
		if truncated {
			data = data[:maxBytes]
		}

		output := string(data)
		if truncated {
			output += fmt.Sprintf("\n[TRUNCATED at %dKB]", maxBytes/1024)
		}

		return matter.ToolResult{Output: output}, nil
	}
}
