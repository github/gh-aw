# ADR-63688: Track skill usage in audit artifacts

**Date**: 2026-09-26
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR adds new skill-usage aggregation to the compact usage artifact pipeline and threads that data through audit processing, output schemas, and tests. Today, named skill invocations are only visible in full agent traces, which makes lightweight audit analysis and cached audit summaries miss an important part of agent behavior. The changes span JavaScript usage-summary generation, Go audit ingestion and reporting, and schema definitions, indicating a cross-cutting architectural decision about how gh-aw should represent skill usage in durable audit artifacts. The decision is how to expose skill-tool activity without retaining or depending on full trace payloads.

### Decision

We will aggregate named `skill` tool invocations into compact usage artifacts and propagate those aggregates into audit processing as `skill:<name>` tool usage plus structured skill-activation records with invocation and failure counts. We will enrich existing detailed skill activations when available and synthesize `usage_summary` activations when only compact aggregate data exists. We chose this because it preserves named skill visibility in lightweight audit flows and cached summaries while avoiding a dependency on full session traces.

### Alternatives Considered

#### Alternative 1: Keep skill visibility only in full agent traces

This was a realistic option because the underlying session events already contain the raw `skill` tool calls. It was not chosen because the PR evidence shows that usage-only and cached audit paths currently lose named skill activity, which prevents lightweight audits from reporting that behavior.

#### Alternative 2: Record only a single total skill count without per-skill names

This was considered because a single aggregate would be simpler to collect and schema-manage. It was not chosen because the implementation and PR description both require audit output to expose named skills as `skill:<name>` tools, which provides materially better observability than a total count alone.

### Consequences

#### Positive
- Lightweight audit artifacts can report named skill usage without requiring full agent trace retention.
- Audit output becomes more faithful to actual agent behavior by including `skill:<name>` tool usage and failure counts.
- Cached and backfilled audit summaries preserve skill data across JavaScript and Go processing paths.

#### Negative
- Skill usage now has to stay consistent across multiple layers: usage-summary generation, Go backfill logic, audit rendering, and JSON schemas.
- The audit data model becomes more complex because skill activations may come from raw logs, safe outputs, or compact `usage_summary` aggregates.
- Additional regression tests are required to keep invocation-count semantics aligned between trace-derived and aggregate-derived records.

#### Neutral
- Schema consumers now need to understand the optional `invocation_count` and `failed_count` fields on skill activation records.
- The change supplements, rather than replaces, detailed trace analysis for workflows that still need per-event raw data.
- Existing audit tooling can continue to treat missing invocation counts as a single invocation for non-aggregate sources.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
