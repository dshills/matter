package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLiteStoreCompliance(t *testing.T) {
	RunCompliance(t, "SQLiteStore", func() Store {
		dbPath := filepath.Join(t.TempDir(), "compliance.db")
		store, err := NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		return store
	})
}

func TestSQLiteWALMode(t *testing.T) {
	store := newTestSQLiteStore(t)

	var mode string
	err := store.DB().QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestSQLiteForeignKeys(t *testing.T) {
	store := newTestSQLiteStore(t)

	var fk int
	err := store.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestSQLiteCreatesDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "dir")
	dbPath := filepath.Join(subdir, "matter.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify the file exists.
	if _, err := filepath.Glob(dbPath); err != nil {
		t.Fatalf("database file not found: %v", err)
	}
}

func TestSQLitePersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// Create store and insert a run.
	store1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}

	ctx := context.Background()
	_ = store1.CreateRun(ctx, &RunRow{
		RunID:  "persist-1",
		Status: "completed",
		Task:   "test task",
	})
	_ = store1.AppendStep(ctx, "persist-1", &StepRow{StepNumber: 1, Decision: "complete"})
	_ = store1.AppendEvent(ctx, "persist-1", &EventRow{Type: "run_completed"})
	_ = store1.IncrementMetrics(ctx, MetricsDelta{RunsStarted: 1, RunsCompleted: 1})
	_ = store1.Close()

	// Reopen and verify data survived.
	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	defer func() { _ = store2.Close() }()

	run, err := store2.GetRun(ctx, "persist-1")
	if err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	if run.Task != "test task" {
		t.Errorf("Task = %q, want 'test task'", run.Task)
	}

	steps, err := store2.GetSteps(ctx, "persist-1")
	if err != nil {
		t.Fatalf("GetSteps after reopen: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("steps = %d, want 1", len(steps))
	}

	events, err := store2.GetEvents(ctx, "persist-1", 0)
	if err != nil {
		t.Fatalf("GetEvents after reopen: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1", len(events))
	}

	m, err := store2.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics after reopen: %v", err)
	}
	if m.RunsStarted != 1 || m.RunsCompleted != 1 {
		t.Errorf("metrics not persisted: started=%d completed=%d", m.RunsStarted, m.RunsCompleted)
	}
}
