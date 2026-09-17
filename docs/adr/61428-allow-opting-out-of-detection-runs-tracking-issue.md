# ADR-61428: Allow opting out of detection runs tracking issue

**Date**: 2026-09-17
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new configuration option to gh-aw threat detection so repositories can suppress the `[aw] Detection Runs` tracking issue without disabling threat detection itself. The PR description explains that today the only way to stop automatic reporting into that tracking issue is to turn off threat detection entirely, which also removes enforcement behaviors such as `continue-on-error` handling and safe-output blocking. The diff updates the threat-detection config model, parser, workflow compilation, schema, tests, and reference docs to separate reporting behavior from detection behavior. The design question is whether tracking-issue reporting should remain coupled to threat detection enforcement or become independently configurable.

### Decision

We will add `safe-outputs.threat-detection.report-as-issue` as a boolean configuration option, defaulting to `true`, to control whether warning and failure conclusions create or update the `[aw] Detection Runs` tracking issue. When set to `false`, gh-aw will still run threat detection and preserve existing enforcement behavior, but the compiled conclusion job will omit the detection-runs logging step. We chose this because the PR evidence shows repositories need to keep detector enforcement active while preventing framework diagnostics from polluting user-facing issue queues.

### Alternatives Considered

#### Alternative 1: Keep tracking-issue reporting coupled to threat detection

The existing behavior always creates or updates the tracking issue whenever threat detection emits warnings or failures. This was considered because it is simpler, preserves a single default reporting path, and requires no additional configuration surface. It was not chosen because the PR description identifies a concrete user problem: disabling the tracking issue today also disables enforcement, which prevents repositories from keeping detection protections while suppressing issue noise.

#### Alternative 2: Disable threat detection entirely when issue reporting is unwanted

Repositories can already avoid the tracking issue by turning off threat detection. This was considered because it uses existing configuration and avoids changes to the parser, schema, and compiler. It was not chosen because it removes important enforcement behavior, including `continue-on-error` caution handling and safe-output blocking, which the PR explicitly preserves.

### Consequences

#### Positive
- Repositories can keep threat detection and its enforcement active without creating or updating the `[aw] Detection Runs` tracking issue.
- The configuration model better separates detection execution from how results are reported to repository users.
- The compiler and tests now explicitly verify that the logging step is omitted while the detection job still runs.

#### Negative
- Threat-detection configuration becomes more complex by adding another reporting-specific option and parsing path.
- Detection results may become less visible to repository maintainers who rely on the tracking issue instead of reviewing workflow logs.
- The compiler now has another conditional branch in conclusion-step generation that must stay aligned with docs and schema.

#### Neutral
- Default behavior remains unchanged because `report-as-issue` is enabled when unset.
- The JSON schema and reference documentation need to describe the new option alongside `continue-on-error`.
- The parser implementation is split into smaller helper functions to accommodate the new field without changing the broader feature scope.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
