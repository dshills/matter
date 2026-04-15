#!/usr/bin/env bash
# matter CLI usage examples
# These examples demonstrate common CLI usage patterns.
# Each section is self-contained -- copy and adapt as needed.

set -euo pipefail

# ============================================================================
# Basic usage
# ============================================================================

# Run a task with mock LLM (no API key needed, great for testing)
matter run --task "List the files in the workspace" --workspace . --mock

# Run with OpenAI (requires OPENAI_API_KEY)
matter run --task "Analyze the codebase structure" --workspace ./my-project

# Run with a config file
matter run --task "Review this code for security issues" \
  --workspace ./my-project \
  --config config.yaml

# ============================================================================
# Capturing output
# ============================================================================

# matter writes JSON to stdout and progress to stderr.
# Capture the JSON result while seeing progress:
matter run --task "Count the Go files" --workspace . --mock > result.json
jq . result.json

# Suppress progress output:
matter run --task "Count the Go files" --workspace . --mock 2>/dev/null > result.json

# Extract specific fields with jq:
matter run --task "Summarize the README" --workspace . --mock 2>/dev/null \
  | jq -r '.final_summary'

# Check exit code for scripting:
if matter run --task "Verify the build" --workspace . --mock 2>/dev/null; then
  echo "Task succeeded"
else
  echo "Task failed"
fi

# ============================================================================
# Using different providers
# ============================================================================

# OpenAI (default provider)
export OPENAI_API_KEY=sk-...
matter run --task "Explain the architecture" --workspace .

# Anthropic Claude
export ANTHROPIC_API_KEY=sk-ant-...
matter run --task "Explain the architecture" --workspace . \
  --config examples/configs/anthropic.yaml

# Google Gemini
export GEMINI_API_KEY=AI...
matter run --task "Explain the architecture" --workspace . \
  --config examples/configs/gemini.yaml

# Local Ollama (no API key needed)
# First: ollama pull llama3.1
matter run --task "Explain the architecture" --workspace . \
  --config examples/configs/ollama.yaml

# ============================================================================
# Configuration inspection
# ============================================================================

# Print the effective config (defaults + file + env overrides)
matter config

# Print config with a config file applied
matter config --config config.yaml

# Override a setting via environment variable
MATTER_AGENT_MAX_STEPS=50 matter config | grep max_steps

# ============================================================================
# Tool inspection
# ============================================================================

# List all registered tools with safety classifications
matter tools

# List tools with a specific config
matter tools --config examples/configs/code-assistant.yaml

# ============================================================================
# Environment variable overrides
# ============================================================================

# Override any config setting via MATTER_* environment variables.
# These take precedence over the config file.

MATTER_AGENT_MAX_STEPS=50 \
MATTER_AGENT_MAX_COST_USD=10.00 \
MATTER_LLM_MODEL=gpt-4o-mini \
MATTER_TOOLS_ENABLE_COMMAND_EXEC=true \
  matter run --task "Run the tests" --workspace . --config config.yaml

# ============================================================================
# Conversation mode (interactive)
# ============================================================================

# When max_asks > 0 (default: 3), the agent can pause and ask questions.
# The CLI will prompt you for input on stderr.
#
# Example session:
#   $ matter run --task "Set up the project" --workspace ./new-project
#   [step 1] Reading project structure...
#   [step 2] Agent asks: What programming language should I use?
#     1. Go
#     2. Python
#     3. TypeScript
#   Your answer: 1
#   [step 3] Setting up Go project...

# Disable conversation mode for fully autonomous runs:
MATTER_AGENT_MAX_ASKS=0 \
  matter run --task "Analyze the codebase" --workspace .
