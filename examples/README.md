# matter examples

Usage examples for the matter CLI, HTTP API server, and Go embedding.

## Task examples

Runnable scripts for specific tasks. Each accepts a workspace path as the first argument (defaults to `.`). Some accept additional arguments for file paths, function names, etc.

```bash
# Run any task against your project
./examples/tasks/analyze-project.sh ./my-project
./examples/tasks/review-file.sh ./my-project internal/auth/handler.go
./examples/tasks/debug-test.sh ./my-project TestUserLogin internal/auth/handler_test.go
```

### Code analysis

| Task | Tools | Description |
|------|-------|-------------|
| [analyze-project.sh](tasks/analyze-project.sh) | read | Summarize what a project does, its language, and structure |
| [find-todos.sh](tasks/find-todos.sh) | read | Find TODO/FIXME/HACK/XXX comments with severity ratings |
| [analyze-deps.sh](tasks/analyze-deps.sh) | read | List dependencies and assess maintenance status |
| [map-modules.sh](tasks/map-modules.sh) | read | Map internal package imports, find circular dependencies |
| [find-dead-code.sh](tasks/find-dead-code.sh) | read | Find exported symbols never referenced outside their package |

### Code review

| Task | Tools | Description |
|------|-------|-------------|
| [review-file.sh](tasks/review-file.sh) | read | Review a file for bugs, races, and security issues |
| [review-errors.sh](tasks/review-errors.sh) | read | Audit error handling: ignored errors, missing context, panics |
| [scan-secrets.sh](tasks/scan-secrets.sh) | read | Scan for hardcoded secrets, API keys, and credentials |
| [review-sql.sh](tasks/review-sql.sh) | read | Check SQL queries for injection vulnerabilities |

### Code generation

These require `enable_workspace_write: true` (use `code-assistant.yaml` config).

| Task | Tools | Description |
|------|-------|-------------|
| [gen-gitignore.sh](tasks/gen-gitignore.sh) | read, write | Generate a .gitignore appropriate for the project |
| [gen-license.sh](tasks/gen-license.sh) | write | Create an MIT LICENSE file with custom holder name |
| [gen-test-stubs.sh](tasks/gen-test-stubs.sh) | read, write | Generate table-driven test stubs for a Go source file |
| [add-json-tags.sh](tasks/add-json-tags.sh) | read, write | Add snake_case JSON struct tags to Go structs |
| [gen-makefile.sh](tasks/gen-makefile.sh) | read, write | Create a Makefile with standard build targets |

### Documentation

| Task | Tools | Description |
|------|-------|-------------|
| [gen-api-docs.sh](tasks/gen-api-docs.sh) | read | Generate Markdown API docs from HTTP handler files |
| [explain-function.sh](tasks/explain-function.sh) | read | Explain a complex function for a new developer |
| [gen-changelog.sh](tasks/gen-changelog.sh) | read, exec | Write a CHANGELOG entry from recent git history |

### DevOps

These require `enable_command_exec: true` (use `code-assistant.yaml` config).

| Task | Tools | Description |
|------|-------|-------------|
| [check-module.sh](tasks/check-module.sh) | exec | Run `go mod tidy` and `go mod verify`, report issues |
| [run-tests.sh](tasks/run-tests.sh) | exec | Run tests, summarize pass/fail, show failures |
| [run-lint.sh](tasks/run-lint.sh) | exec | Run linter, categorize findings by severity |
| [diagnose-build.sh](tasks/diagnose-build.sh) | read, exec | Diagnose a build failure and suggest the fix |

### Research

These require `enable_web_fetch: true` (use `research-agent.yaml` config).

| Task | Tools | Description |
|------|-------|-------------|
| [lookup-package.sh](tasks/lookup-package.sh) | web | Look up a Go package and summarize its API |
| [check-github-release.sh](tasks/check-github-release.sh) | web | Check a GitHub repo's latest release |

### Multi-step

These chain multiple tool calls autonomously across read, write, and exec.

| Task | Tools | Description |
|------|-------|-------------|
| [audit-and-fix.sh](tasks/audit-and-fix.sh) | read, write, exec | Find untested error paths, add tests, verify |
| [rename-function.sh](tasks/rename-function.sh) | read, write, exec | Rename a function across the codebase, verify build and tests |
| [trace-request.sh](tasks/trace-request.sh) | read, write | Trace request lifecycle, write ARCHITECTURE.md |

### Conversation mode

These use `max_asks > 0` so the agent can ask clarifying questions interactively.

| Task | Tools | Description |
|------|-------|-------------|
| [interactive-setup.sh](tasks/interactive-setup.sh) | read, write | Set up a new project interactively |
| [guided-refactor.sh](tasks/guided-refactor.sh) | read, write | Review for refactoring, ask before changing |
| [debug-test.sh](tasks/debug-test.sh) | read, exec | Diagnose a failing test, ask for context |

## Config files

Ready-to-use configuration files for different providers and use cases.

| Config | Description |
|--------|-------------|
| [configs/openai.yaml](configs/openai.yaml) | OpenAI GPT-5.4 (read-only agent) |
| [configs/anthropic.yaml](configs/anthropic.yaml) | Anthropic Claude Sonnet 4.6 (read-only agent) |
| [configs/gemini.yaml](configs/gemini.yaml) | Google Gemini 2.5 Flash (read-only agent) |
| [configs/ollama.yaml](configs/ollama.yaml) | Local Ollama with Llama 3.3 (no API key needed) |
| [configs/ollama-remote.yaml](configs/ollama-remote.yaml) | Remote Ollama instance |
| [configs/code-assistant.yaml](configs/code-assistant.yaml) | Full-featured agent with file writes and command execution |
| [configs/research-agent.yaml](configs/research-agent.yaml) | Read-only agent with web access for research tasks |
| [configs/mcp-tools.yaml](configs/mcp-tools.yaml) | MCP external tool server integration |
| [configs/server-production.yaml](configs/server-production.yaml) | Production HTTP API server with auth and SQLite storage |

## Shell scripts

Annotated shell scripts demonstrating CLI and server API usage.

| Script | Description |
|--------|-------------|
| [cli-usage.sh](cli-usage.sh) | CLI commands, output capture, provider selection, env overrides |
| [server-usage.sh](server-usage.sh) | HTTP API with curl: create runs, stream SSE, cancel, resume |

## Go embedding

Runnable Go programs showing how to use the runner programmatically. These work within the matter module only (`internal/` packages).

| Example | Description |
|---------|-------------|
| [embedding/main.go](embedding/main.go) | Basic: create runner, execute task, handle result |
| [embedding/progress/main.go](embedding/progress/main.go) | Progress callbacks for real-time event logging |
| [embedding/conversation/main.go](embedding/conversation/main.go) | Conversation mode with pause/resume loop |

Build any example with:

```bash
go build ./examples/embedding/
go build ./examples/embedding/progress/
go build ./examples/embedding/conversation/
```
