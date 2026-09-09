# ADR-58018: Add Integrity and MCP Metrics to Conclusion Usage Reporting

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request expands how `gh-aw` records and reports conclusion-job usage data for MCP activity and integrity filtering. The diff adds compact aggregation of gateway and RPC log data, including per-server and per-tool call counts, payload sizes, durations, and failures, then carries those aggregates into `gh aw logs` reporting and conclusion-span OTLP attributes. It also changes RPC parsing so MCP `result.isError` responses are treated as failures and documents the additive `usage-activity-summary/v1` schema updates. Because this changes the observability contract for usage artifacts, CLI reporting, and telemetry emitted by the conclusion job, the decision should be captured explicitly.

### Decision

We will extend conclusion usage reporting to derive compact MCP and integrity-filter aggregates from gateway logs, with fallback to `rpc-messages.jsonl` when `gateway.jsonl` is unavailable, and surface those aggregates consistently in usage artifacts, `gh aw logs`, and conclusion OTLP spans. We will treat MCP `result.isError` responses as failed tool calls so compact summaries and downstream reports reflect tool-level failures even when they are encoded in successful JSON-RPC envelopes. We chose this approach because the PR evidence shows a concrete need to backfill missing conclusion metrics without replacing detailed data, while preserving the additive `usage-activity-summary/v1` contract.

### Alternatives Considered

#### Alternative 1: Keep Existing Firewall and Total-Call Summaries Only

Continue emitting only the existing compact firewall and high-level gateway counters, leaving integrity filtering, payload sizes, durations, and detailed failure handling available only in raw logs.

This was considered because it avoids changing the compact summary schema and reduces parsing complexity. It was not chosen because the PR description explicitly identifies missing integrity-filter and detailed MCP activity metrics in usage artifacts, `gh aw logs`, and OTLP reporting, which this option would leave unresolved.

#### Alternative 2: Require Detailed Raw Logs for All MCP and Integrity Reporting

Avoid compact backfilled aggregates and compute all MCP and integrity reporting exclusively from downloaded detailed gateway or RPC logs at report time.

This was considered because it keeps the summary artifact smaller and avoids duplicating information already present in raw logs. It was not chosen because the diff explicitly adds compact aggregate fields so reports and conclusion telemetry can use MCP and integrity metrics without depending on raw log availability, and so cached or partial reports can be healed with the new summary data.

### Consequences

#### Positive
- Conclusion usage artifacts now carry compact MCP metrics for call volume, failures, payload sizes, and durations, improving downstream reporting without requiring raw-log inspection.
- Integrity-filter activity is summarized by server, tool, and reason, making filtered-event behavior visible in both usage artifacts and logs reports.
- Conclusion OTLP spans now expose firewall, MCP, and integrity metrics as attributes, improving run-level observability for telemetry consumers.

#### Negative
- The usage summary generator becomes more complex because it must reconcile gateway logs and RPC fallback formats, maintain pending request/response correlation, and compute additional aggregates.
- The compact artifact schema grows, increasing the number of fields that tests, docs, and future compatibility changes must preserve.
- Failure semantics change because MCP `result.isError` is now counted as an error, which may alter historical comparisons with earlier reports that treated those responses as successful envelopes.

#### Neutral
- The `usage-activity-summary/v1` schema remains additive rather than being replaced, so existing consumers can continue reading older fields while new consumers adopt the added metrics.
- `gh aw logs` reporting gains aggregate integrity summaries such as `runs_with_filtered_events` in addition to existing detailed filtered-event collections.
- Documentation and tests must now describe and validate both compact summary fields and conclusion-span telemetry attributes for the new metrics.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
