# ADR-76084: Prioritize Default-Branch Runs in Health Checks

**Date**: 2026-11-08
**Status**: Draft
**Deciders**: pelikhan (PR author); maintainer acceptance pending

---

### Context

`gh aw health` samples workflow evidence under shared request and time budgets, so collection order affects which failures appear before collection stops. [ADR-70552](70552-failure-first-source-aware-health-collection.md) introduced failure-first, source-aware collection, while [ADR-71786](71786-bound-health-collection-with-shared-time-budget.md) bounded total collection time and retained explicitly partial results. Repository-wide failure inventory can spend these budgets on pull-request or other-branch runs before examining the repository's default branch. Those failures remain useful evidence, but may describe unmerged code rather than a regression in the default branch. Prioritization must not discard that evidence or imply that an incomplete sample establishes repository health.

### Decision

We will discover the default branch from the target repository's API metadata rather than assuming `main` or trusting the local checkout. We will first collect default-branch, non-pull-request runs with failure-first inventory and representative verification followed by chronological sampling; only then will we collect pull-request and other-branch runs with the same failure-first ordering. Pull-request events belong to the later pass even when their reported head branch matches the default branch, and collected failures retain their original branch/event scope and source-revision evidence. Both passes will share the existing request/time budgets, five failure candidates per workflow, and ten failure-listing pages rather than receive independent allowances.

The later chronological pass will reuse first-pass runs when checking whether a workflow has enough evidence, while still fetching at least one unfiltered page per workflow and excluding default-branch, non-pull-request runs from its new sample. Discovered later-pass failure scopes must still obtain their own current-revision evidence; default-branch evidence does not satisfy those scope-specific requirements. No additional chronological page cap is introduced: source-aware revision boundaries, existing search limits, shared budgets, run deduplication, and partial-result behavior remain in force.

Expose `collection.default_branch`, `collection.default_branch_pass_complete`, and the `default_branch_first` capability. Default-pass completeness requires both complete failure listing and representative verification plus completion of the bounded chronological pass. Reaching a pagination, request, or time limit leaves the affected pass incomplete, even when every candidate seen so far has been checked. Completion describes the collection procedure, not exhaustive historical coverage or a declaration that the default branch is healthy.

### Alternatives Considered

#### Alternative 1: Retain repository-wide failure-first ordering

The existing order treats failures from all branches equally and avoids an additional branch-scoped pass. It is not chosen because a busy pull-request history can consume the shared budget before default-branch evidence is collected, obscuring the maintenance question this command should answer first.

#### Alternative 2: Collect only default-branch runs

Restricting all collection to the default branch would reduce request pressure and simplify interpretation. It is not chosen because pull-request and other-branch failures are still useful for triage; priority should change collection order, not erase their evidence.

#### Alternative 3: Give each branch class an independent budget

Separate budgets would reserve capacity for both passes and could improve other-branch coverage. They are not chosen because they would increase total requests or runtime beyond the existing command contract; partial, explicitly scoped evidence is preferable to silently expanding those limits.

### Consequences

#### Positive
- Default-branch evidence is collected before potentially noisy unmerged changes consume the shared budget.
- Pull-request and other-branch failures remain available with their original provenance instead of being relabeled as default-branch regressions.
- Branch priority and completeness are explicit for consumers interpreting partial reports.

#### Negative
- Other-branch and pull-request evidence may receive little or no collection budget after the default pass.
- Shared inventory caps and source-aware sampling still omit historical evidence; neither a completed pass nor an empty issue list proves repository health.
- Repository metadata discovery adds a request and must succeed before branch priority can be applied.

#### Neutral
- The two-pass orchestration adds branch/event classification, shared pagination accounting, and completeness state without changing authentication or workflow protections.
- Existing source-revision checks, representative verification, and partial-result semantics remain applicable to both passes.

---

*This record documents the proposed design for PR #76084. Draft status does not assert maintainer acceptance.*
