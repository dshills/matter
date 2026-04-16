#!/usr/bin/env bash
# Run tests and summarize pass/fail results.
# Tools: command_exec

matter run --task "Run 'go test ./...' and summarize the results. List
  any failing tests with the error output. Report total pass/fail counts
  and test coverage if available" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
