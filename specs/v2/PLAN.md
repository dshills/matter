# matter v2 — Implementation Plan

This plan implements all 8 features defined in `specs/v2/SPEC.md`. It follows the spec's suggested phasing (A → B → C) but subdivides into 10 granular phases, each independently testable and committable.

---

## Phase 1: LLM Provider Infrastructure

### Goals

Establish the provider architecture (factory, registry, credential resolution, pricing table refactor) without implementing any real provider. This lays the foundation for Phases 2 and 3.

### Files to Create

- `internal/llm/provider.go` — `ProviderConfig` struct, `ProviderFactory` type, `NewClient` factory function, provider registry map
- `internal/llm/provider_test.go` — tests for factory dispatch, unknown provider error, credential resolution
- `internal/llm/pricing.json` — embedded JSON pricing table (replacing compiled-in Go map)

### Files to Modify

- `internal/llm/client.go` — add `ProviderConfig` struct fields, `CredentialChain` resolution function
- `internal/llm/costing.go` — refactor `EstimateCost` to load from embedded `pricing.json` via `//go:embed`; add `fallback_cost_per_1k` support; add pricing file override via env/config
- `internal/llm/costing_test.go` — update tests for JSON-based pricing, fallback cost, override file
- `internal/llm/mock.go` — add `newMockClientFromConfig(ProviderConfig) (Client, error)` factory wrapper
- `internal/config/config.go` — add `APIKey`, `BaseURL`, `PricingFile`, `FallbackCostPer1K`, `ExtraHeaders` fields to `LLMConfig`
- `internal/config/defaults.go` — defaults for new LLM config fields
- `internal/runner/runner.go` — build `ProviderConfig` from config and pass to `llm.NewClient` when not using direct client injection
- `cmd/matter/main.go` — update `createLLMClient` to use `llm.NewClient` factory; retain `--mock` as shorthand; redact `api_key` in `cmdConfig` output

### Key Decisions

- Provider registry is a package-level `map[string]ProviderFactory` — simple, no plugin system
- Credential resolution is a standalone function, not embedded in providers — testable in isolation
- `pricing.json` is `//go:embed`ded so the binary remains self-contained
- `fallback_cost_per_1k: 0` (default) means unknown models are rejected — preserving budget safety

### Risks

- Pricing table format change could break existing `EstimateCost` callers — mitigated by keeping the same function signature
- API key redaction must cover all output paths: `matter config` YAML output and structured log output. Implement via a `RedactedString` type that implements `fmt.Stringer`, `encoding.TextMarshaler`, and `yaml.Marshaler` to return `"***"`, preventing accidental leakage in new code paths. Test explicitly with keys in env vars, config file, and both simultaneously
- Malformed or invalid `pricing.json` (embedded or override file) must return `configuration_error` at startup, not panic at cost-estimation time

### Acceptance Criteria

- [ ] `llm.NewClient("mock", ...)` returns a mock client
- [ ] `llm.NewClient("unknown", ...)` returns an error
- [ ] Credential chain resolves in correct order: `MATTER_LLM_API_KEY` > provider-specific > config
- [ ] `EstimateCost` loads from embedded JSON
- [ ] Custom pricing file overrides embedded defaults
- [ ] Unknown model with `fallback_cost_per_1k: 0` returns error
- [ ] Unknown model with non-zero fallback uses fallback rate
- [ ] `matter config` redacts `api_key` to `"***"` (tested with key from env var, config file, and both)
- [ ] Structured log output does not contain plain-text API keys (verified via `RedactedString` type that implements `fmt.Stringer`, `encoding.TextMarshaler`, and `yaml.Marshaler`)
- [ ] Malformed `pricing.json` returns `configuration_error`
- [ ] Invalid override pricing file returns `configuration_error`
- [ ] All v1 tests pass without modification

---

## Phase 2: OpenAI Provider

### Goals

Implement the OpenAI Chat Completions provider. After this phase, matter can execute real tasks against the OpenAI API.

### Files to Create

- `internal/llm/openai.go` — `newOpenAIClient` factory, `openaiClient` struct implementing `llm.Client`, request/response mapping, error classification
- `internal/llm/openai_test.go` — httptest-based tests for request mapping, response parsing, error classification (401→terminal, 429→retriable, 5xx→retriable, 400→terminal), timeout handling, BaseURL override

### Files to Modify

- `internal/llm/provider.go` — register `"openai"` factory in provider map

### Key Decisions

- Use `net/http` directly — no OpenAI SDK dependency
- `"tool"` role messages mapped to `"user"` with `"[Tool Result] "` prefix per spec
- Default `max_tokens` to 4096 when `Request.MaxTokens` is 0
- Set `User-Agent: matter/<version>` header
- BaseURL support: `{BaseURL}/chat/completions`, strip trailing slashes
- Azure is explicitly NOT supported via BaseURL

### Risks

- OpenAI API changes (unlikely to break chat completions, stable API)
- Rate limiting in integration tests — mitigated by gating behind `OPENAI_API_KEY` env var

### Acceptance Criteria

- [ ] httptest: valid request produces correct Response fields (Content, tokens, latency)
- [ ] httptest: 401 → `ClassTerminal` error
- [ ] httptest: 429 → `ClassRetriable` error
- [ ] httptest: 500 → `ClassRetriable` error
- [ ] httptest: timeout → `ClassRetriable` error
- [ ] httptest: JSON decode failure → `ClassTerminal` error
- [ ] httptest: custom BaseURL routes correctly
- [ ] httptest: system/user/assistant/tool role mapping correct
- [ ] Integration (gated): simple task completes with real GPT-4o

---

## Phase 3: Anthropic Provider

### Goals

Implement the Anthropic Messages provider. After this phase, matter supports both major LLM providers.

### Files to Create

- `internal/llm/anthropic.go` — `newAnthropicClient` factory, `anthropicClient` struct, request/response mapping (system message extraction), error classification
- `internal/llm/anthropic_test.go` — httptest-based tests mirroring OpenAI test structure

### Files to Modify

- `internal/llm/provider.go` — register `"anthropic"` factory in provider map

### Key Decisions

- System message extracted from messages array and sent as top-level `system` field (Anthropic API requirement)
- `x-api-key` header (not `Authorization: Bearer`)
- `anthropic-version: 2023-06-01` header required
- `max_tokens` is required by Anthropic — default to 4096 when 0
- BaseURL support: `{BaseURL}/v1/messages`

### Risks

- Anthropic API versioning — pinned to `2023-06-01`, stable and widely used

### Acceptance Criteria

- [ ] httptest: system message extracted to top-level field
- [ ] httptest: correct headers (`x-api-key`, `anthropic-version`, `content-type`)
- [ ] httptest: valid response maps to correct Response fields
- [ ] httptest: error classification matches OpenAI patterns
- [ ] httptest: BaseURL override works
- [ ] httptest: missing max_tokens defaults to 4096
- [ ] Integration (gated): simple task completes with real Claude

---

## Phase 4: Configurable System Prompt

### Goals

Make the planner's system prompt configurable via YAML config, supporting prefix/suffix modification, full prompt override, and file-based prompts.

### Files to Create

- `internal/planner/prompt_test.go` — dedicated tests for prompt construction with all config combinations

### Files to Modify

- `internal/config/config.go` — add `PlannerConfig` struct with `SystemPrompt`, `SystemPromptFile`, `PromptPrefix`, `PromptSuffix`, `MaxResponseTokens`, `Temperature` fields; add `Planner PlannerConfig` to `Config`
- `internal/config/defaults.go` — defaults: `MaxResponseTokens: 4096`, `Temperature: 0`
- `internal/config/config_test.go` — validation tests for planner config
- `internal/planner/planner.go` — refactor `buildPrompt` to read from config; load file at construction time; use `MaxResponseTokens` and `Temperature` instead of hardcoded values
- `internal/planner/planner_test.go` — update existing tests to use new config

### Key Decisions

- `system_prompt` > `system_prompt_file` > default — if `system_prompt` is set, `prompt_prefix` and `prompt_suffix` are ignored
- Structural sections (Available Tools, Budget, Output Format) are always appended — cannot be overridden
- Prompt file is read once at planner construction time, not on every call
- File read errors at construction time return `configuration_error`
- Empty prompt files are treated as an error (file exists but contains no content) — returns `configuration_error`

### Risks

- Prompt changes could break existing planner behavior — mitigated by keeping default identical to v1
- Large prompt files could inflate context — no size limit imposed (user responsibility)

### Acceptance Criteria

- [ ] Default behavior identical to v1 with no planner config set
- [ ] `prompt_prefix` prepends text before default prompt
- [ ] `prompt_suffix` appends text after instructions, before output format
- [ ] `system_prompt` replaces persona/instructions but keeps tools/budget/format sections
- [ ] `system_prompt_file` loads from file
- [ ] Missing `system_prompt_file` returns `configuration_error`
- [ ] Empty `system_prompt_file` returns `configuration_error`
- [ ] `max_response_tokens` controls LLM request MaxTokens
- [ ] `temperature` controls LLM request temperature
- [ ] `system_prompt` set causes `prompt_prefix`/`prompt_suffix` to be ignored

---

## Phase 5: Command Allowlist

### Goals

Add configurable command allowlist to restrict which binaries the `command_exec` tool can run, using `exec.LookPath` for security.

### Files to Modify

- `internal/config/config.go` — add `CommandAllowlist []string` to `ToolsConfig`
- `internal/tools/builtin/command_exec.go` — update `NewCommandExec` signature to accept `allowlist []string`; add allowlist enforcement with `exec.LookPath` resolution before `exec.CommandContext`
- `internal/tools/builtin/command_exec_test.go` — table-driven tests for allow/reject, path manipulation bypass attempts, empty allowlist (v1 behavior)
- `internal/runner/runner.go` — pass `cfg.Tools.CommandAllowlist` to `NewCommandExec`

### Key Decisions

- Enforcement is in the tool itself, not in policy — defense-in-depth
- The **restricted PATH** is the v1-defined `PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin` already set by `command_exec` (see `internal/tools/builtin/command_exec.go`). Allowlist resolution calls `exec.LookPath` with this restricted PATH set in the environment, not the host's full PATH. This is not a new concept — it reuses the existing sandboxed environment
- Relative paths (`./foo`) and absolute paths (`/tmp/foo`) are rejected unless they resolve to an allowlisted name in the restricted PATH
- Empty allowlist = all commands allowed (backward compatible)
- Rejected commands return a tool error (recoverable), not a fatal error

### Risks

- `exec.LookPath` behavior differs across OS — test on CI platform
- Case sensitivity follows the spec: case-sensitive on Linux, case-insensitive on macOS/Windows. On macOS/Windows, both allowlist entries and resolved binary names are lowercased before comparison. On Linux, comparison is exact-match. Use `runtime.GOOS` to select behavior

### Acceptance Criteria

- [ ] Allowlist `["go", "git"]`: `go version` succeeds
- [ ] Allowlist `["go", "git"]`: `rm -rf /` returns tool error
- [ ] Empty allowlist: all commands allowed
- [ ] `exec.LookPath` used for resolution — only restricted PATH binaries considered
- [ ] Absolute path to binary outside restricted PATH rejected even if base name matches
- [ ] Rejected commands do not terminate the run
- [ ] v1 tests pass without modification (empty allowlist default)

---

## Phase 6: Progress Callbacks

### Goals

Add a callback mechanism for real-time progress events. Reimplement CLI progress output using this mechanism.

### Files to Create

- `internal/observe/progress_test.go` — callback invocation tests, nil callback safety, error handling

### Files to Modify

- `pkg/matter/types.go` — add `ProgressEvent` struct and `ProgressFunc` type
- `internal/observe/observer.go` — add `ProgressFunc` parameter to `StartRun`; store on `RunSession`
- `internal/observe/tracing.go` — invoke `ProgressFunc` in each event method (`RunStarted`, `PlannerStarted`, `PlannerCompleted`, `ToolStarted`, `ToolCompleted`, `LimitExceeded`, `RunCompleted`) after existing logging/tracing. These are the complete set of event types defined in the spec
- `internal/runner/runner.go` — add `SetProgressFunc` method on `Runner`; pass callback through to observer's `StartRun`
- `internal/agent/agent.go` — pass progress function through agent construction to observer
- `cmd/matter/main.go` — reimplement CLI stderr progress output as a `ProgressFunc` registered via `SetProgressFunc`

### Key Decisions

- Callbacks are synchronous — agent loop is suspended until return, per spec. Callbacks are not terminated on timeout. However, if a callback exceeds 500ms, a warning is logged so operators can identify slow consumers. The agent continues after the callback returns regardless of duration
- Callback errors (including panics recovered via `recover()`) are logged but do not affect the run
- Nil callback → no overhead (nil check before invocation)
- Events match tracer event types one-to-one
- CLI progress output becomes a consumer of the callback, not a separate mechanism

### Risks

- Callbacks exceeding 500ms trigger a warning log. Callbacks are never terminated (per spec), so truly blocking consumers will block the agent. Document this clearly in `ProgressFunc` godoc
- Adding callback to observer interface may require updating many call sites — mitigated by making it optional (nil-safe)

### Acceptance Criteria

- [ ] `SetProgressFunc` callback invoked for every event type
- [ ] Callback receives correct run ID, step, and event-specific data
- [ ] Callback errors logged but do not terminate the run
- [ ] No callback registered → no overhead
- [ ] CLI progress output works identically using callback mechanism
- [ ] Events match tracer event types one-to-one
- [ ] All v1 tests pass

---

## Phase 7: Conversation Mode

### Goals

Add `"ask"` decision type allowing the agent to pause for user input and resume with the answer. Integrate with CLI for interactive use.

### Files to Modify

- `pkg/matter/types.go` — add `DecisionTypeAsk` constant, `AskRequest` struct, add `Ask *AskRequest` to `Decision`, add `Paused bool` and `Question *AskRequest` to `RunResult`
- `internal/config/config.go` — add `MaxAsks int` to `AgentConfig`
- `internal/config/defaults.go` — default `MaxAsks: 3`
- `internal/planner/planner.go` — update prompt to include ask format and instructions when `max_asks > 0`; omit when `max_asks == 0`
- `internal/planner/parser.go` — handle `DecisionTypeAsk` parsing, validate `Ask` field presence
- `internal/planner/parser_test.go` — tests for ask decision parsing
- `internal/agent/agent.go` — handle `DecisionTypeAsk` in step loop: store in memory, return paused result; add state export/restore methods for pause/resume
- `internal/agent/limits.go` — add ask counter and `max_asks` limit check
- `internal/agent/limits_test.go` — test ask limit enforcement
- `internal/agent/loop.go` — handle ask decision, pause duration timer
- `internal/runner/runner.go` — add `Resume(ctx, answer)` method; store `pausedAgent` and `pausedReq`; reject new `Run` while paused; pause/resume duration timer
- `internal/runner/runner_test.go` — tests for pause/resume cycle, multiple ask/resume, max_asks enforcement
- `cmd/matter/main.go` — detect paused result, prompt for input, call `Resume` in a loop

### Key Decisions

- Ask counts as a step (increments step counter)
- Duration timer pauses while waiting for user input
- Only one paused run per runner
- `max_asks: 0` disables conversation mode entirely — prompt omits ask format, ask decisions treated as planner errors
- State preservation: memory, metrics, and loop detector are preserved across pause/resume

### Risks

- State preservation complexity — must ensure memory, metrics, and detector are consistent across pause/resume boundary
- CLI stdin reading requires careful handling of EOF and signals
- Paused run with no resume leaks memory — mitigated by documenting runner lifecycle

### Acceptance Criteria

- [ ] Planner can produce `"ask"` decision that pauses the run
- [ ] `RunResult.Paused` is true with question populated
- [ ] `Resume` continues the run with answer in context as user message
- [ ] Duration timer pauses during user input wait
- [ ] `max_asks` limit terminates the run when exceeded
- [ ] `max_asks: 0` disables conversation mode
- [ ] CLI prompts for input and displays options
- [ ] Multiple ask/resume cycles work within a single run
- [ ] Calling `Run` while paused returns error
- [ ] All v1 tests pass (max_asks defaults to 3, no ask decisions from mock)

---

## Phase 8: Multi-Step Planning

### Goals

Allow the planner to return sequences of tool calls that execute without intermediate LLM calls, reducing cost and latency.

### Files to Modify

- `pkg/matter/types.go` — add `ToolCalls []ToolCall` to `Decision`
- `internal/config/config.go` — add `MaxPlanSteps int` to `PlannerConfig`
- `internal/config/defaults.go` — default `MaxPlanSteps: 5`
- `internal/planner/planner.go` — update prompt to include multi-step format and instructions when `max_plan_steps > 1`
- `internal/planner/parser.go` — handle `tool_calls` array parsing; validate length against `max_plan_steps` (reject if over limit, not truncate)
- `internal/planner/parser_test.go` — tests for multi-step parsing, over-limit rejection
- `internal/agent/loop.go` — implement sequential execution of `ToolCalls`: iterate, execute each, store result, run policy check, check limits, update metrics; stop on failure and return to planner
- `internal/agent/loop_detector.go` — each tool call in sequence individually checked
- `internal/agent/agent_test.go` — multi-step execution tests: success sequence, mid-sequence failure, step counting

### Key Decisions

- `ToolCalls` takes precedence over `ToolCall` when both present
- Each tool call in a sequence counts as a separate step toward `max_steps`
- Sequences over `max_plan_steps` are rejected as planner errors (not truncated) — the error includes the limit so the planner can retry with a shorter sequence
- No parallel execution in v2 — tools execute sequentially
- `max_plan_steps: 1` disables multi-step planning

### Risks

- Complex interaction with loop detection — each call must be checked individually, not the sequence as a unit
- Step counting: a 5-tool sequence consumes 5 steps, which could surprise budget-constrained runs — documented in prompt

### Acceptance Criteria

- [ ] Planner can return `tool_calls` array
- [ ] Tools execute in order within a single planning round
- [ ] Failed tool call stops sequence and returns to planner
- [ ] Each tool call counts as a step toward `max_steps`
- [ ] Policy checks run before each tool call
- [ ] Loop detection evaluates each call individually
- [ ] Sequences over `max_plan_steps` rejected as planner error
- [ ] `max_plan_steps: 1` disables multi-step
- [ ] Single `tool_call` (v1 format) continues to work
- [ ] Memory contains results from all executed tools in sequence

---

## Phase 9: MCP Tool Adapter

### Goals

Connect to external MCP servers (stdio and SSE transports), discover tools, and register them as native matter tools.

### Files to Create

- `internal/tools/mcp/client.go` — `MCPClient` struct, `Transport` interface, JSON-RPC 2.0 message types
- `internal/tools/mcp/stdio.go` — `StdioTransport`: subprocess launch, stdin/stdout JSON-RPC communication, lifecycle management
- `internal/tools/mcp/sse.go` — `SSETransport`: HTTP SSE connection, POST-based request sending
- `internal/tools/mcp/adapter.go` — `mcpToolToMatterTool` conversion, `mcpExecuteFunc` closure, `DiscoverAndRegister` function to discover tools from server and register in tool registry
- `internal/tools/mcp/client_test.go` — mock transport tests for tool discovery and execution
- `internal/tools/mcp/stdio_test.go` — subprocess lifecycle tests using a test binary: start/stop, crash recovery, zombie process reaping on cleanup
- `internal/tools/mcp/adapter_test.go` — tool conversion and namespacing tests

### Files to Modify

- `internal/config/config.go` — add `MCPServers []MCPServerConfig` to `ToolsConfig`; add `MCPServerConfig` struct
- `internal/runner/runner.go` — initialize MCP clients during `Run`, discover and register tools, cleanup on run completion

### Key Decisions

- MCP tools are `Safe: false` and `SideEffect: true` by default
- Tool names are namespaced: `servername.toolname`
- MCP tool discovery (`tools/list`) has a 10-second timeout — if an MCP server does not respond in time, it is treated as a startup failure
- MCP server startup failure is non-fatal — logs warning, continues without those tools
- Mid-run disconnect produces recoverable tool errors
- Subprocess environment is restricted: `PATH`, `HOME`, `TMPDIR`, `LANG` plus configured env vars
- MCP tool output subject to same `max_tool_result_chars` truncation

### Risks

- MCP protocol complexity — JSON-RPC 2.0 over stdio requires careful framing (newline-delimited)
- SSE transport implementation — manual parsing of SSE event stream
- Subprocess lifecycle management — must handle crashes, hangs, and cleanup. On Linux, use `SysProcAttr.Pdeathsig` to ensure child processes die when parent exits. On macOS/other platforms, the stdio transport detects parent pipe closure (stdin EOF from child's perspective) as the shutdown signal — well-behaved MCP servers exit on stdin EOF. Additionally, `Close()` sends SIGTERM followed by SIGKILL after a 5-second grace period
- Test complexity — need mock MCP servers for reliable testing

### Acceptance Criteria

- [ ] Stdio MCP server started and tools discovered via `tools/list`
- [ ] SSE MCP server connected and tools discovered
- [ ] MCP tools appear in `matter tools` with namespaced names
- [ ] Agent can call MCP tools and receive results
- [ ] MCP tool failures are recoverable
- [ ] Server startup failure logs warning, does not prevent runner creation
- [ ] Server disconnect mid-run produces tool errors, not crashes
- [ ] Subprocess properly terminated and reaped on runner cleanup (no zombie processes)
- [ ] Subprocess crash mid-run does not leave zombie processes
- [ ] Subprocess environment is restricted to configured env vars plus safe baseline
- [ ] Tool output subject to truncation limits
- [ ] Multiple MCP servers configurable without name collisions

---

## Phase 10: HTTP API Server

### Goals

Build a standalone HTTP API server binary (`matter-server`) supporting async runs, SSE streaming, authentication, and conversation mode integration.

### Files to Create

- `cmd/matter-server/main.go` — server entry point: config loading, server construction, signal handling, graceful shutdown
- `internal/server/server.go` — `Server` struct, route registration, middleware (auth with token redaction in logs, request logging)
- `internal/server/handlers.go` — HTTP handlers: `POST /api/v1/runs`, `GET /api/v1/runs/{id}`, `GET /api/v1/runs/{id}/events`, `DELETE /api/v1/runs/{id}`, `POST /api/v1/runs/{id}/answer`, `GET /api/v1/tools`, `GET /api/v1/health`
- `internal/server/runs.go` — `ActiveRun` struct, `RunStore` (thread-safe map), garbage collection ticker, concurrency limiting
- `internal/server/sse.go` — SSE writer: headers, event formatting, flush-after-write, channel-per-subscriber with buffer, 5-second write timeout for terminal events
- `internal/server/server_test.go` — httptest-based tests for all endpoints, auth, concurrency limits, SSE streaming
- `internal/server/handlers_test.go` — individual handler tests with mock runner

### Files to Modify

- `internal/config/config.go` — add `ServerConfig` struct with `ListenAddr`, `MaxConcurrentRuns`, `MaxPausedRuns`, `RunRetention`, `AuthToken`
- `internal/config/defaults.go` — defaults: `ListenAddr: ":8080"`, `MaxConcurrentRuns: 10`, `MaxPausedRuns: 20`, `RunRetention: 1h`
- `Makefile` — add `build-server` target for `cmd/matter-server`

### Key Decisions

- Separate binary (`matter-server`), not a subcommand — different deployment model
- `net/http` standard library only — no web framework
- Each run in its own goroutine with isolated runner
- SSE: channel-per-subscriber, capacity 100; when buffer is full, oldest intermediate events are dropped so the client sees the most recent state, and a warning is logged; terminal events have 5-second write timeout — on timeout, log a warning and close the connection (no retry)
- Run state is ephemeral (in-memory only) — server restarts lose all active and completed run data. Persistent storage is deferred to a future version. This is a known limitation documented in the server's startup log and health endpoint
- Logging middleware must redact the `Authorization` header value — log `"Bearer ***"` not the actual token
- Paused runs don't count toward `max_concurrent_runs` (no goroutine)
- Completed/failed runs retained for `run_retention`, then GC'd
- Paused runs GC'd after `run_retention` from pause time
- `max_paused_runs` enforced — new pauses beyond limit are cancelled
- Graceful shutdown: stop accepting, wait 30s for active runs, force cancel

### Risks

- Concurrency correctness — thread-safe run store, proper mutex usage
- SSE client management — must handle disconnect detection, slow consumers, resource cleanup
- Memory growth from retained completed runs — mitigated by GC ticker and `run_retention`
- Integration with conversation mode adds complexity to run lifecycle

### Acceptance Criteria

- [ ] `POST /api/v1/runs` starts a run, returns 202 with run ID
- [ ] `GET /api/v1/runs/{id}` returns current status and metrics
- [ ] `GET /api/v1/runs/{id}/events` streams SSE events in real time
- [ ] `DELETE /api/v1/runs/{id}` cancels a running task
- [ ] `POST /api/v1/runs/{id}/answer` resumes a paused run
- [ ] `GET /api/v1/tools` lists registered tools
- [ ] `GET /api/v1/health` returns 200 with version
- [ ] Bearer token auth works when configured
- [ ] Missing/wrong token returns 401
- [ ] Concurrent run limit enforced with 429
- [ ] Paused run limit enforced
- [ ] Completed runs GC'd after retention period
- [ ] Graceful shutdown waits for active runs
- [ ] SSE stream closes when run completes
- [ ] Authorization header redacted in request logs
- [ ] Server startup logs ephemeral storage warning

---

## Dependency Graph

```
Phase 1 (Provider Infra) ──► Phase 2 (OpenAI) ──► Phase 3 (Anthropic)
                                                         │
Phase 4 (Configurable Prompt) ◄──────────────────────────┘ (independent)
Phase 5 (Command Allowlist) ◄──── (independent)
Phase 6 (Progress Callbacks) ──► Phase 7 (Conversation Mode) ──► Phase 10 (HTTP Server)
                                                                      ▲
Phase 8 (Multi-Step Planning) ◄──── (independent)
Phase 9 (MCP Tool Adapter) ◄──── (independent)
```

**Strict dependencies:** Phases 2 and 3 depend on Phase 1. Phase 7 depends on Phase 6. Phase 10 depends on Phases 6 and 7.

**Independent phases:** Phases 4, 5, 8, and 9 have no dependencies on other v2 phases and can be implemented in any order.

**Optional integration:** Phase 10 does not require Phase 9. The HTTP server's `/api/v1/tools` endpoint lists whatever tools are registered — if MCP tools exist (Phase 9 completed), they appear; if not, only built-in tools are listed. Phase 9 can be completed before or after Phase 10.

## Implementation Notes

### Parallelization Opportunities

- Phases 4, 5 can be done in parallel with Phases 2, 3 (no overlap)
- Phase 8 can be done in parallel with Phase 7 (no overlap)
- Phase 9 can be done in parallel with Phase 7 or 8 (no overlap)

### Testing Strategy

- Every phase includes unit tests with mock dependencies
- No real API calls in CI — all LLM provider tests use `httptest` servers
- Integration tests gated behind provider-specific env vars (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`)
- MCP tests use mock servers over stdio pipes
- HTTP server tests use `httptest.Server`
- All v1 tests must pass at every phase boundary

### Backward Compatibility

Every phase preserves v1 behavior:

- New config fields have defaults matching v1 behavior
- `"mock"` provider continues to work
- Single `tool_call` format accepted
- Empty allowlists, empty MCP server lists, `max_asks: 0`, `max_plan_steps: 1` all produce v1 behavior
- HTTP server is a separate binary — CLI unchanged
