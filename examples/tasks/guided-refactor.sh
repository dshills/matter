#!/usr/bin/env bash
# Review for refactoring opportunities, ask before making changes.
# Tools: workspace_read, workspace_write
# Requires: max_asks > 0 (conversation mode)

matter run --task "Review the codebase for the three highest-impact
  refactoring opportunities. For each one, explain the current problem
  and your proposed change. Ask me which ones to proceed with before
  making any modifications" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
