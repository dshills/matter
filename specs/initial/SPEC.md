# SPEC.md — matter

## 1. Overview

**matter** is a Go-based autonomous AI agent framework for building reliable, observable, tool-using agents that can execute multi-step tasks with strong safety controls, deterministic testing, and production-grade operational visibility.

The system is intended for **real autonomous execution**, not just interactive chat. It must support iterative reasoning, structured tool calls, memory management, step-by-step execution, loop prevention, cost control, and clean deployment as a single Go binary.

This specification is written for a coding agent. The implementation must follow this document closely.

---

## 2. Product Goals

### 2.1 Primary Goals

matter must:

1. Accept a user task and execute it through an autonomous agent loop.
2. Allow the agent to call tools through structured, validated tool invocations.
3. Maintain short-term and summarized memory for context continuity.
4. Enforce hard limits on:
   - step count
   - execution duration
   - token usage
   - estimated API cost (computed from token usage and a built-in pricing table keyed by provider and model name)
5. Produce strong observability signals:
   - structured logs
   - per-step traces
   - tool call records
   - token and cost accounting
   - latency metrics
6. Be testable deterministically with mocked or replayed LLM responses.
7. Be implemented in idiomatic Go. Avoid external dependencies unless required for LLM provider clients, JSON Schema validation, or YAML parsing.

### 2.2 Non-Goals

matter is not initially:

1. A multi-tenant SaaS platform.
2. A workflow designer UI.
3. A long-term distributed cluster scheduler.
4. A general desktop assistant with unrestricted machine access.
5. A plugin marketplace.

These may be future extensions, but must not distort the initial implementation.

---

## 3. Core Principles

1. **Reliability over cleverness**  
   The agent must fail safely, visibly, and predictably.

2. **Observable by default**  
   Every step, tool call, LLM call, retry, and failure must be traceable.

3. **Controlled autonomy**  
   Autonomous does not mean unrestricted. Guardrails are first-class.

4. **Structured over free-form**  
   Tool calls and planner outputs must be strongly typed and schema validated.

5. **Simple architecture first**  
   Avoid a complex plugin system or unnecessary abstractions in v1.

6. **Deterministic testability**  
   The core loop must be testable without burning real API credits.

7. **Single-binary deployment**  
   The system should compile cleanly into a standalone service or CLI.

---

## 4. Primary Use Cases

### 4.1 Supported v1 Use Cases

1. Execute a multi-step task using an LLM planner and tool calls.
2. Use tools such as:
   - web fetch
   - file workspace read/write
   - command/code execution in a sandbox
3. Summarize older context when memory grows too large.
4. Track budget and abort when limits are exceeded.
5. Replay agent runs for debugging.
6. Run from:
   - CLI
   - library/package API
   - optional HTTP API (not required for v1)

### 4.2 Example Tasks

1. “Research this topic and write a summary into a file.”
2. “Read the files in this workspace and propose a change plan.”
3. “Generate code, run tests in a sandbox, and summarize failures.”
4. “Inspect inputs, call tools according to the planner's decision logic, and stop when the task is complete.”

---

## 5. High-Level Architecture

matter must be composed of the following core modules:

1. **Agent Core**
2. **Planner**
3. **Memory**
4. **Tool Registry**
5. **Tool Executor**
6. **LLM Client**
7. **Observer / Telemetry**
8. **Policy / Guardrails**
9. **Run Recorder / Replay**
10. **Configuration**
11. **CLI**

### 5.1 Architecture Diagram

```text
User Task
   |
   v
CLI / API
   |
   v
Agent Run Coordinator
   |
   +--> Planner (LLM decision engine)
   |
   +--> Memory Manager
   |
   +--> Tool Registry / Executor
   |
   +--> Policy Enforcement
   |
   +--> Observer / Tracing / Logging
   |
   +--> Recorder / Replay Store
```

---

## 6. Required Directory Structure

The repository should use this structure unless there is a very strong implementation reason to change it.

```text
matter/
├── cmd/
│   └── matter/
│       └── main.go
├── internal/
│   ├── agent/
│   │   ├── agent.go
│   │   ├── loop.go
│   │   ├── limits.go
│   │   ├── errors.go
│   │   └── loop_detector.go
│   ├── planner/
│   │   ├── planner.go
│   │   ├── schema.go
│   │   ├── parser.go
│   │   └── repair.go
│   ├── memory/
│   │   ├── memory.go
│   │   ├── summary.go
│   │   ├── window.go
│   │   └── store.go
│   ├── llm/
│   │   ├── client.go
│   │   ├── openai.go
│   │   ├── anthropic.go
│   │   ├── types.go
│   │   ├── retry.go
│   │   └── costing.go
│   ├── tools/
│   │   ├── registry.go
│   │   ├── executor.go
│   │   ├── types.go
│   │   ├── validation.go
│   │   └── builtin/
│   │       ├── workspace_read.go
│   │       ├── workspace_write.go
│   │       ├── web_fetch.go
│   │       └── command_exec.go
│   ├── observe/
│   │   ├── observer.go
│   │   ├── logging.go
│   │   ├── tracing.go
│   │   ├── metrics.go
│   │   └── recorder.go
│   ├── policy/
│   │   ├── policy.go
│   │   ├── budget.go
│   │   ├── filesystem.go
│   │   └── sandbox.go
│   ├── config/
│   │   ├── config.go
│   │   └── defaults.go
│   └── workspace/
│       ├── paths.go
│       └── guard.go
├── pkg/
│   └── matter/
│       ├── matter.go
│       └── types.go
├── testdata/
├── examples/
├── SPEC.md
├── README.md
├── Makefile
├── go.mod
└── go.sum
```

### 6.1 Core Module Interfaces

The following minimal interfaces define the contracts between core modules:

```go
// Planner produces decisions from context.
type Planner interface {
    Plan(ctx context.Context, messages []Message, tools []Tool) (Decision, error)
}

// MemoryManager handles context accumulation and summarization.
type MemoryManager interface {
    Add(msg Message)
    Context() []Message           // returns planner-ready context
    SummarizeIfNeeded(ctx context.Context) error
}

// ToolRegistry manages tool registration and lookup.
type ToolRegistry interface {
    Register(tool Tool) error
    Get(name string) (Tool, bool)
    List() []Tool                 // stable order
    Schemas() []byte              // JSON schemas for planner prompts
}
```

The `PolicyChecker` interface is defined in Section 11.2. The `Client` interface is defined in Section 12.5.

---

## 7. Agent Run Model

### 7.1 Agent Lifecycle

Each agent run must proceed through these phases:

1. Initialize run context.
2. Load configuration and limits.
3. Register tools.
4. Initialize memory with the user task.
5. Enter agent loop.
6. On each step:
   - build planner input
   - call LLM
   - parse and validate decision
   - enforce policy checks for tools where `Safe == false`
   - execute tool (if the decision type is `DecisionTypeTool`)
   - store result
   - update telemetry
   - evaluate stopping conditions
7. End with:
   - success
   - explicit completion
   - limit exceeded
   - policy violation
   - unrecoverable error

### 7.2 Required Agent Limits

The run must support all of the following hard limits:

- `max_steps`
- `max_duration`
- `max_prompt_tokens`
- `max_completion_tokens`
- `max_total_tokens`
- `max_cost_usd`
- `max_consecutive_errors` (counts consecutive non-retriable errors that return to the agent loop; retried LLM errors do not increment this counter)
- `max_repeated_tool_calls` (if a tool is called with identical name and arguments at least this many times within a sliding window of the last 2N steps where N equals this value, it is an independent hard limit that terminates the run with `limit_exceeded_error`)
- `max_consecutive_no_progress` (counts consecutive steps with no progress, including error steps)

All limits must be configurable. Limits are evaluated in the order listed above after each step. When multiple limits are exceeded simultaneously, the first exceeded limit in this list determines the reported error.

---

## 8. Planner Specification

### 8.1 Planner Role

The planner is the LLM-driven decision engine. On each step it must decide one of:

1. Call a tool (`DecisionTypeTool`)
2. Complete the task with a final answer (`DecisionTypeComplete`)
3. Fail if the task cannot be completed with available context (`DecisionTypeFail`)

The planner must proceed with best effort using available context. Do not build a human clarification workflow into the autonomous loop for v1.

### 8.2 Planner Output Contract

Planner outputs must be parsed into a strongly typed structure.

```go
type DecisionType string

const (
    DecisionTypeTool     DecisionType = "tool"
    DecisionTypeComplete DecisionType = "complete"
    DecisionTypeFail     DecisionType = "fail"
)

type Decision struct {
    Type       DecisionType     `json:"type"`
    Reasoning  string           `json:"reasoning"`
    ToolCall   *ToolCall        `json:"tool_call,omitempty"`
    Final      *FinalAnswer     `json:"final,omitempty"`
}
```

### 8.3 Tool Call Contract

```go
type ToolCall struct {
    Name   string                 `json:"name"`
    Input  map[string]interface{} `json:"input"`
}
```

### 8.4 Final Answer Contract

```go
type FinalAnswer struct {
    Summary string `json:"summary"`
}
```

### 8.5 Planner Output Requirements

1. Planner output must be valid JSON.
2. No markdown code fences may be present in the final parsed object.
3. Output must be schema validated.
4. Invalid output must trigger a repair path:
   - first attempt: parse directly
   - second attempt: repair using local cleanup (strip markdown code fences, trim leading/trailing whitespace, fix trailing commas before `}` or `]`, append missing closing braces/brackets to the end of the string)
   - third attempt: send correction prompt to LLM (this call follows the same retry and error classification rules as a standard planner call; its token usage counts toward run limits, but it does not increment the step counter; at most one correction attempt is allowed per step)
   - final failure: return structured planner error

### 8.6 Planner Prompting Requirements

The planner prompt must include:

1. User task
2. Relevant memory context
3. Available tools and schemas
4. Current limits and remaining budget
5. Instructions:
   - do not invent tools
   - do not repeat failed steps blindly
   - complete when enough information is available
   - prefer minimal tool usage needed to finish the task

---

## 9. Memory Specification

### 9.1 Memory Goals

The memory subsystem must prevent uncontrolled context growth while preserving useful task continuity.

### 9.2 Memory Layers

Implement these conceptual layers:

1. **Recent Message Window**
   - the initial system role message is always pinned and excluded from windowing/summarization
   - the last N user/planner/tool messages are kept verbatim (configured by `recent_messages`), unless token limits force the window to shrink (see below)

2. **Historical Summary**
   - summarized representation of older context
   - when the total message count reaches `summarize_after_messages`, all messages outside the recent window are summarized and replaced with the summary before new messages are added
   - `summarize_after_messages` must be strictly greater than `recent_messages` (so there are always messages outside the window to summarize); if this condition is not met, the system must return a `configuration_error` and refuse to start
   - if `summarize_after_tokens` is reached but the message count is within the recent window, reduce the effective window size by evicting the oldest messages until the token count is below the threshold (the system message and at least the 3 most recent messages are always preserved); evicted messages are summarized using a separate LLM call (using the configured `summarization_model`) outside the planner context before being replaced with the summary

3. **Run Metadata**
   - limits, budget spent, step count, timestamps

4. **Optional Long-Term Store**
   - not required in v1, but interfaces should permit future extension

### 9.3 Required Memory Behaviors

1. Add user/system/tool/planner messages.
2. Provide planner-ready context.
3. Summarize old messages when message count crosses `summarize_after_messages` or estimated token count crosses `summarize_after_tokens`. The token usage and cost of summarization LLM calls must count toward the run's hard limits (`max_total_tokens`, `max_cost_usd`). Summarization calls do not increment the step counter.
4. Retain recent steps in full.
5. Record tool results with truncation policy if outputs are too large.

### 9.4 Message Types

The canonical `Message` type is defined once and shared across all modules (memory, planner, LLM client). The LLM client must map `RolePlanner` to the provider's `"assistant"` role and `RoleTool` to the provider's `"tool"` role when constructing API requests.

```go
type MessageRole string

const (
    RoleUser    MessageRole = "user"
    RoleSystem  MessageRole = "system"
    RolePlanner MessageRole = "assistant"  // maps to LLM provider's assistant role
    RoleTool    MessageRole = "tool"
)

type Message struct {
    Role      MessageRole `json:"role"`
    Content   string      `json:"content"`
    Timestamp time.Time   `json:"timestamp"`
    Step      int         `json:"step"`
}
```

### 9.5 Summarization Requirements

Summarization must:

1. Preserve factual intermediate results.
2. Preserve outstanding goals and unresolved issues.
3. Preserve tool failures relevant to future planning.
4. Never silently discard budget or limit state.

### 9.6 Output Truncation Policy

Tool outputs exceeding configured thresholds must be:

1. truncated for prompt inclusion
2. stored fully in run records where possible
3. summarized for memory use

---

## 10. Tool System Specification

### 10.1 Tool Model

Each tool must define:

- name
- description
- input schema
- execution function
- timeout
- side effect flag (`SideEffect bool` — true if the tool modifies external state such as files, network, or processes)
- safety flag (`Safe bool` — true if the tool is read-only with no side effects; false if it requires policy checks before execution)

### 10.2 Tool Execution Function

```go
// ToolResult holds the output of a tool execution.
type ToolResult struct {
    Output string `json:"output"`
    Error  string `json:"error,omitempty"`
}

// ToolExecuteFunc is the function signature for tool execution.
// It receives a context (for timeout/cancellation) and validated input parameters.
type ToolExecuteFunc func(ctx context.Context, input map[string]interface{}) (ToolResult, error)
```

### 10.3 Tool Type

```go
type Tool struct {
    Name        string
    Description string
    InputSchema []byte           // Must be a valid JSON Schema document
    Timeout     time.Duration
    Safe         bool
    SideEffect   bool
    FatalOnError bool             // If true, execution errors terminate the run
    Execute      ToolExecuteFunc
}
```

### 10.4 Tool Execution Rules

1. Only registered tools may be called.
2. Inputs must be validated before execution.
3. Each tool call must have:
   - step ID
   - call ID
   - timeout
   - start/end timestamps
4. Tool errors must be captured as structured results.
5. Tool failures should generally return to the agent loop unless policy marks them fatal.

### 10.5 Required Built-In Tools for v1

#### 10.5.1 Workspace Read
- Read files only from the designated workspace root.
- Deny path traversal.

#### 10.5.2 Workspace Write
- Write files only within workspace root.
- Reject writes to existing files unless an explicit `overwrite` flag is set in the tool input.

#### 10.5.3 Web Fetch
- Fetch from URLs matching the configured `web_fetch_allowed_domains` list. If the list is empty, all requests must be rejected. If non-empty, only requests to listed domains are permitted.
- Must have timeouts and response size limits. If a response exceeds `max_web_response_bytes`, the tool must return a `ToolResult` with the truncated content appended with a truncation notice (e.g., `\n[TRUNCATED at 512KB]`) in the `Output` field. The `Error` field must remain empty so the step is recognized as progress.

#### 10.5.4 Command Exec
- Execute commands only in a sandboxed environment.
- Must have runtime limits. CPU and memory limits are best-effort (enforced where the OS supports it).
- Network must be configurable and off by default.

### 10.6 Tool Registry Requirements

The registry must support:

1. registration
2. lookup by name
3. schema export for planner prompts
4. duplicate name rejection
5. listing tools in stable order

---

## 11. Sandbox and Safety Specification

### 11.1 Safety Goals

The system must not allow unrestricted code execution, arbitrary filesystem access, or runaway cost/loop behavior.

### 11.2 Policy Enforcement Interface

The policy module must expose the following interface:

```go
type PolicyResult struct {
    Allowed bool   `json:"allowed"`
    Reason  string `json:"reason,omitempty"` // explanation if denied
}

type PolicyChecker interface {
    // CheckToolCall evaluates whether a tool call is permitted given the current run state.
    CheckToolCall(ctx context.Context, tool Tool, input map[string]interface{}) PolicyResult
}
```

The agent loop must call `CheckToolCall` before executing any tool where `Safe == false`. If `Allowed` is false, the tool must not be executed and a `policy_violation_error` must be returned to the agent loop. Policy checks must evaluate: workspace path confinement, budget remaining, and any tool-specific restrictions.

### 11.3 Required Guardrails

1. Workspace path confinement
2. Tool allowlist
3. Per-tool timeout
4. Run timeout
5. Step limit
6. Cost limit
7. Loop detection
8. Repeated-call detection
9. Response size limits
10. Optional network restrictions

### 11.4 Command Execution Sandbox

If command execution is implemented, it must run inside a contained environment with:

- working directory set to workspace
- wall clock timeout (enforced via `context.WithTimeout`)
- output size cap (enforced by limiting bytes read from stdout/stderr)
- working directory set to workspace

Resource limits (CPU, memory, process count) are best-effort for v1:
- On Linux, use cgroups or `ulimit`-style mechanisms if available.
- On other platforms, rely on wall clock timeout as the primary safety mechanism.
- The system must log a warning at startup if configured resource limits cannot be enforced on the current platform.
- Docker or container-based sandboxing is a future extension and must not be required for v1.

For v1, implementation must use a **restricted subprocess policy** with the following enforced constraints:
- working directory set to the workspace root (no escape)
- wall clock timeout via `context.WithTimeout`
- stdout/stderr output capped at `max_output_bytes` (the process continues running but output beyond the cap is discarded; the returned `ToolResult.Output` includes a `\n[OUTPUT TRUNCATED]` notice; the `Error` field remains empty so truncation is not treated as an error)
- network access disabled by default (configurable)
- stdin closed immediately (no interactive input)

The standard library `os/exec` package is the expected mechanism.

### 11.5 Filesystem Rules

The system must reject:

- absolute paths outside workspace
- `..` traversal
- symlink escape if applicable
- writes to hidden/protected paths (files or directories starting with `.`, e.g., `.env`, `.git/`, `.matter/`, `.ssh/`) unless the path is explicitly listed in a configured `allowed_hidden_paths` list

---

## 12. LLM Client Specification

### 12.1 Requirements

The LLM layer must support:

1. provider abstraction
2. request/response normalization
3. retries with backoff
4. timeout handling
5. token usage reporting
6. cost estimation via a built-in pricing table
7. provider-specific implementations

### 12.1.1 Pricing Table

Cost estimation must use a built-in pricing table embedded in the `internal/llm/costing.go` module. The table maps `(provider, model)` pairs to per-token costs:

```go
type ModelPricing struct {
    Provider           string
    Model              string
    PromptCostPer1K    float64  // cost per 1,000 prompt tokens in USD
    CompletionCostPer1K float64 // cost per 1,000 completion tokens in USD
}
```

The table must include pricing for the following v1-supported models at minimum:

| Provider | Model | PromptCostPer1K (USD) | CompletionCostPer1K (USD) |
|---|---|---|---|
| OpenAI | gpt-4o | 0.0025 | 0.0100 |
| OpenAI | gpt-4o-mini | 0.000150 | 0.000600 |
| Anthropic | claude-sonnet-4-20250514 | 0.003 | 0.015 |

If the configured model is not found in the table, the system must refuse to start the run and return a configuration error. This prevents unmonitored cost usage. The table must be updatable by editing the source file — no external API or runtime fetch is required for v1.

### 12.2 v1 Providers

At minimum, define interfaces that support multiple providers. The OpenAI provider must be implemented in v1.

### 12.3 Request Type

The LLM Request uses the canonical `Message` type from Section 9.4. The LLM client is responsible for converting `MessageRole` values to provider-specific role strings when constructing API requests.

```go
type Request struct {
    Model       string    `json:"model"`
    Messages    []Message `json:"messages"`  // uses canonical Message from Section 9.4
    MaxTokens   int       `json:"max_tokens"`
    Temperature float64   `json:"temperature"`
}
```

### 12.4 Response Type

```go
type Response struct {
    Content          string        `json:"content"`
    PromptTokens     int           `json:"prompt_tokens"`
    CompletionTokens int           `json:"completion_tokens"`
    TotalTokens      int           `json:"total_tokens"`
    EstimatedCostUSD float64       `json:"estimated_cost_usd"`
    Provider         string        `json:"provider"`
    Model            string        `json:"model"`
    Latency          time.Duration `json:"latency"`
}
```

### 12.5 Client Interface

```go
type Client interface {
    Complete(ctx context.Context, req Request) (Response, error)
}
```

### 12.6 Required Response Data

The LLM client returns raw text in `Response.Content`. Decision parsing is the responsibility of the **Planner** module, not the LLM client. The LLM client must provide:

- raw text (`Content`)
- prompt tokens
- completion tokens
- total tokens
- estimated cost
- provider/model info
- latency

### 12.7 Retry Rules

Retry on transient failures such as:

- timeout
- rate limit
- temporary upstream server errors

Do not retry blindly on:

- invalid auth
- malformed request
- repeated schema parse failure without correction path

---

## 13. Observability Specification

### 13.1 Logging

All runtime logging must be structured.

Preferred format:
- JSON logs

Each log event should include where relevant:

- run ID
- step number
- component
- tool name
- latency
- token usage
- cost
- error code/message

### 13.2 Tracing

Each run must emit step-level tracing information, including:

1. planner request started
2. planner response received
3. tool call started
4. tool call completed
5. retry performed
6. summary generated
7. limit exceeded
8. run completed

### 13.3 Metrics

At minimum, record:

- runs_started_total
- runs_completed_total
- runs_failed_total
- tool_calls_total
- tool_failures_total
- llm_calls_total
- llm_failures_total
- run_duration_seconds
- tool_duration_seconds
- step_count
- total_tokens
- total_cost_usd

### 13.4 Recorder / Replay

The system must record each run to disk or another store with:

- input task
- config snapshot
- planner prompts
- raw LLM responses
- parsed decisions
- tool calls and outputs
- errors
- final outcome

This is required for debugging and replay.

---

## 14. Loop Detection and Budget Control

### 14.1 Infinite Loop Prevention

The agent must detect repeated behavior patterns, including:

1. identical tool call repeated with same arguments multiple times
2. same planner output repeatedly occurring without progress
3. repeated failures that do not change the plan

### 14.2 Budget Control

The system must stop when:
- estimated cost exceeds configured budget
- token budget is exceeded
- duration budget is exceeded

### 14.3 Progress Detection

A step is considered to have made progress if at least one of the following measurable conditions is true:

1. A tool call returned a non-error result different from the previous call's result, or a result containing a truncation notice (truncated results always count as progress).
2. A file in the workspace was created or modified.
3. The planner produced a `DecisionTypeComplete` or `DecisionTypeFail` decision.
4. The planner produced a `DecisionTypeTool` decision with a tool name or arguments different from the previous step.

A step is considered to have made **no progress** if it does not meet any of the progress conditions above. Specifically, a step has no progress if any of the following are true:

1. The tool call and arguments are identical to a previous call within a sliding window of the last 2N steps (where N is `max_repeated_tool_calls`) and the result is unchanged.
2. The planner output is identical to a previous step's output.
3. A tool call returned an error (errors do not constitute progress).

If the number of consecutive no-progress steps reaches `max_consecutive_no_progress` (configurable, default 3), the run must terminate with a `limit_exceeded_error`.

---

## 15. Error Handling Specification

### 15.1 Errors Must Be Typed

Define explicit error categories, including:

- planner_error
- llm_error
- tool_validation_error
- tool_execution_error
- timeout_error
- limit_exceeded_error
- policy_violation_error
- parse_error
- replay_error
- sandbox_resource_error
- configuration_error

### 15.2 Failure Semantics

Errors must be classified as:

- **retriable** — transient failures that should be retried with backoff
- **recoverable** — non-transient failures returned to the agent loop for replanning
- **terminal** — fatal failures that immediately end the run

Classification mapping:

| Error Type | Classification |
|---|---|
| llm_error (timeout, rate limit, 5xx) | retriable |
| llm_error (auth, 4xx non-rate-limit) | terminal |
| planner_error | recoverable |
| parse_error | recoverable |
| tool_validation_error | recoverable |
| tool_execution_error | recoverable (terminal if policy marks the tool as fatal-on-error) |
| sandbox_resource_error | recoverable |
| timeout_error (tool) | recoverable |
| timeout_error (run) | terminal |
| limit_exceeded_error | terminal |
| policy_violation_error | terminal |
| replay_error | terminal |

### 15.3 Agent Behavior on Error

1. Retriable LLM errors -> retry with backoff
2. Tool validation error -> return error to memory and allow replanning
3. Tool execution error -> record and allow replanning unless fatal
4. Policy violation -> terminate immediately
5. Budget or limit exceeded -> terminate immediately
6. Sandbox resource exhaustion (OOM, CPU limit, process limit) -> treat as recoverable sandbox_resource_error, record and allow replanning

---

## 16. Configuration Specification

### 16.1 Config Sources

Support:
1. config file
2. environment variables
3. CLI flags

Order of precedence:
1. CLI flags
2. environment
3. config file
4. defaults

### 16.2 Example Configuration

```yaml
agent:
  max_steps: 20
  max_duration: 2m
  max_prompt_tokens: 40000
  max_completion_tokens: 10000
  max_total_tokens: 50000
  max_cost_usd: 3.00
  max_consecutive_errors: 3
  max_repeated_tool_calls: 2
  max_consecutive_no_progress: 3

memory:
  recent_messages: 10
  summarize_after_messages: 15
  summarize_after_tokens: 16000
  summarization_model: gpt-4o-mini  # cheaper model used for context summarization
  max_tool_result_chars: 8000
  max_context_chars: 128000

llm:
  provider: openai
  model: gpt-4o
  timeout: 30s
  max_retries: 3

tools:
  enable_workspace_read: true
  enable_workspace_write: true
  enable_web_fetch: true
  enable_command_exec: false
  web_fetch_allowed_domains: []  # empty = allow none (reject all); non-empty = allowlist
  allowed_hidden_paths: []       # hidden paths allowed for write (e.g., [".config/myapp"])

sandbox:
  command_timeout: 20s
  memory_mb: 256
  cpu_shares: 1
  network_enabled: false
  max_output_bytes: 1048576      # 1 MB cap on command output
  max_web_response_bytes: 524288 # 512 KB cap on web fetch responses

observe:
  log_level: info
  record_runs: true
  record_dir: .matter/runs
```

---

## 17. CLI Specification

### 17.1 CLI Goals

The CLI must provide a practical way to run matter locally.

### 17.2 Required Commands

#### `matter run`
Run a task.

Example:
```bash
matter run --task "Read the repo and summarize the architecture" --workspace .
```

#### `matter replay`
Replay a prior run record. Replay must use the recorded LLM responses and recorded tool outputs from the run recording. It must **never** re-execute tools or call the LLM. This ensures replay is safe, deterministic, and side-effect-free. Replay verifies that the agent loop logic (parsing, decision routing, limit enforcement) produces the same outcome given the same recorded inputs.

#### `matter tools`
List registered tools.

#### `matter config`
Print effective config.

### 17.3 CLI Output

CLI should provide human-readable progress while still preserving structured logs if configured.

At minimum, output:
- run started
- step number
- tool calls
- final status
- summary

---

## 18. Public Package API

The project should expose a minimal package API for embedding.

Example target usage:

```go
runner, err := matter.New(cfg)
if err != nil {
    return err
}

result, err := runner.Run(ctx, matter.RunRequest{
    Task:      "Summarize the repository",
    Workspace: ".",
})
if err != nil {
    return err
}

fmt.Println(result.FinalSummary)
```

---

## 19. Testing Specification

### 19.1 Required Test Types

#### Unit Tests
For:
- planner parsing
- memory summarization behavior
- tool validation
- path safety
- budget logic
- loop detection
- config loading

#### Integration Tests
For:
- full agent loop with mocked LLM
- successful tool-driven completion
- tool failure and recovery
- limit exceeded behavior
- replay of recorded runs

#### Safety Tests
For:
- path traversal rejection
- command timeout enforcement
- output truncation
- repeated tool loop detection

### 19.2 Mock LLM Requirement

A mock LLM client must exist for deterministic tests. It should return a predefined sequence of responses and allow assertions on call count and prompts.

### 19.3 Replay Tests

Recorded runs should be replayable to verify:
- decision sequence
- tool sequence
- final result
- failure reproduction

---

## 20. Acceptance Criteria

The implementation is acceptable only if all of the following are true.

### 20.1 Core Run Criteria

1. A task can be submitted through CLI.
2. The agent enters a multi-step loop.
3. The planner can return:
   - tool call
   - complete
   - fail
4. Tools execute through a registry.
5. Memory accumulates and summarizes context when needed.
6. The run terminates correctly on completion.

### 20.2 Guardrail Criteria

1. Infinite or repeated tool loops are detected and terminated.
2. Budget overrun terminates the run.
3. Workspace path traversal is blocked.
4. Tool execution timeouts are enforced.
5. Invalid planner JSON does not crash the process.

### 20.3 Observability Criteria

1. Structured logs exist for each step.
2. LLM call token usage and cost are tracked.
3. Tool calls are recorded.
4. Runs can be recorded and replayed.

### 20.4 Testing Criteria

1. The project includes deterministic tests using a mock LLM.
2. Safety-critical behaviors are covered by tests.
3. Core packages have meaningful test coverage.

---

## 21. Suggested Implementation Order

The coding agent should implement in this order:

1. config
2. types/interfaces
3. tool registry
4. workspace-safe file helpers
5. mock LLM
6. planner parsing/validation
7. memory window + summary interface
8. core agent loop
9. limits and loop detection
10. observer/logging/recording
11. CLI run command
12. built-in tools
13. replay support
14. provider-backed LLM implementation
15. sandboxed command execution

Do not start with provider complexity or sandbox sophistication. Get the core loop working first.

---

## 22. Implementation Constraints

1. Language: **Go**
2. Prefer standard library unless an external dependency materially improves safety, correctness, or maintainability.
3. Keep package boundaries clean and purposeful.
4. Do not introduce a heavyweight dynamic plugin system in v1.
5. Do not build unnecessary abstractions for hypothetical future scale.
6. Keep interfaces small and composable.
7. Favor explicit structs and typed errors over magic.

---

## 23. Future Extensions

Not required for v1, but architecture should not block them:

1. multi-agent coordination
2. MCP tool adapters
3. vector memory backends
4. HTTP API server
5. UI dashboard
6. richer approval workflows
7. cost analytics dashboard
8. artifact management
9. distributed run execution

---

## 24. Final Guidance for the Coding Agent

When implementing matter:

1. Build the narrowest system that fully satisfies this specification.
2. Prefer correctness, observability, and safety over fancy abstractions.
3. Keep the architecture understandable by a single engineer reading the repo cold.
4. Make it easy to test.
5. Make it hard for the agent to go off the rails.
6. Every “clever shortcut” that weakens control or visibility is probably a trap dressed as convenience.

That little trap is how you end up with an “autonomous agent” that autonomously burns money and lies about being done. Charming, but not shippable.
