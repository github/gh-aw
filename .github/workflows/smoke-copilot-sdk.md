---
private: true
emoji: "🔬"
description: Smoke Copilot SDK
on:
  schedule: every 2 days
  slash_command:
    name: smoke-copilot-sdk
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  label_command:
    name: smoke-sdk
    events: [pull_request]
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
name: Smoke Copilot SDK
model: gpt-5.4
engine:
  id: copilot
  copilot-sdk: true
  bare: true
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
max-tool-denials: 3
tools:
  bash:
    - "*"
  edit:
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-sdk"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Copilot SDK Engine Validation

## Tasks

1. **File Writing**: Create a file `/tmp/smoke-copilot-sdk-${{ github.run_id }}.txt` with the content:
   ```
   Copilot SDK smoke test passed at <current date/time>
   ```
   Create the directory if it does not exist.

2. **Verify**: Read the file back with `cat` and confirm it contains the phrase "smoke test passed".

3. **Bash calculation**: Run a bash command to compute `echo $((6 * 7))` and confirm the output is `42`.

## Output

Create an issue titled **"Smoke Test: Copilot SDK - ${{ github.run_id }}"** with:
- ✅ or ❌ for each task above
- Overall status: PASS or FAIL
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}