package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dshills/matter/internal/runner"
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

	ctx, cancel := context.WithCancel(context.Background())

	// Create a fresh runner for this run (isolated per-run state).
	rn, err := runner.New(s.cfg, s.llmClient)
	if err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, "failed to create runner")
		return
	}

	activeRun := &ActiveRun{
		RunID:   runID,
		Status:  StatusRunning,
		Cancel:  cancel,
		Created: time.Now(),
		Runner:  rn,
	}

	// Set up progress callback to broadcast events to SSE subscribers
	// and store events for late-joining subscribers.
	rn.SetProgressFunc(func(event matter.ProgressEvent) {
		event.RunID = runID
		activeRun.mu.Lock()
		activeRun.Events = append(activeRun.Events, event)
		activeRun.mu.Unlock()
		activeRun.broadcast(event)
	})

	if err := s.store.Add(activeRun); err != nil {
		cancel()
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

// executeRun runs the agent task and updates the ActiveRun state on completion.
// Terminal progress events are normally emitted by the runner via the progress
// callback (which calls broadcast). As a safety net, we also broadcast a
// terminal event here to ensure SSE subscribers are closed even if the runner
// does not emit one.
func (s *Server) executeRun(ctx context.Context, rn *runner.Runner, run *ActiveRun, req createRunRequest) {
	result := rn.Run(ctx, matter.RunRequest{
		Task:      req.Task,
		Workspace: req.Workspace,
	})

	run.mu.Lock()
	run.Result = &result

	if result.Paused {
		// Atomically check paused limit and transition.
		if !s.store.TryPause(run) {
			if run.Cancel != nil {
				run.Cancel()
			}
			s.store.TransitionStatus(run, StatusCancelled)
			run.Result = &matter.RunResult{
				Error: fmt.Errorf("paused run limit reached (%d)", s.store.maxPaused),
			}
			run.Runner = nil
			run.mu.Unlock()
			run.broadcast(matter.ProgressEvent{
				RunID:     run.RunID,
				Event:     "run_failed",
				Timestamp: time.Now(),
			})
			return
		}
		// Keep Runner and context alive for Resume.
		run.mu.Unlock()
		run.broadcast(matter.ProgressEvent{
			RunID:     run.RunID,
			Event:     "run_paused",
			Timestamp: time.Now(),
		})
		return
	}

	// Release context and runner — no longer needed for terminal states.
	if run.Cancel != nil {
		run.Cancel()
	}

	var terminalEvent string
	if result.Error != nil {
		s.store.TransitionStatus(run, StatusFailed)
		terminalEvent = "run_failed"
	} else {
		s.store.TransitionStatus(run, StatusCompleted)
		terminalEvent = "run_completed"
	}
	run.Runner = nil
	run.mu.Unlock()

	// Broadcast terminal event to close SSE subscribers.
	run.broadcast(matter.ProgressEvent{
		RunID:     run.RunID,
		Event:     terminalEvent,
		Step:      result.Steps,
		Timestamp: time.Now(),
	})
}

// handleGetRun returns the current status of a run.
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run := s.store.Get(runID)
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	run.mu.Lock()
	resp := buildRunStatusResponse(run)
	run.mu.Unlock()

	writeJSON(w, http.StatusOK, resp)
}

// buildRunStatusResponse constructs the status response from an ActiveRun.
// Caller must hold run.mu.
func buildRunStatusResponse(run *ActiveRun) runStatusResponse {
	resp := runStatusResponse{
		RunID:  run.RunID,
		Status: string(run.Status),
	}

	if run.Result != nil {
		resp.Steps = run.Result.Steps
		resp.TotalTokens = run.Result.TotalTokens
		resp.TotalCostUSD = run.Result.TotalCostUSD
		resp.Duration = time.Since(run.Created).Truncate(100 * time.Millisecond).String()

		if run.Status == StatusCompleted {
			success := run.Result.Success
			resp.Success = &success
			resp.FinalSummary = run.Result.FinalSummary
		}
		if run.Status == StatusFailed && run.Result.Error != nil {
			resp.Error = run.Result.Error.Error()
		}
		if run.Status == StatusPaused && run.Result.Question != nil {
			resp.Question = run.Result.Question.Question
		}
	}

	return resp
}

// handleRunEvents streams SSE events for a run.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run := s.store.Get(runID)
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	writeSSEHeaders(w)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Subscribe while holding the lock so no events are missed between
	// copying existing events and subscribing for new ones.
	run.mu.Lock()
	existing := make([]matter.ProgressEvent, len(run.Events))
	copy(existing, run.Events)
	terminated := run.Status == StatusCompleted || run.Status == StatusFailed || run.Status == StatusCancelled
	ch := run.Subscribe()
	run.mu.Unlock()
	defer run.Unsubscribe(ch)

	for _, event := range existing {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
	}

	// If the run is already done, close the stream.
	if terminated {
		return
	}

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				// Channel closed — run completed.
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

// handleCancelRun cancels a running or paused run.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run := s.store.Get(runID)
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	run.mu.Lock()
	defer run.mu.Unlock()

	switch run.Status {
	case StatusRunning, StatusPaused:
		if run.Cancel != nil {
			run.Cancel()
		}
		s.store.TransitionStatus(run, StatusCancelled)
		run.Runner = nil
		writeJSON(w, http.StatusOK, map[string]string{
			"run_id": runID,
			"status": string(StatusCancelled),
		})
	case StatusCompleted, StatusFailed, StatusCancelled:
		writeError(w, http.StatusConflict, fmt.Sprintf("run already %s", run.Status))
	default:
		writeError(w, http.StatusConflict, "cannot cancel run in current state")
	}
}

// handleAnswer resumes a paused run with the user's answer.
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run := s.store.Get(runID)
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
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

	run.mu.Lock()
	if run.Status != StatusPaused {
		run.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf("run is %s, not paused", run.Status))
		return
	}
	if run.Runner == nil {
		run.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "runner not available for resume")
		return
	}

	// Atomically check concurrent run limit and transition to running.
	if !s.store.TryResume(run) {
		run.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("concurrent run limit reached (%d)", s.store.maxConcurrent))
		return
	}

	rn := run.Runner

	// Create a new cancel context for the resumed run.
	ctx, cancel := context.WithCancel(context.Background())
	run.Cancel = cancel
	run.mu.Unlock()

	// Resume in a background goroutine.
	go func() {
		result := rn.Resume(ctx, req.Answer)

		run.mu.Lock()
		run.Result = &result

		if result.Paused {
			s.store.TransitionStatus(run, StatusPaused)
			run.mu.Unlock()
			run.broadcast(matter.ProgressEvent{
				RunID:     run.RunID,
				Event:     "run_paused",
				Timestamp: time.Now(),
			})
			return
		}

		// Release context and runner for terminal states.
		cancel()

		var terminalEvent string
		if result.Error != nil {
			s.store.TransitionStatus(run, StatusFailed)
			terminalEvent = "run_failed"
		} else {
			s.store.TransitionStatus(run, StatusCompleted)
			terminalEvent = "run_completed"
		}
		run.Runner = nil
		run.mu.Unlock()

		// Broadcast terminal event to close SSE subscribers.
		run.broadcast(matter.ProgressEvent{
			RunID:     run.RunID,
			Event:     terminalEvent,
			Step:      result.Steps,
			Timestamp: time.Now(),
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
