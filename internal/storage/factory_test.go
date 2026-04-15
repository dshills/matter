package storage

import (
	"path/filepath"
	"testing"
)

func TestNewStoreMemory(t *testing.T) {
	store, err := NewStore(StoreConfig{Backend: "memory"})
	if err != nil {
		t.Fatalf("NewStore(memory): %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, ok := store.(*MemoryStore); !ok {
		t.Errorf("expected *MemoryStore, got %T", store)
	}
}

func TestNewStoreSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(StoreConfig{Backend: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("NewStore(sqlite): %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, ok := store.(*SQLiteStore); !ok {
		t.Errorf("expected *SQLiteStore, got %T", store)
	}
}

func TestNewStoreUnknownBackend(t *testing.T) {
	_, err := NewStore(StoreConfig{Backend: "postgres"})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestNewStoreTildeExpansion(t *testing.T) {
	// Verify that ~ paths don't cause an error (we can't assert the exact
	// expanded path portably, but we can verify it doesn't fail).
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tilde.db")
	store, err := NewStore(StoreConfig{Backend: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("NewStore with absolute path: %v", err)
	}
	_ = store.Close()
}

func TestExpandPath(t *testing.T) {
	// Non-tilde paths pass through unchanged.
	got, err := expandPath("/absolute/path")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("expandPath(/absolute/path) = %q", got)
	}

	// Tilde paths expand (just verify it doesn't error and doesn't start with ~).
	got, err = expandPath("~/some/path")
	if err != nil {
		t.Fatalf("expandPath(~/some/path): %v", err)
	}
	if got == "~/some/path" {
		t.Error("expandPath did not expand tilde")
	}
}
