---
steps:
  - name: Build Graft context graph
    env:
      GRAFT_DIR: /tmp/gh-aw/graft
    run: |
      set -euo pipefail
      mkdir -p "$GRAFT_DIR" /tmp/gh-aw/graft-empty-git
      npx -y @nanonets/graft@0.8.0 --dir "$GRAFT_DIR" build "$GITHUB_WORKSPACE"
      test -s "$GRAFT_DIR/.graph/wiring.json" || { echo "::error::Graft wiring graph is missing" >&2; exit 1; }
      npx -y @nanonets/graft@0.8.0 --dir "$GRAFT_DIR" check "$GITHUB_WORKSPACE"
mcp-servers:
  graft:
    command: "npx"
    args:
      - "-y"
      - "@nanonets/graft@0.8.0"
      - "--dir=/tmp/gh-aw/graft"
      - "mcp"
      - "${GITHUB_WORKSPACE}"
    mounts:
      - \${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:ro
      - /tmp/gh-aw/graft-empty-git:\${GITHUB_WORKSPACE}/.git:ro
      - /tmp/gh-aw/graft:/tmp/gh-aw/graft:ro
    allowed:
      - graft_ask
      - graft_skeleton
      - graft_callers
      - graft_grep
      - graft_map
      - graft_check
network:
  allowed:
    - defaults
    - node
---

<!--
# Graft MCP Server
# Shared wrapper for NanoNets Graft codebase intelligence.
#
# Upstream:
# - https://github.com/NanoNets/Graft
# - https://www.npmjs.com/package/@nanonets/graft
#
# Why this wrapper exists:
# - Builds the local Graft graph in /tmp and mounts it and the checkout read-only.
# - Exposes Graft's stdio MCP server through npx using a pinned package version.
# - Restricts access to the read-only Graft analysis tools.
#
# Available tools:
#   - graft_ask
#   - graft_skeleton
#   - graft_callers
#   - graft_grep
#   - graft_map
#   - graft_check
#
# Usage:
#   imports:
#     - shared/mcp/graft.md
-->

Use Graft first for repository intelligence tasks. Start with `graft_check` to confirm the graph is current, `graft_map` for top-level orientation, then use `graft_ask`, `graft_callers`, `graft_grep`, and `graft_skeleton` to analyze the changed subsystems without re-exploring the repository from scratch.
