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
model: copilot/gpt-5
engine:
  id: copilot
  fallback-models:
    - copilot/claude-sonnet-5
    - copilot/gpt-5.3-codex
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
evals:
  - id: sdk_file_readback
    question: Does the output show readback of the run-specific Copilot SDK test file containing the phrase smoke test passed?
  - id: bash_calculation
    question: Does the output show a bash calculation of 6 times 7 returning 42?
  - id: evidence_based_report
    question: Does the requested smoke issue report each required task's status consistently with the observed results rather than marking failed or unverified tasks as passed?
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
          engine: "copilot",
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: ["main"],
        });
---

# Smoke Test: Copilot SDK Engine Validation

## Tasks

1. **File Writing**: Create a file `/tmp/gh-aw/agent/smoke-copilot-sdk-${{ github.run_id }}.txt` with the content:
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