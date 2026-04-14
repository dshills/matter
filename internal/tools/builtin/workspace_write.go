package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// WorkspaceWriteSchema is the JSON Schema for the workspace_write tool.
var WorkspaceWriteSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Relative path to the file to write within the workspace"
		},
		"content": {
			"type": "string",
			"description": "Content to write to the file"
		},
		"overwrite": {
			"type": "boolean",
			"description": "Must be true to overwrite an existing file"
		}
	},
	"required": ["path", "content"],
	"additionalProperties": false
}`)

// NewWorkspaceWrite creates the workspace_write tool for the given workspace root
// and allowed hidden paths list.
func NewWorkspaceWrite(workspaceRoot string, allowedHiddenPaths []string) matter.Tool {
	return matter.Tool{
		Name:        "workspace_write",
		Description: "Write a file within the workspace. Requires overwrite=true for existing files. Rejects writes to hidden paths unless allowed.",
		InputSchema: WorkspaceWriteSchema,
		Timeout:     10 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute:     workspaceWriteFunc(workspaceRoot, allowedHiddenPaths),
	}
}

func workspaceWriteFunc(workspaceRoot string, allowedHiddenPaths []string) matter.ToolExecuteFunc {
	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		path, ok := input["path"].(string)
		if !ok || path == "" {
			return matter.ToolResult{Error: "path is required and must be a string"}, nil
		}

		content, ok := input["content"].(string)
		if !ok {
			return matter.ToolResult{Error: "content is required and must be a string"}, nil
		}

		overwrite, _ := input["overwrite"].(bool)

		resolved, err := workspace.ResolvePath(workspaceRoot, path)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}

		// Check hidden path restrictions using the cleaned relative path.
		cleanPath := filepath.Clean(path)
		if err := workspace.CheckHiddenPath(cleanPath, allowedHiddenPaths); err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}

		// Ensure parent directory exists.
		dir := filepath.Dir(resolved)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to create directory: %s", err)}, nil
		}

		if err := atomicWrite(resolved, content, !overwrite); err != nil {
			if os.IsExist(err) {
				return matter.ToolResult{Error: "file already exists; set overwrite=true to replace it"}, nil
			}
			return matter.ToolResult{Error: fmt.Sprintf("failed to write file: %s", err)}, nil
		}

		return matter.ToolResult{Output: fmt.Sprintf("wrote %d bytes to %s", len(content), path)}, nil
	}
}

// atomicWrite writes content to a temp file in the same directory, syncs it,
// then renames to the target. If exclusive is true, it returns os.ErrExist
// when the target already exists.
func atomicWrite(target, content string, exclusive bool) error {
	if exclusive {
		if _, err := os.Stat(target); err == nil {
			return os.ErrExist
		}
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".matter-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	// Ensure cleanup on any failure path.
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err = tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err = tmp.WriteString(content); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	// Final existence check for exclusive mode to narrow the race window.
	if exclusive {
		if _, err := os.Stat(target); err == nil {
			_ = os.Remove(tmpPath)
			return os.ErrExist
		}
	}

	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	success = true
	return nil
}
