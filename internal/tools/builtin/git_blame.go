package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// GitBlameSchema is the JSON Schema for the git_blame tool.
var GitBlameSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Relative path to the file to blame"
		}
	},
	"required": ["path"],
	"additionalProperties": false
}`)

// NewGitBlame creates the git_blame tool.
func NewGitBlame(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_blame",
		Description: "Show per-line authorship for a file (git blame).",
		InputSchema: GitBlameSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			path, ok := input["path"].(string)
			if !ok || path == "" {
				return matter.ToolResult{Error: "path is required and must be a string"}, nil
			}

			if _, err := workspace.ResolvePath(gh.workspaceRoot, path); err != nil {
				return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
			}

			out, err := gh.runGit(ctx, "blame", path)
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
