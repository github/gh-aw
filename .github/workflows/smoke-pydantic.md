---
private: true
emoji: "🐍"
description: Smoke test workflow that validates Pydantic AI engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-pydantic
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
name: Smoke Pydantic AI
model: copilot/claude-sonnet-4-5
engine:
  id: pydantic-ai
strict: true
imports:
  - pydantic/pydantic-ai-harness/gh-aw/pydantic.md@8e863b5b88c9e41e638f0dc416b8946135b584b7
  - shared/smoke-test-brevity.md
  - shared/reporting.md
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
    close-older-key: "smoke-pydantic"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-pydantic]
  messages:
    footer: "> 🐍 *[{workflow_name}]({run_url}) — Powered by Pydantic AI*{ai_credits_suffix}{history_link}"
    run-started: "🐍 Pydantic AI initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🐍 [{workflow_name}]({run_url}) Pydantic AI delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Pydantic AI encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Pydantic AI Engine Validation

## Test Requirements

1. **Model Connectivity Testing**: Answer the question "What is 2 + 2?" in a single short line.
2. **MCP Tool Testing**: Confirm that the `safeoutputs` MCP tools are available to you.
3. **Coder Toolset Testing**: Use your `read_file` tool to read `README.md` from the repository root and quote its first line verbatim. Do not shell out and do not guess the content — this test only passes when the file tool returns it.

## Output

**ALWAYS create an issue** using the `create_issue` safe-output tool:
- Title: "Smoke Test: Pydantic AI - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test), including the quoted first line of `README.md`
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` safe-output tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) containing:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-pydantic` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).
