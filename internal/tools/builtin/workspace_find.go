package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// skipDirs is the set of directories skipped by workspace_find and workspace_grep.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// WorkspaceFindSchema is the JSON Schema for the workspace_find tool.
var WorkspaceFindSchema = []byte(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "Glob pattern to match file paths (e.g., '**/*.go', 'internal/**/*_test.go')"
		},
		"max_results": {
			"type": "integer",
			"description": "Maximum number of results to return. Default 100, max 500."
		}
	},
	"required": ["pattern"],
	"additionalProperties": false
}`)

// NewWorkspaceFind creates the workspace_find tool for the given workspace root.
func NewWorkspaceFind(workspaceRoot string, allowedHiddenPaths []string) matter.Tool {
	return matter.Tool{
		Name:        "workspace_find",
		Description: "Find files in the workspace matching a glob pattern. Returns relative paths, one per line. Supports ** for recursive matching.",
		InputSchema: WorkspaceFindSchema,
		Timeout:     10 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute:     workspaceFindFunc(workspaceRoot, allowedHiddenPaths),
	}
}

func workspaceFindFunc(workspaceRoot string, allowedHiddenPaths []string) matter.ToolExecuteFunc {
	// Load .gitignore if present.
	gi := loadGitignore(workspaceRoot)

	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		pattern, ok := input["pattern"].(string)
		if !ok || pattern == "" {
			return matter.ToolResult{Error: "pattern is required and must be a string"}, nil
		}

		// Validate the glob pattern.
		if !doublestar.ValidatePattern(pattern) {
			return matter.ToolResult{Error: fmt.Sprintf("invalid glob pattern: %s", pattern)}, nil
		}

		maxResults := 100
		if v, ok := input["max_results"].(float64); ok {
			maxResults = int(v)
		}
		if maxResults <= 0 {
			maxResults = 100
		}
		if maxResults > 500 {
			maxResults = 500
		}

		var matches []string
		totalMatches := 0

		err := filepath.WalkDir(workspaceRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible entries
			}

			// Check context cancellation.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rel, relErr := filepath.Rel(workspaceRoot, path)
			if relErr != nil {
				return nil
			}
			if rel == "." {
				return nil
			}

			// Skip hidden directories unless allowed.
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
				if workspace.CheckHiddenPath(rel, allowedHiddenPaths) != nil {
					return filepath.SkipDir
				}
			}

			// Skip known non-text directories.
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}

			// Skip .gitignore'd paths.
			if gi != nil {
				gitPath := rel
				if d.IsDir() {
					gitPath += "/"
				}
				if gi.MatchesPath(gitPath) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			if d.IsDir() {
				return nil
			}

			// Match against glob pattern.
			matched, matchErr := doublestar.Match(pattern, rel)
			if matchErr != nil {
				return nil // skip invalid match
			}
			if matched {
				totalMatches++
				if len(matches) < maxResults {
					matches = append(matches, rel)
				}
			}

			return nil
		})

		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			return matter.ToolResult{Error: fmt.Sprintf("walking workspace: %s", err)}, nil
		}

		sort.Strings(matches)

		var sb strings.Builder
		for _, m := range matches {
			sb.WriteString(m)
			sb.WriteByte('\n')
		}

		if totalMatches > maxResults {
			fmt.Fprintf(&sb, "[TRUNCATED: showing %d of %d matches]\n", maxResults, totalMatches)
		}

		if len(matches) == 0 {
			return matter.ToolResult{Output: "No files matched the pattern."}, nil
		}

		return matter.ToolResult{Output: sb.String()}, nil
	}
}

// loadGitignore loads and parses a .gitignore file from the workspace root.
// Returns nil if no .gitignore exists.
func loadGitignore(workspaceRoot string) *gitignore.GitIgnore {
	path := filepath.Join(workspaceRoot, ".gitignore")
	gi, err := gitignore.CompileIgnoreFile(path)
	if err != nil {
		return nil
	}
	return gi
}
