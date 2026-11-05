# ADR-54630: Derive conclusion Job Permissions from Resolved Safe-Outputs Configuration

**Date**: 2026-08-21
**Status**: Draft
**Deciders**: pelikhan, copilot-swe-agent

---

### Context

The `conclusion` job compiled into every gh-aw lock file previously requested `issues: write` and `actions: read` unconditionally whenever `report-failed-jobs` was enabled (which defaults to `true`). Three other issue-creating mechanisms — `report-failure-as-issue`, `noop.report-as-issue`, and `missing-tool.create-issue` — silently relied on the same unconditional grant rather than contributing their own permission derivation.

For workflows consumed via `uses:` in GitHub Actions, permissions are validated **statically at load time**, not at runtime. Every permission any nested job requests must be granted by every caller in the chain. An over-declared `issues: write` causes hard load-time failures for callers with a `contents: read`-only ceiling — the workflow is rejected before any job runs, with no job logs to diagnose from. Additionally, calling repositories are forced to grant issue-creation in *their* repos for a capability that is explicitly configured off.

The `safe_outputs` job's permissions were already derived correctly from resolved configuration; the `conclusion` job lagged behind.

### Decision

We will derive `issues: write` and `actions: read` on the `conclusion` job from the resolved safe-outputs configuration at compile time. Four dedicated helper functions (`conclusionReportFailedJobsEnabled`, `conclusionReportFailureAsIssueEnabled`, `conclusionMissingToolCreateIssueEnabled`, `conclusionMayCreateIssue`) mirror the enabled/disabled state of each issue-creating mechanism. `issues: write` is only requested when `conclusionMayCreateIssue` returns `true` (i.e., at least one mechanism is active); `actions: read` is only requested when `report-failed-jobs` is enabled, as that is the only mechanism that lists workflow run jobs.

### Alternatives Considered

#### Alternative 1: Keep Unconditional Emission and Document the Requirement

Keep `issues: write` and `actions: read` as static, unconditional grants and document in the schema that callers must always supply them. This is the simplest code change (no change), but it does not resolve the hard failure for callers with restricted ceilings. It also permanently forces every calling repository to grant issue-creation authority regardless of their intent, which is difficult to justify in a permissions review.

#### Alternative 2: Add a User-Facing Permission Override Config Key

Introduce a new `conclusion.permissions.issues` or similar schema key that users can set to `none` to suppress the grant. This lets users opt out explicitly, but widens the public API surface of the workflow schema. It shifts the burden of knowing *when* the permission is unnecessary onto users rather than deriving it automatically from the safe-outputs config they have already set. It also does not align `conclusion` with the derivation pattern already established for `safe_outputs`.

### Consequences

#### Positive
- Reusable workflows that disable all issue-creating paths (`report-failed-jobs: false`, `report-failure-as-issue: false`, `noop.report-as-issue` unset, `missing-tool.create-issue: false`, `threat-detection.report-as-issue: false`, and no other issue-writing safe outputs) compile a `conclusion` job with no `issues: write`, removing the hard load-time failure for callers with restricted issue permission ceilings. The detection opt-out preserves threat detection and enforcement; `actions: read` remains required for session job-duration collection.
- Permission derivation is now consistent between the `conclusion` job and the `safe_outputs` job, simplifying the mental model for contributors and auditors.

#### Negative
- Each future issue-creating mechanism added to the `conclusion` job must also update `conclusionMayCreateIssue`; forgetting this will silently under-grant the permission and cause runtime failures. This is a maintenance invariant not enforced by the type system.
- The `noop.report-as-issue` path is handled by delegating to the existing `isNoOpReportAsIssueEnabled` helper rather than a new dedicated `conclusion*Enabled` function, creating a minor inconsistency in the helper naming convention that future authors may find surprising.

#### Neutral
- The change adds approximately 45 lines of helper functions to `pkg/workflow/notify_comment.go`, increasing the size of that file but keeping all permission logic co-located with the job builder.
- Tests for the new behavior are added in `notify_comment_test.go`, covering the three cases: all mechanisms disabled (no grants), default configuration (grants present), and `missing-tool.create-issue` explicitly enabled (grant present).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
