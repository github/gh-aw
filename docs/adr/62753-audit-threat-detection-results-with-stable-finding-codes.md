# ADR-62753: Audit threat-detection results with stable finding codes

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw audit` command did not reliably surface failed threat-detection jobs or detected threats in its structured findings, and existing findings lacked stable machine-readable identifiers for downstream automation. The PR changes `pkg/cli/` audit generation, cache invalidation behavior, JSON schemas, tests, and reference documentation. The implementation also shows a compatibility constraint: audit data must continue to consume both current `detection_result.json` artifacts and legacy `THREAT_DETECTION_RESULT` entries in `detection.log`. The architectural question is how audit output should represent threat-detection outcomes and finding identity without exposing potentially sensitive threat-reason details.

### Decision

We will make threat-detection outcomes a first-class part of audit analysis and require every generated audit finding to include a stable `code`, with a top-level `schema_version` to invalidate stale cached audit data. `gh aw audit` will derive threat findings from the `detection` job result, structured `detection_result.json`, legacy `detection.log` verdicts, and execution evidence when cached summaries are reused without detection artifacts. We chose this approach because it gives downstream automation a durable contract while keeping threat reporting compatible with old and new artifacts and avoiding propagation of sensitive threat-reason text.

### Alternatives Considered

#### Alternative 1: Keep audit findings title-driven and treat threat detection as incidental narrative

This would avoid expanding the audit JSON contract and would preserve the current cache format. It was not chosen because the PR evidence shows downstream consumers need stable finding identifiers and explicit threat-detection findings, and title/description text is too unstable for automation.

#### Alternative 2: Report threat detection only from the current structured artifact format

This would simplify parsing by reading only `detection_result.json` and ignoring legacy logs or cached-summary refresh behavior. It was not chosen because the implementation needs to support older runs, detect conflicting or malformed verdicts safely, and avoid false negatives when cached summaries omit detection artifacts.

### Consequences

#### Positive
- Audit consumers get stable `code` values for every finding and a versioned schema contract for cache and JSON parsing.
- Threat-detection failures and detected threats are surfaced explicitly from both current and legacy evidence sources.
- Cached audit output is regenerated when the schema changes or when threat-detection artifacts are required but missing.

#### Negative
- The audit pipeline becomes more complex because it must reconcile job status, structured artifacts, legacy logs, and cached execution evidence.
- Schema changes introduce compatibility work for any external consumers that validate or parse audit output.
- Rejecting malformed or conflicting threat verdicts may suppress findings when artifacts are inconsistent.

#### Neutral
- The structured audit and logs JSON schemas now require `schema_version` and finding `code` fields.
- Threat findings intentionally omit detailed threat reasons, even when artifacts contain them.
- Documentation and tests expand to describe the stable finding-code contract and threat-detection behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
