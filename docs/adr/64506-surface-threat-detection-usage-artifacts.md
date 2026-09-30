# ADR-64506: Surface threat detection in usage artifacts

**Date**: 2026-09-30
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how `gh-aw` preserves and audits threat-detection outcomes after a workflow run completes. The PR description states that the compact conclusion-job `usage` artifact did not reliably retain threat-detection verdicts and categorized failures, which meant `gh aw audit --artifacts usage` could miss security findings unless it downloaded the full detection artifact. The changed files show coordinated updates in audit logic, workflow lock files, tests, and artifact documentation to make the lightweight reporting path sufficient for threat-detection status. The architectural question is how much structured detection state should be copied into the `usage` artifact so downstream audit paths can report security outcomes without depending on the full detection artifact.

### Decision

We will persist a structured threat-detection result file inside the `usage` artifact and teach the audit pipeline to treat that usage-only evidence as authoritative for detection failures, warnings, and verdict-backed threat findings. We will also propagate the detection job result, conclusion, and categorized reason into the conclusion workflow so the usage artifact records enough sanitized state for audit reporting without including raw detection logs or detector-supplied free-form reasons. We chose this because the PR evidence shows that lightweight audit paths need security visibility even when only `usage` artifacts are downloaded.

### Alternatives Considered

#### Alternative 1: Keep relying on the dedicated detection artifact for audit findings

This was a realistic option because `gh-aw` already emits a dedicated detection artifact and existing audit behavior could continue downloading it when needed. It was not chosen because the PR description and tests show that `gh aw audit --artifacts usage` would still miss detection failures and threats, making the lightweight audit mode incomplete for security review.

#### Alternative 2: Copy the full detection logs into the usage artifact

This was considered because full logs would give audit consumers the most detail and would avoid adding special structured fields. It was not chosen because the documentation changes explicitly distinguish structured `detection_result.json` from raw logs, and the PR description says to exclude raw logs and detector-supplied reasons while preserving only validated verdict flags and categorized failure metadata.

### Consequences

#### Positive
- `gh aw audit --artifacts usage` can report threat-detection failures, warnings, and identified threats without a redundant detection-artifact download.
- Security findings become available from a smaller, conclusion-job artifact that downstream reporting paths already consume.
- Tests and documentation now cover the usage-artifact contract for detection outcomes, reducing regressions in audit behavior.

#### Negative
- The conclusion-job artifact contract grows, so workflow generation and audit code must stay synchronized on the `detection_result.json` schema.
- Threat-detection result handling is now split across detection execution, usage-artifact collection, and audit parsing, which increases maintenance complexity.

#### Neutral
- The dedicated detection artifact still exists for full detection outputs; this change primarily upgrades the lightweight usage-only path.
- Generated workflow lock files need widespread regeneration because many workflows share the same usage-artifact collection behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
