package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckHiddenPath rejects writes to hidden paths (files or directories starting
// with ".") unless the full path falls within an explicitly allowed subtree.
// relPath must be a clean relative path within the workspace.
func CheckHiddenPath(relPath string, allowedPaths []string) error {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !hasHiddenSegment(clean) {
		return nil
	}
	if !isAllowed(clean, allowedPaths) {
		return fmt.Errorf("write to hidden path %q is not allowed", clean)
	}
	return nil
}

// hasHiddenSegment returns true if any path segment starts with ".".
func hasHiddenSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// isAllowed checks whether a path is permitted by the allow list.
// A match occurs when:
//   - the path equals an allowed path exactly
//   - the path is a child of an allowed path (allowed path is a directory prefix)
func isAllowed(path string, allowedPaths []string) bool {
	for _, allowed := range allowedPaths {
		allowed = filepath.ToSlash(filepath.Clean(allowed))
		if path == allowed {
			return true
		}
		if strings.HasPrefix(path, allowed+"/") {
			return true
		}
	}
	return false
}
