---
private: true
emoji: "🪶"
description: Smoke Copilot Small
on:
  schedule: every 2 days
  slash_command:
    name: smoke-copilot-small
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  label_command:
    name: smoke
    events: [pull_request]
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
name: Smoke Copilot Small
model: small
engine:
  id: copilot
  bare: true
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
  - shared/playwright-title-test.md
tools:
  bash:
    - "*"
  edit:
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-small"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

# Smoke Test: Copilot Small Model Validation

## Tasks

1. **Write a haiku**: Compose an original 3-line haiku about software testing and save it to `/tmp/smoke-copilot-small-${{ github.run_id }}.txt`.

2. **Verify**: Read the file back with `cat` and confirm it contains exactly 3 lines.

3. **Bash calculation**: Run a bash command to compute `echo $((6 * 7))` and confirm the output is `42`.

## Output

Create an issue titled **"Smoke Test: Copilot Small - ${{ github.run_id }}"** with:
- The haiku from task 1
- ✅ or ❌ for each task above
- Overall status: PASS or FAIL
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}