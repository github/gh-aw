# ADR-61027: Introduce fair multi-target logs scheduling

**Date**: 2026-09-15
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes `gh aw logs` multi-target execution so one target can no longer repeatedly consume shared API, count, and timeout budgets before other targets get a chance to run. The PR description states that the previous scheduling biased those shared budgets toward earlier targets, while the diff replaces a target-level semaphore queue with a round-based batch scheduler shared across all active targets. The implementation also preserves bounded concurrency and removes completed or failed targets from future rounds instead of serializing the entire operation. The architectural question is how multi-target log collection should allocate shared work across competing workflow targets under one command invocation.

### Decision

We will schedule multi-target `gh aw logs` batch fetches with a fair, round-based shared scheduler rather than letting targets race for repeated access to the shared worker semaphore. Each active target will be allowed to start at most one batch per round, rounds will advance only after all active targets complete the current round, and the existing concurrency cap will still bound how many batches run in parallel. We chose this because the PR evidence shows the core problem is budget starvation across targets, and fairness at the batch boundary directly addresses that without removing concurrency or shared continuation behavior.

### Alternatives Considered

#### Alternative 1: Keep the existing first-come, first-served semaphore queue

The prior design let any target that acquired the shared semaphore begin its next batch immediately, subject only to the global concurrency limit. This was considered because it is simpler and can maximize throughput for whichever target is ready first. It was not chosen because the PR description and tests show that this approach biases shared API, count, and timeout budgets toward earlier targets and can prevent later targets from receiving equivalent opportunities to fetch logs.

#### Alternative 2: Fully serialize targets one at a time

Another option was to process all batches for one target before moving to the next, eliminating cross-target contention entirely. This was considered because it would make fairness deterministic and reduce scheduler complexity. It was not chosen because the PR explicitly preserves bounded parallel batch processing, and full serialization would reduce throughput and waste available concurrency when multiple targets could safely make progress together.

### Consequences

#### Positive
- Shared count, API, and timeout budgets are distributed more evenly across active workflow targets.
- Multi-target log collection retains concurrency while preventing a single target from monopolizing repeated batch starts.
- Tests now explicitly verify per-round fairness, shared timeout behavior, and continuation handling across all targets.

#### Negative
- The logs orchestration path gains a new synchronization primitive with additional state and concurrency complexity.
- Fair round scheduling can delay a fast target's next batch until slower active targets complete the current round.
- More target collectors are now started even in rate-limited scenarios, which may complicate reasoning about cancellation paths.

#### Neutral
- Completed or failed targets are removed from subsequent rounds, so fairness applies only to still-active targets.
- The scheduler is threaded through `LogsDownloadOptions`, extending the internal orchestration contract without changing the external CLI interface.
- Error handling for canceled or rate-limited batches now occurs at the batch acquisition point rather than the old target-level semaphore queue.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
