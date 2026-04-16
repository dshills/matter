#!/usr/bin/env bash
# Find exported functions and types that are never referenced elsewhere.
# Tools: workspace_read

matter run --task "Find exported functions and types that are never
  referenced outside their own package. List each with its file and line,
  and suggest which ones are safe to remove" \
  --workspace "${1:-.}"
