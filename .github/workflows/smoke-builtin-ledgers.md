---
private: true
emoji: "🧪"
name: Smoke Built-in Ledgers
description: Integration smoke test for log, set, map, table, counter, and work-pool ledgers
on:
  schedule: every 2 days
  workflow_dispatch:
concurrency:
  group: smoke-builtin-ledgers
  cancel-in-progress: false
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
  ledger:
    log:
      type: log
      compaction: false
      schema:
        type: object
        required: [workflow_run_id]
        properties:
          workflow_run_id: { type: string }
        additionalProperties: false
    set:
      type: set
      compaction: false
      schema:
        type: string
    map:
      type: map
      compaction: false
      schema:
        type: object
        required: [workflow_run_id]
        properties:
          workflow_run_id: { type: string }
        additionalProperties: false
    table:
      type: table
      key: id
      compaction: false
      schema:
        type: object
        required: [id, status]
        properties:
          id: { type: string }
          status:
            enum: [inserted, updated, passed]
        additionalProperties: false
    counter:
      type: counter
      compaction: false
    pool:
      type: work-pool
      identity: [workflow_run_id]
      compaction: false
      schema:
        type: object
        required: [workflow_run_id]
        properties:
          workflow_run_id: { type: string }
        additionalProperties: false
safe-outputs:
  create-issue:
    max: 1
    title-prefix: "[smoke-builtin-ledgers] "
    labels: [automation, testing]
    close-older-issues: true
    close-older-key: "smoke-builtin-ledgers"
  noop:
timeout-minutes: 10
strict: true
features:
  gh-aw-detection: false
---

# Built-in Ledger Integration Smoke Test

Exercise all built-in ledger types through their safe-output operations and verify
the persisted state projection. Writes from a run are projected at the start of
the next run, so validate existing state before submitting this run's writes.

1. Query `/tmp/gh-aw/ledgers/{log,set,map,table,counter,pool}/ledger.db` read-only.
   Confirm each `state` table and its columns exist: `log(position, value)`,
   `set(identity, value)`, `map(key, value)`, `table(key, value)`, and
   `counter(name, value)`. For `pool`, check `work(work_id, payload, state,
   effective_claim_id)` and `claims(claim_id, work_id, claimant, generation,
   previous_claim_id, released, effective, superseded)`. Check prior values
   have valid shapes. For `table`, parse the JSON in `value` and confirm its
   `id` matches `key` and its `status` matches the schema. Do not fail the
   first run because the tables are empty.
2. For `set`, `map`, and `table`, remove the previous run's marker/row if present
   and its ID differs from the current `${{ github.run_id }}`. Use `remove` with
   the old string value for `set`, `ledger_map_delete` with the old `key` for
   `map`, and `ledger_append` with `delete` for `table`. This keeps their state
   bounded and exercises deletion. `log` is append-only.
3. Submit these `ledger_append` calls for the other built-in types:

   ```json
   {"ledger":"log","operation":"append","value":{"workflow_run_id":"${{ github.run_id }}"}}
   {"ledger":"set","operation":"add","value":"${{ github.run_id }}"}
   {"ledger":"table","operation":"insert","value":{"id":"${{ github.run_id }}","status":"inserted"}}
   {"ledger":"table","operation":"update","key":"${{ github.run_id }}","patch":{"status":"updated"}}
   {"ledger":"table","operation":"upsert","value":{"id":"${{ github.run_id }}","status":"passed"}}
   {"ledger":"counter","operation":"increment","name":"smoke","amount":1}
   {"ledger":"counter","operation":"decrement","name":"smoke","amount":1}
   ```

   Use `ledger_map_put` with
   `{"ledger":"map","key":"${{ github.run_id }}","value":{"workflow_run_id":"${{ github.run_id }}"}}`.
   Use `ledger_work_pool_submit` and then `ledger_work_pool_cancel`, each with
   `{"ledger":"pool","work":{"workflow_run_id":"${{ github.run_id }}"}}`.
   Do not use `ledger_append` for `map` or `pool`.

   If this run ID is already in the table projection, use `upsert` instead of
   `insert` for the first table call. Do not send a top-level `key` with
   `insert` or `upsert`; the primary key is `value.id`.
4. Do not claim same-run writes are visible in the read-only projection. On the
   next invocation, verify the prior run's log, set member, map entry (before
   deletion), table row, counter value, and cancelled pool work. Never inspect
   or modify ledger shard files directly.

If every check passes, call `noop` with a brief summary. If any check fails,
create one issue with the failed check and run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
Never include secrets or raw ledger contents in the issue.
