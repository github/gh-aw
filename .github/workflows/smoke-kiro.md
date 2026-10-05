---
private: true
emoji: "🧭"
description: Smoke test workflow that validates Kiro engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-kiro
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
name: Smoke Kiro
model: kiro/auto
engine:
  id: kiro
strict: true
imports:
  - shared/kiro.md
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - github
    - go
runtimes:
  go:
    version: "1.26"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-kiro"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-kiro]
  messages:
    footer: "> 🧭 *[{workflow_name}]({run_url}) — Powered by Kiro*{ai_credits_suffix}{history_link}"
    run-started: "🧭 Kiro initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🧭 [{workflow_name}]({run_url}) Kiro delivered."
    run-failure: "[{workflow_name}]({run_url}) {status}. Kiro encountered an unexpected error."
timeout-minutes: 15
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Kiro Engine Validation

## Test Requirements

1. **GitHub MCP Testing**: Use native GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only). Do not substitute `gh`, curl, or another CLI for this test. If native GitHub MCP tools are unavailable or fail, mark this test as FAIL.
2. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-kiro-${{ github.run_id }}.txt` with content "Smoke test passed for Kiro at $(date)" (create the directory if it doesn't exist)
3. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
4. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as failed and report the failure.
5. **Runtime Configuration Testing**: Verify `kiro-cli --version` matches `GH_AW_ENGINE_VERSION`. Use Node.js to inspect `GH_AW_MCP_CONFIG` and verify it contains a native GitHub MCP server with `type: "http"`, a container-reachable URL, and an authorization header. Report only PASS or FAIL; never print configuration contents, headers, keys, or the prompt.

## Output

**ALWAYS create an issue** with a concise summary of the smoke test run:
- Title: "Smoke Test: Kiro - ${{ github.run_id }}"
- Body should include:
  - Test results (PASS or FAIL for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- PASS or FAIL for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-kiro` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).