#!/usr/bin/env bash
# Check SQL queries for injection vulnerabilities.
# Tools: workspace_read

matter run --task "Find all SQL queries in the codebase. Check whether they
  use parameterized queries or string concatenation. Report any that
  concatenate user input into SQL strings" \
  --workspace "${1:-.}"
