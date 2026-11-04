---
# Hippo Memory - Shared Agentic Workflow Wrapper
# Provides persistent AI agent memory across runs using hippo-memory.
# The .hippo/ store is symlinked into cache-memory so learned lessons survive
# between workflow runs automatically.
#
# See: https://github.com/kitfunso/hippo-memory
#
# Usage:
#   runtimes:
#     node:
#       version: "22"         # hippo-memory requires Node.js 22.5+
#   network:
#     allowed:
#       - node                # Required for npm install -g hippo-memory
#   tools:
#     cache-memory: true      # REQUIRED: persists the .hippo store across runs
#   imports:
#     - shared/hippo-memory.md

tools:
  cache-memory: true

mcp-scripts:
  hippo:
    description: "Execute any hippo-memory CLI command via `mcpscripts hippo --args \"...\"`. Provide arguments after 'hippo'. Examples: args 'learn --git' to extract lessons from git commits, 'sleep' for full consolidation, 'recall \"api errors\" --budget 2000' to retrieve relevant memories."
    inputs:
      args:
        type: string
        description: "Arguments to pass to hippo CLI (without the 'hippo' prefix). Examples: 'learn --git', 'sleep', 'sleep --no-share', 'recall \"build failures\" --budget 3000', 'remember \"always run make fmt before committing\" --tag rule', 'list', 'export', 'export --format markdown'"
        required: true
    run: |
      echo "hippo $INPUT_ARGS"
      # shellcheck disable=SC2086 # INPUT_ARGS intentionally word-splits into multiple hippo CLI arguments
      hippo $INPUT_ARGS

steps:
  - name: Install hippo-memory
    run: |
      npm install -g hippo-memory@2.0.0

  - name: Initialize hippo store
    run: |
      # Symlink .hippo into cache-memory so the SQLite store persists across runs.
      # All writes to .hippo/ land in /tmp/gh-aw/cache-memory/hippo-store/ and are
      # saved/restored automatically by the cache-memory mechanism.
      mkdir -p /tmp/gh-aw/cache-memory/hippo-store

      if [ ! -e ".hippo" ]; then
        ln -s /tmp/gh-aw/cache-memory/hippo-store .hippo
        echo "🔗 Created .hippo → cache-memory/hippo-store"
      elif [ -d ".hippo" ] && [ ! -L ".hippo" ]; then
        # Plain directory present (e.g. first run after adding this import) — migrate
        cp -r .hippo/. /tmp/gh-aw/cache-memory/hippo-store/ 2>/dev/null || true
        rm -rf .hippo
        ln -s /tmp/gh-aw/cache-memory/hippo-store .hippo
        echo "🔗 Migrated existing .hippo/ → cache-memory/hippo-store"
      else
        echo "✅ .hippo already linked to cache-memory/hippo-store"
      fi

      # Initialise if the store has never been set up. Perform a one-time
      # repository scan to seed the memory store with historical incidents.
      # Set HIPPO_SCAN_DAYS to override the default 365-day scan window.
      HIPPO_SCAN_DAYS="${HIPPO_SCAN_DAYS:-365}"
      if [ ! -f ".hippo/config.json" ]; then
        hippo init --scan --days "$HIPPO_SCAN_DAYS"
        echo "✅ Hippo memory store initialised (scan days: $HIPPO_SCAN_DAYS)"
      else
        echo "✅ Hippo store restored from cache"
        hippo list 2>/dev/null | head -5 || true
      fi

      # One-time bootstrap for repositories with an empty or freshly initialised
      # store. This seeds memory with key project docs and recurring incidents.
      if [ ! -f ".hippo/.gh-aw-bootstrap-v1" ]; then
        if [ -f "AGENTS.md" ]; then
          hippo import --markdown AGENTS.md
        fi

        INCIDENTS=(
          'Recurring incident: Codex auth failures can break agent jobs; verify auth/mode/token setup before reruns.'
          'Recurring incident: stale workflow .lock.yml files cause churn and CI friction; run make recompile after markdown workflow edits.'
          'Recurring incident: node: command not found on GPU/self-hosted paths when node runtime/tooling is missing; validate runtimes and PATH early.'
        )
        for incident in "${INCIDENTS[@]}"; do
          hippo remember "$incident" --tag incident
        done

        touch ".hippo/.gh-aw-bootstrap-v1"
        echo "✅ Hippo store bootstrap complete (.gh-aw-bootstrap-v1)"
      fi
---

**IMPORTANT**: Run all hippo-memory commands via `mcpscripts` in bash using the `hippo` tool.

## Hippo Memory Tools

Use `mcpscripts hippo --args "<command>"` with the following command patterns:

### Learning from the Repository

```
mcpscripts hippo --args "learn --git"          # Extract lessons from recent git commits
mcpscripts hippo --args "sleep"                # Full cycle: learn, import MEMORY.md, dedup, share
mcpscripts hippo --args "sleep --no-share"     # Consolidate without promoting to global store
```

### Recalling and Storing Memories

```
mcpscripts hippo --args 'recall "build errors" --budget 3000'     # Retrieve relevant memories
mcpscripts hippo --args 'remember "always run make fmt" --tag rule'  # Store a new memory
mcpscripts hippo --args 'list'                                     # List all memories
mcpscripts hippo --args 'export'                                   # Export all memories as JSON
mcpscripts hippo --args 'export --format markdown'                 # Export as markdown
```

### Inspection and Session State

```
mcpscripts hippo --args 'current show'          # Show active session context
mcpscripts hippo --args 'inspect <id>'          # Inspect a specific memory entry
mcpscripts hippo --args 'last-sleep'            # Show output of the previous sleep run
```

## Persistence

The `.hippo/` store is symlinked to `/tmp/gh-aw/cache-memory/hippo-store/` so the
SQLite index and YAML mirrors are automatically saved and restored across workflow
runs via `cache-memory`.

## Requirements

The importing workflow must provide:
- `runtimes.node.version: "22"` — hippo-memory requires Node.js 22.5+
- `node` in `network.allowed` — needed to `npm install -g hippo-memory`
- `tools.cache-memory: true` — already set by this import, but ensure it is not disabled
