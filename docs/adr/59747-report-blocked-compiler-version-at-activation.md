# ADR-59747: Report blocked compiler versions at activation

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Compiled `gh aw` workflows currently detect blocked compiler versions during the `activation` stage. When that check fails, downstream jobs that normally handle failure reporting are skipped, which creates a silent repository-wide outage for affected workflows. The PR description and linked issue #59600 show that this failure mode can persist without human notice because the only existing notifier runs later in the workflow graph. The implementation therefore needs an explicit decision about whether blocked-version notification should be handled directly in activation, and under what permissions and failure semantics.

### Decision

We will report blocked compiler versions from the activation-stage version check itself, before the workflow exits with the existing hard failure. The activation logic will create or update a deduplicated GitHub issue keyed by blocked compiler version, pass workflow/reporting context into that step, and grant `issues: write` only when that reporting path can execute. We chose this approach because it closes the silent-outage gap at the only point that always observes the blocked-version condition, while keeping notification failures non-fatal so they do not hide the primary compatibility error.

### Alternatives Considered

#### Alternative 1: Keep failure reporting only in downstream conclusion handling

The project could continue relying on the existing downstream failure-reporting job or script. This was considered because it avoids adding notification logic to activation and keeps reporting centralized. It was not chosen because the linked issue and PR both show that blocked-version failures happen before those downstream paths can run, so this design cannot report the exact outage it most needs to surface.

#### Alternative 2: Introduce only a pre-block warning mechanism

Another option would be to add softer warnings, such as deprecation notices or future-block annotations, before versions enter the blocked list. This was considered because warnings could reduce the number of hard outages and might improve upgrade lead time. It was not chosen for this PR because the immediate problem is the current silent failure once a version is already blocked, and warning-only behavior would not restore visibility for repositories already affected.

#### Alternative 3: Add a separate non-agentic watchdog workflow

The repository could provide a standalone watchdog workflow that checks compiled versions outside the agentic activation path. This was considered because an external monitor would remain runnable even when agentic workflows are blocked. It was not chosen here because it adds operational overhead, requires consumers to install and maintain another workflow, and does not fix the built-in reporting path for existing compiled workflows.

### Consequences

#### Positive
- Blocked compiler versions become visible through a deduplicated issue even when activation fails before downstream jobs start.
- The notification logic runs at the only guaranteed observation point for this failure mode, reducing the chance of silent repo-wide outages.
- Limiting `issues: write` to activation only when needed narrows permission scope while preserving automated reporting.

#### Negative
- Activation-stage version checking becomes more complex because it now owns issue lookup/update behavior in addition to compatibility validation.
- The workflow needs additional permission wiring and input propagation, which increases compiler and generated workflow surface area.
- Issue creation/update can fail independently, requiring best-effort error handling and tests to ensure the root blocked-version error remains the primary failure.
- The activation-stage notification only honors a literal `false` for `safe-outputs.report-failure-as-issue`; it does not participate in that setting's category-filter arrays (e.g. `["!blocked_version"]`), since those categories describe the downstream conclusion job's own failure taxonomy and are not available at the activation stage. Workflows relying on category filtering to suppress specific conclusion-job failure types will still receive blocked-version issues unless they disable reporting outright.

#### Neutral
- Generated workflows now pass workflow name and reporting policy into the blocked-version check step.
- Notification deduplication is based on a stable issue title derived from the blocked compiler version.
- The existing hard failure message for blocked versions remains unchanged after reporting is attempted.
- Workflow authors can set `on.report-blocked-version: false` to suppress only the activation-stage notification issue, independent of `check-for-updates` (which disables the whole check and is not allowed in strict mode) and `safe-outputs.report-failure-as-issue` (which also gates the notification). This gives a narrower, always-strict-mode-safe off-switch dedicated to this notification, mirroring the pattern used by `on.stale-check: false`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
