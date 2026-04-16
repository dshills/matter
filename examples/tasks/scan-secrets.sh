#!/usr/bin/env bash
# Scan for hardcoded secrets, API keys, and credentials.
# Tools: workspace_read

matter run --task "Scan all source files, config files, and scripts for
  hardcoded secrets, API keys, passwords, tokens, or connection strings.
  Report each finding with its file and line number. Ignore test fixtures
  that use obviously fake values like 'test-token'" \
  --workspace "${1:-.}"
