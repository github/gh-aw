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
checkout:
  fetch-depth: 0
engine:
  id: copilot
  copilot-sdk: true
skip-if-decision:
  - >-
    Do @/tmp/gh-aw/agent/graft-recent-activity.txt and
    @/tmp/gh-aw/agent/graft-changed-files.txt conclusively establish that the
    last 24 hours contain only trivial housekeeping with no meaningful
    architectural or operational signal to report? Abstain if change summaries
    are insufficient or any change requires Graft analysis to assess its impact.
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
  - shared/graft.md
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
        --name-status > /tmp/gh-aw/agent/graft-recent-activity.txt
      git log --since="24 hours ago" --name-only --pretty=format: \
        | sed '/^$/d' \
        | sort -u > /tmp/gh-aw/agent/graft-changed-files.txt
features:
  gh-aw-detection: true
evals:
  - id: graph_built_and_checked
    question: Did the workflow build a fresh Graft graph and verify it before analysis? Return UNKNOWN when the output records a skip-if-decision no-op before agent execution.
  - id: report_or_noop_produced
    question: Was a daily intelligence issue created when there were meaningful repository changes, or was noop used appropriately when there was nothing to report?
---

# Daily Graft Intelligence

The AWF decision gate may skip clearly report-free housekeeping before invoking
the analyst. Ambiguous changes still receive the full Graft analysis.

You are a repository intelligence analyst. Produce a concise daily report for the last 24 full hours ending at workflow start (UTC) using the pre-built Graft graph as your primary context source.

## Data Sources

1. `/tmp/gh-aw/agent/graft-changed-files.txt` — unique files changed in the last 24 hours
2. `/tmp/gh-aw/agent/graft-recent-activity.txt` — commit metadata and file status lines for the same window
3. Graft CLI — repository map, subsystem summaries, call/dependency relationships, and targeted structural search

## Required workflow

1. Run `cat /tmp/gh-aw/agent/graft-changed-files.txt` as a separate bash call.
2. If the file is empty, call `noop` with a message that no repository files changed in the last 24 full hours.
3. The setup step already runs `graft check` against the wiring graph. The optional deep layer may report `NO GRAPH`; it is not required, and do not repeat the check. If any Graft query reports the wiring graph missing, stale, or unusable at runtime, call `safeoutputs report_incomplete` and stop without creating a daily report.
4. Run `graft --dir=/tmp/gh-aw/graft map` through bash to identify the most connected directories, symbols, and hotspots. If it fails, call `safeoutputs report_incomplete` and stop without creating a daily report.
5. Run `cat /tmp/gh-aw/agent/graft-recent-activity.txt` as a separate bash call and identify the most meaningful changed files or subsystems.
6. Use Graft CLI commands with `--dir=/tmp/gh-aw/graft` to explain those changes:
   - `ask "question" --source` for high-level subsystem understanding
   - `callers "symbol"` for blast-radius analysis around the most important changed symbols or files
   - `skeleton "file"` when you need quick API-surface context for a changed file
   - `grep "pattern"` only for targeted confirmation of a pattern or term
7. Synthesize one report issue with the highest-signal findings only.

Graft is a native CLI; invoke each query as a separate bash call using `graft --dir=/tmp/gh-aw/graft <command> [arguments...]`, without pipes or redirections such as `2>&1 | head`. Safe outputs remain CLI-mounted: emit the report or noop with `safeoutputs create_issue` or `safeoutputs noop`, not native MCP calls. Do not run Graft `check`, `build`, or `mcp` during analysis.

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