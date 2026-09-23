# ADR-62943: Report gateway steering events in audit output

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw audit` command currently counts gateway steering events but does not preserve the event details that explain whether a run is nearing AI-credit exhaustion or its time limit. The PR description and diff show downstream users need those structured warnings in both console and JSON audit output, and cached summaries must carry the same data so repeated audits remain complete. The change spans parsing firewall gateway logs, extending in-memory and cached audit models, rendering a new report section, and updating published schemas and documentation. The decision is whether audit should keep reporting only aggregate steering counts or promote gateway steering warnings into a first-class structured report field.

### Decision

We will expose gateway steering warnings as structured `gateway_steering_events` in audit output, cache them in run summaries, and render them in the console report alongside other operational sections. We will extract both `token_steering` and `timeout_steering` events from gateway log entries, preserving each event's type, message, and timestamp when available. We chose this approach because the PR evidence shows aggregate counts alone obscure the operational reason for steering, while a structured field keeps JSON, cached data, console output, schemas, and documentation aligned.

### Alternatives Considered

#### Alternative 1: Keep reporting only aggregate steering counts

This matches the prior implementation and is the lowest-effort option because it reuses the existing token-usage summary without adding new report fields. It was not chosen because the PR explicitly addresses the gap that counts do not tell operators whether warnings came from AI-credit pressure or time pressure, which makes audit output less actionable.

#### Alternative 2: Surface steering details only in raw logs or documentation

This was a realistic option because the gateway events already exist in firewall log files, and users could inspect those logs outside the audit report. It was not chosen because the diff adds tests, report wiring, cache hydration, and schema changes intended to make steering details part of the supported audit contract rather than an implementation detail hidden in downloaded artifacts.

### Consequences

#### Positive
- Audit consumers can distinguish AI-credit warnings from time-limit warnings directly from console and JSON output.
- Cached summaries and regenerated audit reports remain complete because the new field is stored in `RunSummary` and `ProcessedRun`.
- Published schemas and documentation explicitly describe the new contract, reducing ambiguity for tooling that parses audit output.

#### Negative
- The audit data model and rendering pipeline become more complex because a new structured event type must be threaded through parsing, caching, serialization, and presentation layers.
- The audit cache schema version must be bumped, which invalidates older cached reports and forces regeneration.
- Tests and schemas now need ongoing maintenance whenever gateway steering event semantics change.

#### Neutral
- Existing aggregate token-usage analysis remains in place; this change augments it with event detail rather than replacing it.
- The new report section appears only when gateway steering events are present, so runs without such warnings keep their current output shape apart from the expanded schema.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
