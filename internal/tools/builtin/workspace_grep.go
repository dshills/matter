package builtin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dshills/matter/internal/workspace"
	"github.com/dshills/matter/pkg/matter"
)

// maxGrepFileSize is the maximum file size (1 MB) for workspace_grep to process.
const maxGrepFileSize = 1 * 1024 * 1024

// WorkspaceGrepSchema is the JSON Schema for the workspace_grep tool.
var WorkspaceGrepSchema = []byte(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "Regular expression pattern to search for (Go regexp syntax)"
		},
		"path": {
			"type": "string",
			"description": "Relative directory or file path to search within. Defaults to workspace root."
		},
		"glob": {
			"type": "string",
			"description": "Optional glob filter for file names (e.g., '*.go', '*.ts'). Applied to base filename only."
		},
		"max_results": {
			"type": "integer",
			"description": "Maximum number of matching lines to return. Default 100, max 500."
		},
		"context_lines": {
			"type": "integer",
			"description": "Number of context lines before and after each match. Default 0, max 5."
		}
	},
	"required": ["pattern"],
	"additionalProperties": false
}`)

// NewWorkspaceGrep creates the workspace_grep tool for the given workspace root.
func NewWorkspaceGrep(workspaceRoot string, maxOutputBytes int, allowedHiddenPaths []string) matter.Tool {
	return matter.Tool{
		Name:        "workspace_grep",
		Description: "Search file content for a regex pattern. Returns matching lines with file paths and line numbers in grep format.",
		InputSchema: WorkspaceGrepSchema,
		Timeout:     30 * time.Second,
		Safe:        true,
		SideEffect:  false,
		Execute:     workspaceGrepFunc(workspaceRoot, maxOutputBytes, allowedHiddenPaths),
	}
}

func workspaceGrepFunc(workspaceRoot string, maxOutputBytes int, allowedHiddenPaths []string) matter.ToolExecuteFunc {
	gi := loadGitignore(workspaceRoot)

	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		patternStr, ok := input["pattern"].(string)
		if !ok || patternStr == "" {
			return matter.ToolResult{Error: "pattern is required and must be a string"}, nil
		}

		re, err := regexp.Compile(patternStr)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("invalid regex: %s", err)}, nil
		}

		// Resolve search root.
		searchRoot := workspaceRoot
		if p, ok := input["path"].(string); ok && p != "" {
			resolved, resolveErr := workspace.ResolvePath(workspaceRoot, p)
			if resolveErr != nil {
				return matter.ToolResult{Error: fmt.Sprintf("path rejected: %s", resolveErr)}, nil
			}
			searchRoot = resolved
		}

		var globPattern string
		if g, ok := input["glob"].(string); ok && g != "" {
			globPattern = g
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

		contextLines := 0
		if v, ok := input["context_lines"].(float64); ok {
			contextLines = int(v)
		}
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > 5 {
			contextLines = 5
		}

		var sb strings.Builder
		matchCount := 0
		totalMatches := 0
		skippedLarge := 0
		needSeparator := false

		walkErr := filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rel, relErr := filepath.Rel(workspaceRoot, path)
			if relErr != nil {
				return nil
			}

			// Skip hidden directories unless allowed.
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
				if workspace.CheckHiddenPath(rel, allowedHiddenPaths) != nil {
					return filepath.SkipDir
				}
			}

			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}

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

			// Apply glob filter on filename.
			if globPattern != "" {
				matched, matchErr := filepath.Match(globPattern, d.Name())
				if matchErr != nil || !matched {
					return nil
				}
			}

			// Check file size.
			info, statErr := d.Info()
			if statErr != nil {
				return nil
			}
			if info.Size() > maxGrepFileSize {
				skippedLarge++
				return nil
			}

			// Check for binary content.
			if isBinaryFile(path) {
				return nil
			}

			// Search the file.
			fileMatches := searchFile(path, rel, re, contextLines)
			for _, fm := range fileMatches {
				totalMatches++
				if matchCount < maxResults {
					if needSeparator && contextLines > 0 {
						sb.WriteString("--\n")
					}
					sb.WriteString(fm)
					matchCount++
					needSeparator = true
				}
			}

			return nil
		})

		if walkErr != nil && walkErr != context.Canceled && walkErr != context.DeadlineExceeded {
			return matter.ToolResult{Error: fmt.Sprintf("searching: %s", walkErr)}, nil
		}

		if skippedLarge > 0 {
			fmt.Fprintf(&sb, "[NOTICE: %d files skipped (>1MB)]\n", skippedLarge)
		}

		if totalMatches > maxResults {
			fmt.Fprintf(&sb, "[TRUNCATED: showing %d of %d matches]\n", maxResults, totalMatches)
		}

		output := sb.String()
		if maxOutputBytes > 0 && len(output) > maxOutputBytes {
			output = output[:maxOutputBytes] + "\n[TRUNCATED]"
		}

		if matchCount == 0 && skippedLarge == 0 {
			return matter.ToolResult{Output: "No matches found."}, nil
		}

		return matter.ToolResult{Output: output}, nil
	}
}

// searchFile searches a single file for regex matches and returns formatted output lines.
func searchFile(path, relPath string, re *regexp.Regexp, contextLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if contextLines == 0 {
		// Simple mode: just matching lines.
		var results []string
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s\n", relPath, i+1, line))
			}
		}
		return results
	}

	// Context mode: group matches with surrounding lines.
	var results []string
	matched := make([]bool, len(lines))
	for i, line := range lines {
		if re.MatchString(line) {
			matched[i] = true
		}
	}

	for i, isMatch := range matched {
		if !isMatch {
			continue
		}

		var group strings.Builder
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}

		for j := start; j <= end; j++ {
			if matched[j] {
				fmt.Fprintf(&group, "%s:%d:%s\n", relPath, j+1, lines[j])
			} else {
				fmt.Fprintf(&group, "%s-%d-%s\n", relPath, j+1, lines[j])
			}
		}

		results = append(results, group.String())
	}

	return results
}

// isBinaryFile checks if a file appears to be binary by looking for null bytes
// in the first 512 bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}
