# matter v2 Specification

## Overview

This specification defines the next set of features for the matter autonomous AI agent framework. It covers all items identified as Critical or High impact from the v1 review: real LLM provider integrations, configurable planner prompts, command allowlisting, progress callbacks, conversation mode, multi-step planning, HTTP API server, and MCP tool adapters.

Each section is self-contained and references the v1 spec (`specs/initial/SPEC.md`) where it extends existing behavior. All new features must preserve v1's safety guarantees: workspace confinement, budget enforcement, loop detection, and policy checks.

## Baseline Assumptions

- The v1 spec is fully implemented through Phase 9 (CLI, runner, tools, memory, planner, observer, policy, config).
- The v1 `llm.Client` interface (`Complete(ctx, req) (Response, error)`) remains the foundation.
- The v1 `config.Config` struct is extended, not replaced.
- The v1 `matter.Decision` type is extended with new decision types.
- All new features are opt-in; a v1 config file must continue to work without modification.

---

## 1. Real LLM Provider Integrations

### 1.1 Problem

matter currently has only a mock LLM client. The framework cannot execute real tasks without provider implementations for OpenAI and Anthropic APIs.

### 1.2 Provider Architecture

Each provider is a standalone implementation of `llm.Client` in its own file. Providers are selected by the `llm.provider` config field and constructed by a factory function.

```go
// internal/llm/provider.go

// ProviderFactory creates an LLM client from config.
// Returns an error if required credentials are missing or the provider is unknown.
type ProviderFactory func(cfg ProviderConfig) (Client, error)

// ProviderConfig holds provider-specific configuration extracted from the
// top-level config at construction time.
type ProviderConfig struct {
    Provider    string
    Model       string
    Timeout     time.Duration
    APIKey      string        // resolved from credential chain
    BaseURL     string        // optional override for proxies/self-hosted
    ExtraHeaders map[string]string // optional custom headers
}
```

A provider registry maps provider names to factories:

```go
// internal/llm/providers.go

var providers = map[string]ProviderFactory{
    "openai":    newOpenAIClient,
    "anthropic": newAnthropicClient,
    "mock":      newMockClientFromConfig,
}

// NewClient creates an LLM client for the configured provider.
func NewClient(cfg ProviderConfig) (Client, error) {
    factory, ok := providers[cfg.Provider]
    if !ok {
        return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
    }
    return factory(cfg)
}
```

### 1.3 Credential Resolution

API keys are resolved in the following order (first non-empty wins):

1. Environment variable: `MATTER_LLM_API_KEY`
2. Provider-specific environment variable: `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`
3. Config file field: `llm.api_key`

If no key is found for a non-mock provider, `NewClient` returns a `configuration_error`.

API keys must never appear in:
- Structured log output
- Run records (the `SafeConfig` type already omits `LLMConfig` internals)
- CLI `matter config` output — the `api_key` field must be redacted to `"***"` when printing

### 1.4 OpenAI Provider

**File:** `internal/llm/openai.go`

**API:** OpenAI Chat Completions API (`POST https://api.openai.com/v1/chat/completions`)

**Request mapping:**
- `llm.Request.Messages` → OpenAI `messages` array. Map `MessageRole` values: `"system"` → `"system"`, `"user"` → `"user"`, `"assistant"` → `"assistant"`, `"tool"` → `"user"` (prefixed with `"[Tool Result] "`)
- `llm.Request.MaxTokens` → `max_tokens` (if 0, default to 4096)
- `llm.Request.Temperature` → `temperature`
- `llm.Request.Model` → `model`

**Response mapping:**
- `choices[0].message.content` → `Response.Content`
- `usage.prompt_tokens` → `Response.PromptTokens`
- `usage.completion_tokens` → `Response.CompletionTokens`
- Sum → `Response.TotalTokens`
- Cost computed via the existing `llm.EstimateCost` standalone helper function (not a Client method) using the pricing table
- `Response.Latency` measured from request start to response body read

**Error mapping:**
- HTTP 401, 403 → `llm_error` with `ClassTerminal` (bad credentials, do not retry)
- HTTP 429 → `llm_error` with `ClassRetriable` (rate limited)
- HTTP 500, 502, 503 → `llm_error` with `ClassRetriable` (server error)
- HTTP 400 → `llm_error` with `ClassTerminal` (malformed request)
- Network timeout → `llm_error` with `ClassRetriable`
- JSON decode failure → `llm_error` with `ClassTerminal`

**Implementation constraints:**
- Use `net/http` directly. No OpenAI SDK dependency.
- Set `User-Agent: matter/<version>`.
- Respect `ProviderConfig.Timeout` via `context.WithTimeout`.
- Support `ProviderConfig.BaseURL` for proxy configurations. When `BaseURL` is set, the provider sends requests to `{BaseURL}/chat/completions` instead of the default OpenAI endpoint. Trailing slashes in `BaseURL` are stripped before path construction.
- **Azure OpenAI** is not supported via BaseURL alone (Azure uses a different URL structure and `api-key` header). Azure support is deferred to a future version or a dedicated `"azure"` provider. Do not claim Azure support via BaseURL.

### 1.5 Anthropic Provider

**File:** `internal/llm/anthropic.go`

**API:** Anthropic Messages API (`POST https://api.anthropic.com/v1/messages`)

**Request mapping:**
- System message extracted from `Messages` and sent as top-level `system` field
- Remaining messages → `messages` array. Map roles: `"user"` → `"user"`, `"assistant"` → `"assistant"`, `"tool"` → `"user"` (prefixed with `"[Tool Result] "`)
- `llm.Request.MaxTokens` → `max_tokens` (required by Anthropic)
- `llm.Request.Temperature` → `temperature`
- `llm.Request.Model` → `model`

**Response mapping:**
- `content[0].text` → `Response.Content`
- `usage.input_tokens` → `Response.PromptTokens`
- `usage.output_tokens` → `Response.CompletionTokens`
- Sum → `Response.TotalTokens`
- Cost computed via pricing table

**Headers:**
- `x-api-key: <api_key>`
- `anthropic-version: 2023-06-01`
- `content-type: application/json`

**Error mapping:** Same classification as OpenAI (401/403 terminal, 429 retriable, 5xx retriable, 400 terminal).

**Implementation constraints:**
- Use `net/http` directly. No Anthropic SDK dependency.
- Anthropic requires `max_tokens` — if `llm.Request.MaxTokens` is 0, default to 4096.
- Support `ProviderConfig.BaseURL` for proxy configurations. When `BaseURL` is set, the provider sends requests to `{BaseURL}/v1/messages` instead of the default Anthropic endpoint. Trailing slashes in `BaseURL` are stripped before path construction.

### 1.6 Pricing Table Changes

The compiled-in `PricingTable` is replaced with an external JSON file that can be updated without recompilation.

**File:** `internal/llm/pricing.json` (embedded via `//go:embed`)

```json
[
  {"provider": "openai", "model": "gpt-4o", "prompt_cost_per_1k": 0.0025, "completion_cost_per_1k": 0.0100},
  {"provider": "openai", "model": "gpt-4o-mini", "prompt_cost_per_1k": 0.000150, "completion_cost_per_1k": 0.000600},
  {"provider": "anthropic", "model": "claude-sonnet-4-20250514", "prompt_cost_per_1k": 0.003, "completion_cost_per_1k": 0.015}
]
```

**Override:** If both `MATTER_PRICING_FILE` env var and `llm.pricing_file` config field are set, the environment variable takes precedence (consistent with matter's general config precedence: env > file > defaults). The override file replaces the embedded default entirely. This allows operators to add new models without rebuilding.

**Fallback for unknown models:** If the configured model is not in the pricing table, the runner checks for `llm.fallback_cost_per_1k` in config. If set, that value is used for both prompt and completion cost estimation. If not set, the runner returns a `configuration_error` — budget enforcement requires accurate cost tracking. This preserves v1's safety guarantee that cost limits are meaningful.

### 1.7 Config Changes

```yaml
llm:
  provider: openai          # "openai", "anthropic", or "mock"
  model: gpt-4o
  api_key: ""               # optional; env vars preferred
  base_url: ""              # optional; override API endpoint
  timeout: 30s
  max_retries: 3
  pricing_file: ""          # optional; path to custom pricing JSON
  fallback_cost_per_1k: 0   # cost per 1k tokens for models not in pricing table; 0 = reject unknown models
  extra_headers: {}         # optional; additional HTTP headers
```

### 1.8 CLI Changes

The `--mock` flag on `matter run` is replaced by provider selection through config. If `llm.provider` is `"mock"`, the mock client is used. The `--mock` flag is retained as a shorthand that overrides `llm.provider` to `"mock"`.

### 1.9 Acceptance Criteria

1. `matter run --task "..." --workspace .` with `OPENAI_API_KEY` set completes a simple task using GPT-4o
2. Switching `llm.provider` to `anthropic` with `ANTHROPIC_API_KEY` set completes the same task
3. Missing API key returns `configuration_error` with clear message
4. Rate limit (429) triggers retry with backoff; auth error (401) terminates immediately
5. `matter config` redacts `api_key` field to `"***"`
6. Custom `base_url` routes requests to an alternative endpoint
7. Custom pricing file overrides embedded defaults
8. Unknown model with `fallback_cost_per_1k: 0` returns `configuration_error`; with a non-zero fallback, cost is estimated using the fallback rate
9. Provider-specific unit tests use httptest servers (no real API calls in CI)
10. All v1 tests continue to pass

---

## 2. Configurable System Prompt

### 2.1 Problem

The planner's system prompt is hardcoded in `internal/planner/planner.go:buildPrompt()`. Users building domain-specific agents (code review, data analysis, customer support) cannot customize the agent's persona, instructions, or constraints without modifying source code.

### 2.2 Design

Add a `planner` config section with prompt customization fields:

```yaml
planner:
  system_prompt: ""              # persona override; replaces default persona and instructions while preserving tool/budget/format sections
  system_prompt_file: ""         # path to file containing system prompt (alternative to inline)
  prompt_prefix: ""              # prepended before the default system prompt
  prompt_suffix: ""              # appended after the default instructions, before output format
  max_response_tokens: 4096     # max tokens for planner LLM response (was hardcoded)
  temperature: 0                 # planner temperature (was hardcoded to 0)
```

**Precedence:** `system_prompt` > `system_prompt_file` > default. If `system_prompt` is set, it replaces the persona and instruction sections of the prompt; `prompt_prefix` and `prompt_suffix` are ignored (they only modify the default prompt). The structural sections (Available Tools, Budget, Output Format) are always appended by the framework and cannot be overridden.

### 2.3 Prompt Structure

The default prompt structure becomes:

```
{prompt_prefix}

You are an autonomous agent. Your job is to complete the user's task...

## Task
{task}

## Available Tools
{tool_schemas}

## Budget
{budget_info}

## Instructions
- Do not invent tools...
- ...

{prompt_suffix}

## Output Format
Respond with a single JSON object...
```

When `system_prompt` is set, the entire prompt is replaced with:

```
{system_prompt}

## Available Tools
{tool_schemas}

## Budget
{budget_info}

## Output Format
Respond with a single JSON object...
```

The output format section is always appended and cannot be overridden. The agent's JSON contract is non-negotiable.

### 2.4 Planner Config Struct

```go
// PlannerConfig controls planner behavior.
type PlannerConfig struct {
    SystemPrompt      string  `yaml:"system_prompt"`
    SystemPromptFile  string  `yaml:"system_prompt_file"`
    PromptPrefix      string  `yaml:"prompt_prefix"`
    PromptSuffix      string  `yaml:"prompt_suffix"`
    MaxResponseTokens int     `yaml:"max_response_tokens"`
    Temperature       float64 `yaml:"temperature"`
}
```

Added to `config.Config` as `Planner PlannerConfig`.

### 2.5 Implementation

- `internal/planner/planner.go`: `buildPrompt` reads from config; loads file if `SystemPromptFile` is set
- `internal/planner/planner.go`: `Decide` uses `cfg.Planner.MaxResponseTokens` and `cfg.Planner.Temperature` instead of hardcoded values
- Prompt file is read once at planner construction time, not on every call
- File read errors during construction return `configuration_error`

### 2.6 Acceptance Criteria

1. Setting `prompt_prefix` prepends text before the default prompt
2. Setting `prompt_suffix` appends text after the instructions section
3. Setting `system_prompt` replaces the entire default prompt
4. Setting `system_prompt_file` loads prompt from file
5. Output format section is always present regardless of overrides
6. `max_response_tokens` controls the LLM request MaxTokens
7. `temperature` controls the LLM request temperature
8. Missing `system_prompt_file` returns `configuration_error`
9. Default behavior is identical to v1 when no planner config is set

---

## 3. Command Allowlist

### 3.1 Problem

The `command_exec` tool allows execution of any binary available in PATH. If the LLM is compromised via prompt injection or produces unexpected output, it could execute destructive commands (`rm -rf`, `curl` to exfiltrate data, etc.).

### 3.2 Design

Add a `command_allowlist` field to the tools config:

```yaml
tools:
  enable_command_exec: true
  command_allowlist: ["go", "git", "make", "grep", "find", "ls", "cat", "wc"]
```

**Behavior:**

- If `command_allowlist` is non-empty, only listed command names are permitted. The check resolves the command to its absolute path using `exec.LookPath` (which searches the restricted PATH) and then verifies the resolved base name is in the allowlist. Relative paths (`./foo`) and absolute paths (`/tmp/foo`) are rejected outright unless they resolve to a binary whose name is in the allowlist AND the binary resides within a directory in the restricted PATH. This prevents path manipulation bypasses.
- If `command_allowlist` is empty and `enable_command_exec` is true, all commands are allowed (v1 behavior, for backward compatibility).
- The allowlist is enforced inside `commandExecFunc` before `exec.CommandContext`, not in policy. This is defense-in-depth: the tool itself rejects disallowed commands even if policy is misconfigured.

### 3.3 Config Changes

```go
type ToolsConfig struct {
    // ... existing fields ...
    CommandAllowlist []string `yaml:"command_allowlist"`
}
```

### 3.4 Tool Constructor Change

```go
func NewCommandExec(workspaceRoot string, timeout time.Duration, maxOutputBytes int, allowlist []string) matter.Tool
```

The allowlist is captured in the closure. Before executing, the function resolves the command via `exec.LookPath` (using the restricted PATH), then verifies the resolved binary's base name is in the allowlist and the binary resides within a PATH directory. If rejected:

```go
return matter.ToolResult{Error: fmt.Sprintf("command %q is not in the allowlist", cmdName)}, nil
```

This is a tool-level error (returned to planner for replanning), not a fatal error.

### 3.5 Acceptance Criteria

1. With allowlist `["go", "git"]`, running `go version` succeeds
2. With allowlist `["go", "git"]`, running `rm -rf /` returns tool error "command not in allowlist"
3. With empty allowlist, all commands are allowed (backward compatible)
4. Allowlist resolves via `exec.LookPath` — only binaries in the restricted PATH are considered
5. Absolute paths to binaries outside the restricted PATH are rejected even if the base name matches
6. Allowlist is case-sensitive on Linux, case-insensitive on macOS/Windows
6. Rejected commands do not terminate the run (recoverable error)

---

## 4. Progress Callbacks

### 4.1 Problem

Library users embedding matter have no way to receive real-time progress updates. The CLI prints to stderr, but programmatic consumers need a callback mechanism.

### 4.2 Design

Define a progress callback type and add it to the runner:

```go
// pkg/matter/types.go

// ProgressEvent describes a step-level event during a run.
type ProgressEvent struct {
    RunID     string         `json:"run_id"`
    Step      int            `json:"step"`
    Event     string         `json:"event"`      // "run_started", "planner_started", "planner_completed", "tool_started", "tool_completed", "limit_exceeded", "run_completed"
    Data      map[string]any `json:"data"`        // event-specific fields
    Timestamp time.Time      `json:"timestamp"`
}

// ProgressFunc is called for each step-level event during a run.
// The agent loop is suspended until the callback returns. Implementations
// should return within 100ms; callbacks exceeding this are not terminated
// but will delay the agent loop. Errors are logged but do not affect the run.
type ProgressFunc func(event ProgressEvent)
```

### 4.3 Runner Integration

```go
// internal/runner/runner.go

type Runner struct {
    // ... existing fields ...
    progressFn matter.ProgressFunc
}

// SetProgressFunc registers a callback for real-time progress events.
// Must be called before Run. Not safe for concurrent use.
func (r *Runner) SetProgressFunc(fn matter.ProgressFunc) {
    r.progressFn = fn
}
```

The runner passes the callback to the observer, which invokes it alongside existing logging/tracing. The callback receives the same events the tracer records.

### 4.4 Observer Integration

The `RunSession` receives an optional `ProgressFunc` via `StartRun`. Each event method (`PlannerStarted`, `ToolCompleted`, etc.) calls the function after logging and tracing:

```go
func (s *RunSession) PlannerCompleted(step int, tokens int, cost float64, latency time.Duration) {
    // ... existing logging and tracing ...

    if s.progressFn != nil {
        s.progressFn(matter.ProgressEvent{
            RunID: s.runID,
            Step:  step,
            Event: "planner_completed",
            Data: map[string]any{
                "tokens":  tokens,
                "cost":    cost,
                "latency": latency.String(),
            },
            Timestamp: time.Now(),
        })
    }
}
```

### 4.5 CLI Integration

The CLI's stderr progress output is reimplemented as a `ProgressFunc`:

```go
runner.SetProgressFunc(func(e matter.ProgressEvent) {
    switch e.Event {
    case "run_started":
        fmt.Fprintf(os.Stderr, "Run %s started\n", e.RunID)
    case "tool_completed":
        fmt.Fprintf(os.Stderr, "  [step %d] %s (%s)\n", e.Step, e.Data["tool"], e.Data["duration"])
    case "run_completed":
        fmt.Fprintf(os.Stderr, "\nCompleted: %v (%d steps)\n", e.Data["success"], e.Step)
    }
})
```

### 4.6 Acceptance Criteria

1. `SetProgressFunc` callback is invoked for every event type during a run
2. Callback receives correct run ID, step number, and event-specific data
3. Callback errors are logged but do not terminate the run
4. Callbacks should return within 100ms; slow callbacks delay the agent loop
5. No callback registered → no overhead (nil check)
6. CLI progress output works identically using the callback mechanism
7. Events match the existing tracer event types one-to-one

---

## 5. Conversation Mode

### 5.1 Problem

The agent can only complete or fail. If a task is ambiguous or the agent needs clarification, it must either guess (risking wasted steps) or fail. A mechanism for the agent to ask the user a question and resume with the answer would improve task completion rates.

### 5.2 Design

Add a new decision type `"ask"` to the planner output:

```go
// pkg/matter/types.go

const DecisionTypeAsk DecisionType = "ask"

// AskRequest represents a question the agent needs answered before proceeding.
type AskRequest struct {
    Question string   `json:"question"`
    Options  []string `json:"options,omitempty"` // optional suggested answers
}

type Decision struct {
    Type      DecisionType `json:"type"`
    Reasoning string       `json:"reasoning"`
    ToolCall  *ToolCall    `json:"tool_call,omitempty"`
    Final     *FinalAnswer `json:"final,omitempty"`
    Ask       *AskRequest  `json:"ask,omitempty"`       // new
}
```

### 5.3 RunResult Changes

When the agent produces an `"ask"` decision, the run pauses and returns a `RunResult` with the question:

```go
// pkg/matter/types.go

type RunResult struct {
    FinalSummary string
    Steps        int
    TotalTokens  int
    TotalCostUSD float64
    Success      bool
    Error        error
    Paused       bool        // true if the run is waiting for user input
    Question     *AskRequest // set when Paused is true
}
```

### 5.4 Resumption

The runner provides a `Resume` method to continue a paused run:

```go
// internal/runner/runner.go

// Resume continues a paused run with the user's answer.
// Returns ErrNotPaused if the run is not in a paused state.
func (r *Runner) Resume(ctx context.Context, answer string) matter.RunResult
```

Internally, the answer is added to memory as a `RoleUser` message and the agent loop continues from where it paused.

### 5.5 Agent Loop Changes

When the planner returns `DecisionTypeAsk`:

1. Store the ask decision as an assistant message in memory
2. Emit a `"run_paused"` progress event
3. Save the agent state (memory, metrics, detector) to the runner
4. Return `RunResult{Paused: true, Question: decision.Ask}`

When `Resume` is called:

1. Restore agent state
2. Add the user's answer as a `RoleUser` message
3. Re-enter the agent loop (the next planner call will see the question and answer in context)

### 5.6 State Preservation

The paused agent state is held in memory on the runner. Only one paused run per runner is supported. Calling `Run` while a run is paused returns an error.

```go
type Runner struct {
    // ... existing fields ...
    pausedAgent *agent.Agent   // non-nil when a run is paused
    pausedReq   matter.RunRequest
}
```

### 5.7 CLI Integration

The CLI detects a paused result and prompts for input:

```go
result := r.Run(ctx, req)
for result.Paused {
    fmt.Fprintf(os.Stderr, "\nAgent question: %s\n", result.Question.Question)
    if len(result.Question.Options) > 0 {
        for i, opt := range result.Question.Options {
            fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt)
        }
    }
    fmt.Fprint(os.Stderr, "> ")
    answer := readLine(os.Stdin)
    result = r.Resume(ctx, answer)
}
```

### 5.8 Budget Implications

- An `"ask"` decision counts as a step (increments step counter)
- Time spent waiting for user input does NOT count toward `max_duration`. The duration timer pauses when the run pauses and resumes when `Resume` is called.
- A maximum of `max_asks` (default: 3) ask decisions per run prevents the agent from endlessly requesting input. This is a new limit field in `AgentConfig`.

### 5.9 Planner Prompt Changes

The output format section adds:

```
Ask: {"type":"ask","reasoning":"...","ask":{"question":"...","options":["A","B"]}}
```

The instructions section adds:

```
- Ask the user only when the task is genuinely ambiguous. Do not ask for confirmation of routine actions.
- Use options when the question has a small number of likely answers.
```

### 5.10 Config Changes

```yaml
agent:
  # ... existing fields ...
  max_asks: 3               # maximum "ask" decisions per run; 0 disables conversation mode
```

When `max_asks` is 0, the planner prompt omits the ask format and instructions. The agent treats any `"ask"` decision as a planner error.

### 5.11 Acceptance Criteria

1. Planner can produce an `"ask"` decision that pauses the run
2. `RunResult.Paused` is true with the question populated
3. `Resume` continues the run with the user's answer in context
4. The answer appears as a user message in memory
5. Duration timer pauses during user input wait
6. `max_asks` limit terminates the run when exceeded
7. `max_asks: 0` disables conversation mode entirely
8. CLI prompts for input and displays options when present
9. Multiple ask/resume cycles work within a single run
10. Paused state is preserved across ask/resume (memory, metrics, detector intact)

---

## 6. Multi-Step Planning

### 6.1 Problem

The agent makes one LLM call per tool call. For straightforward sequences (read 3 files, then summarize), this wastes 3 LLM calls when the plan is obvious after the first. Multi-step planning reduces cost and latency by letting the planner output a sequence of tool calls.

### 6.2 Design

Extend the `Decision` type to support ordered tool call sequences:

```go
type Decision struct {
    Type      DecisionType `json:"type"`
    Reasoning string       `json:"reasoning"`
    ToolCall  *ToolCall    `json:"tool_call,omitempty"`
    ToolCalls []ToolCall   `json:"tool_calls,omitempty"`  // new: ordered sequence
    Final     *FinalAnswer `json:"final,omitempty"`
    Ask       *AskRequest  `json:"ask,omitempty"`
}
```

**Interpretation:**
- If `ToolCalls` is set (len > 0), it takes precedence over `ToolCall`
- If only `ToolCall` is set, it behaves as v1 (single tool call)
- Maximum sequence length: `max_plan_steps` (default: 5, configurable)

### 6.3 Execution Semantics

When the planner returns a `ToolCalls` sequence:

1. Execute tools in order, one at a time (parallel tool execution is not supported in v2)
2. After each tool call: store result in memory, run policy check, check limits, update metrics
3. If any tool call fails (error or policy violation): stop the sequence, return to the planner with all results so far in memory
4. If all tool calls succeed: return to the planner for the next decision
5. Each tool call in the sequence counts as a separate step toward `max_steps`
6. The planner is NOT called between tool calls in the sequence — that's the point

### 6.4 Planner Prompt Changes

The output format section adds:

```
Multi-step plan: {"type":"tool","reasoning":"...","tool_calls":[{"name":"...","input":{...}},{"name":"...","input":{...}}]}
```

The instructions section adds:

```
- When multiple tool calls are needed and their inputs don't depend on each other's outputs, return them as a tool_calls array.
- Each tool in the sequence is executed in order. If one fails, the rest are skipped and you will be asked to replan.
- Limit sequences to straightforward operations. Do not chain calls where later inputs depend on earlier outputs.
```

### 6.5 Config Changes

```yaml
planner:
  # ... existing fields ...
  max_plan_steps: 5          # maximum tool calls in a single plan sequence; 1 disables multi-step
```

When `max_plan_steps` is 1, multi-step planning is disabled. Sequences longer than the limit are rejected as a planner error — the agent does not execute partial sequences. The error message includes the limit, allowing the planner to produce a shorter plan on retry.

### 6.6 Loop Detection

Each tool call in a sequence is individually checked by the loop detector. A sequence of repeated calls triggers detection the same as individual repeated calls would.

### 6.7 Acceptance Criteria

1. Planner can return a `tool_calls` array with multiple tool calls
2. Tool calls execute in order within a single planning round
3. Failed tool call stops the sequence and returns to planner
4. Each tool call counts as a step toward `max_steps`
5. Policy checks run before each tool call in the sequence
6. Loop detection evaluates each call individually
7. Sequences longer than `max_plan_steps` are rejected as planner errors
8. `max_plan_steps: 1` disables multi-step planning
9. Single `tool_call` (v1 format) continues to work
10. Memory contains results from all executed tools in the sequence

---

## 7. HTTP API Server

### 7.1 Problem

matter is CLI-only. Deploying it as a service (behind a load balancer, called from other applications, managed by orchestrators) requires an HTTP API.

### 7.2 Design

A new `cmd/matter-server/main.go` binary provides an HTTP API. It uses the same `internal/runner` package as the CLI. The server is stateless per request — each run creates a fresh runner.

### 7.3 API Endpoints

#### POST /api/v1/runs

Start a new agent run.

**Request:**
```json
{
  "task": "Summarize the repository structure",
  "workspace": "/path/to/project",
  "config_overrides": {
    "agent": {"max_steps": 10},
    "tools": {"enable_command_exec": false}
  }
}
```

**Response (202 Accepted):**
```json
{
  "run_id": "run-1776167276277864000",
  "status": "running"
}
```

Runs execute asynchronously. The server holds active runs in memory.

#### GET /api/v1/runs/{run_id}

Get run status and result.

**Response (running):**
```json
{
  "run_id": "run-...",
  "status": "running",
  "steps": 3,
  "tokens": 1200,
  "cost_usd": 0.0150,
  "elapsed": "4.2s"
}
```

**Response (completed):**
```json
{
  "run_id": "run-...",
  "status": "completed",
  "success": true,
  "final_summary": "The repository has 14 packages...",
  "steps": 7,
  "total_tokens": 3400,
  "total_cost_usd": 0.0380,
  "duration": "12.4s"
}
```

**Response (failed):**
```json
{
  "run_id": "run-...",
  "status": "failed",
  "error": "limit exceeded: max_cost_usd",
  "steps": 5,
  "total_tokens": 2100,
  "total_cost_usd": 3.01,
  "duration": "8.1s"
}
```

#### GET /api/v1/runs/{run_id}/events

Server-Sent Events stream of progress events. Each event is a JSON-encoded `ProgressEvent` (from Section 4).

```
event: planner_completed
data: {"run_id":"run-...","step":1,"event":"planner_completed","data":{"tokens":450,"cost":0.005},"timestamp":"..."}

event: tool_started
data: {"run_id":"run-...","step":1,"event":"tool_started","data":{"tool":"workspace_read"},"timestamp":"..."}
```

The stream closes when the run completes or fails.

#### DELETE /api/v1/runs/{run_id}

Cancel a running or paused task. Returns 200 if cancelled, 404 if not found, 409 if already completed or failed.

The run's context is cancelled, causing the current step to abort. The run result reflects cancellation.

#### GET /api/v1/tools

List registered tools.

**Response:**
```json
{
  "tools": [
    {"name": "workspace_read", "description": "...", "safe": true, "side_effect": false},
    {"name": "workspace_write", "description": "...", "safe": false, "side_effect": true}
  ]
}
```

#### GET /api/v1/health

Liveness check.

**Response:**
```json
{"status": "ok", "version": "0.2.0"}
```

### 7.4 Server Configuration

```yaml
server:
  listen_addr: ":8080"         # bind address
  max_concurrent_runs: 10      # limit active goroutines (running runs)
  max_paused_runs: 20          # limit paused runs held in memory; new pauses beyond this are cancelled
  run_retention: 1h            # how long to keep completed run results in memory
  auth_token: ""               # optional bearer token for API auth; empty = no auth
```

### 7.5 Authentication

If `server.auth_token` is set, all endpoints except `/api/v1/health` require `Authorization: Bearer <token>`. Requests without the correct token receive 401.

### 7.6 Run Lifecycle

Runs are stored in a thread-safe `map[string]*ActiveRun`:

```go
type ActiveRun struct {
    mu       sync.Mutex
    RunID    string
    Status   string // "running", "completed", "failed", "cancelled"
    Result   matter.RunResult
    Cancel   context.CancelFunc
    Events   []matter.ProgressEvent
    Created  time.Time
}
```

Completed and failed runs are retained for `run_retention` duration, then garbage collected by a background ticker. Paused runs that have not received an answer within `run_retention` are cancelled and garbage collected — the pause timer starts when the run enters the paused state.

### 7.7 Concurrency

- `max_concurrent_runs` limits active goroutines. Paused runs do not count toward this limit (they hold memory but no goroutine). New requests beyond the limit receive 429 Too Many Requests.
- Each run executes in its own goroutine with an isolated runner.
- The events endpoint uses a channel-per-subscriber model. Events are buffered (capacity 100); slow consumers miss intermediate events rather than blocking the agent. Terminal events (`run_completed`, `run_failed`, `run_paused`) use best-effort delivery with a 5-second write timeout per subscriber. If a subscriber cannot receive the event within the timeout, the event is dropped for that subscriber and the connection is closed. Terminal events are delivered asynchronously — they do not block the agent loop or progress callbacks.

### 7.8 Conversation Mode Integration

If conversation mode is enabled (`max_asks > 0`) and the run pauses:

- Run status becomes `"paused"`
- GET response includes the question
- A new endpoint `POST /api/v1/runs/{run_id}/answer` accepts `{"answer": "..."}` and resumes the run

### 7.9 Implementation Constraints

- Use `net/http` standard library. No web framework dependency.
- JSON encoding/decoding via `encoding/json`.
- SSE implemented manually (write headers, flush after each event).
- Graceful shutdown on SIGTERM: stop accepting new runs, wait for active runs (up to 30s), then force cancel.

### 7.10 Acceptance Criteria

1. `POST /api/v1/runs` starts a run and returns 202 with run ID
2. `GET /api/v1/runs/{id}` returns current status and metrics
3. `GET /api/v1/runs/{id}/events` streams SSE events in real time
4. `DELETE /api/v1/runs/{id}` cancels a running task
5. `GET /api/v1/tools` lists registered tools
6. `GET /api/v1/health` returns 200 with version
7. Bearer token authentication works when configured
8. Concurrent run limit enforced with 429 response
9. Completed runs are garbage collected after retention period
10. Graceful shutdown waits for active runs
11. Paused runs expose question and accept answer via API

---

## 8. MCP Tool Adapter

### 8.1 Problem

The matter tool system requires Go code for each tool. The Model Context Protocol (MCP) defines a standard for external tool servers that expose tools via JSON-RPC over stdio or HTTP. Supporting MCP would instantly unlock tools for databases, APIs, file systems, and more without custom Go implementations.

### 8.2 Design

An MCP adapter connects to external MCP servers and registers their tools in the matter registry as native `matter.Tool` entries.

### 8.3 MCP Server Configuration

```yaml
tools:
  mcp_servers:
    - name: "github"
      transport: "stdio"          # "stdio" or "sse"
      command: "npx"              # for stdio transport
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:                        # optional env vars for the subprocess
        GITHUB_TOKEN: "${GITHUB_TOKEN}"
    - name: "database"
      transport: "sse"
      url: "http://localhost:3001/sse"
```

### 8.4 MCP Client

**File:** `internal/tools/mcp/client.go`

The MCP client implements the client side of the MCP protocol:

```go
type MCPClient struct {
    name      string
    transport Transport
}

type Transport interface {
    Send(ctx context.Context, method string, params any) (json.RawMessage, error)
    Close() error
}
```

**Transports:**
- `StdioTransport`: launches the server as a subprocess, communicates via stdin/stdout using JSON-RPC 2.0
- `SSETransport`: connects to an HTTP SSE endpoint, sends requests via HTTP POST

### 8.5 Tool Discovery

On initialization, the adapter calls `tools/list` on each MCP server:

```json
{"jsonrpc": "2.0", "method": "tools/list", "id": 1}
```

The response contains tool definitions with name, description, and JSON Schema for inputs. Each tool is converted to a `matter.Tool`:

```go
func mcpToolToMatterTool(serverName string, mcpTool MCPToolDef, client *MCPClient) matter.Tool {
    return matter.Tool{
        Name:        serverName + "." + mcpTool.Name,  // namespaced to avoid collisions
        Description: mcpTool.Description,
        InputSchema: mcpTool.InputSchema,              // MCP uses JSON Schema natively
        Timeout:     30 * time.Second,                 // configurable per-server
        Safe:        false,                            // MCP tools are untrusted by default
        SideEffect:  true,                             // assume side effects
        Execute:     mcpExecuteFunc(client, mcpTool.Name),
    }
}
```

### 8.6 Tool Execution

When the agent calls an MCP tool, the adapter sends `tools/call` to the server:

```json
{"jsonrpc": "2.0", "method": "tools/call", "params": {"name": "read_file", "arguments": {"path": "src/main.go"}}, "id": 2}
```

The response content is mapped to `matter.ToolResult`:

```go
func mcpExecuteFunc(client *MCPClient, toolName string) matter.ToolExecuteFunc {
    return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
        resp, err := client.transport.Send(ctx, "tools/call", map[string]any{
            "name":      toolName,
            "arguments": input,
        })
        if err != nil {
            return matter.ToolResult{Error: fmt.Sprintf("MCP call failed: %s", err)}, nil
        }
        // Parse MCP response content
        return parseMCPResult(resp)
    }
}
```

### 8.7 Lifecycle Management

- **Stdio servers** are started as subprocesses during runner initialization and killed on runner cleanup
- **SSE servers** are connected during initialization; the connection is closed on cleanup
- If an MCP server fails to start or connect, the runner logs a warning and continues without those tools (non-fatal)
- If an MCP server disconnects mid-run, tool calls to its tools return errors (recoverable, planner can replan)

### 8.8 Safety

- MCP tools are registered with `Safe: false` and `SideEffect: true` by default
- All MCP tool calls go through policy checks (workspace confinement, budget, etc.)
- MCP server subprocesses inherit a safe baseline environment (`PATH`, `HOME`, `TMPDIR`, `LANG`) plus any env vars explicitly listed in the server's `env` config. The full host environment is NOT inherited. If the server requires additional variables (e.g., `NODE_PATH`), they must be listed explicitly in the config
- Output from MCP tools is subject to the same `max_tool_result_chars` truncation as built-in tools
- MCP tool names are namespaced (`servername.toolname`) to prevent collisions with built-in tools and between servers

### 8.9 Config Changes

```go
type ToolsConfig struct {
    // ... existing fields ...
    MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

type MCPServerConfig struct {
    Name      string            `yaml:"name"`
    Transport string            `yaml:"transport"`   // "stdio" or "sse"
    Command   string            `yaml:"command"`     // stdio only
    Args      []string          `yaml:"args"`        // stdio only
    URL       string            `yaml:"url"`         // sse only
    Env       map[string]string `yaml:"env"`         // stdio only
    Timeout   time.Duration     `yaml:"timeout"`     // per-tool timeout override
}
```

### 8.10 Acceptance Criteria

1. Stdio MCP server is started and tools are discovered via `tools/list`
2. SSE MCP server is connected and tools are discovered
3. MCP tools appear in `matter tools` output with namespaced names
4. Agent can call MCP tools and receive results
5. MCP tool failures are recoverable (planner replans)
6. Server startup failure logs warning but does not prevent runner creation
7. Server disconnect mid-run produces tool errors, not crashes
8. MCP subprocess environment is restricted to configured env vars
9. MCP tool output is subject to truncation limits
10. Multiple MCP servers can be configured simultaneously without name collisions

---

## Implementation Order

The features have the following dependency relationships:

```
1. LLM Providers        (no dependencies — unblocks real usage)
2. Configurable Prompt   (no dependencies — low effort)
3. Command Allowlist     (no dependencies — low effort)
4. Progress Callbacks    (no dependencies — low effort)
5. Conversation Mode     (depends on: Progress Callbacks for CLI integration)
6. Multi-Step Planning   (no dependencies — but benefits from real LLM providers)
7. MCP Tool Adapter      (no dependencies — independent subsystem)
8. HTTP API Server       (depends on: Progress Callbacks, Conversation Mode)
```

**Suggested phasing:**

- **Phase A:** Items 1-4 (all independent, can be parallelized)
- **Phase B:** Items 5-6 (extend planner and agent loop)
- **Phase C:** Items 7-8 (new subsystems)

---

## Compatibility

All v2 features are backward compatible with v1:

- A v1 config file works without modification (new fields have defaults)
- The `"mock"` provider continues to work identically
- Single `tool_call` decisions (v1 format) are still accepted
- `max_asks: 0` (default) disables conversation mode
- `max_plan_steps: 1` (or absent) disables multi-step planning
- Empty `mcp_servers` list means no MCP tools are loaded
- The HTTP server is a separate binary (`matter-server`) — the CLI is unchanged

---

## Testing Strategy

### Unit Tests

Each feature requires isolated unit tests with mock dependencies:

- LLM providers: httptest servers returning canned responses
- Configurable prompt: verify prompt construction with various config combinations
- Command allowlist: table-driven tests for allow/reject
- Progress callbacks: verify callback invocation count and event data
- Conversation mode: mock LLM returning ask decisions, verify pause/resume
- Multi-step planning: mock LLM returning tool_calls arrays, verify execution order
- MCP adapter: mock MCP server over stdio pipe, verify tool discovery and execution
- HTTP server: httptest.Server with full request/response cycle

### Integration Tests

- End-to-end run with OpenAI (gated behind `OPENAI_API_KEY` env var, skipped in CI without it)
- Multi-step plan with tool failure mid-sequence
- Conversation mode with multiple ask/resume cycles
- MCP server lifecycle (start, discover, call, disconnect, recover)
- HTTP API: start server, create run, poll status, stream events, cancel

### Safety Tests

- Command allowlist bypass attempts (path manipulation, symlinks)
- MCP tool environment isolation
- API authentication enforcement
- Concurrent run limit enforcement
