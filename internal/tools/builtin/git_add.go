package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// GitAddSchema is the JSON Schema for the git_add tool.
var GitAddSchema = []byte(`{
	"type": "object",
	"properties": {
		"paths": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Relative paths to stage. Use [\".\"] to stage all changes."
		}
	},
	"required": ["paths"],
	"additionalProperties": false
}`)

// NewGitAdd creates the git_add tool.
func NewGitAdd(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_add",
		Description: "Stage files for commit. Use [\".\"] to stage all changes.",
		InputSchema: GitAddSchema,
		Timeout:     20 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			rawPaths, ok := input["paths"].([]any)
			if !ok || len(rawPaths) == 0 {
				return matter.ToolResult{Error: "paths is required and must be a non-empty array of strings"}, nil
			}

			args := []string{"add", "--"}
			for _, rp := range rawPaths {
				p, ok := rp.(string)
				if !ok || p == "" {
					return matter.ToolResult{Error: "each path must be a non-empty string"}, nil
				}
				if p != "." {
					if _, err := workspace.ResolvePath(gh.workspaceRoot, p); err != nil {
						return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
					}
				}
				args = append(args, p)
			}

			out, err := gh.runGit(ctx, args...)
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			if out == "" {
				out = fmt.Sprintf("Staged %d paths", len(rawPaths))
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
