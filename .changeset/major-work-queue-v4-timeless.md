---
"gh-aw": major
---

Replace the work-queue ledger protocol with version 4: no installed Policy, no
clocks in scheduling or recovery, and no delivery-verification barriers. Pool
limits now come from the compiled `.github/workflows/aw.json` configuration on
every run, so changing `work_queue.pools.<pool>.concurrency` takes effect on the
next dispatcher run without draining the queue.

- Scheduling is priority first, then submission order (FIFO), bounded by each
  pool's `concurrency` and optional `per_account_limit`. One Claim per Dispatch.
- A dispatcher records each successful `workflow_dispatch` launch; a failed launch
  returns its Work to the pool. Workers bind their own run, finish with a
  `Completion` that records the effects they applied, or a cancellation that
  counts one attempt. Work is cancelled after `retry.max_attempts` attempts.
- Scheduled `agentics-maintenance` releases Claims whose run completed or never
  appeared, checkpoints the ledger after 512 lines, and projects backing Issues.
  No step waits, sleeps or expires Work by time.
- The first v4 dispatcher, producer or maintenance run converts an existing v3
  ledger: available Work (with its linked Issue) is carried over in FIFO order;
  claimed, completed and cancelled v3 Work is not. Git history keeps the v3 log.

**Breaking changes and migration:**

- `aw.json` `work_queue`: remove `mode`, `class_weights`, `accounting_weights`
  and `retry.backoff_seconds`; they are rejected. `concurrency`,
  `pending_limit`, `retry.max_attempts`, `pools` and `issues` are unchanged.
- Frontmatter `work-queue-policy` and `safe-outputs.dispatch-workflow.authorization`
  are removed; delete them and recompile.
- Worker `work_queue_assignment` input is now `{"version":4,"dispatch_id":"…"}`;
  recompile workers and dispatchers together.
- `gh aw work-queue sync-issues` and the `work_queue_explain` MCP tool are
  removed; `work-queue state` and `inspect` show Dispatch state, bound run and
  recorded effects instead of delivery barriers and policy epochs.
- Effects are recorded at-least-once: if a worker job dies after applying an
  effect but before finishing, maintenance releases the Claim and the Work may
  run again.
