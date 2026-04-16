package builtin

import (
	"context"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// GitCommitSchema is the JSON Schema for the git_commit tool.
var GitCommitSchema = []byte(`{
	"type": "object",
	"properties": {
		"message": {
			"type": "string",
			"description": "Commit message"
		}
	},
	"required": ["message"],
	"additionalProperties": false
}`)

// NewGitCommit creates the git_commit tool.
func NewGitCommit(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_commit",
		Description: "Create a commit with the staged changes.",
		InputSchema: GitCommitSchema,
		Timeout:     20 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			msg, ok := input["message"].(string)
			if !ok || msg == "" {
				return matter.ToolResult{Error: "message is required and must be a non-empty string"}, nil
			}

			out, err := gh.runGit(ctx, "commit", "-m", msg)
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
