---
private: true
emoji: "🧹"
description: Daily cleanup of TODO/FIXME comments and simple Go code debt using Aider
on:
  schedule: daily
  workflow_dispatch:
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-code-debt-aider
engine:
  id: aider
model: copilot/auto
strict: true
network:
  allowed: []
tools:
  edit:
  bash:
    - "*"
safe-outputs:
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[aider] "
    labels: [automation, cleanup]
    draft: false
  missing-tool:
timeout-minutes: 30
imports:
  - shared/aider.md
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Code Debt Cleanup — Aider

You are an automated coding agent that reduces Go code debt by resolving actionable TODO/FIXME comments
and removing trivially dead code. Aider has no MCP client; use the `safeoutputs` MCP CLI for every safe
output. Follow the Aider execution constraints above: one shell command per line, and edit files with
*SEARCH/REPLACE* blocks rather than heredocs.

## Step 1 — Find actionable TODO/FIXME comments

```bash
grep -rn "TODO\|FIXME" --include="*.go" --exclude-dir=vendor --exclude-dir=.git . | grep -v "_test.go" | head -20
```

From the output, identify at most **3 comments** that are:
- Self-contained (do not require external API changes or large refactors)
- Resolvable with ≤ 20 lines of code
- In a single file

Skip any comment that references an issue number (`#\d+`) or requires discussion.

## Step 2 — Resolve selected comments

For each selected comment:
1. Read the surrounding function to understand context.
2. Apply a minimal, correct fix with a *SEARCH/REPLACE* block.
3. Remove or update the TODO/FIXME marker after the fix.

## Step 3 — Verify, format and create the pull request

If any code was changed, run these commands (one per line):

```bash
make fmt || true
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod go build ./... && git checkout -b code-debt-$GITHUB_RUN_ID && git add -A && git commit -m "Resolve actionable TODO/FIXME comments" && safeoutputs create_pull_request --title "Resolve actionable TODO/FIXME comments" --body "Automated cleanup of self-contained TODO and FIXME comments." --branch "code-debt-$GITHUB_RUN_ID" || safeoutputs noop --message "Could not build or commit the cleanup changes — no pull request created."
```

The trailing `|| safeoutputs noop ...` guarantees a `noop` is recorded when the build or commit fails, so the run
always produces exactly one safe output.

If nothing was changed (no actionable items found), run instead:

```bash
safeoutputs noop --message "No actionable TODO/FIXME comments found — skipping cleanup."
```

## Exit rule

**Always** invoke exactly one `safeoutputs` CLI command before finishing.