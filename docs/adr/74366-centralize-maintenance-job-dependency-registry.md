# ADR-74366: Centralize Generated Maintenance Job Dependencies in a Registry

**Date**: 2026-05-08
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent

---

### Context

gh-aw compiles workflows that may contain authored custom jobs alongside compiler-generated jobs. Some generated jobs are post-agent "maintenance" jobs (`push_repo_memory`, `update_cache_memory`, `update_drive_memory`, `push_ledger_changes`, `push_evals_state`) plus `push_experiments_state`, which runs after activation. The authored-job dependency graph did not know these jobs' prerequisites, so a custom job with `needs: [push_repo_memory]` was treated as not depending on `agent` and was added to `agent.needs`, creating a cycle (issue #74103). The list of maintenance job names was also duplicated and inconsistent across `IsCompilerOwnedJobName` and `on.needs` reserved-target validation.

### Decision

We will add a single registry, `constants.MaintenanceJobDependencies`, mapping each generated maintenance job ID to its prerequisite job (`agent`, or `activation` for `push_experiments_state`). The authored-job dependency graph seeds edges from this registry unless the user authored a job with the same name, and both `IsCompilerOwnedJobName` and `isReservedOnNeedsTarget` derive maintenance membership from it. `KnownBuiltInJobNames` stays unchanged, so maintenance jobs remain non-customizable.

### Alternatives Considered

#### Alternative 1: Require users to list `agent` explicitly

Document that custom jobs must use `needs: [agent, push_repo_memory]`. This needs no compiler change, but it leaves an easy way to create a confusing compiler-generated cycle. It was rejected because the transitive dependency can be inferred.

#### Alternative 2: Special-case each maintenance job at its call site

Hard-code `push_repo_memory` and similar names in the dependency builder, as validation already did. This is a smaller change, but it keeps several hand-maintained lists that already disagreed (validation listed only two maintenance jobs). It was rejected in favor of one shared source of truth.

#### Alternative 3: Add maintenance jobs to `KnownBuiltInJobNames`

This would reuse the existing built-in registry, but built-ins are customization targets with different semantics. It was rejected to keep maintenance jobs distinct.

### Consequences

#### Positive
- Custom jobs depending on maintenance jobs are ordered correctly after `agent` without false cycles.
- Compiler ownership, dependency modeling, and `on.needs` validation share one list. A new maintenance job only needs one registry entry.

#### Negative
- The graph seeds maintenance edges even when the feature (for example, repo-memory) is disabled, so dependency inference can assume a job that will not be generated.
- `on.needs` now rejects more names (`update_drive_memory`, `push_ledger_changes`, `push_evals_state`, `push_experiments_state`), which could break workflows that relied on them.

#### Neutral
- Authored jobs that reuse maintenance names keep their authored dependencies and are not overridden by the registry.
- The prerequisite is a single job; maintenance jobs with multiple prerequisites would need a schema change.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
