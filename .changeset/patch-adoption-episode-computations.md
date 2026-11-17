---
"gh-aw": patch
---

Use shared delivery/adoption reporting in `gh aw logs --adoption` and single-run
`audit --adoption`. Remove the scheduled collector and duplicate JavaScript evaluators,
and have the impact report consume CLI classifications. Preserve dispatched run IDs
and destination repositories, and use the linked agent PR's repository for assignment
effect checks.

Keep ambiguous legacy workflow calls in separate episodes and make conflicting lineage deterministic. Populate AIC per adopted output from full observed run usage, including detection, independently of agent-only caches. Warn on unavailable usage without discarding evaluated observations.

Expose parent repository, run, attempt, workflow, call identity, and event type from `aw_context` in unified session episode metadata. Record the executing workflow's compiler-owned identity separately from the caller's workflow reference. Add the current `workflow_call_id` to `aw_info.json` and preserve it as `workflowCallId` in the unified session. Preserve the current run attempt and legacy lineage inputs.
