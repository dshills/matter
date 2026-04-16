#!/usr/bin/env bash
# Add snake_case JSON struct tags to Go structs in a directory.
# Tools: workspace_read, workspace_write
#
# Usage: ./add-json-tags.sh [workspace] [dir]
#   ./add-json-tags.sh ./my-project internal/models/

WORKSPACE="${1:-.}"
DIR="${2:-internal/models/}"

matter run --task "Read all Go structs in ${DIR} and add json struct tags
  using snake_case naming. Add omitempty to pointer fields and optional
  fields. Do not modify structs that already have json tags" \
  --workspace "$WORKSPACE" \
  --config examples/configs/code-assistant.yaml
