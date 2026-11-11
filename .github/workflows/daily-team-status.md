---
description: |
  This workflow is a daily team status reporter creating upbeat activity summaries.
  Gathers recent repository activity (issues, PRs, discussions, releases, code changes)
  and generates engaging GitHub issues with productivity insights, community
  highlights, and project recommendations. Uses a positive, encouraging tone with
  moderate emoji usage to boost team morale.

on:
  schedule: daily
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read

network: defaults

tools:
  bash: ["cat", "ls", "find", "grep", "head", "tail", "wc"]
  github:
    min-integrity: none # This workflow is allowed to examine and comment on any issues

safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    title-prefix: "[team-status] "
    labels: [report, daily-status]
    close-older-issues: true
source: githubnext/agentics/workflows/team-status.md@main
source-sha: "139fa09ef17cde4135e696ff8e7af09bc5ddfa6e"
---

# Team Status

Create an upbeat daily status report for the team as a GitHub issue.

## Report Window

- **Report window**: last 24 full hours ending at workflow start (UTC)
- Compute the window boundaries before gathering activity and report them explicitly as ISO-8601 UTC timestamps (`YYYY-MM-DDTHH:MM:SSZ`), not just a date.
- Only count activity (issues, PRs, discussions, releases, commits) whose relevant timestamp falls inside this window, and state the window next to every count so other reports can be reconciled against it.

## What to include

- Recent repository activity (issues, PRs, discussions, releases, code changes)
- Team productivity suggestions and improvement ideas
- Community engagement highlights
- Project investment and feature recommendations

## Style

- Be positive, encouraging, and helpful 🌟
- Use emojis moderately for engagement
- Keep it concise - adjust length based on actual activity
- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body
- Wrap long activity item lists and recommendation lists in `<details><summary>...</summary>...</details>` to reduce scrolling

## Report Structure

Structure the report with this hierarchy:

- `### 🌟 Team Snapshot`
  - Start with a `- **Window**: window_start=<ISO-8601 UTC> → window_end=<ISO-8601 UTC>` line
  - 1-2 paragraph overview of today's team momentum and notable signals
- `### 🛠️ Activity Highlights`
  - Keep concise highlights visible
  - State counts (including merged PRs) as counts within `window_start`/`window_end`
  - Put detailed activity lists in `<details>` blocks
- `### 💡 Recommendations`
  - Put long recommendation lists in `<details>` blocks
- `### 📌 Next Steps`
  - Short actionable closing section

## Process

1. Gather recent activity from the repository
2. Create a new GitHub issue with your findings and insights