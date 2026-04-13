// Package workspace provides path safety for workspace-confined file operations.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath resolves a relative path against the workspace root and validates
// that the result stays within the workspace. It rejects absolute paths,
// traversal attempts, and symlink escapes.
func ResolvePath(workspaceRoot, relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", relPath)
	}
	if containsTraversal(relPath) {
		return "", fmt.Errorf("path traversal is not allowed: %s", relPath)
	}

	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root: %w", err)
	}

	joined := filepath.Join(absRoot, relPath)

	// Evaluate symlinks to detect escapes.
	resolved, err := evalIfExists(joined)
	if err != nil {
		return "", fmt.Errorf("resolving symlinks: %w", err)
	}

	resolvedRoot, err := evalIfExists(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root symlinks: %w", err)
	}

	if !isWithin(resolvedRoot, resolved) {
		return "", fmt.Errorf("path escapes workspace: %s", relPath)
	}

	return resolved, nil
}

// containsTraversal checks for ".." segments in a path.
func containsTraversal(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// isWithin returns true if target is equal to or a subdirectory of root.
func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// evalIfExists resolves symlinks if the path exists. For paths that don't
// exist yet (e.g., new files to be written), it walks up the directory tree
// to find the nearest existing ancestor, resolves it, and appends the
// remaining non-existent segments.
func evalIfExists(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// Walk up to find the nearest existing ancestor.
	parent := filepath.Dir(p)
	if parent == p {
		// Reached filesystem root — return as-is.
		return p, nil
	}
	name := filepath.Base(p)
	resolvedParent, err := evalIfExists(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, name), nil
}
