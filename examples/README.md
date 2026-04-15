# matter examples

Usage examples for the matter CLI, HTTP API server, and Go embedding.

## Config files

Ready-to-use configuration files for different providers and use cases.

| Config | Description |
|--------|-------------|
| [configs/openai.yaml](configs/openai.yaml) | OpenAI GPT-4o (read-only agent) |
| [configs/anthropic.yaml](configs/anthropic.yaml) | Anthropic Claude (read-only agent) |
| [configs/gemini.yaml](configs/gemini.yaml) | Google Gemini (read-only agent) |
| [configs/ollama.yaml](configs/ollama.yaml) | Local Ollama (no API key needed) |
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
