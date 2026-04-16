#!/usr/bin/env bash
# Look up a Go package and summarize its API.
# Tools: web_fetch
#
# Usage: ./lookup-package.sh [package]
#   ./lookup-package.sh golang.org/x/sync/errgroup

PKG="${1:-golang.org/x/sync/errgroup}"

matter run --task "Fetch the documentation for the ${PKG} package from
  pkg.go.dev. Summarize what it does, its key types and functions, and
  give a practical usage example" \
  --config examples/configs/research-agent.yaml
