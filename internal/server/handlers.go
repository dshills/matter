package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dshills/matter/internal/runner"
	"github.com/dshills/matter/internal/storage"
	"github.com/dshills/matter/pkg/matter"
)

// createRunRequest is the request body for POST /api/v1/runs.
type createRunRequest struct {
	Task      string `json:"task"`
	Workspace string `json:"workspace"`
}

// createRunResponse is the response for POST /api/v1/runs.
type createRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// runStatusResponse is the response for GET /api/v1/runs/{id}.
type runStatusResponse struct {
	RunID        string  `json:"run_id"`
	Status       string  `json:"status"`
	Success      *bool   `json:"success,omitempty"`
	FinalSummary string  `json:"final_summary,omitempty"`
	Error        string  `json:"error,omitempty"`
	Steps        int     `json:"steps"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	Duration     string  `json:"duration,omitempty"`
	Question     string  `json:"question,omitempty"`
}

// answerRequest is the request body for POST /api/v1/runs/{id}/answer.
type answerRequest struct {
	Answer string `json:"answer"`
}

// toolResponse is a single tool in the GET /api/v1/tools response.
type toolResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Safe        bool   `json:"safe"`
	SideEffect  bool   `json:"side_effect"`
}

// handleHealth returns server health status.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": Version,
	})
}

// handleCreateRun starts a new agent run asynchronously.
func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Task == "" {
		writeError(w, http.StatusBadRequest, "task is required")
		return
	}

	runID := generateRunID()
	now := time.Now()

	ctx, cancel := context.WithCancel(context.Background())

	// Create a fresh runner for this run (isolated per-run state).
	rn, err := runner.New(s.cfg, s.llmClient)
	if err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, "failed to create runner")
		return
	}

	// Persist the run in the store.
	runRow := &storage.RunRow{
		RunID:     runID,
		Status:    "running",
		Task:      req.Task,
		Workspace: req.Workspace,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateRun(r.Context(), runRow); err != nil {
		cancel()
		log.Printf("failed to persist run: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create run")
		return
	}

	activeRun := &ActiveRun{
		RunID:  runID,
		Status: StatusRunning,
		Cancel: cancel,
		Runner: rn,
	}

	// Set up progress callback to broadcast events to SSE subscribers
	// and persist events to the store.
	rn.SetProgressFunc(func(event matter.ProgressEvent) {
		event.RunID = runID
		// Persist event to store (best-effort).
		data, _ := json.Marshal(event.Data)
		if err := s.store.AppendEvent(context.Background(), runID, &storage.EventRow{
			Type:      event.Event,
			Data:      string(data),
			Timestamp: event.Timestamp,
		}); err != nil {
			log.Printf("failed to persist event for run %s: %v", runID, err)
		}
		activeRun.broadcast(event)
	})

	if err := s.tracker.Add(activeRun); err != nil {
		cancel()
		// Clean up the persisted run.
		runRow.Status = "failed"
		runRow.ErrorMessage = err.Error()
		_ = s.store.UpdateRun(r.Context(), runRow)
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	// Execute the run in a background goroutine.
	go s.executeRun(ctx, rn, activeRun, req)

	writeJSON(w, http.StatusAccepted, createRunResponse{
		RunID:  runID,
		Status: string(StatusRunning),
	})
}

// executeRun runs the agent task and updates state on completion.
func (s *Server) executeRun(ctx context.Context, rn *runner.Runner, run *ActiveRun, req createRunRequest) {
	result := rn.Run(ctx, matter.RunRequest{
		Task:      req.Task,
		Workspace: req.Workspace,
	})

	now := time.Now()

	run.mu.Lock()

	if result.Paused {
		// Atomically check paused limit and transition.
		if !s.tracker.TryPause(run) {
			if run.Cancel != nil {
				run.Cancel()
			}
			s.tracker.TransitionStatus(run, StatusCancelled)
			run.Runner = nil
			run.mu.Unlock()

			// Update store.
			s.updateRunFromResult(run.RunID, &matter.RunResult{
				Error: fmt.Errorf("paused run limit reached (%d)", s.tracker.maxPaused),
			}, "failed", now)
			s.tracker.Remove(run.RunID)

			run.broadcast(matter.ProgressEvent{
				RunID:     run.RunID,
				Event:     "run_failed",
				Timestamp: now,
			})
			return
		}
		// Keep Runner alive for Resume. Update store with paused state.
		run.mu.Unlock()

		s.updateRunPaused(run.RunID, result, now)

		run.broadcast(matter.ProgressEvent{
			RunID:     run.RunID,
			Event:     "run_paused",
			Timestamp: now,
		})
		return
	}

	// Release context and runner — no longer needed for terminal states.
	if run.Cancel != nil {
		run.Cancel()
	}

	var terminalEvent string
	var status string
	if result.Error != nil {
		s.tracker.TransitionStatus(run, StatusFailed)
		terminalEvent = "run_failed"
		status = "failed"
	} else {
		s.tracker.TransitionStatus(run, StatusCompleted)
		terminalEvent = "run_completed"
		status = "completed"
	}
	run.Runner = nil
	run.mu.Unlock()

	// Update persistent store and remove from tracker.
	s.updateRunFromResult(run.RunID, &result, status, now)
	s.tracker.Remove(run.RunID)

	// Broadcast terminal event to close SSE subscribers.
	run.broadcast(matter.ProgressEvent{
		RunID:     run.RunID,
		Event:     terminalEvent,
		Step:      result.Steps,
		Timestamp: now,
	})
}

// updateRunFromResult persists the final result of a run to the store.
func (s *Server) updateRunFromResult(runID string, result *matter.RunResult, status string, now time.Time) {
	ctx := context.Background()
	stored, err := s.store.GetRun(ctx, runID)
	if err != nil {
		log.Printf("failed to get run %s for update: %v", runID, err)
		return
	}

	stored.Status = status
	stored.UpdatedAt = now
	stored.CompletedAt = &now
	stored.Steps = result.Steps
	stored.TotalTokens = result.TotalTokens
	stored.TotalCostUSD = result.TotalCostUSD
	stored.DurationMS = now.Sub(stored.CreatedAt).Milliseconds()

	if result.Error != nil {
		stored.ErrorMessage = result.Error.Error()
	} else {
		stored.Success = &result.Success
		stored.Summary = result.FinalSummary
	}

	if err := s.store.UpdateRun(ctx, stored); err != nil {
		log.Printf("failed to update run %s: %v", runID, err)
	}
}

// updateRunPaused persists the paused state of a run to the store.
func (s *Server) updateRunPaused(runID string, result matter.RunResult, now time.Time) {
	ctx := context.Background()
	stored, err := s.store.GetRun(ctx, runID)
	if err != nil {
		log.Printf("failed to get run %s for pause update: %v", runID, err)
		return
	}

	stored.Status = "paused"
	stored.UpdatedAt = now
	stored.Steps = result.Steps
	stored.TotalTokens = result.TotalTokens
	stored.TotalCostUSD = result.TotalCostUSD
	stored.DurationMS = now.Sub(stored.CreatedAt).Milliseconds()
	if result.Question != nil {
		stored.Question = result.Question.Question
	}

	if err := s.store.UpdateRun(ctx, stored); err != nil {
		log.Printf("failed to update paused run %s: %v", runID, err)
	}
}

// handleGetRun returns the current status of a run.
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	stored, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read run")
		return
	}

	resp := buildRunStatusResponseFromStore(stored)
	writeJSON(w, http.StatusOK, resp)
}

// buildRunStatusResponseFromStore constructs the status response from a stored run.
func buildRunStatusResponseFromStore(run *storage.RunRow) runStatusResponse {
	resp := runStatusResponse{
		RunID:  run.RunID,
		Status: run.Status,
		Steps:  run.Steps,
	}

	if run.TotalTokens > 0 {
		resp.TotalTokens = run.TotalTokens
	}
	if run.TotalCostUSD > 0 {
		resp.TotalCostUSD = run.TotalCostUSD
	}
	if run.DurationMS > 0 {
		resp.Duration = time.Duration(run.DurationMS * int64(time.Millisecond)).Truncate(100 * time.Millisecond).String()
	}

	switch run.Status {
	case "completed":
		resp.Success = run.Success
		resp.FinalSummary = run.Summary
	case "failed":
		resp.Error = run.ErrorMessage
	case "paused":
		resp.Question = run.Question
	}

	return resp
}

// handleRunEvents streams SSE events for a run.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	// Check the run exists in the store.
	stored, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read run")
		return
	}

	writeSSEHeaders(w)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Replay historical events from the store.
	events, err := s.store.GetEvents(r.Context(), runID, 0)
	if err != nil {
		log.Printf("failed to get events for run %s: %v", runID, err)
	}
	for _, ev := range events {
		pe := eventRowToProgressEvent(ev, runID)
		if err := writeSSEEvent(w, pe); err != nil {
			return
		}
	}

	// If the run is already terminal, no need to subscribe for live events.
	terminated := stored.Status == "completed" || stored.Status == "failed" || stored.Status == "cancelled"
	if terminated {
		return
	}

	// Subscribe for live events from the tracker.
	active := s.tracker.Get(runID)
	if active == nil {
		// Run completed between the store read and now.
		return
	}

	ch := active.Subscribe()
	defer active.Unsubscribe(ch)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// eventRowToProgressEvent converts a stored event to a ProgressEvent.
func eventRowToProgressEvent(ev storage.EventRow, runID string) matter.ProgressEvent {
	pe := matter.ProgressEvent{
		RunID:     runID,
		Event:     ev.Type,
		Timestamp: ev.Timestamp,
	}
	if ev.Data != "" {
		var data map[string]any
		if err := json.Unmarshal([]byte(ev.Data), &data); err == nil {
			pe.Data = data
		}
	}
	return pe
}

// handleCancelRun cancels a running or paused run.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	// Check the run exists in the store.
	stored, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read run")
		return
	}

	switch stored.Status {
	case "running", "paused":
		// Cancel via tracker if active.
		active := s.tracker.Get(runID)
		if active != nil {
			active.mu.Lock()
			if active.Cancel != nil {
				active.Cancel()
			}
			s.tracker.TransitionStatus(active, StatusCancelled)
			active.Runner = nil
			active.mu.Unlock()
			s.tracker.Remove(runID)
		}

		// Update store.
		now := time.Now()
		stored.Status = "cancelled"
		stored.UpdatedAt = now
		stored.CompletedAt = &now
		if err := s.store.UpdateRun(r.Context(), stored); err != nil {
			log.Printf("failed to update cancelled run %s: %v", runID, err)
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"run_id": runID,
			"status": "cancelled",
		})
	case "completed", "failed", "cancelled":
		writeError(w, http.StatusConflict, fmt.Sprintf("run already %s", stored.Status))
	default:
		writeError(w, http.StatusConflict, "cannot cancel run in current state")
	}
}

// handleAnswer resumes a paused run with the user's answer.
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	// Look up in tracker (paused runs must be active).
	active := s.tracker.Get(runID)
	if active == nil {
		writeError(w, http.StatusNotFound, "run not found or not active")
		return
	}

	var req answerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Answer == "" {
		writeError(w, http.StatusBadRequest, "answer is required")
		return
	}

	active.mu.Lock()
	if active.Status != StatusPaused {
		active.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf("run is %s, not paused", active.Status))
		return
	}
	if active.Runner == nil {
		active.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "runner not available for resume")
		return
	}

	// Atomically check concurrent run limit and transition to running.
	if !s.tracker.TryResume(active) {
		active.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("concurrent run limit reached (%d)", s.tracker.maxConcurrent))
		return
	}

	rn := active.Runner

	// Create a new cancel context for the resumed run.
	ctx, cancel := context.WithCancel(context.Background())
	active.Cancel = cancel
	active.mu.Unlock()

	// Update store status.
	storeCtx := context.Background()
	stored, err := s.store.GetRun(storeCtx, runID)
	if err == nil {
		stored.Status = "running"
		stored.UpdatedAt = time.Now()
		stored.Question = ""
		_ = s.store.UpdateRun(storeCtx, stored)
	}

	// Resume in a background goroutine.
	go func() {
		result := rn.Resume(ctx, req.Answer)
		now := time.Now()

		active.mu.Lock()

		if result.Paused {
			s.tracker.TransitionStatus(active, StatusPaused)
			active.mu.Unlock()

			s.updateRunPaused(runID, result, now)

			active.broadcast(matter.ProgressEvent{
				RunID:     runID,
				Event:     "run_paused",
				Timestamp: now,
			})
			return
		}

		// Release context and runner for terminal states.
		cancel()

		var terminalEvent string
		var status string
		if result.Error != nil {
			s.tracker.TransitionStatus(active, StatusFailed)
			terminalEvent = "run_failed"
			status = "failed"
		} else {
			s.tracker.TransitionStatus(active, StatusCompleted)
			terminalEvent = "run_completed"
			status = "completed"
		}
		active.Runner = nil
		active.mu.Unlock()

		s.updateRunFromResult(runID, &result, status, now)
		s.tracker.Remove(runID)

		active.broadcast(matter.ProgressEvent{
			RunID:     runID,
			Event:     terminalEvent,
			Step:      result.Steps,
			Timestamp: now,
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"run_id": runID,
		"status": string(StatusRunning),
	})
}

// handleListTools returns the list of registered tools, cached after
// the first request to avoid creating a runner on every call.
func (s *Server) handleListTools(w http.ResponseWriter, _ *http.Request) {
	s.toolsOnce.Do(func() {
		rn, err := runner.New(s.cfg, s.llmClient)
		if err != nil {
			return
		}
		tools := rn.Tools()
		s.tools = make([]toolResponse, len(tools))
		for i, t := range tools {
			s.tools[i] = toolResponse{
				Name:        t.Name,
				Description: t.Description,
				Safe:        t.Safe,
				SideEffect:  t.SideEffect,
			}
		}
	})

	writeJSON(w, http.StatusOK, map[string]any{"tools": s.tools})
}

// generateRunID returns a cryptographically random run identifier.
func generateRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "run-" + hex.EncodeToString(b)
}
