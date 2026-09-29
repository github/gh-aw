---
private: true
emoji: "🧪"
name: Smoke Repo-Memory Ledger
description: Smoke test for structured repo-memory ledger status, append, and query operations
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
  repo-memory:
    branch-name: memory/smoke-repo-memory-ledger
    description: "Smoke-test records for repo-memory ledger operations"
    allowed-extensions: [".jsonl"]
    ledger:
      compaction:
        min-segments: 32
        max-segments: 32
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

# Repo-Memory Ledger Smoke Test

Exercise ledger status, append, and query without writing ledger files directly.

1. Call `ledger_status`. Confirm the tool responds with ledger status and
   diagnostics; report any malformed or incomplete records, but do not fail this
   run solely because of unrelated historical diagnostics.
2. Call `ledger_query` for type `repo_memory_ledger_smoke` with
   `where: {"payload.workflow_run_id": {"eq": "${{ github.run_id }}"}}`.
3. If no record exists for this run, call `ledger_append` once with type
   `repo_memory_ledger_smoke` and payload:

   ```json
   {
     "workflow_run_id": "${{ github.run_id }}",
     "result": "passed"
   }
   ```

4. Query the same type and run ID again. Verify that a matching record exists
   and that its payload contains the expected run ID and result. On workflow
   reruns, reuse the existing run record instead of appending a duplicate.
5. Do not inspect or modify ledger shard files directly and do not invoke
   compaction; the trusted persistence job owns both.

If every check passes, call `noop` with a brief summary. If any ledger operation
fails or the round-trip record is incorrect, create one issue using the
`create_issue` safe output. Include the failed check, relevant redacted error
message, and run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
Never include secrets, raw ledger contents, or filesystem paths in the issue.
