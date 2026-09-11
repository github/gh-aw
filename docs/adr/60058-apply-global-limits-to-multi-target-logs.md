# ADR-60058: Apply Global Limits to Multi-Target Logs

**Date**: 2026-09-10
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command already accepts multiple workflow targets and downloads them concurrently into a single combined report. This PR changes that path because the existing behavior applied `--timeout` and `--count` independently per target, which let queued or parallel targets exceed the user’s intended global budget. The diff shows new shared coordination for timeout and count handling, preservation of partial results when a target fails mid-collection, and updated continuation behavior for queued targets that make no progress. The architectural question is how multi-target log collection should enforce user-supplied limits when several targets are processed within one invocation.

### Decision

We will treat `--timeout` and `--count` as global limits for the entire multi-target `gh aw logs` operation rather than per-target limits. We decided to enforce one shared wall-clock context and one shared atomic count budget across concurrent targets because the PR evidence shows users expect a single invocation budget, resumable partial results, and a globally sorted capped result set. This keeps CLI semantics aligned with the combined-report model instead of letting each target consume a fresh independent quota.

### Alternatives Considered

#### Alternative 1: Keep timeout and count limits per target

The command could continue giving each workflow target its own timeout window and count quota. This was considered because it is simpler and preserves the previous implementation model where each target runs mostly independently. It was not chosen because the PR description and tests show this leads to surprising behavior in combined downloads, including queued targets receiving fresh time budgets and total returned runs exceeding the user’s requested count.

#### Alternative 2: Process multiple targets sequentially under one shared budget

Another option would be to stop running targets concurrently and instead process them one at a time while decrementing a shared timeout and count budget. This was considered because it would simplify global coordination and avoid cross-target atomic accounting. It was not chosen because the existing command model explicitly supports concurrent target processing, and this PR preserves that model while adding shared limits instead of trading off performance and responsiveness.

#### Alternative 3: Enforce limits only after collecting all per-target results

The system could collect runs independently from each target, then globally sort and trim the merged result set at the end. This was considered because it would minimize invasive changes to the collection pipeline. It was not chosen because the diff shows the desired behavior is not just output trimming: the operation must stop active and queued work when the shared count or timeout is exhausted, preserve partial progress, and generate resumable continuations for targets that did not get to run.

### Consequences

#### Positive
- Users get predictable multi-target semantics where a single `gh aw logs ... --count N --timeout T` invocation respects one combined run budget and one total wall-clock deadline.
- Concurrent targets can still run in parallel while sharing an atomic count limit, preserving performance benefits without over-collecting runs.
- Partial runs and continuation cursors are preserved when a target fails or when shared limits stop queued targets, improving resumability.

#### Negative
- The logs collection pipeline becomes more complex because timeout and count coordination must now be shared safely across multiple concurrent targets.
- Shared-limit behavior introduces more edge cases in continuation generation, partial-failure handling, and cached-run accounting that require broader tests and future maintenance.
- Per-target independence is reduced, which may surprise anyone who previously relied on each target receiving a full fresh quota.

#### Neutral
- CLI help and reference documentation now explicitly describe multi-target `--count` and `--timeout` semantics.
- The implementation adds shared state types such as a global count limiter and passes them through existing orchestrator options.
- Result ordering remains globally sorted after collection, but final limiting now reflects the combined operation contract rather than target-local collection behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
