#!/usr/bin/env bash
# Map internal package dependencies and find circular imports.
# Tools: workspace_read

matter run --task "Read the directory structure and key files to produce
  a module dependency map. Show which packages import which other packages
  and identify any circular or concerning dependency patterns" \
  --workspace "${1:-.}"
