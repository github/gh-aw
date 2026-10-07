---
emoji: "🧠"
name: Daily Graft Intelligence
private: true
description: Daily repository intelligence report that uses Graft to explain changed subsystems, blast radius, and hotspots from the last 24 hours
on:
  schedule:
    - cron: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
strict: true
tracker-id: daily-graft-intelligence
timeout-minutes: 20
sandbox:
  agent:
    id: awf
imports:
  - uses: shared/daily-issue-base.md
    with:
      title-prefix: "[graft-intelligence] "
      expires: 2d
      labels: [automated-analysis, cookie]
  - shared/gh.md
  - shared/mcp/graft.md
  - shared/noop-reminder.md
safe-outputs:
  mentions: false
  allowed-github-references: []
tools:
  cli-proxy: true
  bash:
    - "cat /tmp/gh-aw/agent/graft-changed-files.txt"
    - "cat /tmp/gh-aw/agent/graft-recent-activity.txt"
steps:
  - name: Collect recent repository activity
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      git log --since="24 hours ago" --date=iso-strict \
        --format='commit %H%nAuthor: %an <%ae>%nDate: %ad%nSubject: %s%n' \
        --name-status > /tmp/gh-aw/agent/graft-recent-activity.txt || true
      git log --since="24 hours ago" --name-only --pretty=format: \
        | sed '/^$/d' \
        | sort -u > /tmp/gh-aw/agent/graft-changed-files.txt || true
features:
  gh-aw-detection: true
evals:
  - id: graph_built_and_checked
    question: Did the workflow build a fresh Graft graph and verify it before analysis?
  - id: report_or_noop_produced
    question: Was a daily intelligence issue created when there were meaningful repository changes, or was noop used appropriately when there was nothing to report?
---

# Daily Graft Intelligence

You are a repository intelligence analyst. Produce a concise daily report for the last 24 full hours ending at workflow start (UTC) using the pre-built Graft graph as your primary context source.

## Data Sources

1. `/tmp/gh-aw/agent/graft-changed-files.txt` — unique files changed in the last 24 hours
2. `/tmp/gh-aw/agent/graft-recent-activity.txt` — commit metadata and file status lines for the same window
3. Graft MCP tools — repository map, subsystem summaries, call/dependency relationships, and targeted structural search

## Required workflow

1. Run `cat /tmp/gh-aw/agent/graft-changed-files.txt` as a separate bash call.
2. If the file is empty, call `noop` with a message that no repository files changed in the last 24 full hours.
3. Run `graft graft_check` through bash to confirm the graph is current.
4. Run `graft graft_map` through bash to identify the most connected directories, symbols, and hotspots.
5. Run `cat /tmp/gh-aw/agent/graft-recent-activity.txt` as a separate bash call and identify the most meaningful changed files or subsystems.
6. Use Graft tools to explain those changes:
   - `graft_ask` for high-level subsystem understanding
   - `graft_callers` for blast-radius analysis around the most important changed symbols or files
   - `graft_skeleton` when you need quick API-surface context for a changed file
   - `graft_grep` only for targeted confirmation of a pattern or term
7. Synthesize one report issue with the highest-signal findings only.

Graft and safe outputs are CLI-mounted in this workflow. Invoke every Graft tool as `graft <tool> [--param value...]` through bash, and emit the report or noop with `safeoutputs create_issue` or `safeoutputs noop`. Do not call CLI-mounted servers as native MCP tools.

## Analysis goals

Focus on the questions a maintainer would ask first:

- Which subsystems changed?
- Why do those changes matter in the context of the broader codebase?
- What nearby callers, dependencies, or hotspots suggest elevated blast radius?
- What architectural or maintenance follow-ups are worth tracking?

Do not restate raw commit logs. Turn the raw changes into codebase-aware intelligence.

## Report requirements

- Use `###` headings only.
- Keep the visible summary brief.
- Put long per-file or per-symbol breakdowns inside `<details>` blocks.
- Call out uncertainty explicitly when Graft context is incomplete.
- Avoid `@mentions` and GitHub issue/PR references.
- Include at most 5 changed areas and at most 3 follow-up recommendations.

## Suggested structure

### Summary
- 2-4 bullets with the most important signals from the day

### Changed Areas
- Short explanation of the most important subsystems or files

### Blast Radius and Hotspots
- Highlight callers, dependencies, and hotspot context that increase risk or importance

### Recommendations
- Concrete follow-ups for maintainers

### References
- Mention the reporting window and the workflow run ID

If the changes are trivial housekeeping with no meaningful architectural or operational signal, call `noop` instead of creating a low-value issue.