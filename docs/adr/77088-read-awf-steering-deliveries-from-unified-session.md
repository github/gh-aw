# ADR-77088: Read AWF Per-Conversation Steering Deliveries from the Unified Session Only

**Date**: 2026-05-20
**Status**: Draft
**Deciders**: Unknown (drafted by the Design Decision Gate from PR #77088)

---

### Context

AWF releases after v0.29.11 send each budget notice to every conversation in a run, once, and add a `little_left` notice whose `threshold` is `null`. Each delivery is recorded in a new `steering_delivery` field of `token-usage.jsonl`, and `*_steering` events gain `trigger` and `conversation`. gh-aw dropped `steering_delivery`, and its decoder coerced a `null` threshold to 0, so `gh aw audit` could not show which conversations received a notice and could misreport a `little_left` notice as a 0% threshold. The existing audit output (`steering_notices`, `gateway_steering_events`) is a compatibility surface with at most one notice per threshold per run, and `gateway_steering_events` already prefers the raw API-proxy event log with the unified session as fallback (see ADR-68428).

### Decision

We will carry `steering_delivery` into the unified session as a typed `steeringDelivery` field (`SteeringDelivery` in `unified_session.d.ts`, schema generated from it) that keeps only `type`, `trigger`, `threshold`, `conversation` and `first`, and drops any record that violates AWF's trigger/threshold/conversation/`first` contract. We will expose a new `steering_deliveries` list in `gh aw audit` and `gh aw logs --json`, sourced exclusively from the session file, with `null` thresholds preserved. `steering_notices` is left unchanged, token-usage decoding stays lenient, and the audit and logs caches are version-bumped so older results are rebuilt.

### Alternatives Considered

#### Alternative 1: Extend `steering_notices` to one entry per delivery

Reuse the existing list and add `conversation`/`trigger` to it. This avoids a new field, but changes the established at-most-once-per-threshold semantics that consumers and the smoke test rely on, and a `null` threshold would not fit its current shape.

#### Alternative 2: Source deliveries from the raw API-proxy log with session fallback

Mirror `gateway_steering_events` by preferring the raw log. Rejected because the delivery record lives in `token-usage.jsonl`, and a single validated, schema-enforced source avoids the heuristic de-duplication and partial-log divergence that the fallback approach has.

#### Alternative 3: Decode leniently and pass through whatever AWF sends

Forward `steering_delivery` unvalidated. Simpler, but would let malformed records (for example `threshold: 0`, bad conversation keys, missing `first`) reach audit output and undermine the schema; strict dropping was chosen so decoders and JSON schemas reject the same inputs.

### Consequences

#### Positive
- Audits can show which conversations received each notice, and `little_left` is reported as `null` threshold, not 0.
- Decoders and the generated session/audit/logs schemas enforce the same contract, so invalid deliveries are rejected consistently.
- Existing `steering_notices` consumers and AWF v0.29.11-and-older artifacts keep working unchanged.

#### Negative
- Two overlapping steering views now exist (`steering_notices` and `steering_deliveries`) with different sources and semantics, increasing documentation and maintenance burden.
- `steering_deliveries` is empty when the session file is not retained, with no raw-log fallback.
- Strictly dropping non-conforming records can silently hide deliveries if AWF's contract evolves; gh-aw must be updated in step.
- Bumping `auditSchemaVersion` (14) and `cachedLogsJSONLSchemaVersion` (6) forces cache rebuilds.

#### Neutral
- The unified agent session spec moves to 2.10.0 (T-UAS-071 updated) and the smoke test T1/T2 checks now accept a `little_left` delivery and validate at-most-once against legacy `steering` records only.
- Console and timeline output show "little left" instead of a percentage and sanitize session-derived steering strings.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
