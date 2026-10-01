---
private: true
emoji: "⚡"
description: "Smoke test for MAI-Code-1-Flash (mai-code-1-flash-picker) — pricing: $0.75/M input, $0.075/M cached, $4.50/M output"
on:
  schedule: every 2 days
  slash_command:
    name: smoke-copilot-mai
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  label_command:
    name: smoke
    events: [pull_request]
  reaction: "eyes"
  status-comment: true
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
  issues: read
  pull-requests: read
name: Smoke Copilot MAI
model: mai-code-1-flash-picker
engine:
  id: copilot
  bare: true
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
  - uses: shared/session-artifact-check.md
    with:
      session-artifact: copilot-events
network:
  allowed:
    - defaults
    - github
tools:
  bash:
    - "*"
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-mai"
    labels: [automation, testing]
  add-comment:
    hide-older-comments: true
    max: 1
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: MAI-Code-1-Flash Commit Summary

## Task

Retrieve and summarize commit messages from the last 24h in `${{ github.repository }}`.

1. **Get commits**: Run the following bash command to fetch commits from the last 24 hours:

   ```bash
   gh api "/repos/${{ github.repository }}/commits?since=$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')" \
     --jq '.[] | "- \(.commit.message | split("\n")[0]) (\(.sha[:7]))"'
   ```

   Mark as ✅ if the command ran without error, ❌ if it failed.

2. **Summarize**: Write a concise 2–4 sentence summary grouping the commits by theme or area. If there are no commits, write "No commits in the last 24h."

## Output

**Create an issue** titled **"Smoke Test: MAI-Code-1-Flash - ${{ github.run_id }}"** with:
- The commit list (or "No commits in the last 24h" if empty)
- The summary
- ✅ or ❌ for the bash tool test
- Overall status: PASS or FAIL
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
- Timestamp

**Only if triggered by a pull_request event**: Use `add_comment` to post a brief comment (max 5 lines) to the triggering PR with the summary and overall status.