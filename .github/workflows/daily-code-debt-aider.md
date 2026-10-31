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
env:
  GH_AW_AIDER_MAX_REFLECTIONS: "8"
engine:
  id: aider
model: copilot/auto
strict: true
network:
  allowed: []
runtimes:
  go:
    version: "1.27.3"
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
grep -rnE '^[[:space:]]*//[[:space:]]*(TODO|FIXME)([[:space:]:]|$)' --include="*.go" --exclude="*_test.go" --exclude-dir=vendor --exclude-dir=.git --exclude-dir=testdata . | head -20
```

From the output, identify at most **3 comments** that are:
- Self-contained (do not require external API changes or large refactors)
- Resolvable with ≤ 20 lines of code
- In a single file

Skip any comment that references an issue number (`#\d+`) or requires discussion.
Only actual source comments are candidates, not identifiers, string literals, generated
workflow templates, or test fixtures. Do not rename a symbol just because its name contains TODO.
Never assume discovery found a candidate. If the command returns no eligible comments,
call `safeoutputs noop` and stop without edits. Do not invent `example.go`, sample code,
or a hypothetical TODO to demonstrate a cleanup.

## Step 2 — Resolve selected comments

For each selected comment:
1. Read the surrounding function in a fenced `bash` command and consume its output
   before proposing an edit. SEARCH must match the exact source, not a grep excerpt.
   Use only an existing tracked file returned by discovery. Verify it with
   `git ls-files --error-unmatch` before reading it; do not create, rename, or delete files.
   Do not use an empty SEARCH block or edit a file whose contents have not been returned
   by a successful read in this session.
2. Apply a minimal, correct fix with a *SEARCH/REPLACE* block.
3. Remove or update the TODO/FIXME marker after the fix.

## Step 3 — Verify, format and create the pull request

If any code was changed, run these commands (one per line):

```bash
make fmt-go
go build ./...
```

Run formatting first and consume its output before running the build. Run the build
separately and consume its exit status and complete compiler diagnostics before any
commit or publication. On either failure, stop and call `safeoutputs report_incomplete`
with the failed command in `--reason` and the concrete stderr and exit code in `--details`.
Do not combine validation with a generic `|| safeoutputs report_incomplete` fallback:
that masks which command failed and can make a failed build appear successful.
Preserve TLS verification; do not retry certificate failures with insecure settings.

Only after both commands pass, inspect `git diff --name-status` and `git diff --check`.
Confirm every changed file is an existing selected source file and the diff contains
only the intended cleanup. If unexpected files appear, report the mismatch as incomplete
and do not commit or publish them. Stage only those explicitly inspected paths, never
`git add -A`. Create branch `code-debt-$GITHUB_RUN_ID`, commit the selected cleanup, then call
`safeoutputs create_pull_request` with that branch, title "Resolve actionable TODO/FIXME comments",
and body describing the actual changes and successful validation. Report any commit or
publication error with its concrete diagnostics; do not claim a PR was created without
confirmation from the safe-output tool.

If nothing was changed (no actionable items found), run instead:

```bash
safeoutputs noop --message "No actionable TODO/FIXME comments found — skipping cleanup."
```

## Exit rule

**Always** invoke exactly one `safeoutputs` CLI command before finishing.