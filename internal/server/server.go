package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/storage"
)

// Version is the server version reported in /health.
const Version = "0.2.0"

// Server is the HTTP API server for matter.
type Server struct {
	cfg       config.Config
	llmClient llm.Client
	store     storage.Store
	tracker   *ActiveRunTracker
	mux       *http.ServeMux
	server    *http.Server
	gcDone    chan struct{}

	toolsOnce sync.Once
	tools     []toolResponse // cached on first request
}

// New creates a new HTTP API server.
func New(cfg config.Config, llmClient llm.Client, store storage.Store) *Server {
	tracker := NewActiveRunTracker(
		cfg.Server.MaxConcurrentRuns,
		cfg.Server.MaxPausedRuns,
	)

	s := &Server{
		cfg:       cfg,
		llmClient: llmClient,
		store:     store,
		tracker:   tracker,
		mux:       http.NewServeMux(),
		gcDone:    make(chan struct{}),
	}

	s.registerRoutes()
	return s
}

// registerRoutes sets up the API endpoints.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/runs", s.requireAuth(s.handleCreateRun))
	s.mux.HandleFunc("GET /api/v1/runs/{id}", s.requireAuth(s.handleGetRun))
	s.mux.HandleFunc("GET /api/v1/runs/{id}/events", s.requireAuth(s.handleRunEvents))
	s.mux.HandleFunc("DELETE /api/v1/runs/{id}", s.requireAuth(s.handleCancelRun))
	s.mux.HandleFunc("POST /api/v1/runs/{id}/answer", s.requireAuth(s.handleAnswer))
	s.mux.HandleFunc("GET /api/v1/tools", s.requireAuth(s.handleListTools))
}

// reconcileOnStartup transitions any previously-running runs to failed
// and initializes the paused counter from the store.
func (s *Server) reconcileOnStartup() {
	ctx := context.Background()

	// Fail any runs that were running when the server last stopped.
	runningRuns, err := s.store.ListRuns(ctx, storage.RunFilter{Status: "running", Limit: 200})
	if err != nil {
		log.Printf("startup reconciliation: failed to list running runs: %v", err)
	}
	for _, run := range runningRuns {
		run.Status = "failed"
		run.ErrorMessage = "server restarted"
		now := time.Now()
		run.CompletedAt = &now
		run.UpdatedAt = now
		if err := s.store.UpdateRun(ctx, &run); err != nil {
			log.Printf("startup reconciliation: failed to update run %s: %v", run.RunID, err)
		}
	}
	if len(runningRuns) > 0 {
		log.Printf("startup reconciliation: marked %d stale running runs as failed", len(runningRuns))
	}

	// Initialize the paused counter from persisted data.
	pausedRuns, err := s.store.ListRuns(ctx, storage.RunFilter{Status: "paused", Limit: 200})
	if err != nil {
		log.Printf("startup reconciliation: failed to count paused runs: %v", err)
	}
	if len(pausedRuns) > 0 {
		s.tracker.SetPausedCount(len(pausedRuns))
		log.Printf("startup reconciliation: found %d paused runs", len(pausedRuns))
	}
}

// ListenAndServe starts the server and blocks until it's shut down.
func (s *Server) ListenAndServe() error {
	s.reconcileOnStartup()

	s.server = &http.Server{
		Addr:              s.cfg.Server.ListenAddr,
		Handler:           s.requestLogging(s.mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start GC ticker.
	go s.runGC()

	log.Printf("matter-server listening on %s", s.cfg.Server.ListenAddr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the server. It stops accepting new connections,
// waits up to 30 seconds for active runs, then force-cancels remaining runs.
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop GC ticker.
	close(s.gcDone)

	// Cancel all active runs.
	s.tracker.CancelAll()

	// Stop accepting new HTTP requests and drain existing connections.
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// runGC periodically garbage-collects expired runs from the store.
func (s *Server) runGC() {
	interval := s.cfg.Storage.GCInterval
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.gcDone:
			return
		case now := <-ticker.C:
			completedBefore := now.Add(-s.cfg.Storage.Retention)
			pausedBefore := now.Add(-s.cfg.Storage.PausedRetention)
			removed, err := s.store.DeleteExpiredRuns(context.Background(), completedBefore, pausedBefore)
			if err != nil {
				log.Printf("GC: error deleting expired runs: %v", err)
			}
			if removed > 0 {
				log.Printf("GC: removed %d expired runs", removed)
			}
		}
	}
}

// requireAuth wraps a handler with bearer token authentication.
// If no auth token is configured, the handler is called directly.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Server.AuthToken == "" {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.cfg.Server.AuthToken

		// Use constant-time comparison to prevent timing side-channel attacks.
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		next(w, r)
	}
}

// requestLogging wraps a handler with request logging middleware.
// The Authorization header value is redacted in logs.
func (s *Server) requestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Redact Authorization header for logging.
		auth := r.Header.Get("Authorization")
		redacted := ""
		if auth != "" {
			if strings.HasPrefix(auth, "Bearer ") {
				redacted = "Bearer ***"
			} else {
				redacted = "***"
			}
		}

		next.ServeHTTP(w, r)

		elapsed := time.Since(start)
		if redacted != "" {
			log.Printf("%s %s auth=%s %s", r.Method, r.URL.Path, redacted, elapsed)
		} else {
			log.Printf("%s %s %s", r.Method, r.URL.Path, elapsed)
		}
	})
}

// Handler returns the HTTP handler for testing with httptest.
func (s *Server) Handler() http.Handler {
	return s.requestLogging(s.mux)
}

// Tracker returns the active run tracker for testing.
func (s *Server) Tracker() *ActiveRunTracker {
	return s.tracker
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
