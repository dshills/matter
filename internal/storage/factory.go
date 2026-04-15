package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StoreConfig holds the configuration needed to create a Store.
// This mirrors the subset of config.StorageConfig needed for store
// construction. Retention and GC fields are not included here because
// they are consumed by the server's GC goroutine, not by the store.
type StoreConfig struct {
	Backend string // "sqlite" or "memory"
	Path    string // SQLite file path (only used when Backend is "sqlite")
}

// NewStore creates a Store based on the given configuration.
func NewStore(cfg StoreConfig) (Store, error) {
	switch cfg.Backend {
	case "memory":
		return NewMemoryStore(), nil
	case "sqlite":
		path, err := expandPath(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("resolving storage path: %w", err)
		}
		return NewSQLiteStore(path)
	default:
		return nil, fmt.Errorf("unknown storage backend: %q", cfg.Backend)
	}
}

// expandPath resolves ~ to the user's home directory.
func expandPath(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
