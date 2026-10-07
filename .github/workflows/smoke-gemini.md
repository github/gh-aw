---
private: true
emoji: "🧪"
description: Smoke test workflow that validates Gemini engine functionality every two days
on:
  schedule: every 2 days
  slash_command:
    name: smoke-gemini
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "rocket"
  status-comment: true
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
name: Smoke Gemini
engine:
  id: gemini
  model: copilot/gemini-3.8-flash
strict: true
imports:
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
  - shared/playwright-title-test.md
  - uses: shared/session-artifact-check.md
    with:
      session-artifact: agent-stdio
network:
  allowed:
    - defaults
    - github
    - go
tools:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "*"
  web-fetch:
runtimes:
  go:
    version: "1.26"
safe-outputs:
    allowed-domains: [default-safe-outputs]
    threat-detection:
      engine: copilot
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      close-older-issues: true
      close-older-key: "smoke-gemini"
      labels: [automation, testing]
    add-labels:
      allowed: [smoke-gemini]
    messages:
      footer: "> ✨ *[{workflow_name}]({run_url}) — Powered by Gemini*{ai_credits_suffix}{history_link}"
      run-started: "✨ Gemini awakens... [{workflow_name}]({run_url}) begins its journey on this {event_type}..."
      run-success: "🚀 [{workflow_name}]({run_url}) **MISSION COMPLETE!** Gemini has spoken. ✨"
      run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Gemini encountered unexpected challenges..."
timeout-minutes: 10
sandbox:
  agent:
    id: awf
---

# Smoke Test: Gemini Engine Validation

## Test Requirements

Execute all 5 tests sequentially in this agent. Do not delegate to sub-agents.

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **Web Fetch Testing**: Use Gemini's native `web_fetch` tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright)
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-gemini-${{ github.run_id }}.txt` with content "Smoke test passed for Gemini at $(date)"
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as ❌ and report the failure.

After completing all tests, proceed to the Output section below.

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Gemini - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-gemini` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).