# ADR-57976: Add Concurrent Multi-Workflow Logs Reports

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request expands `gh aw logs` from handling a single workflow target per invocation to aggregating logs across multiple workflow targets and repositories. The diff adds cross-repository target parsing, concurrent collection for multiple targets, shared rate-limit coordination, isolated per-target cache directories, and per-target continuation cursors for partial results. The PR description also requires resilient behavior when some targets are missing or inaccessible but others still succeed. Because this changes the command contract, execution model, and report structure for a core CLI workflow, the decision should be recorded explicitly.

### Decision

We will make `gh aw logs` accept multiple workflow targets, download them concurrently, and render a single combined report while isolating cache/output state per repository and workflow target. We will parse both local workflow arguments and cross-repository forms such as `owner/repo/workflow` and `owner/repo/.github/workflows/workflow.yml`, coordinate rate-limit checks across workers, and preserve per-target continuations when only partial results are available. We chose this approach because the feature goal in the PR is to provide resilient multi-workflow reporting without collisions between repositories or one failing target preventing useful output from successful targets.

### Alternatives Considered

#### Alternative 1: Keep Single-Workflow Invocations Only

Continue requiring one `gh aw logs` invocation per workflow and rely on users or scripts to merge results externally.

This was considered because it preserves the current command model and avoids adding cross-target orchestration, shared rate-limit handling, and combined reporting logic. It was not chosen because the PR explicitly adds a multi-target CLI contract and resilience behavior that users cannot get from separate invocations without extra tooling and repeated setup cost.

#### Alternative 2: Support Multiple Targets Sequentially

Accept multiple workflow arguments but process them one at a time, producing a combined report only after all downloads complete.

This was considered because it would reduce concurrency complexity and make rate-limit coordination simpler. It was not chosen because the PR evidence shows a deliberate decision to process targets concurrently, bound aggregate download concurrency, and cancel or continue cleanly across workers so that multi-workflow reporting is faster and more resilient.

### Consequences

#### Positive
- Users can gather one combined logs report for several workflows, including workflows in other repositories, from a single command.
- Cache and artifact directories are isolated per repository and workflow target, avoiding collisions when run IDs overlap across repositories.
- Shared rate-limit coordination and partial-failure handling let successful targets still produce useful output when other targets are missing, inaccessible, or incomplete.

#### Negative
- The logs command, orchestration layer, and report schema become more complex because they now manage multiple targets, shared concurrency limits, and per-target continuations.
- Error handling becomes less binary: some invalid or missing targets are skipped with warnings, which can make failures less obvious than a single-target hard stop.
- Tests and future maintenance must cover more path-parsing and concurrency edge cases, especially around cross-repository targets and cancellation.

#### Neutral
- The CLI usage string and help text now describe repeatable workflow arguments and cross-repository target syntax.
- The logs summary JSON gains a `continuations` collection in addition to the existing single continuation field to represent combined partial results.
- Internal orchestration APIs now separate collection from rendering so single-target and multi-target flows can reuse shared processing.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
