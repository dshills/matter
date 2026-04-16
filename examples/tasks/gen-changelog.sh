#!/usr/bin/env bash
# Write a user-facing CHANGELOG entry from recent git history.
# Tools: workspace_read, command_exec

matter run --task "Run 'git log --oneline -20' to read recent commits and
  the source changes to write a user-facing CHANGELOG entry. Group changes
  into Added, Changed, Fixed, and Removed sections. Use plain language,
  not commit hashes" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
