#!/usr/bin/env bash
# Find untested error paths and add test cases, verifying after each change.
# Tools: workspace_read, workspace_write, command_exec

matter run --task "Read all Go files in internal/. Find any function that
  returns an error but has no test covering the error path. For the first
  three such functions found, add test cases that exercise the error
  return. Run 'go test ./internal/...' after each change to verify" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
