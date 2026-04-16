#!/usr/bin/env bash
# Find all TODO, FIXME, HACK, and XXX comments with severity ratings.
# Tools: workspace_read

matter run --task "Search all source files for TODO, FIXME, HACK, and XXX
  comments. List each one with its file path, line content, and a severity
  assessment (critical, should-fix, nice-to-have)" \
  --workspace "${1:-.}"
