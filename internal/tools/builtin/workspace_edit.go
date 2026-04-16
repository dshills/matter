package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// maxEditFileSize is the maximum file size (2 MB) that workspace_edit will process.
const maxEditFileSize = 2 * 1024 * 1024

// WorkspaceEditSchema is the JSON Schema for the workspace_edit tool.
var WorkspaceEditSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Relative path to the file to edit"
		},
		"old_text": {
			"type": "string",
			"description": "Exact text to find in the file (must match exactly, including whitespace)"
		},
		"new_text": {
			"type": "string",
			"description": "Text to replace old_text with"
		}
	},
	"required": ["path", "old_text", "new_text"],
	"additionalProperties": false
}`)

// NewWorkspaceEdit creates the workspace_edit tool for the given workspace root.
func NewWorkspaceEdit(workspaceRoot string, allowedHiddenPaths []string) matter.Tool {
	return matter.Tool{
		Name:        "workspace_edit",
		Description: "Replace a specific text region in a file. Finds the exact old_text and replaces it with new_text. The old_text must appear exactly once.",
		InputSchema: WorkspaceEditSchema,
		Timeout:     10 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute:     workspaceEditFunc(workspaceRoot, allowedHiddenPaths),
	}
}

func workspaceEditFunc(workspaceRoot string, allowedHiddenPaths []string) matter.ToolExecuteFunc {
	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		path, ok := input["path"].(string)
		if !ok || path == "" {
			return matter.ToolResult{Error: "path is required and must be a string"}, nil
		}
		oldText, ok := input["old_text"].(string)
		if !ok || oldText == "" {
			return matter.ToolResult{Error: "old_text is required and must be a non-empty string"}, nil
		}
		newText, ok := input["new_text"].(string)
		if !ok {
			return matter.ToolResult{Error: "new_text is required and must be a string"}, nil
		}
		if oldText == newText {
			return matter.ToolResult{Error: "old_text and new_text are identical"}, nil
		}

		// Validate path.
		resolved, err := workspace.ResolvePath(workspaceRoot, path)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}
		cleanPath := filepath.Clean(path)
		if err := workspace.CheckHiddenPath(cleanPath, allowedHiddenPaths); err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
		}

		// Check file exists.
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				return matter.ToolResult{Error: fmt.Sprintf("file not found: %s", path)}, nil
			}
			return matter.ToolResult{Error: fmt.Sprintf("cannot access file: %s", err)}, nil
		}
		if info.Size() > maxEditFileSize {
			return matter.ToolResult{Error: fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxEditFileSize)}, nil
		}

		// Read file content.
		content, err := os.ReadFile(resolved)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("cannot read file: %s", err)}, nil
		}

		// Find occurrences of old_text.
		oldBytes := []byte(oldText)
		count := bytes.Count(content, oldBytes)
		if count == 0 {
			return matter.ToolResult{Error: "old_text not found in file"}, nil
		}
		if count > 1 {
			return matter.ToolResult{Error: fmt.Sprintf("old_text is ambiguous (found %d occurrences). Provide more surrounding context to make it unique.", count)}, nil
		}

		// Find the line number of the match.
		idx := bytes.Index(content, oldBytes)
		lineNum := bytes.Count(content[:idx], []byte("\n")) + 1

		// Replace.
		newContent := bytes.Replace(content, oldBytes, []byte(newText), 1)

		// Atomic write preserving original permissions.
		if err := atomicWriteBytes(resolved, newContent, info.Mode()); err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to write file: %s", err)}, nil
		}

		return matter.ToolResult{
			Output: fmt.Sprintf("Edited %s: replaced %d bytes (old_text length) at line %d", path, len(oldText), lineNum),
		}, nil
	}
}

// atomicWriteBytes writes content to a temp file then renames to target,
// preserving the original file mode.
func atomicWriteBytes(target string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".matter-edit-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	// Check if file was recreated during write (TOCTOU minimization).
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}

	success = true
	return nil
}
