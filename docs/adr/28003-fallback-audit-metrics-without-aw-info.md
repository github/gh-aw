# ADR-28003: Fallback Strategy for Audit Metrics When aw_info.json Is Absent

**Date**: 2026-04-23
**Status**: Draft
**Deciders**: pelikhan


---

## Part 1 — Narrative (Human-Friendly)

### Context

The `gh aw audit` command aggregates run-level metrics (token usage, turn count, engine config) to produce audit reports. These metrics are primarily sourced from `aw_info.json`, a structured artifact written by newer workflow runs. Legacy runs that predate the introduction of `aw_info.json` do not produce this artifact, causing the audit command to emit `engine_config: null`, `metrics.token_usage: null`, and `metrics.turns: null` even when alternative data sources — `agent_usage.json` and raw agent log files (`agent-stdio.log`, `events.jsonl`) — are present in the run directory. This gap reduces the usefulness of audit reports for historical analysis and fleet-wide comparisons.

### Decision

We will implement a multi-level fallback strategy in the audit pipeline that recovers metrics from alternative artifacts and log files when `aw_info.json` is absent. For token usage, the pipeline will fall back to `agent_usage.json` when the firewall proxy `token-usage.jsonl` is unavailable. For engine config, the pipeline will infer the engine by parsing available log files with all registered engine parsers and selecting the parser that recovers the strongest signal (prioritizing turn count, then token usage, then tool calls). For turn count and token usage in the audit report, the pipeline will cascade through: run-level parsed metrics → artifact token summaries → log inference.

### Alternatives Considered

#### Alternative 1: Require aw_info.json and Backfill Historical Data

Enforce `aw_info.json` as a mandatory artifact and run a one-time migration to retroactively populate it for historical runs. This was rejected because it requires coordinating infrastructure changes across all historical workflow runs and cannot recover data that was never recorded.

#### Alternative 2: Surface Null Values and Document Limitations

Accept `null` metric fields for older runs and document that pre-`aw_info.json` runs have incomplete audit data. This was rejected because it degrades the audit tool's utility for historical fleet analysis and provides no path forward for operators who need accurate metrics across their entire run history.

### Consequences

#### Positive
- Audit reports are populated for legacy runs, enabling accurate historical fleet analysis.
- The fallback chain is additive and non-destructive: runs with `aw_info.json` are unaffected.
- `agent_usage.json` token data (including `aic`) is surfaced through the same `TokenUsageSummary` abstraction already used by the primary path.

#### Negative
- The audit pipeline now has three distinct code paths for metric acquisition, increasing complexity and surface area for bugs.
- Inferred engine identification via log scoring is heuristic: the parser selection algorithm (weighted by turns, then token usage, then tool calls) may misidentify the engine when log content is ambiguous or shared across parsers.
- `agent_usage.json` is treated as a single-request summary, so per-model and per-request breakdowns are not available via this fallback.

#### Neutral
- `agent_usage.json` fallback data is converted into the same aggregate token and AI Credits summaries as the primary path without adding per-entry fields to `token-usage.jsonl` records.
- The engine inference function (`inferBestEngineMetricsFromContent`) iterates all registered engines and may add latency proportional to the number of registered parsers for runs without `aw_info.json`.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Token Usage Acquisition

1. Implementations **MUST** attempt to load token usage from `token-usage.jsonl` (firewall proxy log) first.
2. Implementations **MUST** fall back to `agent_usage.json` when `token-usage.jsonl` is absent or cannot be located.
3. Implementations **MUST NOT** apply custom token weight overrides (from `aw_info.json`) to the `agent_usage.json` fallback path, as custom weights are only meaningful alongside the firewall proxy data.
4. Implementations **SHOULD** search for `agent_usage.json` at the root of the run directory before walking subdirectories, to minimize filesystem traversal.

### Audit Metric Fallback Chain

1. Implementations **MUST** populate `metrics.token_usage` by cascading through, in order: (1) run-level parsed log metrics, (2) `input_tokens + output_tokens` from the artifact `TokenUsageSummary`, (3) token usage inferred from log content.
2. Implementations **MUST** populate `metrics.turns` by cascading through, in order: (1) run-level parsed log metrics, (2) turn count inferred from log content.
3. Implementations **MUST NOT** overwrite a non-zero metric value with a fallback value from a lower-priority source.

### Engine Config Inference

1. When `aw_info.json` is absent, implementations **MUST** attempt engine inference by parsing available log files using all engines registered in the global engine registry.
2. Implementations **MUST** select the inferred engine by maximising a weighted score: `turns * 100000 + len(tool_calls) * 1000 + token_usage`.
3. Implementations **MUST NOT** return an inferred engine config if no registered engine parser recovers any useful signal (turns, token usage, or tool calls).
4. Implementations **SHOULD** prefer `events.jsonl` over `agent-stdio.log` for engine inference when both are present.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/24834078573) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
