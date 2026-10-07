---
private: true
emoji: "🧪"
name: Smoke Work Queue
description: Smoke test for queue snapshot tools and rejection of unassigned finish
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
  create-issue:
    max: 1
    title-prefix: "[smoke-work-queue] "
    labels: [automation, testing]
    close-older-issues: true
    close-older-key: "smoke-work-queue"
  noop:
jobs:
  verify_smoke_result:
    needs: [agent, safe_outputs]
    runs-on: ubuntu-slim
    timeout-minutes: 3
    permissions:
      actions: read
    steps:
      - name: Download smoke evidence
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          name: agent
          path: /tmp/gh-aw/
      - name: Verify smoke result
        run: |
          if grep -Eq '"type"[[:space:]]*:[[:space:]]*"create_issue"' /tmp/gh-aw/safeoutputs.jsonl; then
            echo "::error::Work queue smoke test reported a tool failure"
            exit 1
          fi
          if test -s /tmp/gh-aw/work-queue.finish.jsonl; then
            echo "::error::An unassigned observer recorded a worker finish intent"
            exit 1
          fi
          grep -Eq '"type"[[:space:]]*:[[:space:]]*"noop"' /tmp/gh-aw/safeoutputs.jsonl
timeout-minutes: 10
strict: true
features:
  gh-aw-detection: false
---

# Work Queue Smoke Test

Exercise the queue MCP server mounted from the activation snapshot and rejection
of unassigned finish attempts. This workflow is a read-only queue observer, not a
dispatcher or worker; it must not mutate the durable queue log. Its ordinary
failure-report and `noop` safe outputs use normal workflow authorization, not
queue-control or worker Claim authority.

Use the tool interface advertised in the runtime prompt. If `work-queue` is
listed in `<mcp-clis>`, invoke `work-queue work_queue_read` and
`work-queue work_queue_claim_finish` from bash with JSON arguments. The tool names
are subcommands, not standalone executables.

1. Call `work_queue_read` with
   `work: "__gh_aw_smoke__-${{ github.run_id }}"`. Verify the returned work is
   `absent` and that the response includes `snapshot_sha` (which is `null` when
   the queue branch does not exist).
2. Call `work_queue_explain` for the same absent Work and confirm that its
   explanation is explicitly snapshot-based, not a durable grant.
3. If `work_queue_claim_finish` is advertised, call it with
   `outcome: "completed"` and verify that the missing assignment is rejected
   without recording a finish intent. If observer tools omit this mutator,
   verify that it is unavailable. Do not invent a Claim selector or write an
   intent file manually.
4. If the observer checks pass, call `noop` with a concise success summary.
5. If a check fails, create one issue with the failed tool name and this
   run URL:
   `${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
   Do not include snapshot contents, identifiers other than the run ID, or
   unredacted errors in the issue.

The final verification job fails the run after processing any failure issue or
recorded worker finish. A `noop` with no finish artifact counts as smoke success,
not proof of native run binding, effect delivery, or full runtime conformance.
