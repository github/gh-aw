---
private: true
emoji: "🔧"
timeout-minutes: 10
strict: true
on:
  schedule: daily
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  issues: read
  pull-requests: read
  contents: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [issues, pull_requests, repos]
safe-outputs:
  assign-to-user:
    target: "*"
    max: 5
  add-comment:
    target: "*"
    max: 5
  noop:
imports:
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
evals:
  - id: assignment-made
    question: Did the agent assign one or more unassigned issues to a user, or correctly call noop when no unassigned issues were found?
  - id: contributor-selected
    question: Does the agent output show that a relevant contributor was selected from recent merged PRs?
  - id: comment-posted
    question: Did the agent post a comment explaining the assignment decision?
---

{{#runtime-import? .github/shared-instructions.md}}

### Auto-Assign Issues

Find up to **5** open issues that:
- **Has no assignees** - When you retrieve issues from GitHub, explicitly check the `assignees` field. Skip any issue where `issue.assignees` is not empty or has length > 0.
- Does not have label `ai-generated`
- Does not have a `campaign:*` label (these are managed by campaign orchestrators)
- Does not have labels: `no-bot`, `no-campaign`
- Was not opened by `github-actions` or any bot

Process the oldest unassigned issues first, up to 5 per run.

Then list the 5 most recent contributors from merged PRs. For each selected issue, pick the
contributor who seems most relevant based on the issue's area — use the issue's component
labels (for example `cli`, `compiler`, `mcp`, `safe-outputs`, `workflows`) and the files those
contributors recently touched to match owner to area. Prefer a contributor who has recently
worked on the same area; fall back to the most recent contributor when no area match is clear.

Do not assign the same contributor to more than 2 issues in a single run, so the backfill
spreads ownership rather than overloading one person.

For each issue you assign:
1. Use `assign-to-user` to assign the issue
2. Use `add-comment` with a short explanation (1-2 sentences) naming the area match

If no unassigned issue exists, call `noop` with "No unassigned issues found — no action needed"
and stop.