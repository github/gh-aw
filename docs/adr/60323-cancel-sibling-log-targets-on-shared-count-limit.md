# ADR-60323: Cancel Sibling Log Targets on Shared Count Limit

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command already supports downloading multiple workflow targets concurrently under one invocation. PR #60323 fixes a case where `--count N` was intended to be a shared maximum across targets, but a target already mid-batch could continue running after a sibling exhausted that shared budget, causing over-fetching and delayed stop behavior. The diff adds shared cancellation wiring in the multi-target orchestration path, updates cancellation handling so shared-budget stops are treated as successful completion rather than user-facing errors, and isolates new regression coverage in CI. The architectural question is how multi-target log collection should stop active sibling work once a shared run-count budget is reached.

### Decision

We will cancel the shared multi-target logs context as soon as the shared `--count` budget is exhausted. We decided to wire the shared `logsCountLimit` to a single cancellable context used by all concurrent targets, because the PR evidence shows that merely checking the count limit between iterations is not enough to stop targets already queued or in flight. We will also treat that resulting `context.Canceled` state as an expected, silent success path when it was caused by the shared count limit.

### Alternatives Considered

#### Alternative 1: Keep polling the shared count limit between batches only

The existing design let each target check `countLimit.isReached()` at a few orchestration checkpoints and between iterations. This was considered because it minimizes coordination changes and preserves target-local control flow. It was not chosen because the PR description and regression test show that a target already mid-operation can continue to completion long after another target has already consumed the shared budget.

#### Alternative 2: Remove concurrency and process targets sequentially

Another option would be to serialize multi-target downloads so the shared count limit is naturally enforced by processing one target at a time. This was considered because it simplifies shared-budget enforcement and avoids cross-target cancellation semantics. It was not chosen because the existing command intentionally supports concurrent multi-target collection, and this PR preserves that behavior while making the shared count contract correct.

#### Alternative 3: Allow in-flight over-fetch, then trim merged results afterward

The command could continue letting active targets finish their current work and only enforce the shared limit when constructing the final merged report. This was considered because it avoids propagating cancellation through active downloads. It was not chosen because the PR evidence shows the problem is not just final output size: users also need prompt interruption of sibling work, lower wasted downloads, and avoidance of spurious cancellation warnings/errors while preserving already collected results.

### Consequences

#### Positive
- Multi-target `gh aw logs --count N` stops sibling targets promptly once the shared budget is exhausted, reducing wasted work and over-fetching.
- The command preserves concurrent downloads while making the shared count contract match user expectations and regression tests.
- Shared-budget cancellation is treated as a clean stop, so already accumulated results are retained and users do not see misleading cancellation errors.

#### Negative
- The orchestration code becomes more complex because shared count tracking now also coordinates one-time cancellation across concurrent targets.
- Cancellation handling must distinguish shared-budget exhaustion from genuine user cancellation or timeout, increasing edge-case maintenance burden.
- Additional dedicated tests and CI partitioning are needed to keep this behavior covered without interfering with broader logs test groups.

#### Neutral
- The implementation reuses existing context propagation paths rather than introducing a separate interruption mechanism for per-run downloads.
- CI and Make targets now isolate the new multi-target count regression tests into a dedicated group.
- This ADR narrows the decision to shared `--count` exhaustion behavior; broader multi-target limit semantics remain governed by existing logs ADRs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
