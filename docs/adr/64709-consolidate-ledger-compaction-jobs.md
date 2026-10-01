# ADR-64709: Consolidate ledger compaction into shared maintenance jobs

**Date**: 2026-10-01
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes Agentic Maintenance ledger compaction from one plan job and one apply job per ledger into a single shared planning job and a single shared apply job for all compaction-enabled ledgers. The diff shows the workflow generator, generated workflow, tests, and ledger-compaction documentation all being updated together, which indicates an architectural change in how maintenance work is orchestrated rather than a local refactor. The PR description states that planning should remain read-only and application should remain write-enabled, while each ledger still keeps its own validation and plan file. The visible constraints are that the trust boundary between untrusted planning and trusted application must remain intact, ledger-specific targeting via `inputs.ledger` must still work, and the workflow must handle multiple ledgers without reintroducing per-ledger jobs.

### Decision

We will consolidate ledger compaction orchestration into two repository-wide Agentic Maintenance jobs: one untrusted read-only `ledger_compaction_plan` job that runs ledger-specific planning steps and uploads all created plans as a single artifact, and one trusted `ledger_compaction_apply` job that downloads that artifact and conditionally applies each ledger plan. We will preserve per-ledger plan files, per-ledger step conditions, and per-ledger apply validation inside those shared jobs rather than keeping separate jobs for each ledger. We chose this because the PR evidence shows a desire to reduce the number of maintenance jobs while preserving the existing security boundary and ledger-specific validation behavior.

### Alternatives Considered

#### Alternative 1: Keep one plan/apply job pair per ledger

This was the prior design and remains a realistic option because it isolates each ledger into its own concurrency group and artifact flow. It was not chosen because the PR explicitly replaces those repeated jobs with two shared jobs, indicating that the existing per-ledger fan-out is more workflow overhead than the maintainers want for this maintenance path.

#### Alternative 2: Use a single fully shared job that both plans and applies compaction

This was a plausible simplification because one job could avoid artifact passing and further reduce orchestration complexity. It was not chosen because the diff and documentation preserve a strict split between untrusted read-only planning and trusted write-enabled application, and combining them would weaken that trust boundary.

### Consequences

#### Positive
- The maintenance workflow now has a smaller, more centralized job graph for ledger compaction.
- The security model remains explicit by keeping planning read-only and application write-enabled in separate jobs.
- Per-ledger validation and selective execution are preserved through step-level gating and separate plan files.

#### Negative
- Shared jobs introduce more internal conditional logic and outputs, making the two jobs denser and somewhat harder to reason about than isolated per-ledger jobs.
- All compaction-enabled ledgers now share a repository-wide concurrency group, which may reduce parallelism compared with independent per-ledger groups.
- The workflow generator and tests must keep step IDs, per-ledger outputs, and artifact layout synchronized across Go and generated YAML.

#### Neutral
- Plan artifacts are still used, but now as one directory-backed artifact containing multiple per-ledger plan files.
- Ledger targeting remains input-driven, but the selection now occurs at the step level instead of the job level.
- Documentation needs to describe shared jobs and repository-wide concurrency rather than per-ledger jobs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
