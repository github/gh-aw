# ADR-56975: Use Authoritative AWF AI-Credit Totals

**Date**: 2026-08-29
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes how `gh-aw` reports AI Credits usage from AWF token-usage telemetry across both JavaScript and Go reporting paths. The PR body documents a concrete defect: `gh-aw` recomputed AI Credit totals from raw token counts, double-subtracted cached tokens for AWF records that already reported cache information separately, and produced totals roughly half of the true value. The diff introduces shared handling for AWF-reported `ai_credits_this_response` and `ai_credits_total` fields, explicit `input_tokens_include_cache` semantics, deduplication by `event:request_id`, chronological processing, fallback warnings, and matching tests and fixtures. Because this changes the accounting contract used for summaries, console output, audits, forecasts, and generated artifacts, the decision should be recorded explicitly.

### Decision

We will treat AWF-reported AI Credit fields as the authoritative source for user-facing AI Credits reporting whenever valid values are present, and we will fall back to legacy token-based recomputation only for older or malformed records. We will also make cache-token accounting explicit via `input_tokens_include_cache` semantics instead of inferring cache inclusion purely from provider behavior. We chose this approach because the PR evidence shows that recomputing modern AWF records in `gh-aw` can misprice runs, while preserving fallback logic maintains compatibility for historical telemetry.

### Alternatives Considered

#### Alternative 1: Continue Recomputing All AI Credits From Token Counts in gh-aw

Keep `gh-aw` as the sole calculator of AI Credit totals and ignore AWF-reported per-request and cumulative fields.

This was considered because it preserves a single local accounting path and avoids trusting producer-supplied totals. It was not chosen because the PR body and fixture demonstrate that the local recomputation was already wrong for modern AWF records with separate cache semantics, producing materially incorrect totals in public reporting.

#### Alternative 2: Use AWF-Reported Totals Only, Without Legacy Fallback or Explicit Cache Semantics

Switch entirely to AWF-provided fields and drop token-based repricing behavior for records that do not include the new fields.

This was considered because it simplifies the reporting path for current telemetry. It was not chosen because the diff explicitly supports older records and malformed fields, and removing fallback behavior would break compatibility with historical logs and make partial or transitional datasets harder to interpret.

### Consequences

#### Positive
- AI Credits reported in step summaries, `agent_usage.json`, audit output, and forecasts will match AWF’s authoritative totals for modern records.
- Explicit `input_tokens_include_cache` handling removes ambiguity around cache-token accounting and prevents the double-subtraction bug described in the PR.
- Shared resolution logic across JavaScript and Go surfaces reduces cross-surface drift and keeps diagnostics consistent.

#### Negative
- Reporting now depends on the correctness and presence of AWF-emitted accounting fields for modern records, increasing coupling to AWF telemetry semantics.
- The parser and formatting logic become more complex because they must support mixed generations of records, malformed-field fallbacks, warnings, deduplication, and chronological reconstruction.
- Preserving AWF cumulative totals even when they diverge from summed per-request values may expose inconsistencies that require additional operator explanation.

#### Neutral
- Budget enforcement, retries, authentication, and failure classification remain unchanged; the PR limits the decision to diagnostics and public reporting paths.
- Historical records without AWF AI Credit fields continue to use legacy repricing, so the repository will temporarily support two accounting modes.
- The ADR documents an accounting and telemetry interpretation rule rather than a broader runtime architecture change.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
