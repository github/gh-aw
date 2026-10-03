# ADR-65257: Prefer sanitized detection results

**Date**: 2026-10-03
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Unified session assembly currently has two possible sources for threat-detection verdicts: the raw `threat-detection/detection_result.json` artifact and the sanitized `usage/detection/detection_result.json` conclusion result. The PR description and diff show that preferring the raw verdict loses important outcome data such as detection job status, conclusion, and categorical failure reasons, and it can expose detector-specific details that the publication pipeline is meant to omit. The same PR also updates the Go audit reader to consume `detection.result` events from `usage/aw_session.jsonl`, which makes unified sessions a cross-runtime contract rather than a JavaScript-only implementation detail. The visible constraints are that warn-mode detections must still be represented accurately, unavailable verdict flags must remain absent rather than fabricated, private detector transcripts and free-form reasons must stay excluded, and older artifact layouts must continue to work.

### Decision

We will treat `usage/detection/detection_result.json` as the authoritative source for `detection.result` when assembling unified sessions, and fall back to the raw `threat-detection/detection_result.json` verdict only when the sanitized conclusion result is missing. We will preserve the structured detection fields `jobResult`, `conclusion`, `reason`, `promptInjection`, `secretLeak`, and `maliciousPatch` in unified-session payloads, renderers, and type definitions while excluding detector transcripts and free-form `reasons`. We will also have the Go audit pipeline read version-1 `detection.result` observations from `usage/aw_session.jsonl`, while retaining legacy artifact compatibility for older runs.

### Alternatives Considered

#### Alternative 1: Keep preferring the raw threat-detection verdict

This keeps the collection logic simple and continues using the detector's original structured output as the primary source of truth. It was not chosen because the PR evidence shows that the raw verdict omits job outcome fields, loses categorical failure reasons, and is a worse fit for the privacy-preserving unified-session contract.

#### Alternative 2: Keep unified sessions minimal and require audits to read separate detection artifacts

This would avoid expanding the unified-session schema and would leave the Go audit pipeline dependent on the legacy detection files for job outcomes and verdict details. It was not chosen because the diff explicitly moves both the JavaScript collector and the Go audit reader toward a shared `detection.result` contract, which reduces duplicated interpretation logic and lets a unified session stand on its own.

### Consequences

#### Positive
- Unified sessions now retain complete detection outcomes, including warn-mode states where the job succeeds but the conclusion is `warning`.
- The published session format preserves privacy better by keeping structured verdict fields while excluding detector transcripts and free-form detector reasons.
- The Go audit reader and JavaScript session collector now share a more explicit cross-runtime contract around `detection.result`.

#### Negative
- The detection contract becomes more complex because collectors and readers must handle authoritative sanitized results, raw-result fallback, and legacy artifact compatibility.
- The specification, type definitions, renderers, and both JavaScript and Go tests must stay synchronized as the detection schema evolves.
- Conflicting or malformed unified-session detection records now require explicit validation and error handling instead of being silently ignored.

#### Neutral
- Older unified sessions that only contain verdict flags remain usable because separately recorded conclusion results can still fill in missing status fields.
- The unified-session specification version increases to document the broader `detection.result` payload and source-precedence rules.
- Detection provenance remains part of the contract so readers can identify whether evidence came from the sanitized usage artifact or a raw fallback file.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
