# matter

Autonomous AI agent framework for building reliable, observable, tool-using agents with safety controls and deterministic testing.

matter accepts a task, enters an autonomous agent loop (plan via LLM, execute tools, store results, check limits), and terminates with a structured result. It ships as two Go binaries (`matter` CLI and `matter-server` HTTP API) with no external runtime dependencies.

## Features

- **Autonomous execution** -- step-based agent loop with structured tool calls, not interactive chat
- **Real LLM providers** -- OpenAI and Anthropic integrations with credential chain resolution, retry/backoff, and cost tracking
- **Safety controls** -- resource budgets (steps, duration, tokens, cost, consecutive errors, repeated tool calls) plus workspace confinement and policy enforcement
- **Built-in tools** -- file read/write, web fetch (with domain allowlist and SSRF protection), command execution (with allowlist and restricted environment)
- **MCP tool support** -- connect external tools via Model Context Protocol (stdio and SSE transports)
- **HTTP API server** -- REST API with async run management, SSE event streaming, bearer token auth, and graceful shutdown
- **Conversation mode** -- pause/resume runs for human-in-the-loop interactions (ask_user decision type)
- **Multi-step planning** -- agents can plan and execute sequences of tool calls in a single LLM response
- **Progress callbacks** -- real-time event streaming for step progress, tool calls, and run lifecycle
- **Configurable prompts** -- customizable system prompt with prefix/suffix overrides and file-based templates
- **Observability** -- structured JSON logging, per-step tracing, in-memory metrics, run recording for replay
- **Memory management** -- sliding message window with automatic LLM-driven summarization
- **Deterministic testing** -- first-class mock LLM client for reproducible test runs
- **Minimal dependencies** -- Go standard library plus YAML parsing and JSON Schema validation

## Requirements

- Go 1.22 or later

## Installation

### From source

```bash
git clone https://github.com/dshills/matter.git
cd matter
make install
```

This installs the `matter` CLI and `matter-server` HTTP API server to your `$GOPATH/bin`.

### Build locally

```bash
make build          # builds matter CLI
make build-server   # builds matter-server
```

## Quick start

```bash
# Run a task with the mock LLM client (no API key needed)
matter run --task "List the files in the workspace" --workspace ./my-project --mock

# Run a task with OpenAI
export OPENAI_API_KEY=sk-...
matter run --task "Analyze the codebase" --workspace ./my-project

# Run a task with Anthropic
export ANTHROPIC_API_KEY=sk-ant-...
matter run --task "Analyze the codebase" --workspace ./my-project --config config.yaml

# Print the effective configuration
matter config

# List registered tools
matter tools

# Start the HTTP API server
matter-server --listen :8080
```

## CLI usage

```
matter <command> [flags]
```

### Commands

| Command  | Description                              |
|----------|------------------------------------------|
| `run`    | Execute an agent task                    |
| `config` | Print effective configuration as YAML   |
| `tools`  | List registered tools with safety flags  |
| `help`   | Show help message                        |

### `matter run`

```bash
matter run --task "Refactor the auth module" --workspace ./my-project --config config.yaml
```

| Flag          | Default | Description                                    |
|---------------|---------|------------------------------------------------|
| `--task`      | (required) | Task description for the agent              |
| `--workspace` | `.`     | Workspace directory the agent operates in      |
| `--config`    | (none)  | Path to YAML config file                       |
| `--mock`      | `false` | Use mock LLM client for testing                |

Output is JSON to stdout; progress logs go to stderr.

**Exit codes:** 0 = success, 1 = agent failure, 2 = config/argument error.

### `matter config`

```bash
matter config --config config.yaml
```

Loads the config file (or defaults), applies environment variable overrides, and prints the effective configuration as YAML.

### `matter tools`

```bash
matter tools --config config.yaml
```

Prints a table of registered tools with name, safety classification, side effect flag, and description.

## Configuration

Configuration is loaded with the following precedence (highest to lowest):

1. Environment variables (`MATTER_*` prefix)
2. YAML config file (`--config`)
3. Built-in defaults

### Example config file

```yaml
agent:
  max_steps: 20
  max_duration: 2m
  max_total_tokens: 50000
  max_cost_usd: 3.00
  max_consecutive_errors: 3
  max_repeated_tool_calls: 2

memory:
  recent_messages: 10            # messages kept after summarization
  summarize_after_messages: 15   # total messages before triggering summarization
  summarize_after_tokens: 16000
  summarization_model: gpt-4o-mini
  max_tool_result_chars: 8000

planner:
  system_prompt: "You are a careful coding assistant."  # or use system_prompt_file
  prompt_prefix: "Focus on safety."
  prompt_suffix: "Explain your reasoning."

llm:
  provider: openai   # openai, anthropic, or mock
  model: gpt-4o
  api_key: ""        # or set OPENAI_API_KEY / ANTHROPIC_API_KEY env var
  timeout: 30s
  max_retries: 3

tools:
  enable_workspace_read: true
  enable_workspace_write: true
  enable_web_fetch: true
  enable_command_exec: false
  command_allowlist:         # empty = all commands allowed (when enabled)
    - go
    - git
    - make
  web_fetch_allowed_domains:
    - api.github.com
    - docs.example.com
  allowed_hidden_paths:
    - .config
  mcp_servers:               # external MCP tool servers
    - name: my-tools
      transport: stdio       # stdio or sse
      command: my-mcp-server
      args: ["--verbose"]

sandbox:
  command_timeout: 20s
  max_output_bytes: 1048576
  max_web_response_bytes: 524288

observe:
  log_level: info
  record_runs: true
  record_dir: .matter/runs

server:                      # matter-server settings
  listen_addr: ":8080"
  auth_token: ""             # or set MATTER_SERVER_AUTH_TOKEN env var
  max_concurrent_runs: 10
  max_paused_runs: 20
  run_retention: 1h
```

### Environment variables

Every config field can be overridden with an environment variable. The pattern is `MATTER_<SECTION>_<FIELD>` in uppercase:

```bash
export MATTER_AGENT_MAX_STEPS=50
export MATTER_LLM_MODEL=gpt-4o-mini
export MATTER_TOOLS_ENABLE_COMMAND_EXEC=true
export MATTER_OBSERVE_LOG_LEVEL=debug
```

## Built-in tools

| Tool | Safe | Side Effect | Description |
|------|------|-------------|-------------|
| `workspace_read` | yes | no | Read files from the workspace. Large files are truncated. Hidden paths blocked unless allowed. |
| `workspace_write` | no | yes | Write files within the workspace. Atomic writes via temp file + sync + rename. Requires `overwrite=true` for existing files. |
| `web_fetch` | no | no | HTTP GET from allowed domains only. Redirect targets validated against allowlist. Responses truncated at limit. |
| `command_exec` | no | yes | Execute commands in the workspace. Disabled by default. Runs with a minimal environment. Supports command allowlist (`command_allowlist` config). |

> **Note:** When `command_allowlist` is empty and `command_exec` is enabled, all commands in PATH are permitted. Use the allowlist to restrict to specific commands in production.

### MCP tools

matter can connect to external [Model Context Protocol](https://modelcontextprotocol.io/) servers to extend its tool set. Configure MCP servers in the `tools.mcp_servers` config section. Both `stdio` and `sse` transports are supported. MCP tools are auto-registered into the tool registry with their original schemas.

## Architecture

```
    ┌──────────────┐       ┌───────────────┐
    │  CLI         │       │  HTTP Server  │
    │  cmd/matter  │       │  matter-server│
    └──────┬───────┘       └──────┬────────┘
           │                      │
           └──────────┬───────────┘
                      │
               ┌──────▼───────┐
               │    Runner    │
               │   internal/  │
               │    runner    │
               └──────┬───────┘
                      │
         ┌────────────▼────────────┐
         │         Agent           │
         │    internal/agent       │
         │                         │
         │  ┌─────────────────┐    │
         │  │   Step Loop     │    │
         │  │                 │    │
         │  │ 1. Check limits │    │
         │  │ 2. Plan (LLM)  │    │
         │  │ 3. Execute tool │    │
         │  │ 4. Store result │    │
         │  │ 5. Repeat      │    │
         │  └─────────────────┘    │
         └────────────┬────────────┘
                      │
    ┌─────────┬───────┼───────┬──────────┐
    │         │       │       │          │
┌───▼────┐┌───▼──┐┌───▼──┐┌───▼──┐┌──────▼──┐
│Planner ││Memory││Tools ││Policy││Observer │
│  LLM   ││      ││      ││      ││         │
│Decision││Window││Reg.  ││Budget││ Logger  │
│ Parse  ││Summ. ││Exec. ││Confin││ Tracer  │
│ Repair ││      ││Valid. ││      ││ Metrics │
└────────┘└──────┘│MCP   │└──────┘│Recorder │
                  └──────┘        │Progress │
                                  └─────────┘
```

### Module overview

| Package | Responsibility |
|---------|---------------|
| `cmd/matter` | CLI entry point with command routing |
| `cmd/matter-server` | HTTP API server binary |
| `pkg/matter` | Public shared types (Message, Decision, Tool, RunRequest, RunResult, ProgressEvent) |
| `internal/runner` | Wires all modules together; creates per-run agent with isolated state |
| `internal/agent` | Agent loop coordinator with lifecycle management, limit enforcement, and conversation mode (pause/resume) |
| `internal/planner` | LLM decision engine producing typed decisions (tool call, complete, fail, ask_user, plan) with JSON repair pipeline |
| `internal/memory` | Context management with sliding message window and automatic summarization |
| `internal/llm` | Provider-abstracted LLM client (OpenAI, Anthropic, mock) with retry, backoff, and cost estimation |
| `internal/tools` | Tool registry, executor, and JSON Schema input validation |
| `internal/tools/builtin` | Built-in tool implementations (workspace_read/write, web_fetch, command_exec) |
| `internal/tools/mcp` | MCP client and tool adapter (stdio and SSE transports) |
| `internal/server` | HTTP API server with async run management, SSE event streaming, and bearer token auth |
| `internal/observe` | Structured logging, event tracing, metrics counters, run recording, progress callbacks |
| `internal/policy` | Budget enforcement, filesystem confinement, tool restrictions |
| `internal/config` | Configuration loading (file + env overlay), validation, defaults |
| `internal/workspace` | Path safety (traversal prevention, symlink escape, workspace confinement) |
| `internal/errtype` | Typed error categories with classification (retriable, recoverable, terminal) |

### Agent loop

Each run follows this flow:

1. **Initialize** -- create memory manager, seed system prompt and task message
2. **Step loop** (up to `max_steps`):
   - Evaluate all limits (steps, duration, tokens, cost, errors, progress)
   - Build planner prompt with task, memory context, tool schemas, and budget info
   - Call LLM and parse the decision (with repair pipeline: direct parse, local cleanup, LLM correction)
   - If **tool call**: validate via policy, execute with timeout, store result in memory, check for loops
   - If **plan**: execute each step sequentially (multi-step planning)
   - If **ask_user**: pause the run and wait for human input (conversation mode)
   - If **complete** or **fail**: return result
3. **Finalize** -- record run trace, flush metrics, write run record to disk

### Key design decisions

- **Typed decisions** -- planner outputs are strongly typed JSON (`Decision` with `DecisionType` enum: tool_call, complete, fail, ask_user, plan), not free-form text
- **Per-run isolation** -- each `Run()` call creates a fresh tool registry, policy checker, and agent to prevent state leakage
- **Thread-safe observability** -- Observer (shared factory) + RunSession (per-run handle) pattern; logger and metrics are thread-safe across concurrent runs
- **Atomic file writes** -- workspace_write uses temp file + chmod + sync + rename for crash safety
- **SSRF prevention** -- web_fetch validates redirect targets against the domain allowlist
- **Restricted subprocess environment** -- command_exec runs with only PATH, HOME, and TMPDIR set; optional allowlist restricts available commands
- **Constant-time auth** -- HTTP server uses `crypto/subtle.ConstantTimeCompare` for bearer token validation
- **Cryptographic run IDs** -- `crypto/rand` for unpredictable run identifiers

## HTTP API server

`matter-server` exposes agent runs over HTTP with async execution and SSE event streaming.

```bash
# Start with defaults
matter-server

# With config and custom listen address
matter-server --config config.yaml --listen :9090

# With auth token (recommended for production)
export MATTER_SERVER_AUTH_TOKEN=my-secret-token
matter-server
```

### API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/health` | Server health check (no auth required) |
| `POST` | `/api/v1/runs` | Start a new agent run (async, returns 202) |
| `GET` | `/api/v1/runs/{id}` | Get run status, metrics, and result |
| `GET` | `/api/v1/runs/{id}/events` | SSE event stream (supports late-join replay) |
| `DELETE` | `/api/v1/runs/{id}` | Cancel a running or paused run |
| `POST` | `/api/v1/runs/{id}/answer` | Resume a paused run with user input |
| `GET` | `/api/v1/tools` | List registered tools |

All endpoints except `/health` require a `Bearer` token when `auth_token` is configured.

### Example: create and monitor a run

```bash
# Start a run
curl -X POST http://localhost:8080/api/v1/runs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task": "Analyze the codebase", "workspace": "/path/to/project"}'

# Stream events (SSE)
curl -N http://localhost:8080/api/v1/runs/run-abc123/events \
  -H "Authorization: Bearer $TOKEN"

# Check status
curl http://localhost:8080/api/v1/runs/run-abc123 \
  -H "Authorization: Bearer $TOKEN"
```

## Embedding as a library

### In-module usage example

> **Note:** The runner and supporting packages currently live under `internal/` and cannot be imported by external Go modules. A public embedding API is planned (see Roadmap). The example below applies only to code within this repository (e.g., custom `cmd/` binaries).

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/dshills/matter/internal/config"
    "github.com/dshills/matter/internal/llm"
    "github.com/dshills/matter/internal/runner"
    "github.com/dshills/matter/pkg/matter"
)

func main() {
    cfg := config.DefaultConfig()
    cfg.Agent.MaxSteps = 10
    cfg.LLM.Provider = "openai"
    cfg.LLM.Model = "gpt-4o"

    client, err := llm.NewClient(llm.ProviderConfig{
        Provider: cfg.LLM.Provider,
        APIKey:   cfg.LLM.APIKey,
        Model:    cfg.LLM.Model,
        Timeout:  cfg.LLM.Timeout,
    })
    if err != nil {
        log.Fatal(err)
    }

    r, err := runner.New(cfg, client)
    if err != nil {
        log.Fatal(err)
    }

    result := r.Run(context.Background(), matter.RunRequest{
        Task:      "Analyze the codebase structure",
        Workspace: "/path/to/project",
    })

    fmt.Printf("Success: %v\n", result.Success)
    fmt.Printf("Summary: %s\n", result.FinalSummary)
    fmt.Printf("Steps: %d, Tokens: %d, Cost: $%.4f\n",
        result.Steps, result.TotalTokens, result.TotalCostUSD)
}
```

## Development

```bash
# Run all checks
make all

# Run tests
make test

# Run a single test
go test ./internal/agent -run TestLoopDetection

# Lint
make lint

# Build CLI and server
make build
make build-server

# Install both binaries
make install

# Show all make targets
make help
```

## Project status

matter v2 is complete. All planned features are implemented: real LLM providers (OpenAI, Anthropic), configurable prompts, command allowlist, progress callbacks, conversation mode, multi-step planning, MCP tool support, and HTTP API server.

### Roadmap

- `matter replay` command for replaying recorded runs
- Public `pkg/` API for embedding as a library
- Persistent run storage for the HTTP server (currently ephemeral)
- Additional LLM providers

## License

See [LICENSE](LICENSE) for details.
