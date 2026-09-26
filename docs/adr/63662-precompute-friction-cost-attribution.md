# ADR-63662: Precompute friction-cost attribution

**Date**: 2026-09-26
**Status**: Draft
**Deciders**: Unknown

---

### Context

This pull request introduces a new friction-cost measurement module, related tests, additive schema updates, and documentation for `gh aw audit`, `gh aw logs`, and the `activity/summary.json` usage artifact. The PR description states that friction cost is the estimated avoidable marginal execution cost, canonically measured in AIC, and that attribution should be precomputed during workflow conclusion so downstream consumers can rely on the compact `usage` artifact instead of reprocessing raw logs. The diff adds deterministic causal grouping, driver-specific attribution states, uncertainty metadata, and backward-compatible artifact fields, which together establish a new architecture for how execution-friction telemetry is derived and consumed. The decision is how gh-aw should represent and publish friction-cost attribution for audit and fleet-analysis use cases.

### Decision

We will precompute friction-cost attribution during the workflow conclusion job and store it as an additive `friction-cost/v1` section in the usage summary artifact. We will use AIC as the canonical unit, expose driver-level and event-level attribution with measured, causal, statistical, and unavailable states, and preserve uncertainty and provenance metadata so consumers can distinguish directly observed values from estimates. We chose this so `gh aw audit`, `gh aw logs`, and other usage-only consumers can read a compact, consistent artifact instead of downloading and re-deriving results from raw logs.

### Alternatives Considered

#### Alternative 1: Compute friction cost on demand from raw logs in each consumer

This was a realistic option because existing audit and log-analysis commands already read execution data and could derive friction metrics when needed. It was not chosen because the PR explicitly adds precomputation to avoid forcing audit and fleet-analysis consumers to download raw logs, and because repeating the derivation in multiple consumers would duplicate complex attribution logic such as causal grouping and uncertainty handling.

#### Alternative 2: Record only aggregate friction totals without event-level attribution details

This was considered because a totals-only artifact would be simpler and smaller to store and display. It was not chosen because the diff and PR description both add driver aggregates, event attribution, confidence, bounds, and provenance, indicating that downstream analysis needs explainable per-driver and per-event breakdowns rather than an opaque single total.

### Consequences

#### Positive
- Audit and log consumers can read friction-cost results directly from the usage artifact without reprocessing raw logs.
- The system has one deterministic attribution implementation for tool failures, integrity filtering, firewall blocks, and errored model invocations.
- Uncertainty, attribution state, and provenance are preserved, making estimated values distinguishable from directly measured ones.
- Backward-compatible additive schema changes let older artifact readers continue functioning while enabling richer analysis for newer consumers.

#### Negative
- The conclusion job becomes more complex because it now owns attribution logic, causal grouping, uncertainty propagation, and artifact shaping.
- Once precomputed output becomes the preferred source, errors in the attribution model can affect multiple downstream consumers at once.
- The artifact schema and documentation surface area expand, increasing maintenance and testing burden.
- Event-level attribution records may require truncation or summarization controls to avoid artifact growth.

#### Neutral
- Historical runs that lack precomputed friction data will still require conservative reconstruction or partial summaries.
- AIC becomes the canonical comparison unit even though additional dimensions such as tokens, turns, tool calls, and latency are still reported when supported.
- Schema, console output, and JSONL contracts need additive coordination across documentation, tests, and consuming commands.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
