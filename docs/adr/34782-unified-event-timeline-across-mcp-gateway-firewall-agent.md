# ADR-34782: Unified Event Timeline Across MCP Gateway, AWF Firewall, and Agent Logs

**Date**: 2026-05-25
**Status**: Draft; JavaScript step-summary portion superseded
**Deciders**: Unknown

---

## JavaScript step-summary supersession (2026-10-08)

The unified session collector and publication views described in
[the unified agent session specification](../src/content/docs/specs/unified-agent-session-specification.md#85-unified-file-publication-views)
supersede this ADR's JavaScript timeline design. The conclusion collector writes
`aw_session.jsonl`, and `unified_session_render.cjs` publishes one collapsible
`Unified session` section with a fenced CLI-style transcript. Gateway diagnostics
no longer append a second timeline table.

The retired `unified_timeline.cjs` renderer and its dedicated tests have been
removed. Copilot SDK events remain collected by `unified_session.cjs`. This
eliminates the duplicate JavaScript collection/rendering implementation and its
Go/JavaScript synchronization cost.

The Go audit, logs, and view consumers of `gateway_logs_timeline.go` and
`gateway_logs_timeline_render.go` remain supported and unchanged. The historical
decision and requirements below are retained for context, but requirements for
the JavaScript renderer, dual-language parity, and the
`writeStepSummaryWithTokenUsage` integration no longer apply.

---

## Part 1 — Narrative (Human-Friendly)

### Context

Three independent runtime subsystems each emit their own JSONL event stream to disk: the MCP Gateway (`mcp-logs/gateway.jsonl` or `rpc-messages.jsonl`), the AWF network firewall (`sandbox/firewall/audit/audit.jsonl`), and the Copilot agent CLI (`sandbox/agent/logs/copilot-session-state/<uuid>/events.jsonl`). The schemas and timestamp formats are inconsistent — RFC3339 strings for gateway and agent events, Unix `float64` seconds for the firewall audit, and divergent field names (`type` vs `event`, `server_id` vs `server_name`). When a workflow run misbehaves, an operator needs to reconstruct the order of operations across all three subsystems but currently must open three files side-by-side and align timestamps manually. The existing per-source renderers (audit CLI, logs CLI, JS step summary) report each stream in isolation, so causality between an agent turn, the resulting gateway tool call, and the downstream network egress is invisible.

### Decision

We will merge events from all three sources into a single chronologically sorted **unified timeline** with a common normalized schema (`source`, `kind`, `time`, `detail`, `status`) and render it in two surfaces: the Go audit/logs CLI console output, and the JavaScript GITHUB_STEP_SUMMARY. Each source has its own `collect*Events` function that converts native records into the normalized schema; a merge step sorts by timestamp and a renderer emits a summary line plus a per-event table. The Go and JavaScript implementations share the same source labels (`GW`/`FW`/`AG`), event-kind constants, and icon set so console and step-summary outputs are visually consistent.

### Alternatives Considered

#### Alternative 1: Improve Per-Source Renderers Only

Keep each log file rendered independently and invest in richer per-source views (e.g., a fuller gateway tool-call list, better firewall decision grouping). This was rejected because it does not solve the actual debugging problem: causality across subsystems remains invisible. The cost of opening three files and mentally aligning timestamps would persist.

#### Alternative 2: Centralized Event Bus at Runtime

Modify the gateway, AWF, and agent CLI to write into a single shared event store (or stream events over a socket) at runtime, so correlation happens at write time rather than during post-run rendering. This was rejected because the three components are independently versioned (gateway is in-tree, AWF is an external binary released on its own cadence, agent CLI is a third-party Copilot tool) and a shared write protocol would require coordinated releases across all three. The blast radius is much larger than a read-side merge, and the JSONL artifacts already exist.

#### Alternative 3: Single Implementation Language

Implement the unified timeline only in Go (and have the JS step summary shell out) or only in JavaScript (and have the Go CLI call a Node child process). This was rejected because the JS module already runs inside the GitHub Actions JS environment as part of `parse_mcp_gateway_log.cjs` and cannot easily call Go; conversely, requiring Node to be available during `audit`/`logs` CLI invocation would add a runtime dependency to a Go binary. The duplication cost was judged smaller than the cross-language interop cost.

### Consequences

#### Positive
- Cross-subsystem causality is visible in a single sorted view: an `agent_turn` followed by a `tool_call` followed by a `net_allowed` becomes obvious at a glance.
- A new event source can be added by implementing one `collect*Events` function plus a few constants — the merge, sort, and render layers are source-agnostic.
- Reuses existing on-disk JSONL artifacts; no runtime changes to gateway, AWF, or agent CLI are required.
- The same normalized event schema, source codes, and icons are shared between the Go and JS outputs, so the two surfaces stay visually consistent.

#### Negative
- Two parallel implementations (~545 LOC in `unified_timeline.cjs` plus the Go equivalents in `gateway_logs_timeline.go` / `gateway_logs_timeline_render.go`) must be kept in sync; a new event kind or source means edits in both places.
- Timestamp normalization is fragile: the firewall uses Unix float seconds while the others use RFC3339 strings, and events with unparseable timestamps are silently dropped rather than surfaced.
- The merge is post-hoc and assumes wall-clock alignment across processes; if the firewall and agent containers have skewed clocks, the apparent ordering can mislead.
- Adds a 545-line JS module loaded by `parse_mcp_gateway_log.cjs` on every step-summary write, even when no events exist; the cost is small but non-zero.

#### Neutral
- Existing per-source renderers (e.g., aggregated gateway metrics, firewall audit summary) remain in place — the unified timeline is appended in addition to, not in place of, them.
- The JS renderer wraps the table in `<details>` to keep the step summary compact; the Go renderer prints inline. The two surfaces have intentionally different verbosity defaults.
- Truncating `detail` to 48 characters is consistent across both implementations; long tool names or hosts are elided.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Event Schema

1. Every timeline event **MUST** carry the fields `source`, `kind`, `time`, `detail`, and `status`.
2. The `source` field **MUST** be one of the defined source constants: `gateway`, `firewall`, or `agent`.
3. The `kind` field **MUST** be one of the defined event-kind constants for the event's source.
4. The `time` field **MUST** be a parsed timestamp; events with unparseable timestamps **MUST** be dropped during collection rather than emitted with a zero or substitute time.
5. New event sources added in the future **MUST** define their own `source` constant and at least one `kind` constant, and **MUST NOT** reuse another source's `kind` constants.

### Source Identification

1. The Go and JavaScript implementations **MUST** use the same source labels: `GW` for gateway, `FW` for firewall, `AG` for agent.
2. The Go and JavaScript implementations **MUST** use the same event-kind string constants (e.g., `tool_call`, `difc_filtered`, `net_allowed`, `agent_turn`).
3. The Go and JavaScript implementations **SHOULD** use the same display icons for each event kind so the two output surfaces remain visually consistent.

### Collection

1. Each source **MUST** be collected by a dedicated function (`collectGatewayEvents`, `collectFirewallEvents`, `collectAgentEvents` and their Go equivalents) that returns normalized events.
2. Collectors **MUST** return an empty list when their source artifact is missing, and **MUST NOT** raise an error or abort the unified collection.
3. Collectors **MUST NOT** read or write any file outside their declared source path(s).
4. The agent collector **MUST** locate `events.jsonl` by searching one level deep under the canonical `copilot-session-state` directory and **SHOULD NOT** perform unbounded recursive searches.

### Merge and Ordering

1. The unified timeline **MUST** be sorted by `time` ascending.
2. The merge step **MUST NOT** deduplicate, filter, or rewrite events from individual collectors beyond ordering.
3. The merge step **MUST** preserve every event returned by each collector.

### Rendering

1. The Go console renderer **MUST** print a summary header with total event count and per-source counts before the per-event table.
2. The JavaScript step-summary renderer **MUST** wrap the timeline in a `<details>` block so it does not consume vertical space by default.
3. Both renderers **MUST** escape Markdown-significant characters (at minimum, the pipe `|`) in `detail` and `status` cells.
4. Both renderers **MUST** return an empty string (or equivalent no-op) when there are zero events, and **MUST NOT** emit an empty table or header.
5. Renderers **SHOULD** truncate `detail` values to a consistent maximum length (48 characters) and indicate truncation with an ellipsis.

### Integration Points

1. The Go audit pipeline **MUST** invoke unified-timeline rendering from `renderAuditReport` after `renderAuditGatewayMetrics`.
2. The Go logs orchestrator **MUST** invoke unified-timeline rendering after `displayAggregatedGatewayMetrics` in the console path.
3. The JavaScript `writeStepSummaryWithTokenUsage` function **MUST** append the unified timeline Markdown to `core.summary` before every `write()` call, regardless of which gateway log format was detected.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
