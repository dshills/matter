#!/usr/bin/env bash
# Create an MIT LICENSE file.
# Tools: workspace_write
#
# Usage: ./gen-license.sh [workspace] [holder]
#   ./gen-license.sh ./my-project "My Company Inc."

WORKSPACE="${1:-.}"
HOLDER="${2:-My Company Inc.}"

matter run --task "Create an MIT LICENSE file with copyright year 2026 and
  copyright holder '${HOLDER}'" \
  --workspace "$WORKSPACE" \
  --config examples/configs/code-assistant.yaml
