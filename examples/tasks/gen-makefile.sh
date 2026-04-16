#!/usr/bin/env bash
# Create a Makefile with standard build targets.
# Tools: workspace_read, workspace_write

matter run --task "Read the project to determine the build system, then
  create a Makefile with targets for build, test, lint, clean, and install.
  Use Go conventions if it is a Go project, npm conventions if Node, etc." \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
