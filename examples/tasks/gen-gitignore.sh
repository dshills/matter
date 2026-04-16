#!/usr/bin/env bash
# Generate a .gitignore file appropriate for the project.
# Tools: workspace_read, workspace_write

matter run --task "Read the project structure to determine the language and
  build tools used, then create a comprehensive .gitignore file appropriate
  for this project. Include build artifacts, IDE files, OS files, and
  dependency directories" \
  --workspace "${1:-.}" \
  --config examples/configs/code-assistant.yaml
