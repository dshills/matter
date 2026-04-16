#!/usr/bin/env bash
# Explain what a project does by reading its README and source files.
# Tools: workspace_read

matter run --task "Read the project's README and source files, then write a
  one-paragraph summary of what this project does, what language it uses,
  and how it is structured" \
  --workspace "${1:-.}"
