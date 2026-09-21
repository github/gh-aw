---
private: true
emoji: "🧪"
name: Smoke Multi PR
description: Test creating multiple pull requests in a single workflow run
on:
  schedule: every 2 days
  slash_command:
    name: smoke-multi-pr
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke-multi-pr"]
  reaction: "eyes"
  status-comment: true
permissions:
  contents: read
  pull-requests: read
engine: copilot
strict: true
network:
  allowed:
    - defaults
    - node
tools:
  cli-proxy: true
  edit:
  bash:
    - "date"
    - "echo *"
    - "printf *"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  create-pull-request:
    title-prefix: "[smoke-multi-pr] "
    if-no-changes: "warn"
    labels: [ai-generated]
    expires: 2h
    max: 2
  add-comment:
    hide-older-comments: true
    max: 1
  messages:
    append-only-comments: true
    footer: "> 🧪 *Multi PR smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🧪 [{workflow_name}]({run_url}) is now testing multiple PR creation..."
    run-success: "✅ [{workflow_name}]({run_url}) successfully created multiple PRs."
    run-failure: "❌ [{workflow_name}]({run_url}) failed to create multiple PRs. Check the logs."
timeout-minutes: 10
imports:
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Multiple Pull Request Creation

This workflow validates that multiple pull requests can be created in a single workflow run.

## Test Requirements

Create exactly TWO pull requests with distinct changes:

### PR 1: Documentation Update

1. Create a separate new branch off main `smoke-multi-pr-doc-${{ github.run_id }}-1` for the first PR
2. Create a file `tmp-smoke-multi-pr-doc-${{ github.run_id }}-1.txt` with content:
   ```
   Documentation smoke test for multi-PR workflow
   Run ID: ${{ github.run_id }}
   Created at: [current timestamp using date command]
   PR: 1 of 2
   ```
3. Create a pull request with:
   - Title: "PR 1: Documentation smoke test"
   - Body: "First of two PRs created by smoke-multi-pr workflow run ${{ github.run_id }}."

### PR 2: CRLF Line Endings Test

This PR specifically tests that the `create_pull_request` safe output correctly handles files with CRLF (Windows-style) line endings.

1. Create a separate new branch off main `smoke-multi-pr-crlf-${{ github.run_id }}-2` for the second PR
2. Create a file `tmp-smoke-multi-pr-crlf-${{ github.run_id }}-2.txt` with **CRLF line endings** using `printf`:
   ```bash
   printf "CRLF smoke test for multi-PR workflow\r\nRun ID: ${{ github.run_id }}\r\nCreated at: $(date)\r\nPR: 2 of 2\r\nLine ending: CRLF (Windows-style \\r\\n)\r\n" > tmp-smoke-multi-pr-crlf-${{ github.run_id }}-2.txt
   ```
   Verify the file has CRLF endings by running `cat -A tmp-smoke-multi-pr-crlf-${{ github.run_id }}-2.txt` — each line should end with `^M$`.
3. Create a pull request with:
   - Title: "PR 2: CRLF line endings smoke test"
   - Body: "Second of two PRs created by smoke-multi-pr workflow run ${{ github.run_id }}. This PR tests that patch application works correctly with CRLF line endings."

## Success Criteria

Both PRs must be created successfully. After creating both PRs, add a comment to the triggering context summarizing:
- The two PR numbers created
- Links to both PRs
- Confirmation that multi-PR creation is working
- Confirmation that CRLF line endings were handled correctly (PR 2)