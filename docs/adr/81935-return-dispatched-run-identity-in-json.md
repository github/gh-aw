# 81935. Return dispatched workflow run identity in JSON output

- **Status:** Draft
- **Date:** 2026-11-28
- **PR:** [#81935](https://github.com/github/gh-aw/pull/81935)

## Context

`gh aw run --json` writes a JSON array with one result per triggered workflow. Automations that consume this output need to identify the exact run created by each dispatch. They should not parse stderr, and they should not rely on a latest-run lookup, because that lookup can select a different dispatch of the same workflow.

Before this change, `resolveWorkflowRunInfo` in `pkg/cli/run_workflow_execution.go` accepted any run URL found in the dispatch output. When the output contained no run info, it fell back to `getLatestWorkflowRunWithRetry`. JSON results could therefore attribute a run that was not the one this command dispatched. `WorkflowRunResult` had no field that said whether the identity was known.

The PR does not state which `gh` CLI versions print the run URL from `gh workflow run`. That output format is treated here as an external dependency.

## Decision

In JSON mode, the identity of a dispatched run comes only from the dispatch response, and the command never infers it from the latest run:

1. `resolveWorkflowRunInfo` accepts dispatch output in JSON mode only when the trimmed output is exactly a run URL with a positive run ID. Otherwise it returns `dispatch did not return a workflow run identity`, and it does not call the latest-run lookup.
2. `WorkflowRunResult` gains a `run_identity` field with the value `known` or `unknown`. A known identity sets `run_id` and `run_url`. An unknown identity omits both fields.
3. An unknown identity does not mean the dispatch failed. The result keeps `status` as `triggered`, and `--wait` does not wait on an unidentified run.
4. With `--repeat` in JSON mode, the command stops if a dispatch succeeds but its identity is unknown. The command reports an error instead of starting another iteration. Any error in an iteration also stops further iterations, and each iteration writes its own JSON array.
5. The change is additive. The result array shape and the existing `workflow`, `lock_file`, `status`, and `error` fields are preserved.

## Alternatives Considered

1. **Keep the latest-run lookup as a fallback in JSON mode.** This keeps every result populated, but the fallback can select another dispatch of the same workflow. That is the failure this change is meant to remove, so it was rejected.
2. **Correlate the dispatch with the workflow run list (by time window or workflow ID).** This would work without relying on the dispatch output. The PR's tests assert that no `run list` or `/runs` calls are made, which rules out this approach. Correlation by time can also match a concurrent dispatch.
3. **Treat an unknown identity as a dispatch error.** This keeps the result shape simpler, but it would report a started workflow as failed. The PR instead keeps `status` as `triggered` and marks only the identity as `unknown`.

## Consequences

### Positive

- Automations can attribute each `triggered` result to the exact run that was dispatched, using `run_id` and `run_url`, without parsing stderr.
- Repeat mode cannot start a new dispatch while the previous dispatch is still unidentified and untracked.
- Existing consumers keep the same field names. `run_identity` is additive and documented in `docs/src/content/docs/setup/cli.md`.

### Negative

- Consumers that assumed `run_id` is always present for `triggered` results must check `run_identity` first. `run_id` and `run_url` are omitted when the identity is unknown.
- `--repeat --json` can stop earlier than before when `gh workflow run` does not print the run URL.
- JSON mode no longer falls back to the latest-run lookup. A change in the `gh` output format would now produce unknown identities rather than a best-effort match.
