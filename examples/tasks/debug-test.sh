#!/usr/bin/env bash
# Diagnose a failing test by reading source and asking for context.
# Tools: workspace_read, command_exec
# Requires: max_asks > 0 (conversation mode)
#
# Usage: ./debug-test.sh [workspace] [test_name] [test_file]
#   ./debug-test.sh ./my-project TestUserLogin internal/auth/handler_test.go

WORKSPACE="${1:-.}"
TEST="${2:-TestMain}"
FILE="${3:-main_test.go}"

matter run --task "The test ${TEST} in ${FILE} is failing. Read the test,
  the code it tests, and any related files to diagnose the failure. Ask me
  for any additional context you need before proposing a fix" \
  --workspace "$WORKSPACE" \
  --config examples/configs/code-assistant.yaml
