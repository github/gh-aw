---
# Serena MCP Server - Go Code Analysis
# Language Server Protocol (LSP)-based tool for deep Go code analysis
#
# Documentation: https://github.com/oraios/serena
#
# Capabilities:
#   - Semantic code analysis using LSP (go to definition, find references, etc.)
#   - Symbol lookup and cross-file navigation
#   - Type inference and structural analysis
#   - Deeper insights than text-based grep approaches
#
# Usage:
#   imports:
#     - shared/mcp/serena-go.md

imports:
  - uses: ./serena.md
    with:
      languages: ["go"]
pre-agent-steps:
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Verify Go installation
    run: go version
---

## Go-specific constraints

1. Analyze only `.go` files.
2. Skip files ending in `_test.go`.
3. Prioritize `pkg/` as the primary analysis area.
