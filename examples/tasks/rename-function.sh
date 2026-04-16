#!/usr/bin/env bash
# Rename a function across the codebase and verify the build.
# Tools: workspace_read, workspace_write, command_exec
#
# Usage: ./rename-function.sh [workspace] [file] [old_name] [new_name]
#   ./rename-function.sh ./my-project internal/pipeline/worker.go processData transformRecords

WORKSPACE="${1:-.}"
FILE="${2:-main.go}"
OLD="${3:-processData}"
NEW="${4:-transformRecords}"

matter run --task "Rename the function '${OLD}' in ${FILE} to '${NEW}'.
  Update all callers across the codebase. Run 'go build ./...' to verify
  the rename compiles, then run 'go test ./...' to verify no tests break" \
  --workspace "$WORKSPACE" \
  --config examples/configs/code-assistant.yaml
