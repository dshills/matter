#!/usr/bin/env bash
# Check Go module health with go mod tidy and verify.
# Tools: command_exec

matter run --task "Run 'go mod tidy' and 'go mod verify' and report
  whether the module is clean. If there are missing or unused dependencies,
  list them" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
