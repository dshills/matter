#!/usr/bin/env bash
# Run linter and categorize findings by severity.
# Tools: command_exec

matter run --task "Run 'golangci-lint run ./...' and categorize the
  findings by severity. For each issue, briefly explain what the linter
  is flagging and whether it matters" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
