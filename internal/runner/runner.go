// Package runner wires together all internal modules into a ready-to-use agent
// runner. It is the integration layer between the public types in pkg/matter
// and the internal implementations.
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dshills/matter/internal/agent"
	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/observe"
	"github.com/dshills/matter/internal/policy"
	"github.com/dshills/matter/internal/tools"
	"github.com/dshills/matter/internal/tools/builtin"
	"github.com/dshills/matter/pkg/matter"
)

// Runner is the top-level entry point for executing matter agent runs.
// Create one via New, then call Run for each task.
type Runner struct {
	llmClient llm.Client
	observer  *observe.Observer
	cfg       config.Config
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
func (r *Runner) Run(ctx context.Context, req matter.RunRequest) matter.RunResult {
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

	// Create fresh policy state for this run.
	policyState := &policy.RunState{
		MaxSteps:       r.cfg.Agent.MaxSteps,
		MaxTotalTokens: r.cfg.Agent.MaxTotalTokens,
		MaxCostUSD:     r.cfg.Agent.MaxCostUSD,
		WorkspaceRoot:  abs,
	}
	checker := policy.NewChecker(policyState)

	// Create agent with per-run components.
	ag := agent.New(r.cfg, r.llmClient, registry, checker)
	ag.SetObserver(r.observer)

	return ag.Run(ctx, req)
}

// Tools returns the list of tools that would be registered with the current config.
// Uses "." as the workspace root for display purposes.
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
		t := builtin.NewCommandExec(workspace, cfg.Sandbox.CommandTimeout, cfg.Sandbox.MaxOutputBytes)
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	return nil
}
