---
private: true
emoji: "🧪"
description: Smoke test workflow that validates DeepSeek Harness engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-deepseek-harness
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "eyes"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
name: Smoke DeepSeek Harness
model: copilot/auto
engine:
  id: deepseek-harness
strict: true
imports:
  - shared/deepseek-harness.md
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
    close-older-key: "smoke-deepseek-harness"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-deepseek-harness]
  messages:
    footer: "> *[{workflow_name}]({run_url}) — Powered by DeepSeek Harness*{ai_credits_suffix}{history_link}"
    run-started: "DeepSeek Harness initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "[{workflow_name}]({run_url}) DeepSeek Harness delivered."
    run-failure: "[{workflow_name}]({run_url}) {status}. DeepSeek Harness encountered an unexpected failure."
timeout-minutes: 15
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: DeepSeek Harness Engine Validation

## Test Requirements

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **Web Fetch Testing**: Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright for this test - use the web-fetch MCP tool directly)
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-deepseek-harness-${{ github.run_id }}.txt` with content "Smoke test passed for DeepSeek Harness at $(date)" (create the directory if it doesn't exist)
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as failed and report the failure.

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: DeepSeek Harness - ${{ github.run_id }}"
- Body should include:
  - Test results for each test
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with each test result and the overall PASS or FAIL status.

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-deepseek-harness` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).