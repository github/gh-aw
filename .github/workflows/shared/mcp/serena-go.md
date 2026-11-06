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
  - shared/go-node-setup.md
  - uses: ./serena.md
    with:
      languages: ["go"]
      path: '${{ env.GH_AW_SERENA_GO_PATH }}:\${PATH}'
pre-agent-steps:
  - name: Expose Go to Serena container
    run: |
      set -euo pipefail
      unset GOROOT
      go_root="$(GOTOOLCHAIN=local go env GOROOT)"
      case "$go_root" in
        "${RUNNER_TOOL_CACHE:?RUNNER_TOOL_CACHE must be set}"/go/*) ;;
        *)
          echo "Serena Go runtime is outside its read-only runner tool-cache mount; check actions/setup-go and RUNNER_TOOL_CACHE." >&2
          exit 1
          ;;
      esac
      if [ ! -x "$go_root/bin/go" ]; then
        echo "Serena Go runtime has no executable bin/go in the mounted toolchain." >&2
        exit 1
      fi
      export GOROOT="$go_root"
      gopls_bin="$RUNNER_TOOL_CACHE/gopls/0.22.0/bin"
      if ! CGO_ENABLED=0 GOTOOLCHAIN=local GOBIN="$gopls_bin" "$go_root/bin/go" install golang.org/x/tools/gopls@v0.22.0; then
        echo "Serena Go preflight failed: could not install gopls v0.22.0 with the selected Go toolchain." >&2
        exit 1
      fi
      if ! "$gopls_bin/gopls" version; then
        echo "Serena Go preflight failed: installed gopls cannot start." >&2
        exit 1
      fi
      printf 'GOROOT=%s\n' "$go_root" >> "$GITHUB_ENV"
      printf 'GH_AW_SERENA_GO_PATH=%s/bin:%s:%s\n' "$go_root" "$gopls_bin" "$PATH" >> "$GITHUB_ENV"
      "$go_root/bin/go" version
  - name: Check Serena Go semantic analysis
    timeout-minutes: 5
    run: |
      set -euo pipefail
      if ! docker run --rm --init \
        --network host \
        --mount "type=bind,source=$GITHUB_WORKSPACE,target=$GITHUB_WORKSPACE" \
        --mount "type=bind,source=$RUNNER_TOOL_CACHE,target=$RUNNER_TOOL_CACHE,readonly" \
        --env "PATH=$GH_AW_SERENA_GO_PATH:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
        --entrypoint /usr/bin/timeout \
        ghcr.io/oraios/serena:1.7.0@sha256:6c9459e4246a39c9deaa4f23fb05a526ac6e237b24c8e84a927a098fa1ab6730 \
        240 /workspaces/serena/.venv/bin/serena project health-check "$GITHUB_WORKSPACE"; then
        echo "Serena Go preflight failed: Go language-server semantic health check failed or timed out. Check the gopls and Serena diagnostics above; the agent will not start." >&2
        exit 1
      fi
      echo "Serena Go preflight passed: gopls started and semantic symbol analysis succeeded."
---

## Go-specific constraints

1. Analyze only `.go` files.
2. Skip files ending in `_test.go`.
3. Prioritize `pkg/` as the primary analysis area.

The selected Go binary directory and runner PATH are forwarded explicitly through
the MCP gateway to Serena's PATH, preserving other setup runtimes in the mounted
tool cache (for example Node for a project's TypeScript or Bash language servers)
and appending the gateway's existing PATH. No additional host paths are mounted.
The existing runner tool-cache mount remains read-only. Setup provisions
Node.js 24 in that mounted cache so project-level TypeScript and Bash language
servers do not depend on the runner's unmounted system Node installation.
Node setup runs only on the host before PATH capture; it does not add Node or npm
domains to the agent or detection sandbox network policy.
Setup installs pinned gopls v0.22.0 there as a static binary and checks that it
starts. Before the MCP
gateway or agent starts, Serena's project health check runs in the same image
with the same workspace and tool-cache mounts, exercising symbol overview and
symbol lookup through the language server. A failed or timed-out check fails
preflight rather than leaving the agent to report a missing tool.
Root discovery clears any earlier `GOROOT`, disables automatic toolchain selection,
and refreshes the host `GOROOT` so later builds use the same selected toolchain.
If Serena initialization later fails, report the language-server error and stop
tasks that require semantic analysis; do not substitute text-only analysis or
install tools ad hoc.
