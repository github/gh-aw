---
private: true
emoji: "💾"
description: Smoke test workflow that validates experimental GitHub Drives memory
on:
  schedule: every 2 days
  slash_command:
    name: smoke-drive
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "rocket"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
name: Smoke Drive
model: copilot/gpt-5.3-codex
engine:
  id: codex
strict: true
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
tools:
  drive-memory:
    drive-name: smoke-drive
    disk-size: 100M
    prefetch: true
    allowed-extensions: [".txt"]
  bash:
    - "*"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-drive"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-drive]
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Drive Memory Validation

## Test Requirements

1. **Drive memory persistence**: Read `/tmp/gh-aw/drive-memory/smoke-drive.txt` if it exists. Then replace it with exactly `run_id=${{ github.run_id }}` followed by a newline, and read it back. Do not create files with extensions other than `.txt` in the drive-memory directory.
2. **Repository access**: Run `git log --oneline -1` in the repository checkout and confirm a commit is reported.

## Output

**ALWAYS create an issue** using the `create_issue` safe-output tool:
- Title: "Smoke Test: Drive - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test), including whether a previous drive-memory value was found
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` safe-output tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) containing:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-drive` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).
