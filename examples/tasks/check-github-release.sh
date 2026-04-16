#!/usr/bin/env bash
# Check a GitHub repo's latest release via the API.
# Tools: web_fetch
#
# Usage: ./check-github-release.sh [owner/repo]
#   ./check-github-release.sh golangci/golangci-lint

REPO="${1:-golangci/golangci-lint}"

matter run --task "Fetch the latest release information for the
  ${REPO} repository from the GitHub API. Report the version number,
  release date, and key changes" \
  --config examples/configs/research-agent.yaml
