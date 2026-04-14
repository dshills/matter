// Package runner wires together all internal modules into a ready-to-use agent
// runner. It is the integration layer between the public types in pkg/matter
// and the internal implementations.
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dshills/matter/internal/agent"
	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/observe"
	"github.com/dshills/matter/internal/policy"
	"github.com/dshills/matter/internal/tools"
	"github.com/dshills/matter/internal/tools/builtin"
	mcppkg "github.com/dshills/matter/internal/tools/mcp"
	"github.com/dshills/matter/pkg/matter"
)

// ErrNotPaused is returned by Resume when no run is paused.
var ErrNotPaused = fmt.Errorf("no paused run to resume")

// ErrRunWhilePaused is returned by Run when a run is already paused.
var ErrRunWhilePaused = fmt.Errorf("cannot start a new run while a run is paused")

// Runner is the top-level entry point for executing matter agent runs.
// Create one via New, then call Run for each task.
//
// Runner is not safe for concurrent use. The CLI executes runs sequentially,
// and the spec (§4.3) defines a single-run-at-a-time model. If a future
// HTTP server needs concurrency, synchronization should be added at that layer.
type Runner struct {
	llmClient  llm.Client
	observer   *observe.Observer
	cfg        config.Config
	progressFn matter.ProgressFunc

	// Pause/resume state (only one paused run at a time).
	pausedAgent *agent.Agent
	pausedReq   matter.RunRequest
	pausedAt    time.Time
}

// New creates a Runner with validated config and initialized shared subsystems.
// Per-run components (registry, policy, agent) are created fresh in each Run call
// to ensure correct workspace paths and isolated budget tracking.
//
// llmClient is the LLM backend and must not be nil. Use llm.NewMockClient
// explicitly in tests.
func New(cfg config.Config, llmClient llm.Client) (*Runner, error) {
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if llmClient == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	// Wrap with retry logic.
	retryClient := llm.NewRetryClient(llmClient, cfg.LLM.MaxRetries)

	// Create observer (shared across runs — logger and metrics are thread-safe).
	obs := observe.NewObserver(cfg.Observe, os.Stderr)

	return &Runner{
		llmClient: retryClient,
		observer:  obs,
		cfg:       cfg,
	}, nil
}

// Run executes a task and returns the result.
// Each call creates a fresh tool registry (bound to the request's workspace),
// policy checker, and agent to ensure isolation between runs.
// Returns an error if a run is currently paused (call Resume first).
func (r *Runner) Run(ctx context.Context, req matter.RunRequest) matter.RunResult {
	if r.pausedAgent != nil {
		return matter.RunResult{Error: ErrRunWhilePaused}
	}

	workspace := req.Workspace
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return matter.RunResult{Error: fmt.Errorf("resolving workspace path: %w", err)}
	}
	req.Workspace = abs

	// Build tool registry with the resolved workspace path.
	registry := tools.NewRegistry()
	if err := registerBuiltinTools(registry, r.cfg, abs); err != nil {
		return matter.RunResult{Error: fmt.Errorf("registering tools: %w", err)}
	}

	// Register MCP tools from configured servers. Failures are non-fatal —
	// the run proceeds with whatever tools were successfully registered.
	// MCP clients are created per-run (not shared across runs) to ensure
	// isolation: MCP servers may hold stateful tool context (caches, cursors,
	// auth sessions) that should not leak between independent tasks.
	mcpClients := registerMCPTools(ctx, registry, r.cfg.Tools.MCPServers, r.observer)
	defer closeMCPClients(mcpClients)

	// Create fresh policy state for this run.
	policyState := &policy.RunState{
		MaxSteps:       r.cfg.Agent.MaxSteps,
		MaxTotalTokens: r.cfg.Agent.MaxTotalTokens,
		MaxCostUSD:     r.cfg.Agent.MaxCostUSD,
		WorkspaceRoot:  abs,
	}
	checker := policy.NewChecker(policyState)

	// Create agent with per-run components.
	ag, err := agent.New(r.cfg, r.llmClient, registry, checker)
	if err != nil {
		return matter.RunResult{Error: err}
	}
	ag.SetObserver(r.observer)
	if r.progressFn != nil {
		ag.SetProgressFunc(r.progressFn)
	}

	result := ag.Run(ctx, req)

	// If the run paused, save agent state for Resume. The paused state is
	// short-lived in CLI usage (user responds or hits EOF). For long-lived
	// servers, callers should implement their own timeout/cleanup logic.
	if result.Paused {
		r.pausedAgent = ag
		r.pausedReq = req
		r.pausedAt = time.Now()
	}

	return result
}

// Resume continues a paused run with the user's answer.
// Returns ErrNotPaused if no run is paused.
func (r *Runner) Resume(ctx context.Context, answer string) matter.RunResult {
	if r.pausedAgent == nil {
		return matter.RunResult{Error: ErrNotPaused}
	}

	ag := r.pausedAgent
	req := r.pausedReq
	pausedDuration := time.Since(r.pausedAt)

	// Clear paused state before resuming (agent may pause again).
	r.pausedAgent = nil
	r.pausedReq = matter.RunRequest{}
	r.pausedAt = time.Time{}

	result := ag.ResumeWithAnswer(ctx, req, answer, pausedDuration)

	// If the run paused again, save state.
	if result.Paused {
		r.pausedAgent = ag
		r.pausedReq = req
		r.pausedAt = time.Now()
	}

	return result
}

// IsPaused returns true if a run is currently paused awaiting user input.
func (r *Runner) IsPaused() bool {
	return r.pausedAgent != nil
}

// Abort clears any paused run state, allowing a new Run call.
// This is useful for callers that need to abandon a paused conversation
// (e.g., on timeout or user disconnect). The observer session associated
// with the aborted agent is released when the agent is garbage-collected;
// no EndRun event is emitted for aborted runs.
func (r *Runner) Abort() {
	r.pausedAgent = nil
	r.pausedReq = matter.RunRequest{}
	r.pausedAt = time.Time{}
}

// SetProgressFunc registers a callback for real-time progress events.
// Must be called before Run. Not safe for concurrent use — this is a
// configuration method, not a runtime method. Callers must not call
// SetProgressFunc concurrently with Run (same pattern as http.Server).
func (r *Runner) SetProgressFunc(fn matter.ProgressFunc) {
	r.progressFn = fn
}

// Tools returns the list of tools that would be registered with the current config.
// Uses "." as the workspace root for display purposes. Does not include MCP tools
// because MCP servers are not started for this introspection-only method.
func (r *Runner) Tools() []matter.Tool {
	registry := tools.NewRegistry()
	_ = registerBuiltinTools(registry, r.cfg, ".")
	return registry.List()
}

// Config returns the effective configuration.
func (r *Runner) Config() config.Config {
	return r.cfg
}

// registerBuiltinTools adds enabled built-in tools to the registry using
// the given workspace root for filesystem-bound tools.
func registerBuiltinTools(reg *tools.Registry, cfg config.Config, workspace string) error {
	if cfg.Tools.EnableWorkspaceRead {
		t := builtin.NewWorkspaceRead(workspace, cfg.Sandbox.MaxOutputBytes, cfg.Tools.AllowedHiddenPaths...)
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	if cfg.Tools.EnableWorkspaceWrite {
		t := builtin.NewWorkspaceWrite(workspace, cfg.Tools.AllowedHiddenPaths)
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	if cfg.Tools.EnableWebFetch {
		t := builtin.NewWebFetch(cfg.Tools.WebFetchAllowedDomains, cfg.Sandbox.MaxWebResponseBytes)
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	if cfg.Tools.EnableCommandExec {
		t := builtin.NewCommandExec(workspace, cfg.Sandbox.CommandTimeout, cfg.Sandbox.MaxOutputBytes, cfg.Tools.CommandAllowlist)
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	return nil
}

// registerMCPTools connects to configured MCP servers, discovers their tools,
// and registers them in the registry. Returns the list of active MCP clients
// for cleanup. Individual server failures are logged but non-fatal — the run
// proceeds with whatever tools were successfully registered.
//
// Servers are connected sequentially for simplicity. Concurrent connection
// would reduce startup latency with multiple servers but adds complexity
// around error collection and registry thread safety. Sequential is adequate
// for the typical 1-3 server configuration.
func registerMCPTools(ctx context.Context, reg *tools.Registry, servers []config.MCPServerConfig, obs *observe.Observer) []*mcppkg.MCPClient {
	if len(servers) == 0 {
		return nil
	}

	var clients []*mcppkg.MCPClient

	for _, srv := range servers {
		client, err := connectMCPServer(ctx, reg, srv)
		if err != nil {
			obs.Logger.Warn(0, "runner", fmt.Sprintf("MCP server %q (%s) failed to start", srv.Name, srv.Transport), map[string]any{"error": err.Error()})
			continue
		}
		obs.Logger.Info(0, "runner", fmt.Sprintf("MCP server %q registered tools", srv.Name), nil)
		clients = append(clients, client)
	}

	return clients
}

// connectMCPServer creates a transport, discovers tools, and registers them
// for a single MCP server configuration.
func connectMCPServer(ctx context.Context, reg *tools.Registry, srv config.MCPServerConfig) (*mcppkg.MCPClient, error) {
	var transport mcppkg.Transport
	var err error

	switch srv.Transport {
	case "stdio":
		transport, err = mcppkg.NewStdioTransport(srv.Command, srv.Args, srv.Env)
	case "sse":
		transport, err = mcppkg.NewSSETransport(ctx, srv.URL)
	default:
		return nil, fmt.Errorf("unsupported transport %q", srv.Transport)
	}
	if err != nil {
		return nil, fmt.Errorf("creating transport: %w", err)
	}

	registerFn := func(tool matter.Tool) error {
		return reg.Register(tool)
	}

	client, err := mcppkg.DiscoverAndRegister(ctx, srv.Name, transport, srv.Timeout, registerFn)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// closeMCPClients shuts down all active MCP clients. Called via defer
// after a run completes. Close errors are intentionally ignored — the
// run result is already determined and the subprocess/connection will
// be cleaned up by the OS on process exit if Close fails.
func closeMCPClients(clients []*mcppkg.MCPClient) {
	for _, c := range clients {
		_ = c.Close()
	}
}
