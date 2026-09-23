---
private: true
emoji: "🧑‍✈️"
description: Smoke test workflow that validates Aider engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-aider
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "rocket"
  status-comment: true
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
name: Smoke Aider
model: copilot/auto
engine:
  id: aider
strict: true
imports:
  - shared/aider.md
  - shared/smoke-test-brevity.md
  - shared/reporting.md
network:
  allowed: []
tools:
  edit:
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
    close-older-key: "smoke-aider"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-aider]
  messages:
    footer: "> 🧑‍✈️ *[{workflow_name}]({run_url}) — Powered by Aider*{ai_credits_suffix}{history_link}"
    run-started: "🧑‍✈️ Aider initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🧑‍✈️ [{workflow_name}]({run_url}) Aider delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Aider encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Aider Engine Validation

Aider has no MCP client support, so this smoke test exercises only the CLI-native
capabilities: prompt delivery, shell commands, and file editing. Safe outputs are
emitted through the `safeoutputs` MCP CLI.

## Test Requirements

1. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-aider-${{ github.run_id }}.txt` with content "Smoke test passed for Aider" (create the directory if it doesn't exist)
2. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
3. **Repository Access Testing**: Run `git log --oneline -1` in the repository checkout and confirm a commit is reported

## Output

**ALWAYS create an issue** with `safeoutputs create_issue`, for example
`safeoutputs create_issue --title "..." --body "..."`:
- Title: "Smoke Test: Aider - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

**Only if this workflow was triggered by a pull_request event**: invoke
`safeoutputs add_comment --body "..."` with a **very brief**
comment (max 5-10 lines) containing:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, also invoke
`safeoutputs add_labels --labels "smoke-aider"`.