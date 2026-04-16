#!/usr/bin/env bash
# Generate Markdown API documentation from HTTP handler source files.
# Tools: workspace_read

matter run --task "Read all HTTP handler files and generate a Markdown
  document listing every API endpoint with its method, path, request body
  schema, response schema, and status codes. Group endpoints by resource" \
  --workspace "${1:-.}"
