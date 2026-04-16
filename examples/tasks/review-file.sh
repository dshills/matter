#!/usr/bin/env bash
# Review a specific file for bugs, race conditions, and security issues.
# Tools: workspace_read
#
# Usage: ./review-file.sh [workspace] [file]
#   ./review-file.sh ./my-project internal/auth/handler.go

WORKSPACE="${1:-.}"
FILE="${2:-main.go}"

matter run --task "Read ${FILE} and review it for bugs,
  race conditions, error handling gaps, and security issues. For each
  finding, explain the risk and suggest a fix" \
  --workspace "$WORKSPACE"
