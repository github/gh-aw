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
runtimes:
  python:
    version: "3.11"
imports:
  - uses: shared/session-artifact-check.md
    with:
      session-artifact: copilot-events
sandbox:
  agent:
    id: awf
tools:
  ledger:
    smoke:
      type: table
      key: workflow_run_id
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
    question: Did the agent validate a prior persisted record and submit the current run record if absent?
---

# Git-Backed Ledger Smoke Test

Exercise the replayed read-only SQLite projection and safe-output append without editing ledger files directly.
Use the configured Python runtime's `sqlite3` standard library with `mode=ro`
to query the projection. It is a snapshot made before this run's safe outputs;
accepted writes become durable only after `push_ledger_changes` succeeds.

1. Query the projection at `/tmp/gh-aw/ledgers/smoke/ledger.db` for diagnostics.
   Report malformed or incomplete records, but do not fail solely because of
   unrelated historical diagnostics.
2. Query `state` for prior run records. Verify any existing rows have a
   `key` matching the JSON `workflow_run_id`, with `record_type` equal to
   `repo_memory_ledger_smoke` and `result` equal to `passed`. An empty table
   is expected on the first successful run. Also query for the current run:

   ```sql
   SELECT key, value FROM state
   WHERE key = '${{ github.run_id }}'
   LIMIT 1;
   ```

3. If no record exists, submit one `ledger_append` record:

   ```json
   {
     "ledger": "smoke",
     "operation": "upsert",
     "value": {
       "record_type": "repo_memory_ledger_smoke",
       "workflow_run_id": "${{ github.run_id }}",
       "result": "passed"
     }
   }
   ```

4. Do not expect the current run's append to appear in this run's projection.
   Verify it on the next run; on reruns, reuse an existing current-run row
   instead of appending a duplicate.
5. Do not inspect or modify ledger shard files directly.

If every check passes, call `noop` with a brief summary. If any ledger operation
fails or a prior persisted record is incorrect, create one issue using the
`create_issue` safe output. Include the failed check, relevant redacted error
message, and run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
Never include secrets, raw ledger contents, or filesystem paths in the issue.
