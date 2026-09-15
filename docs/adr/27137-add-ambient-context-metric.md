# ADR-27137: Track Ambient Context via First LLM Invocation Token Metrics

**Date**: 2026-04-19
**Status**: Draft
**Deciders**: pelikhan, Copilot


---

## Part 1 — Narrative (Human-Friendly)

### Context

The gh-aw tooling collects and aggregates per-run token usage from the firewall proxy's `token-usage.jsonl` log. Aggregate totals (total input, cache-read, and output tokens) are already surfaced in `audit` and `logs` outputs, but they conflate system-prompt overhead with actual task-execution cost. Teams want to compare how "heavy" the ambient context (system prompt, tools list, memory) is across different workflow configurations without writing custom log analysis. The first LLM invocation in a run is a natural proxy for ambient context size because it fires before the agent has accumulated any conversation history, so its input token count primarily reflects the static context loaded at startup.

### Decision

We will introduce an `AmbientContextMetrics` struct that captures the token footprint (`input_tokens`, `cached_tokens`) of the chronologically first LLM invocation in `token-usage.jsonl`, and expose it as an optional `ambient_context` field in both the `audit` and `logs` JSON output schemas. Chronological ordering is determined by the `timestamp` field (RFC 3339 / RFC 3339 Nano); file order is used as a stable tiebreaker for entries that share a timestamp or lack one.

### Alternatives Considered

#### Alternative 1: Average or Median Token Count Across All Invocations

Computing an average or median across all invocations was considered as a way to characterize "typical" invocation cost. This was rejected because it mixes task-execution turns — which accumulate conversation history and grow over time — with the initial system-prompt turn, making it a poor proxy for ambient context size. The metric would also vary with run length, complicating cross-run comparisons.

#### Alternative 2: Expose the Full Ordered Invocation List and Let Consumers Filter

Surfacing the complete sorted list of token usage entries in the output and letting downstream tools select the first entry was considered to give consumers maximum flexibility. This was rejected because it would significantly increase output payload size for long-running agents (which may make hundreds of LLM calls) and because the first-invocation semantic is stable and well-understood enough to encode directly in the tool.

#### Alternative 3: Use the Invocation with the Fewest Input Tokens as a Proxy

Using the minimum-input-token invocation as a proxy for ambient context (assuming "lighter" calls reflect smaller context) was considered. This was rejected because minimum-token invocations can occur at any point during a run if the agent routes cheap subtasks to a smaller or cheaper model, making the metric unreliable as an ambient context indicator.

### Consequences

#### Positive
- Teams can compare ambient context overhead across workflow configurations using a single structured field without parsing raw logs.
- The metric is optional (`omitempty`) so existing JSON consumers of `audit` and `logs` output are not broken when it is absent.
- Chronological sorting of token usage entries is now an explicit, tested behavior that can be reused for future metrics that need temporal ordering.

#### Negative
- The first invocation is a heuristic proxy, not a guaranteed measure of system-prompt size. If a workflow fires a lightweight "warm-up" or health-check LLM call before the main agent invocation, the metric will reflect that call's token counts rather than the agent's true ambient context.
- Adding `AmbientContext` to `TokenUsageSummary` changes `parseTokenUsageFile` from streaming aggregation to collect-then-aggregate, which increases peak memory usage proportionally to the number of log entries (though this is bounded by the `1 MB` scanner buffer and is not expected to be significant in practice).

#### Neutral
- `token_usage.go` now imports `time` from the standard library for timestamp parsing.
- The `parseTokenUsageFile` function's internal processing order changed (collect all entries, then aggregate), but the functional output for existing aggregate fields (`TotalInputTokens`, `CacheEfficiency`, etc.) is unchanged.
- Reference documentation for `audit` and `logs` commands was updated to describe the new field.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Ambient Context Extraction

1. Implementations **MUST** compute ambient context metrics from the single earliest (chronologically first) token usage entry in `token-usage.jsonl`.
2. Implementations **MUST** sort entries by the `timestamp` field using RFC 3339 Nano format first, falling back to RFC 3339 format, when timestamps are present.
3. Implementations **MUST** use file-insertion order (entry index) as a stable tiebreaker when two entries share a timestamp or when one or both entries lack a timestamp.
4. Implementations **MUST NOT** include token counts from any invocation other than the first sorted entry in the `AmbientContextMetrics` calculation.
5. Implementations **MUST** set `aic` to `input_tokens + cache_read_tokens` for the ambient context metric.
6. Implementations **SHOULD** return `nil` and omit the field when no token usage entries are available, rather than emitting a zero-value struct.

### Output Schema

1. Implementations **MUST** expose the `ambient_context` field as an optional (`omitempty`) JSON object in the `MetricsData` struct used by `audit` JSON output.
2. Implementations **MUST** expose the `ambient_context` field as an optional (`omitempty`) JSON field on each `RunData` entry in `logs` JSON output.
3. Implementations **MUST NOT** render `ambient_context` in console-formatted tabular output; the field **MUST** carry a `console:"-"` tag.
4. Implementations **MAY** expose `ambient_context` in future report sections (e.g., audit diff, multi-run trend analysis) as the metric matures.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
