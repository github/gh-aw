---
private: true
emoji: "🪿"
description: Smoke test workflow that validates Goose engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-goose
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
  copilot-requests: write
name: Smoke Goose
model: copilot/auto
engine:
  id: goose
strict: true
imports:
  - shared/goose.md
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - github
tools:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "*"
  web-fetch:
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-goose"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-goose]
  messages:
    footer: "> 🪿 *[{workflow_name}]({run_url}) — Powered by Goose*{ai_credits_suffix}{history_link}"
    run-started: "🪿 Goose initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🪿 [{workflow_name}]({run_url}) Goose delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Goose encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Goose Engine Validation

## Test Requirements

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **Web Fetch Testing**: Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright for this test - use the web-fetch MCP tool directly)
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-goose-${{ github.run_id }}.txt` with content "Smoke test passed for Goose at $(date)" (create the directory if it doesn't exist)
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as ❌ and report the failure.

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Goose - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-goose` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).