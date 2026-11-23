---
"gh-aw": patch
---

Durable session restore now accepts an explicit parent only from `aw_context.continuation_run_id`, which must be a positive integer for a different, completed run of the same workflow, repository, and branch. Behavior change: dispatches that carry `aw_context` without `continuation_run_id` (slash-command router, `dispatch-workflow`, and `call-workflow` runs) now start a fresh durable session instead of restoring from `run_id` or `parent_run_id`. To continue from a specific run, set `aw_context.continuation_run_id` to that run ID. Runs without `aw_context` still restore from the latest successful run on the same branch.
