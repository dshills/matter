#!/usr/bin/env bash
# Trace a request lifecycle and write an architecture document.
# Tools: workspace_read, workspace_write

matter run --task "Trace the request lifecycle from the HTTP handler in
  internal/server/handlers.go through the service layer to the database
  calls. Read each file in the chain. Write an ARCHITECTURE.md document
  explaining the flow with a text diagram" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
