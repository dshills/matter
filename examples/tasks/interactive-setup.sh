#!/usr/bin/env bash
# Interactively set up a new project (agent will ask clarifying questions).
# Tools: workspace_read, workspace_write
# Requires: max_asks > 0 (conversation mode)

matter run --task "Help me set up a new Go project. Ask me what the
  project does, what dependencies I want, and whether I need a CLI,
  library, or server. Then create the initial file structure" \
  --workspace "${1:-./new-project}" \
  --config examples/configs/code-assistant.yaml
