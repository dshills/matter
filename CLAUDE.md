# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**matter** is a Go-based autonomous AI agent framework for building reliable, observable, tool-using agents that execute multi-step tasks with safety controls, deterministic testing, and production-grade observability. It compiles to a single binary.

## Build & Development Commands

```bash
# Build
go build ./...

# Run tests
go test ./...

# Run a single test
go test ./internal/agent -run TestLoopDetection

# Lint (required after any Go changes)
golangci-lint run ./...

# Run the CLI
go run ./cmd/matter/main.go run --task "..." --workspace .
```

## Architecture

The system follows a step-based agent loop: accept task -> enter loop -> (plan via LLM -> optionally execute tool -> store result -> check limits) -> terminate.

### Core Modules

- **`cmd/matter/`** — CLI entry point (`run`, `replay`, `tools`, `config` commands)
- **`pkg/matter/`** — Public embedding API (`matter.New(cfg)` / `runner.Run(ctx, req)`)
- **`internal/agent/`** — Agent run coordinator: lifecycle, step loop, limit enforcement, loop detection
- **`internal/planner/`** — LLM decision engine producing typed `Decision` structs (tool call, complete, fail). Includes JSON repair pipeline (direct parse -> local cleanup -> LLM correction -> error)
- **`internal/memory/`** — Context management with recent message window + historical summaries. Handles output truncation and summarization triggers
- **`internal/llm/`** — Provider-abstracted LLM client (`Client` interface with `Complete` method). Retry with backoff, token/cost tracking. Mock client required for deterministic tests
- **`internal/tools/`** — Tool registry + executor with schema validation, timeout, safety classification. Built-in tools in `builtin/` subdirectory
- **`internal/observe/`** — Structured JSON logging, per-step tracing, metrics counters, run recording for replay
- **`internal/policy/`** — Guardrails: budget enforcement, filesystem confinement, sandbox rules, approval gates
- **`internal/config/`** — Config loading with precedence: CLI flags > env vars > config file > defaults
- **`internal/workspace/`** — Path safety (no traversal, no symlink escape, workspace-root confinement)

### Key Design Decisions

- Planner outputs are strongly typed JSON (`Decision` with `DecisionType` enum), not free-form text
- All tool calls go through registry validation before execution
- Memory auto-summarizes old context when message/token thresholds are crossed
- Eight hard limits enforced per run: steps, duration, prompt/completion/total tokens, cost, consecutive errors, repeated tool calls
- Run recorder captures full execution trace for replay-based debugging
- Mock LLM client is a first-class component, not an afterthought

## Specs and Plans

Specs and plans live in `./specs/`. The primary spec is at `specs/initial/SPEC.md`.

## Development Workflow

1. `/spec-review` — Validate SPEC.md with speccritic
2. `/plan` — Generate PLAN.md from spec, validate with plancritic
3. `/implement` — Implement one phase at a time
4. Post-phase: `prism review` -> `realitycheck check` -> `clarion pack` -> `verifier analyze`
5. `/commit` when a phase passes validation

## Implementation Constraints

- Go standard library preferred; external deps only when they materially improve safety/correctness
- Clean package boundaries — `internal/` for private, `pkg/` for public API
- Interfaces should be small and composable
- No heavyweight plugin system or speculative abstractions in v1
- Config via YAML file, env vars, or CLI flags

## Code Search Protocol

Use this decision tree — in order — before reading any source file:

### Structural questions → atlas (always first)
- "Where is X defined?" → `atlas find symbol X --agent`
- "What calls X?" → `atlas who-calls X --agent`
- "What does X call?" → `atlas calls X --agent`
- "What implements interface X?" → `atlas implementations X --agent`
- "Which tests cover X?" → `atlas tests-for X --agent`
- "What routes exist?" → `atlas list routes --agent`
- "What changed?" → `atlas index --since HEAD~1 && atlas stale --agent`

### Before reading a large file → summarize first
`atlas summarize file <path> --agent`
Only read the file directly if the summary is insufficient.

### Content/pattern questions → rg
- Error strings, log messages, string literals
- Comments, TODOs, inline notes
- Non-Go/TS files (YAML, SQL, Markdown)
- Unstaged files not yet indexed

### Never read source files to answer these questions
If atlas has the answer, do not use Read or Bash(cat).
Atlas is authoritative — its index is maintained by a PostToolUse hook on Write/Edit/MultiEdit.
