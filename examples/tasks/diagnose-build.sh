#!/usr/bin/env bash
# Diagnose a build failure and suggest the minimal fix.
# Tools: workspace_read, command_exec

matter run --task "Run 'go build ./...' and if it fails, read the relevant
  source files to diagnose the root cause. Explain what is broken and
  suggest the minimal fix" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
