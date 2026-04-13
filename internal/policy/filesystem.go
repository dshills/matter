package policy

import (
	"fmt"

	"github.com/dshills/matter/internal/workspace"
)

// pathKeys are common input field names that contain file paths.
var pathKeys = []string{"path", "file", "filename", "filepath", "directory", "dir"}

// CheckFilesystem validates that any path-like inputs stay within the workspace.
func CheckFilesystem(workspaceRoot string, input map[string]any) Result {
	if workspaceRoot == "" {
		return Result{Allowed: true}
	}

	for _, key := range pathKeys {
		val, ok := input[key]
		if !ok {
			continue
		}
		path, ok := val.(string)
		if !ok || path == "" {
			continue
		}
		if _, err := workspace.ResolvePath(workspaceRoot, path); err != nil {
			return Result{
				Allowed: false,
				Reason:  fmt.Sprintf("path %q denied: %v", path, err),
			}
		}
	}

	return Result{Allowed: true}
}
