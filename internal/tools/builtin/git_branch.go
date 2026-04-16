package builtin

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// gitBranchNameRe validates branch names: no spaces, .., ~, ^, :, \, control chars, no leading -.
var gitBranchNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_.-]*$`)

// GitBranchSchema is the JSON Schema for the git_branch tool.
var GitBranchSchema = []byte(`{
	"type": "object",
	"properties": {
		"name": {
			"type": "string",
			"description": "Name of the branch to create. Omit to list branches."
		},
		"checkout": {
			"type": "boolean",
			"description": "If true, switch to the branch after creating it. Default false."
		}
	},
	"additionalProperties": false
}`)

// NewGitBranch creates the git_branch tool.
func NewGitBranch(gh *GitHelper) matter.Tool {
	return matter.Tool{
		Name:        "git_branch",
		Description: "List branches or create a new branch.",
		InputSchema: GitBranchSchema,
		Timeout:     20 * time.Second,
		Safe:        false,
		SideEffect:  true,
		Execute: func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
			name, hasName := input["name"].(string)

			if !hasName || name == "" {
				// List branches.
				out, err := gh.runGit(ctx, "branch", "-a")
				if err != nil {
					return matter.ToolResult{Error: err.Error()}, nil
				}
				if out == "" {
					out = "no branches"
				}
				return matter.ToolResult{Output: out}, nil
			}

			// Validate branch name.
			if !gitBranchNameRe.MatchString(name) {
				return matter.ToolResult{Error: fmt.Sprintf("invalid branch name %q: must match %s", name, gitBranchNameRe.String())}, nil
			}
			if containsDotDot(name) {
				return matter.ToolResult{Error: fmt.Sprintf("invalid branch name %q: must not contain '..'", name)}, nil
			}

			checkout, _ := input["checkout"].(bool)
			if checkout {
				out, err := gh.runGit(ctx, "checkout", "-b", name)
				if err != nil {
					return matter.ToolResult{Error: err.Error()}, nil
				}
				return matter.ToolResult{Output: out}, nil
			}

			out, err := gh.runGit(ctx, "branch", name)
			if err != nil {
				return matter.ToolResult{Error: err.Error()}, nil
			}
			if out == "" {
				out = fmt.Sprintf("Created branch %s", name)
			}
			return matter.ToolResult{Output: out}, nil
		},
	}
}

// containsDotDot checks for ".." anywhere in the string.
func containsDotDot(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}
	return false
}
