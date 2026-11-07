# ADR-74949: Derive Conclusion App Token Issue Permissions

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

When `safe-outputs.github-app` is configured, conclusion reporting steps use the safe-outputs App token. A workflow that only enables pull-request outputs can therefore mint that token without `issues: write`, even though failure reporting, failed-job reporting, detection-run logging, incomplete reporting, or no-op reporting may create or update issues. The missing scope silently disables those reports while the workflow continues. The permission must remain least-privilege and must not broaden the safe-output execution token unnecessarily.

### Decision

We will derive the conclusion App token's `issues: write` permission from the resolved conclusion reporting configuration. In addition to existing issue-writing paths, a runnable threat-detection job with issue reporting enabled will cause the conclusion permission set to include `issues: write`; disabled or non-runnable detection will not. We will preserve the existing safe-output execution permissions and cover enabled, disabled, and work-queue detection cases with regression tests.

### Alternatives Considered

#### Alternative 1: Run conclusion reporting with the job `GITHUB_TOKEN`

This would use the job token's existing issue permission and avoid changing the App token mint. It was not chosen because the conclusion reporting steps currently use the safe-outputs App token, and changing token routing would alter the established authentication boundary for those steps.

#### Alternative 2: Allow standard issue scopes in `safe-outputs.github-app.permissions`

This would let workflow authors request `issues: write` directly in the App configuration. It was not chosen because the current configuration accepts only App-specific read scopes, and it would move conclusion-reporting permission inference to each workflow rather than deriving it from the reporting features that actually run.

#### Alternative 3: Grant `issues: write` to every conclusion App token

This is simple and would cover all current and future issue reporters. It was rejected because it grants write access when no conclusion mechanism can create or update an issue, violating the existing least-privilege, configuration-derived permission model.

### Consequences

#### Positive

- Failure and threat-detection issue reporting continues to work when conclusion steps use a GitHub App token.
- The permission remains conditional on resolved, runnable reporting configuration instead of being granted unconditionally.
- Regression coverage documents the interaction between detection reporting, work-queue workers, and other conclusion issue-reporting paths.

#### Negative

- Workflows that enable detection issue reporting grant `issues: write` to the conclusion App token, increasing that token's authority.
- New conclusion reporters that create or update issues must be added to the permission inference logic and its table-driven tests.

#### Neutral

- Disabling detection issue reporting, or making detection non-runnable, continues to omit `issues: write` when no other issue-writing path is enabled.
- The safe-output execution permission set remains separate from the conclusion App token's inferred reporting permissions.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
