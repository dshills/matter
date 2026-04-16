package builtin

import (
	"context"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// GitStatusSchema is the JSON Schema for the git_status tool.
var GitStatusSchema = []byte(`{
	"type": "object",
	"properties": {},
	"additionalProperties": false
}`)

// NewGitStatus creates the git_status tool.
func NewGitStatus(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_status",
		Description: "Show the working tree status (git status --porcelain=v1).",
		InputSchema: GitStatusSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute: func(ctx context.Context, _ map[string]any) (matter.ToolResult, error) {
			out, err := gh.runGit(ctx, "status", "--porcelain=v1")
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			if out == "" {
				out = "nothing to commit, working tree clean"
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}
