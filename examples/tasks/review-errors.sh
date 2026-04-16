#!/usr/bin/env bash
# Audit error handling patterns across the codebase.
# Tools: workspace_read

matter run --task "Read all Go files and evaluate the error handling patterns.
  Find places where errors are silently ignored (assigned to _), where error
  messages lack context, or where panics are used instead of error returns" \
  --workspace "${1:-.}"
