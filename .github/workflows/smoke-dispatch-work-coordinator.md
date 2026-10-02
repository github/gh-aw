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
  dispatch-work-coordinator: true
safe-outputs:
  steps:
    - name: Verify coordinator finish intent artifact
      run: |
        test -s /tmp/gh-aw/dispatch-work-coordinator.finish.jsonl
        grep -Fx '{"outcome":"completed"}' /tmp/gh-aw/dispatch-work-coordinator.finish.jsonl
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
