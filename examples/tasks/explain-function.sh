#!/usr/bin/env bash
# Explain a complex function in detail for a new developer.
# Tools: workspace_read
#
# Usage: ./explain-function.sh [workspace] [file] [function]
#   ./explain-function.sh ./my-project internal/planner/parser.go parseDecision

WORKSPACE="${1:-.}"
FILE="${2:-main.go}"
FUNC="${3:-main}"

matter run --task "Read ${FILE} and explain the ${FUNC} function in detail.
  Describe each step, what edge cases it handles, and why key design choices
  were made. Write the explanation for a developer who is new to the codebase" \
  --workspace "$WORKSPACE"
