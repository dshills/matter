#!/usr/bin/env bash
# Generate table-driven test stubs for a Go source file.
# Tools: workspace_read, workspace_write
#
# Usage: ./gen-test-stubs.sh [workspace] [file]
#   ./gen-test-stubs.sh ./my-project internal/auth/handler.go

WORKSPACE="${1:-.}"
FILE="${2:-main.go}"

matter run --task "Read ${FILE} and create a corresponding test file
  with table-driven test stubs for every exported function. Use the standard
  testing package. Include test cases for happy path, error cases, and edge
  cases. Leave the test body as t.Skip('not implemented') so I can fill
  them in" \
  --workspace "$WORKSPACE" \
  --config examples/configs/code-assistant.yaml
