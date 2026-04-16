package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// GitDiffSchema is the JSON Schema for the git_diff tool.
var GitDiffSchema = []byte(`{
	"type": "object",
	"properties": {
		"staged": {
			"type": "boolean",
			"description": "If true, show staged changes (git diff --cached). Default false (unstaged)."
		},
		"path": {
			"type": "string",
			"description": "Optional: limit diff to a specific file path (relative to workspace)"
		}
	},
	"additionalProperties": false
}`)

// NewGitDiff creates the git_diff tool.
func NewGitDiff(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_diff",
		Description: "Show changes in the working tree or staging area.",
		InputSchema: GitDiffSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			args := []string{"diff"}

			staged, _ := input["staged"].(bool)
			if staged {
				args = append(args, "--cached")
			}

			if p, ok := input["path"].(string); ok && p != "" {
				if _, err := workspace.ResolvePath(gh.workspaceRoot, p); err != nil {
					return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
				}
				args = append(args, "--", p)
			}

			out, err := gh.runGit(ctx, args...)
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			if out == "" {
				out = "no changes"
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
