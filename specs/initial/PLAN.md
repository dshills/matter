# PLAN.md — matter

## Overview

This plan implements the matter autonomous AI agent framework in 10 phases, ordered by dependency. Each phase is independently testable and produces a working subset of the system. The plan follows the spec's guidance (Section 21): get the core loop working with a mock LLM before adding provider complexity or sandbox sophistication.

---

## Phase 1: Foundation — Project Structure, Configuration, and Shared Types

### Goals
- Initialize the Go module and project directory structure.
- Define all shared types used across modules (Message, Decision, ToolCall, etc.).
- Implement the configuration system with YAML file loading, environment variable overlay, CLI flag precedence, and defaults.
- Define all typed error categories.
- Validate configuration invariants at startup.

### Files to Create
- `go.mod`, `go.sum`
- `Makefile` (build, test, lint targets)
- `internal/config/config.go` — Config struct, loading logic, precedence rules
- `internal/config/defaults.go` — Default values for all config fields
- `internal/config/config_test.go`
- `pkg/matter/types.go` — Canonical `Message`, `MessageRole`, `Decision`, `DecisionType`, `ToolCall`, `FinalAnswer`, `ToolResult`, `ToolExecuteFunc` types
- `internal/agent/errors.go` — All typed error categories (planner_error, llm_error, tool_validation_error, etc.) with classification (retriable/recoverable/terminal)

### Key Decisions
- Use `gopkg.in/yaml.v3` for YAML parsing (spec permits external deps for YAML).
- Config struct mirrors spec Section 16.2 exactly.
- All shared types live in `pkg/matter/types.go` so both `internal/` and `pkg/` packages can import them.
- Error types use Go's `errors.Is`/`errors.As` pattern with sentinel errors and typed wrappers.

### Acceptance Criteria
- `go build ./...` succeeds.
- Config loads from YAML file with correct defaults.
- Environment variables override file values.
- `configuration_error` returned when `summarize_after_messages <= recent_messages`.
- `configuration_error` returned when configured model is not in pricing table.
- All error types are defined and classifiable as retriable/recoverable/terminal.
- Unit tests pass for config loading, defaults, validation, and error classification.

### Risks
- None significant. Pure foundational work.

---

## Phase 2: Tool System and Workspace Safety

### Goals
- Implement the tool registry with registration, lookup, schema export, duplicate rejection, and stable ordering.
- Implement tool input validation against JSON Schema.
- Implement the tool executor with timeout enforcement and structured result capture.
- Implement workspace path safety (traversal rejection, symlink escape, hidden path protection).

### Files to Create
- `internal/tools/types.go` — `Tool` struct (re-exports from pkg/matter where needed)
- `internal/tools/registry.go` — `ToolRegistry` interface implementation
- `internal/tools/registry_test.go`
- `internal/tools/executor.go` — Tool execution with timeout, step/call ID tracking, timestamps
- `internal/tools/executor_test.go`
- `internal/tools/validation.go` — JSON Schema input validation
- `internal/tools/validation_test.go`
- `internal/workspace/paths.go` — Path resolution, traversal detection, symlink checks
- `internal/workspace/guard.go` — Hidden path protection, `allowed_hidden_paths` check
- `internal/workspace/paths_test.go`
- `internal/workspace/guard_test.go`

### Key Decisions
- Use `github.com/santhosh-tekuri/jsonschema/v5` for JSON Schema validation (spec permits external deps for schema validation).
- `ToolRegistry` stores tools in a `map[string]Tool` for lookup and `[]Tool` slice for stable iteration order.
- Tool executor wraps execution with `context.WithTimeout` using the tool's configured timeout.
- Workspace path safety is a standalone package so policy, tools, and agent can all use it.

### Acceptance Criteria
- Tools can be registered and looked up by name.
- Duplicate tool names are rejected with an error.
- `List()` returns tools in registration order.
- `Schemas()` returns valid JSON schema array for planner prompts.
- Tool inputs are validated against JSON Schema before execution.
- Tool execution respects timeout (cancels via context).
- Tool execution captures start/end timestamps and call ID.
- Path traversal (`../`, absolute paths outside workspace) is rejected.
- Symlink escape is detected and rejected.
- Writes to `.env`, `.git/`, `.ssh/` are rejected unless in `allowed_hidden_paths`.
- All safety behaviors covered by unit tests.

### Risks
- JSON Schema library choice affects validation fidelity. `santhosh-tekuri/jsonschema` is well-maintained and supports draft-07+.

---

## Phase 3: LLM Client Abstraction and Mock

### Goals
- Define the LLM `Client` interface and request/response types.
- Implement the mock LLM client for deterministic testing.
- Implement retry logic with exponential backoff.
- Implement the built-in pricing table and cost estimation.

### Files to Create
- `internal/llm/client.go` — `Client` interface, `Request`, `Response` types (using canonical Message)
- `internal/llm/retry.go` — Retry wrapper with exponential backoff, transient error detection
- `internal/llm/retry_test.go`
- `internal/llm/costing.go` — `ModelPricing` struct, pricing table, cost calculation
- `internal/llm/costing_test.go`
- `internal/llm/mock.go` — Mock client: predefined response sequences, call count assertions, prompt capture
- `internal/llm/mock_test.go`

### Key Decisions
- `Request` uses `[]Message` from `pkg/matter/types.go` (canonical Message type).
- Mock client accepts a `[]Response` slice and returns them in order; panics or returns error if exhausted.
- Mock client records all requests for test assertions.
- Retry logic distinguishes retriable (timeout, 429, 5xx) from terminal (401, 400) errors using error classification from Phase 1.
- Pricing table is a `[]ModelPricing` var initialized in `costing.go`. Cost = `(promptTokens * PromptCostPer1K / 1000) + (completionTokens * CompletionCostPer1K / 1000)`.

### Acceptance Criteria
- Mock client returns predefined responses in sequence.
- Mock client records requests and supports call count assertions.
- Retry logic retries on transient errors with backoff.
- Retry logic does NOT retry on terminal errors (auth, 400).
- Retry logic respects `max_retries` config.
- Cost estimation returns correct USD values for all v1 models.
- Cost estimation returns error for unknown models.
- Unit tests cover all retry scenarios and cost calculations.

### Risks
- None significant. Mock-first approach eliminates external dependencies for testing.

---

## Phase 4: Planner — Parsing, Validation, and Repair

### Goals
- Implement the planner module that takes context + tools and produces typed `Decision` structs.
- Implement JSON parsing of LLM output into `Decision`.
- Implement the three-stage repair pipeline (direct parse → local cleanup → LLM correction).
- Implement schema validation of parsed decisions.

### Files to Create
- `internal/planner/planner.go` — `Planner` interface implementation, prompt construction
- `internal/planner/planner_test.go`
- `internal/planner/schema.go` — Decision schema validation
- `internal/planner/parser.go` — JSON parsing with repair pipeline
- `internal/planner/parser_test.go`
- `internal/planner/repair.go` — Local cleanup (strip fences, fix commas, close braces), LLM correction prompt
- `internal/planner/repair_test.go`

### Key Decisions
- Planner constructs a system prompt containing: user task, memory context, tool schemas, budget/limits remaining, and behavioral instructions (per spec Section 8.6).
- Parser repair pipeline: (1) direct `json.Unmarshal`, (2) strip markdown fences + trim whitespace + fix trailing commas + append closing braces, (3) send correction prompt to LLM (at most once per step, token usage counts toward limits).
- Decision validation ensures: `Type` is one of the three enum values, `ToolCall` is present when `Type == "tool"`, `Final` is present when `Type == "complete"`.
- The planner does NOT make LLM calls directly — it receives an `llm.Client` and delegates.

### Acceptance Criteria
- Valid JSON decision is parsed correctly for all three decision types.
- Markdown-fenced JSON is repaired by local cleanup.
- Trailing commas and unclosed braces are repaired by local cleanup.
- LLM correction is attempted exactly once when local cleanup fails.
- Invalid decision type returns `planner_error`.
- Missing `ToolCall` when `Type == "tool"` returns `planner_error`.
- Planner prompt includes all required sections (task, context, tools, budget, instructions).
- Unit tests cover all repair paths and validation scenarios.

### Risks
- Repair heuristics may not cover all LLM output quirks. The spec limits us to the defined cleanup steps, which is appropriate for v1.

---

## Phase 5: Memory — Window, Summarization, and Store

### Goals
- Implement the memory manager with recent message window and historical summary.
- Implement summarization triggers (message count and token count thresholds).
- Implement context building for planner input (pinned system message + summary + recent window).
- Implement tool output truncation policy.

### Files to Create
- `internal/memory/memory.go` — `MemoryManager` interface implementation
- `internal/memory/memory_test.go`
- `internal/memory/window.go` — Recent message window with configurable size
- `internal/memory/window_test.go`
- `internal/memory/summary.go` — Summarization logic using LLM client, summary prompt
- `internal/memory/summary_test.go`
- `internal/memory/store.go` — Message storage, run metadata tracking

### Key Decisions
- System message is pinned at index 0 and never evicted or summarized.
- Message count trigger: when total messages reach `summarize_after_messages`, messages outside the recent window are summarized and replaced.
- Token count trigger: when estimated tokens exceed `summarize_after_tokens`, oldest messages in the window are evicted (preserving system message + at least 3 most recent), summarized via separate LLM call using `summarization_model`.
- Summarization uses a separate `llm.Client.Complete()` call — token usage and cost count toward run limits but do not increment step counter.
- Tool outputs exceeding `max_tool_result_chars` are truncated for prompt inclusion but stored fully in run records (Phase 7).
- Token estimation uses a simple heuristic: `len(content) / 4` (chars to tokens approximation).

### Acceptance Criteria
- Messages are added and retrieved in order.
- System message is always first in context output.
- Messages beyond `recent_messages` are evicted when `summarize_after_messages` threshold is crossed.
- Evicted messages are summarized (verified via mock LLM).
- Token-based eviction shrinks window while preserving system message + 3 most recent.
- Summarization preserves key information (factual results, goals, failures).
- Tool outputs exceeding threshold are truncated with `[TRUNCATED]` notice.
- Summarization token/cost usage is tracked.
- Unit tests with mock LLM cover all summarization trigger paths.

### Risks
- Token estimation heuristic (chars/4) is imprecise. Acceptable for v1; can be replaced with a tokenizer later.

---

## Phase 6: Policy Enforcement and Agent Core Loop

### Goals
- Implement the `PolicyChecker` interface with workspace confinement, budget checks, and tool restrictions.
- Implement the core agent loop that orchestrates planner → policy → tool execution → memory → limits.
- Implement all hard limit checks (steps, duration, tokens, cost, errors, repeated calls, no-progress).
- Implement loop detection and progress tracking.

### Files to Create
- `internal/policy/policy.go` — `PolicyChecker` implementation
- `internal/policy/policy_test.go`
- `internal/policy/budget.go` — Budget remaining checks
- `internal/policy/filesystem.go` — Workspace path confinement checks (delegates to workspace package)
- `internal/agent/agent.go` — Agent struct, `Run()` method, initialization
- `internal/agent/agent_test.go`
- `internal/agent/loop.go` — Core step loop logic
- `internal/agent/loop_test.go`
- `internal/agent/limits.go` — Limit evaluation (all 9 limits in spec order)
- `internal/agent/limits_test.go`
- `internal/agent/loop_detector.go` — Progress tracking, repeated call detection, no-progress counting
- `internal/agent/loop_detector_test.go`

### Key Decisions
- Agent `Run()` initializes context → loads config → registers tools → seeds memory → enters loop → returns result.
- Each step: build context → call planner → check policy (if unsafe tool) → execute tool → add result to memory → update metrics → check limits.
- Limits evaluated in spec order after each step: max_steps, max_duration, max_prompt_tokens, max_completion_tokens, max_total_tokens, max_cost_usd, max_consecutive_errors, max_repeated_tool_calls, max_consecutive_no_progress.
- Progress detection per spec Section 14.3: truncated results count as progress, errors do not.
- Repeated tool call detection uses sliding window of 2N steps.
- `max_consecutive_errors` counts only non-retriable errors that return to the loop (retried LLM errors don't count).
- PolicyChecker is called before tool execution only when `tool.Safe == false`.

### Acceptance Criteria
- Agent completes a multi-step task using mock LLM and test tools.
- Agent terminates on `DecisionTypeComplete` with final answer.
- Agent terminates on `DecisionTypeFail`.
- Each hard limit triggers `limit_exceeded_error` when exceeded (9 separate test cases).
- Limits are evaluated in spec order (first exceeded wins).
- Repeated tool calls are detected and terminate the run.
- No-progress steps are counted and terminate the run at threshold.
- Policy violation for unsafe tool in disallowed path terminates the run.
- Tool errors are recoverable (returned to memory for replanning).
- `FatalOnError` tool errors terminate the run.
- Integration tests with mock LLM verify full loop behavior.

### Risks
- Most complex phase. The agent loop ties together all prior modules. Thorough testing with mock LLM is critical.
- Limit evaluation order must match spec exactly to avoid precedence bugs.

---

## Phase 7: Observability — Logging, Tracing, Metrics, and Recording

### Goals
- Implement structured JSON logging with run/step context.
- Implement step-level tracing for all agent events.
- Implement in-memory metrics counters.
- Implement run recording to disk for debugging and replay.

### Files to Create
- `internal/observe/observer.go` — `Observer` interface, aggregation of logging/tracing/metrics/recording
- `internal/observe/observer_test.go`
- `internal/observe/logging.go` — Structured JSON logger with contextual fields (run ID, step, component, tool, latency, tokens, cost)
- `internal/observe/tracing.go` — Step-level trace events (planner started/completed, tool started/completed, retry, summary, limit exceeded, run completed)
- `internal/observe/metrics.go` — In-memory counters for all spec-required metrics
- `internal/observe/metrics_test.go`
- `internal/observe/recorder.go` — Run recorder: writes JSON record per run to `record_dir`
- `internal/observe/recorder_test.go`

### Key Decisions
- Observer is a facade that wraps logger, tracer, metrics, and recorder.
- Agent loop calls observer hooks at each event point — observer is injected into the agent, not tightly coupled.
- Run records are JSON files containing: input task, config snapshot, all planner prompts, raw LLM responses, parsed decisions, tool calls/outputs, errors, final outcome.
- Recording is always-on (per spec: "The system must record each run"). `record_dir` defaults to `.matter/runs`.
- Metrics are in-memory counters exposed via the observer. No Prometheus endpoint in v1.
- Log output goes to stderr by default; structured JSON format.

### Acceptance Criteria
- Each agent step produces structured JSON log entries with run ID, step number, component.
- Tool calls are logged with name, latency, result status.
- LLM calls are logged with token usage and cost.
- All spec-required trace events are emitted.
- All spec-required metrics are tracked and retrievable.
- Run records are written to disk as JSON files.
- Run records contain all required fields (task, config, prompts, responses, decisions, tools, errors, outcome).
- Observer integrates cleanly with agent loop from Phase 6.

### Risks
- Recording every LLM prompt/response can produce large files. Acceptable for v1; truncation or compression is a future concern.

---

## Phase 8: Built-in Tools

### Goals
- Implement the four required built-in tools: workspace_read, workspace_write, web_fetch, command_exec.
- Each tool defines its JSON Schema, safety classification, and execution function.
- Tools integrate with workspace safety and policy enforcement.

### Files to Create
- `internal/tools/builtin/workspace_read.go` — Read files from workspace root, path traversal denied
- `internal/tools/builtin/workspace_read_test.go`
- `internal/tools/builtin/workspace_write.go` — Write files within workspace root, overwrite flag required for existing files, hidden path protection
- `internal/tools/builtin/workspace_write_test.go`
- `internal/tools/builtin/web_fetch.go` — HTTP GET with domain allowlist, timeout, response size cap, truncation notice
- `internal/tools/builtin/web_fetch_test.go`
- `internal/tools/builtin/command_exec.go` — Subprocess execution with timeout, output cap, working directory confinement, stdin closed, network config
- `internal/tools/builtin/command_exec_test.go`

### Key Decisions
- `workspace_read`: `Safe=true`, `SideEffect=false`. Input schema: `{path: string}`. Returns file contents.
- `workspace_write`: `Safe=false`, `SideEffect=true`. Input schema: `{path: string, content: string, overwrite: bool}`. Rejects without `overwrite=true` for existing files.
- `web_fetch`: `Safe=false`, `SideEffect=false`. Input schema: `{url: string}`. Checks domain against `web_fetch_allowed_domains`. Truncates at `max_web_response_bytes` with notice.
- `command_exec`: `Safe=false`, `SideEffect=true`, `FatalOnError=false`. Input schema: `{command: string, args: []string}`. Uses `os/exec` with `context.WithTimeout`. Caps output at `max_output_bytes`. Stdin closed immediately. Network configurable.
- All tools use `internal/workspace` for path safety checks.

### Acceptance Criteria
- workspace_read reads files within workspace; rejects path traversal.
- workspace_write creates new files; requires `overwrite` flag for existing; rejects hidden paths.
- web_fetch fetches from allowed domains; rejects disallowed; truncates large responses with notice.
- web_fetch with empty `allowed_domains` rejects all requests.
- command_exec enforces timeout; caps output; closes stdin; sets working directory.
- command_exec truncated output includes `[OUTPUT TRUNCATED]` notice with empty Error field.
- All tools have valid JSON Schema definitions.
- Safety and side-effect flags are correctly set per tool.
- Unit tests cover happy paths and all safety boundaries.

### Risks
- `command_exec` network disabling is platform-dependent. v1 relies on documentation/convention rather than syscall-level enforcement on non-Linux.

---

## Phase 9: CLI and Public Package API

### Goals
- Implement the CLI with `run`, `replay`, `tools`, and `config` commands.
- Implement the public package API (`pkg/matter`) for embedding.
- Wire together all internal modules through the CLI entry point.

### Files to Create
- `cmd/matter/main.go` — CLI entry point, command routing
- `pkg/matter/matter.go` — `New(cfg)`, `Run(ctx, req)` public API
- `pkg/matter/matter_test.go`

### Key Decisions
- Use standard library `flag` package for CLI flags (no cobra/urfave dependency — spec says minimize deps).
- CLI `run` command: `--task`, `--workspace`, `--config` flags. Loads config → creates runner via `pkg/matter.New()` → calls `Run()` → prints result.
- CLI `config` command: loads and prints effective config as YAML.
- CLI `tools` command: lists registered tools with name, description, safety flags.
- CLI `replay` command: placeholder in this phase (implemented in Phase 10).
- `pkg/matter.New(cfg)` initializes agent, tools, LLM client, memory, observer, policy.
- `pkg/matter.Run(ctx, req)` delegates to agent loop.
- Human-readable progress output to stderr; final result to stdout.

### Acceptance Criteria
- `matter run --task "..." --workspace .` executes a task end-to-end with the configured LLM provider.
- `matter config --config path/to/config.yaml` prints effective config.
- `matter tools` lists all registered built-in tools.
- `pkg/matter.New()` returns error for invalid config.
- `pkg/matter.Run()` returns `RunResult` with `FinalSummary`, `Steps`, `TotalCost`, etc.
- CLI displays progress (step number, tool calls, final status) during execution.
- Exit codes: 0 for success, 1 for agent failure, 2 for config error.

### Risks
- Using `flag` package directly means no subcommand support out of the box. Implement with `os.Args[1]` dispatch (simple switch on first arg). Acceptable for 4 commands.

---

## Phase 10: Replay, OpenAI Provider, and Final Integration

### Goals
- Implement the replay system that re-executes the agent loop using recorded LLM responses and tool outputs.
- Implement the OpenAI LLM provider.
- Complete the CLI `replay` command.
- Run full integration tests covering all acceptance criteria.

### Files to Create
- `internal/observe/replay.go` — Replay runner: loads run record, creates mock LLM + mock tools from recorded data, re-runs agent loop, compares outcomes
- `internal/observe/replay_test.go`
- `internal/llm/openai.go` — OpenAI API client implementing `Client` interface
- `internal/llm/openai_test.go`
- `testdata/` — Sample run recordings for replay tests
- Integration test files across packages

### Key Decisions
- Replay constructs a mock LLM client that returns recorded responses in order, and mock tool executors that return recorded outputs. The agent loop runs identically to a live run — this verifies parsing, decision routing, limit enforcement, and termination logic.
- Replay must never call real LLM or execute real tools.
- Replay compares: decision sequence, tool call sequence, final result, termination reason. Mismatches are reported as `replay_error`.
- OpenAI client uses `net/http` directly (no SDK dependency). Constructs chat completion requests, parses responses, extracts token usage.
- OpenAI client maps `MessageRole` values to OpenAI's expected role strings.
- API key sourced from `OPENAI_API_KEY` env var or config.

### Acceptance Criteria
- `matter replay --run path/to/run.json` replays a recorded run deterministically.
- Replay detects decision sequence mismatches and reports them.
- Replay never makes network calls or executes tools.
- OpenAI provider sends valid chat completion requests and parses responses.
- OpenAI provider extracts token usage and computes cost from pricing table.
- OpenAI provider retries on transient errors (429, 5xx) with backoff.
- OpenAI provider fails immediately on auth errors (401).
- End-to-end: `matter run` with OpenAI completes a simple task.
- All 20 acceptance criteria from spec Section 20 are satisfied.
- Integration tests cover: successful completion, tool failure recovery, limit exceeded, replay verification.

### Risks
- OpenAI API changes could break the client. Using stable chat completions endpoint mitigates this.
- Integration tests with real OpenAI require API key and cost money. Use mock LLM for CI; real provider tests are manual/optional.

---

## Dependency Graph

```text
Phase 1: Foundation
   |
   v
Phase 2: Tool System + Workspace Safety
   |
   v
Phase 3: LLM Client + Mock
   |
   v
Phase 4: Planner ──────────────────┐
   |                                |
   v                                |
Phase 5: Memory                     |
   |                                |
   v                                |
Phase 6: Policy + Agent Core <──────┘
   |
   v
Phase 7: Observability
   |
   v
Phase 8: Built-in Tools
   |
   v
Phase 9: CLI + Public API
   |
   v
Phase 10: Replay + OpenAI Provider
```

## Notes

- Each phase should be committed and pass `go build ./...` and `go test ./...` before proceeding.
- Run `golangci-lint run ./...` after each phase.
- Phases 1-6 form the critical path. The agent loop (Phase 6) is the integration point — everything before it is a dependency; everything after enhances it.
- The mock LLM (Phase 3) is used extensively in Phases 4-10 for deterministic testing.
- External dependencies are limited to: `gopkg.in/yaml.v3` (config), `github.com/santhosh-tekuri/jsonschema/v5` (tool validation).
