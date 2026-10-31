---
private: true
emoji: "🧪"
description: Smoke test workflow that validates Pi engine functionality with Copilot auto model resolution
on:
  schedule: every 2 days
  slash_command:
    name: smoke-pi
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "rocket"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
name: Smoke Pi
model: copilot/auto
engine:
  id: pi
  config: '{"mcp":{"exposure":"direct"}}'
  fallback-models:
    - copilot/gpt-5.6-luna
    - copilot/claude-sonnet-5
max-tool-calls: 80
strict: true
sandbox:
  agent:
    config:
      filesystem:
        allow-write:
          - ${{ github.workspace }}
          - /tmp/gh-aw/agent
          - /tmp/gh-aw/pi-agent-dir
runtimes:
  node: {}
imports:
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - github
    - go
tools:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
    mode: gh-proxy
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
      close-older-key: "smoke-pi"
      labels: [automation, testing]
    add-labels:
      allowed: [smoke-pi]
    messages:
      footer: "> 🥧 *[{workflow_name}]({run_url}) — Powered by Pi*{ai_credits_suffix}{history_link}"
      run-started: "🥧 Pi initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
      run-success: "🚀 [{workflow_name}]({run_url}) **MISSION COMPLETE!** Pi delivered. 🥧"
      run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Pi encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
post-steps:
  - name: Assert unified-session contract
    if: always()
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const path = require("node:path");
        const { sessionContractMain } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/smoke_model_routing_assertions.cjs"));
        await sessionContractMain({
          core,
          engine: "pi",
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: ["main"],
        });
---

# Smoke Test: Pi Engine Validation

## Test Requirements

Execute the following tests sequentially. Wait for each tool result and verify its
success before starting the next test; use additional turns as needed. Never submit
file creation, its readback, and Codemode verification as parallel tool calls.

1. **GitHub CLI Testing**: Use `gh` CLI commands to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **Web Fetch Testing**: Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright for this test - use the web-fetch MCP tool directly)
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-pi-${{ github.run_id }}.txt` with content "Smoke test passed for Pi at $(date)" (create the directory if it doesn't exist)
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as ❌ and report the failure.
6. **Codemode Testing**: Only after the file creation and readback succeed, use the native `codemode` tool's provided tool-call API to invoke the registered Bash tool for `cat /tmp/gh-aw/agent/smoke-test-pi-${{ github.run_id }}.txt` and `echo pi-codemode-ok` in parallel. Await both calls and return only their actual file contents and command output. Verify both results. Codemode is not Node.js: do not use `require`, import filesystem modules, or fabricate the echo result as a constant.

## Output

Call the native safe-output tools directly: `create_issue`, `add_comment`, and
`add_labels`. Never use `gh issue create`, `gh pr comment`, or other `gh` write
commands: the GitHub proxy is read-only. `noop` and `report_incomplete` are tool
calls, not shell commands or JSON to print. A fenced JSON example is not a tool call.

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Pi - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-pi` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).

Record failed tests in the issue too; a failed test does not remove the reporting
requirement. If you cannot submit the required summary, call `report_incomplete`
with the actual blocker. Do not substitute `noop` for a missing report.