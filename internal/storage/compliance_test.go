package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// RunCompliance runs the shared compliance test suite against any Store implementation.
// The factory function must return a fresh, empty store for each test.
func RunCompliance(t *testing.T, name string, factory func() Store) {
	t.Helper()

	t.Run(name+"/CreateAndGetRun", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		now := time.Now().Truncate(time.Millisecond)
		run := &RunRow{
			RunID:     "run-1",
			Status:    "running",
			Task:      "test task",
			Workspace: "/tmp/ws",
			Model:     "gpt-4o",
			Provider:  "openai",
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := s.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}

		got, err := s.GetRun(ctx, "run-1")
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.RunID != "run-1" {
			t.Errorf("RunID = %q, want run-1", got.RunID)
		}
		if got.Status != "running" {
			t.Errorf("Status = %q, want running", got.Status)
		}
		if got.Task != "test task" {
			t.Errorf("Task = %q, want 'test task'", got.Task)
		}
	})

	t.Run(name+"/CreateDuplicate", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		run := &RunRow{RunID: "dup-1", Status: "running", Task: "t"}
		if err := s.CreateRun(ctx, run); err != nil {
			t.Fatalf("first create: %v", err)
		}
		err := s.CreateRun(ctx, run)
		if err == nil {
			t.Fatal("expected error for duplicate run")
		}
		var conflict *ErrConflict
		if !errors.As(err, &conflict) {
			t.Errorf("expected ErrConflict, got %T: %v", err, err)
		}
	})

	t.Run(name+"/GetRunNotFound", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_, err := s.GetRun(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent run")
		}
		var nf *ErrNotFound
		if !errors.As(err, &nf) {
			t.Errorf("expected ErrNotFound, got %T: %v", err, err)
		}
	})

	t.Run(name+"/UpdateRun", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		now := time.Now().Truncate(time.Millisecond)
		run := &RunRow{RunID: "upd-1", Status: "running", Task: "t", CreatedAt: now, UpdatedAt: now}
		_ = s.CreateRun(ctx, run)

		success := true
		completedAt := now.Add(time.Minute)
		run.Status = "completed"
		run.Success = &success
		run.Summary = "done"
		run.CompletedAt = &completedAt
		run.UpdatedAt = completedAt

		if err := s.UpdateRun(ctx, run); err != nil {
			t.Fatalf("UpdateRun: %v", err)
		}

		got, _ := s.GetRun(ctx, "upd-1")
		if got.Status != "completed" {
			t.Errorf("Status = %q, want completed", got.Status)
		}
		if got.Success == nil || !*got.Success {
			t.Error("Success should be true")
		}
		if got.Summary != "done" {
			t.Errorf("Summary = %q, want done", got.Summary)
		}
	})

	t.Run(name+"/UpdateRunNotFound", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		err := s.UpdateRun(ctx, &RunRow{RunID: "nope"})
		if err == nil {
			t.Fatal("expected error for nonexistent run")
		}
	})

	t.Run(name+"/ListRunsAll", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			_ = s.CreateRun(ctx, &RunRow{
				RunID:     fmt.Sprintf("list-%d", i),
				Status:    "completed",
				Task:      "t",
				CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
				UpdatedAt: time.Now().Add(time.Duration(i) * time.Second),
			})
		}

		runs, err := s.ListRuns(ctx, RunFilter{})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) != 5 {
			t.Errorf("got %d runs, want 5", len(runs))
		}
	})

	t.Run(name+"/ListRunsFilterStatus", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "r1", Status: "running", Task: "t", CreatedAt: time.Now()})
		_ = s.CreateRun(ctx, &RunRow{RunID: "r2", Status: "completed", Task: "t", CreatedAt: time.Now()})
		_ = s.CreateRun(ctx, &RunRow{RunID: "r3", Status: "running", Task: "t", CreatedAt: time.Now()})

		runs, _ := s.ListRuns(ctx, RunFilter{Status: "running"})
		if len(runs) != 2 {
			t.Errorf("got %d running runs, want 2", len(runs))
		}
	})

	t.Run(name+"/ListRunsPagination", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		for i := 0; i < 10; i++ {
			_ = s.CreateRun(ctx, &RunRow{
				RunID:     fmt.Sprintf("pg-%d", i),
				Status:    "completed",
				Task:      "t",
				CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
			})
		}

		page1, _ := s.ListRuns(ctx, RunFilter{Limit: 3, Offset: 0})
		page2, _ := s.ListRuns(ctx, RunFilter{Limit: 3, Offset: 3})

		if len(page1) != 3 {
			t.Errorf("page1 len = %d, want 3", len(page1))
		}
		if len(page2) != 3 {
			t.Errorf("page2 len = %d, want 3", len(page2))
		}
		if len(page1) > 0 && len(page2) > 0 && page1[0].RunID == page2[0].RunID {
			t.Error("pages should not overlap")
		}
	})

	t.Run(name+"/ListRunsTimeFilter", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 5; i++ {
			_ = s.CreateRun(ctx, &RunRow{
				RunID:     fmt.Sprintf("tf-%d", i),
				Status:    "completed",
				Task:      "t",
				CreatedAt: base.Add(time.Duration(i) * 24 * time.Hour),
			})
		}

		after := base.Add(48 * time.Hour) // after day 2
		runs, _ := s.ListRuns(ctx, RunFilter{After: &after})
		if len(runs) != 2 { // day 3 and day 4
			t.Errorf("got %d runs after filter, want 2", len(runs))
		}

		before := base.Add(48 * time.Hour) // before day 2
		runs, _ = s.ListRuns(ctx, RunFilter{Before: &before})
		if len(runs) != 2 { // day 0 and day 1
			t.Errorf("got %d runs before filter, want 2", len(runs))
		}
	})

	t.Run(name+"/ListRunsLimitCap", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			_ = s.CreateRun(ctx, &RunRow{
				RunID:  fmt.Sprintf("cap-%d", i),
				Status: "completed",
				Task:   "t",
			})
		}

		// Request limit > 200 should be capped.
		runs, _ := s.ListRuns(ctx, RunFilter{Limit: 999})
		if len(runs) != 5 {
			t.Errorf("got %d runs, want 5", len(runs))
		}
	})

	t.Run(name+"/ListRunsOffsetBeyondEnd", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "only", Status: "completed", Task: "t"})

		runs, err := s.ListRuns(ctx, RunFilter{Offset: 100})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) != 0 {
			t.Errorf("got %d runs, want 0 for offset beyond end", len(runs))
		}
	})

	t.Run(name+"/DeleteRunCascade", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "del-1", Status: "running", Task: "t"})
		_ = s.AppendStep(ctx, "del-1", &StepRow{StepNumber: 1, Decision: "tool_call"})
		_ = s.AppendStep(ctx, "del-1", &StepRow{StepNumber: 2, Decision: "complete"})
		_ = s.AppendEvent(ctx, "del-1", &EventRow{Type: "run_started"})

		if err := s.DeleteRun(ctx, "del-1"); err != nil {
			t.Fatalf("DeleteRun: %v", err)
		}

		// Run should be gone.
		_, err := s.GetRun(ctx, "del-1")
		if err == nil {
			t.Error("expected error after delete")
		}

		// Steps should be gone.
		_, err = s.GetSteps(ctx, "del-1")
		if err == nil {
			t.Error("expected error for steps after cascade delete")
		}

		// Events should be gone.
		_, err = s.GetEvents(ctx, "del-1", 0)
		if err == nil {
			t.Error("expected error for events after cascade delete")
		}
	})

	t.Run(name+"/DeleteRunNotFound", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		err := s.DeleteRun(ctx, "nope")
		if err == nil {
			t.Fatal("expected error for nonexistent run")
		}
	})

	t.Run(name+"/AppendAndGetSteps", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "steps-1", Status: "running", Task: "t"})

		for i := 1; i <= 3; i++ {
			err := s.AppendStep(ctx, "steps-1", &StepRow{
				StepNumber: i,
				Decision:   "tool_call",
				ToolName:   fmt.Sprintf("tool-%d", i),
				Tokens:     100 * i,
			})
			if err != nil {
				t.Fatalf("AppendStep %d: %v", i, err)
			}
		}

		steps, err := s.GetSteps(ctx, "steps-1")
		if err != nil {
			t.Fatalf("GetSteps: %v", err)
		}
		if len(steps) != 3 {
			t.Fatalf("got %d steps, want 3", len(steps))
		}

		// StepIDs should be auto-incremented and unique.
		seen := make(map[int64]bool)
		for _, step := range steps {
			if seen[step.StepID] {
				t.Errorf("duplicate StepID: %d", step.StepID)
			}
			seen[step.StepID] = true
			if step.RunID != "steps-1" {
				t.Errorf("RunID = %q, want steps-1", step.RunID)
			}
		}
	})

	t.Run(name+"/AppendStepRunNotFound", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		err := s.AppendStep(ctx, "nope", &StepRow{StepNumber: 1})
		if err == nil {
			t.Fatal("expected error for nonexistent run")
		}
	})

	t.Run(name+"/AppendAndGetEvents", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "ev-1", Status: "running", Task: "t"})

		for i := 0; i < 5; i++ {
			err := s.AppendEvent(ctx, "ev-1", &EventRow{
				Type: fmt.Sprintf("event-%d", i),
				Data: fmt.Sprintf(`{"step":%d}`, i),
			})
			if err != nil {
				t.Fatalf("AppendEvent %d: %v", i, err)
			}
		}

		// Get all events.
		events, err := s.GetEvents(ctx, "ev-1", 0)
		if err != nil {
			t.Fatalf("GetEvents: %v", err)
		}
		if len(events) != 5 {
			t.Fatalf("got %d events, want 5", len(events))
		}

		// Seqs should be monotonically increasing.
		for i := 1; i < len(events); i++ {
			if events[i].Seq <= events[i-1].Seq {
				t.Errorf("seq not increasing: %d <= %d", events[i].Seq, events[i-1].Seq)
			}
		}

		// Get events after a cursor.
		cursor := events[2].Seq
		after, err := s.GetEvents(ctx, "ev-1", cursor)
		if err != nil {
			t.Fatalf("GetEvents after cursor: %v", err)
		}
		if len(after) != 2 {
			t.Errorf("got %d events after cursor, want 2", len(after))
		}
	})

	t.Run(name+"/AppendEventRunNotFound", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		err := s.AppendEvent(ctx, "nope", &EventRow{Type: "test"})
		if err == nil {
			t.Fatal("expected error for nonexistent run")
		}
	})

	t.Run(name+"/IncrementAndGetMetrics", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.IncrementMetrics(ctx, MetricsDelta{
			RunsStarted: 3, ToolCalls: 10, TotalTokens: 5000, TotalCostUSD: 0.05,
		})
		_ = s.IncrementMetrics(ctx, MetricsDelta{
			RunsCompleted: 2, RunsFailed: 1, LLMCalls: 5, TotalTokens: 3000,
		})

		m, err := s.GetMetrics(ctx)
		if err != nil {
			t.Fatalf("GetMetrics: %v", err)
		}
		if m.RunsStarted != 3 {
			t.Errorf("RunsStarted = %d, want 3", m.RunsStarted)
		}
		if m.RunsCompleted != 2 {
			t.Errorf("RunsCompleted = %d, want 2", m.RunsCompleted)
		}
		if m.RunsFailed != 1 {
			t.Errorf("RunsFailed = %d, want 1", m.RunsFailed)
		}
		if m.ToolCalls != 10 {
			t.Errorf("ToolCalls = %d, want 10", m.ToolCalls)
		}
		if m.LLMCalls != 5 {
			t.Errorf("LLMCalls = %d, want 5", m.LLMCalls)
		}
		if m.TotalTokens != 8000 {
			t.Errorf("TotalTokens = %d, want 8000", m.TotalTokens)
		}
		if m.TotalCostUSD != 0.05 {
			t.Errorf("TotalCostUSD = %f, want 0.05", m.TotalCostUSD)
		}
	})

	t.Run(name+"/DeleteExpiredRuns", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		old := time.Now().Add(-48 * time.Hour)
		recent := time.Now().Add(-1 * time.Hour)
		oldComplete := old
		recentComplete := recent

		// Old completed run — should be deleted.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "old-done", Status: "completed", Task: "t",
			CreatedAt: old, UpdatedAt: old, CompletedAt: &oldComplete,
		})
		// Recent completed run — should survive.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "new-done", Status: "completed", Task: "t",
			CreatedAt: recent, UpdatedAt: recent, CompletedAt: &recentComplete,
		})
		// Old paused run — should be deleted.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "old-paused", Status: "paused", Task: "t",
			CreatedAt: old, UpdatedAt: old,
		})
		// Recent paused run — should survive.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "new-paused", Status: "paused", Task: "t",
			CreatedAt: recent, UpdatedAt: recent,
		})
		// Running run — should always survive.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "still-running", Status: "running", Task: "t",
			CreatedAt: old, UpdatedAt: old,
		})

		cutoff := time.Now().Add(-24 * time.Hour)
		deleted, err := s.DeleteExpiredRuns(ctx, cutoff, cutoff)
		if err != nil {
			t.Fatalf("DeleteExpiredRuns: %v", err)
		}
		if deleted != 2 {
			t.Errorf("deleted = %d, want 2", deleted)
		}

		// Verify survivors.
		remaining, _ := s.ListRuns(ctx, RunFilter{Limit: 200})
		if len(remaining) != 3 {
			t.Errorf("remaining = %d, want 3", len(remaining))
		}
	})

	t.Run(name+"/Concurrency", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		const goroutines = 20
		var wg sync.WaitGroup

		// Pre-create runs for step/event appends.
		for i := 0; i < goroutines; i++ {
			_ = s.CreateRun(ctx, &RunRow{
				RunID:     fmt.Sprintf("conc-%d", i),
				Status:    "running",
				Task:      "t",
				CreatedAt: time.Now(),
			})
		}

		wg.Add(goroutines * 3) // each goroutine does 3 operations

		for i := 0; i < goroutines; i++ {
			runID := fmt.Sprintf("conc-%d", i)

			// Append steps.
			go func() {
				defer wg.Done()
				_ = s.AppendStep(ctx, runID, &StepRow{StepNumber: 1, Decision: "tool_call"})
			}()

			// Append events.
			go func() {
				defer wg.Done()
				_ = s.AppendEvent(ctx, runID, &EventRow{Type: "test"})
			}()

			// Increment metrics.
			go func() {
				defer wg.Done()
				_ = s.IncrementMetrics(ctx, MetricsDelta{RunsStarted: 1, TotalTokens: 100})
			}()
		}

		wg.Wait()

		// Verify metrics are consistent.
		m, err := s.GetMetrics(ctx)
		if err != nil {
			t.Fatalf("GetMetrics: %v", err)
		}
		if m.RunsStarted != goroutines {
			t.Errorf("RunsStarted = %d, want %d", m.RunsStarted, goroutines)
		}
		if m.TotalTokens != goroutines*100 {
			t.Errorf("TotalTokens = %d, want %d", m.TotalTokens, goroutines*100)
		}
	})

	t.Run(name+"/ListRunsOrderByUpdatedAt", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		// Create runs with inverted update order.
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "ord-a", Status: "completed", Task: "t",
			CreatedAt: base, UpdatedAt: base.Add(2 * time.Hour),
		})
		_ = s.CreateRun(ctx, &RunRow{
			RunID: "ord-b", Status: "completed", Task: "t",
			CreatedAt: base.Add(time.Hour), UpdatedAt: base,
		})

		runs, _ := s.ListRuns(ctx, RunFilter{OrderBy: "updated_at"})
		if len(runs) != 2 {
			t.Fatalf("got %d runs, want 2", len(runs))
		}
		if runs[0].RunID != "ord-b" {
			t.Errorf("first by updated_at = %q, want ord-b", runs[0].RunID)
		}
	})

	t.Run(name+"/GetStepsEmpty", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "empty-steps", Status: "running", Task: "t"})
		steps, err := s.GetSteps(ctx, "empty-steps")
		if err != nil {
			t.Fatalf("GetSteps: %v", err)
		}
		if len(steps) != 0 {
			t.Errorf("got %d steps, want 0", len(steps))
		}
	})

	t.Run(name+"/GetEventsEmpty", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		_ = s.CreateRun(ctx, &RunRow{RunID: "empty-ev", Status: "running", Task: "t"})
		events, err := s.GetEvents(ctx, "empty-ev", 0)
		if err != nil {
			t.Fatalf("GetEvents: %v", err)
		}
		if len(events) != 0 {
			t.Errorf("got %d events, want 0", len(events))
		}
	})

	t.Run(name+"/MetricsInitiallyZero", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		m, err := s.GetMetrics(ctx)
		if err != nil {
			t.Fatalf("GetMetrics: %v", err)
		}
		if m.RunsStarted != 0 || m.TotalTokens != 0 || m.TotalCostUSD != 0 {
			t.Error("initial metrics should be zero")
		}
	})

	t.Run(name+"/RunMutation isolation", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		run := &RunRow{RunID: "iso-1", Status: "running", Task: "t"}
		_ = s.CreateRun(ctx, run)

		// Mutating the original should not affect the stored copy.
		run.Status = "failed"

		got, _ := s.GetRun(ctx, "iso-1")
		if got.Status != "running" {
			t.Errorf("Status = %q, want running (mutation leaked)", got.Status)
		}
	})

	t.Run(name+"/RunMutation isolation pointer and slice", func(t *testing.T) {
		s := factory()
		defer func() { _ = s.Close() }()
		ctx := context.Background()

		success := true
		state := []byte(`{"key":"value"}`)
		run := &RunRow{
			RunID:       "iso-2",
			Status:      "completed",
			Task:        "t",
			Success:     &success,
			PausedState: state,
		}
		_ = s.CreateRun(ctx, run)

		// Mutating the original pointer value should not leak.
		success = false
		state[0] = 'X'

		got, _ := s.GetRun(ctx, "iso-2")
		if got.Success == nil || !*got.Success {
			t.Error("Success mutation leaked through pointer")
		}
		if got.PausedState[0] == 'X' {
			t.Error("PausedState mutation leaked through slice")
		}

		// Mutating the returned copy should not affect the store.
		*got.Success = false
		got.PausedState[0] = 'Z'

		got2, _ := s.GetRun(ctx, "iso-2")
		if got2.Success == nil || !*got2.Success {
			t.Error("Success mutation leaked through returned copy")
		}
		if got2.PausedState[0] == 'Z' {
			t.Error("PausedState mutation leaked through returned copy")
		}
	})
}
