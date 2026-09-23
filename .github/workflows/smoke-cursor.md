---
private: true
emoji: "🖱️"
description: Smoke test workflow that validates Cursor engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-cursor
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
name: Smoke Cursor
model: cursor/auto
engine:
  id: cursor
strict: true
imports:
  - shared/cursor.md
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/smoke-test-brevity.md
network:
  allowed: []
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-cursor"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-cursor]
  messages:
    footer: "> 🖱️ *[{workflow_name}]({run_url}) — Powered by Cursor*{ai_credits_suffix}{history_link}"
    run-started: "🖱️ Cursor initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🚀 [{workflow_name}]({run_url}) Cursor delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Cursor encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Cursor Engine Validation

## Test Requirements

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-cursor-${{ github.run_id }}.txt` with content "Smoke test passed for Cursor at $(date)" (create the directory if it doesn't exist)
3. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
4. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as failed and report the failure.

## Output

**ALWAYS create an issue** with a concise summary of the smoke test run:
- Title: "Smoke Test: Cursor - ${{ github.run_id }}"
- Body should include:
  - Test results (PASS or FAIL for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- PASS or FAIL for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-cursor` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).