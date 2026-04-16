package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// GitLogSchema is the JSON Schema for the git_log tool.
var GitLogSchema = []byte(`{
	"type": "object",
	"properties": {
		"max_count": {
			"type": "integer",
			"description": "Number of commits to show. Default 10, max 50."
		},
		"oneline": {
			"type": "boolean",
			"description": "If true, use --oneline format. Default true."
		},
		"path": {
			"type": "string",
			"description": "Optional: show history for a specific file path"
		}
	},
	"additionalProperties": false
}`)

// NewGitLog creates the git_log tool.
func NewGitLog(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_log",
		Description: "Show recent commit history.",
		InputSchema: GitLogSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			maxCount := 10
			if v, ok := input["max_count"].(float64); ok {
				maxCount = int(v)
			}
			if maxCount <= 0 {
				maxCount = 10
			}
			if maxCount > 50 {
				maxCount = 50
			}

			oneline := true
			if v, ok := input["oneline"].(bool); ok {
				oneline = v
			}

			args := []string{"log", fmt.Sprintf("--max-count=%d", maxCount)}
			if oneline {
				args = append(args, "--oneline")
			} else {
				args = append(args, "--format=medium")
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
				out = "no commits"
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
