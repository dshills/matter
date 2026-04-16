#!/usr/bin/env bash
# Analyze project dependencies and assess maintenance status.
# Tools: workspace_read

matter run --task "Read go.mod (or package.json, requirements.txt, etc.)
  and list all direct dependencies. For each one, explain what it does
  and whether it appears to be actively maintained" \
  --workspace "${1:-.}"
