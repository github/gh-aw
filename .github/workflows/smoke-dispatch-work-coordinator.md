---
private: true
emoji: "🧪"
name: Smoke Dispatch Work Coordinator
description: End-to-end smoke test for dispatch coordinator snapshot and finish tools
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  model: copilot/gpt-5.3-codex
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  work-queue: true
safe-outputs:
  steps:
    - name: Verify coordinator finish intent artifact
      uses: actions/github-script@v9.0.0
      env:
        GH_AW_VERIFY_CONFIG: '{"safeOutputsPath":"/tmp/gh-aw/safeoutputs.jsonl","finishIntentPath":"/tmp/gh-aw/dispatch-work-coordinator.finish.jsonl"}'
      with:
        script: |
          const fs = require('node:fs');
          const { safeOutputsPath, finishIntentPath } = JSON.parse(process.env.GH_AW_VERIFY_CONFIG);
          const records = (path) => fs.readFileSync(path, 'utf8')
            .split('\n').filter(Boolean).map((line) => JSON.parse(line));

          if (records(safeOutputsPath).some((record) => record?.type === 'create_issue')) {
            core.info('Dispatch coordinator smoke failure reported; processing the failure issue');
          } else if (!fs.statSync(finishIntentPath).size ||
                     !records(finishIntentPath).some((record) => record?.outcome === 'completed')) {
            throw new Error('Dispatch coordinator finish intent artifact is missing or incomplete');
          }
  create-issue:
    max: 1
    title-prefix: "[smoke-dispatch-work-coordinator] "
    labels: [automation, testing]
    close-older-issues: true
    close-older-key: "smoke-dispatch-work-coordinator"
  noop:
timeout-minutes: 10
strict: true
features:
  gh-aw-detection: false
---

# Dispatch Work Coordinator Smoke Test

Exercise the coordinator MCP server mounted from the activation snapshot and the
trusted safe-output finish-intent path. This workflow has no inbound worker claim,
so it must not mutate the durable coordinator log.

1. Call `dispatch_work_coordinator_read` with
   `work: "__gh_aw_smoke__-${{ github.run_id }}"`. Verify the returned work is
   `absent` and that the response includes a snapshot version.
2. Call `dispatch_claim_finish` with `outcome: "completed"`. Verify it reports
   that the finish intent was recorded. Do not supply work or claim identifiers.
   The safe-output check must find this finish intent in the downloaded agent
   artifact before the `noop` handler runs.
3. If both tool checks pass, call `noop` with a concise success summary.
4. If either check fails, create one issue with the failed tool name and this
   run URL:
   `${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
   Do not include snapshot contents, identifiers other than the run ID, or
   unredacted errors in the issue.
