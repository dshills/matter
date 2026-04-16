package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// GitCheckoutSchema is the JSON Schema for the git_checkout tool.
var GitCheckoutSchema = []byte(`{
	"type": "object",
	"properties": {
		"branch": {
			"type": "string",
			"description": "Branch name to switch to"
		},
		"path": {
			"type": "string",
			"description": "File path to restore to its committed state (discards uncommitted changes)"
		}
	},
	"oneOf": [
		{"required": ["branch"]},
		{"required": ["path"]}
	],
	"additionalProperties": false
}`)

// NewGitCheckout creates the git_checkout tool.
func NewGitCheckout(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_checkout",
		Description: "Switch branches or restore a file to its committed state.",
		InputSchema: GitCheckoutSchema,
		Timeout:     20 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			branch, hasBranch := input["branch"].(string)
			path, hasPath := input["path"].(string)

			if hasBranch && branch != "" && hasPath && path != "" {
				return matter.ToolResult{Error: "specify either 'branch' or 'path', not both"}, nil
			}

			if hasBranch && branch != "" {
				// Reject branch names that could be interpreted as flags.
				if strings.HasPrefix(branch, "-") {
					return matter.ToolResult{Error: fmt.Sprintf("invalid branch name %q: must not start with '-'", branch)}, nil
				}
				out, err := gh.runGit(ctx, "checkout", branch)
				if err != nil {
					return matter.ToolResult{Error: err.Error()}, nil
				}
				if out == "" {
					out = fmt.Sprintf("Switched to branch %s", branch)
				}
				return matter.ToolResult{Output: out}, nil
			}

			if hasPath && path != "" {
				if _, err := workspace.ResolvePath(gh.workspaceRoot, path); err != nil {
					return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", err)}, nil
				}
				out, err := gh.runGit(ctx, "checkout", "--", path)
				if err != nil {
					return matter.ToolResult{Error: err.Error()}, nil
				}
				if out == "" {
					out = fmt.Sprintf("Restored %s to committed state", path)
				}
				return matter.ToolResult{Output: out}, nil
			}

			return matter.ToolResult{Error: "specify either 'branch' or 'path'"}, nil
		},
	}
}
