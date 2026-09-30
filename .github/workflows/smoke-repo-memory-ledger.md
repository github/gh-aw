---
private: true
emoji: "🧪"
name: Smoke Repo-Memory Ledger
description: Smoke test for structured Git-backed ledger projections and append operations
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
model: copilot/gpt-5.3-codex
imports:
  - uses: shared/session-artifact-check.md
    with:
      session-artifact: copilot-events
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  ledger:
    smoke:
      schema:
        type: object
        required: [record_type, workflow_run_id, result]
        properties:
          record_type:
            enum: [repo_memory_ledger_smoke]
          workflow_run_id:
            type: string
          result:
            enum: [passed]
        additionalProperties: false
      replay:
        script: |
          return {
            tables: {
              runs: {
                columns: { workflow_run_id: "text", result: "text" },
                primaryKey: ["workflow_run_id"],
                rows: records
                  .filter(record => record.payload.record_type === "repo_memory_ledger_smoke")
                  .map(({ payload }) => ({
                    workflow_run_id: payload.workflow_run_id,
                    result: payload.result
                  }))
              }
            }
          };
      max-record-kb: 4
      max-patch-kb: 10
safe-outputs:
  create-issue:
    max: 1
    title-prefix: "[smoke-repo-memory-ledger] "
    labels: [automation, testing]
    close-older-issues: true
    close-older-key: "smoke-repo-memory-ledger"
  noop:
timeout-minutes: 10
strict: true
features:
  gh-aw-detection: false
evals:
  - id: ledger_status_checked
    question: Did the agent successfully inspect repo-memory ledger status?
  - id: ledger_record_round_trip
    question: Did the agent append or find the current run record and verify it with a ledger query?
---

# Git-Backed Ledger Smoke Test

Exercise the replayed read-only SQLite projection and safe-output append without editing ledger files directly.

1. Query the projection at `/tmp/gh-aw/ledgers/smoke/ledger.db` for diagnostics.
   Report malformed or incomplete records, but do not fail solely because of
   unrelated historical diagnostics.
2. Query the projection for the current run:

   ```sql
   SELECT workflow_run_id, result FROM runs
   WHERE workflow_run_id = '${{ github.run_id }}'
   LIMIT 1;
   ```

3. If no record exists, submit one `ledger_append` record:

   ```json
   {
     "record_type": "repo_memory_ledger_smoke",
     "workflow_run_id": "${{ github.run_id }}",
     "result": "passed"
   }
   ```

4. Query `runs` for the same run ID again. Verify it contains the expected run ID
   and result. On reruns, reuse the existing record instead of appending a duplicate.
5. Do not inspect or modify ledger shard files directly.

If every check passes, call `noop` with a brief summary. If any ledger operation
fails or the round-trip record is incorrect, create one issue using the
`create_issue` safe output. Include the failed check, relevant redacted error
message, and run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
Never include secrets, raw ledger contents, or filesystem paths in the issue.
