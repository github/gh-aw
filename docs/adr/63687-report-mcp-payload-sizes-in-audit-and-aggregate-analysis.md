# ADR-63687: Report MCP payload sizes in audit and aggregate analysis

**Date**: 2026-09-26
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR adds MCP payload-size metrics to the usage activity summary generator, audit backfill path, aggregate MCP usage rollups, and related tests. Today, audit and aggregate reports expose totals and some maxima, but they do not consistently surface average request and response sizes when only compact usage artifacts are available. Historical data and usage-only flows therefore make it difficult to compare which tools and servers contribute most to context growth without raw gateway logs. The decision is how gh-aw should represent and normalize MCP payload-size metrics across current and historical usage data.

### Decision

We will persist average and maximum MCP request and response sizes in compact usage activity summaries and normalize those payload statistics when building audit and aggregate analysis output. We will derive rounded averages from additive totals and call counts using the same semantics as the usage artifact producer, and derive server maxima from per-tool maxima so historical and legacy records can be reported consistently. We chose this because it preserves privacy-safe size metadata while making usage-only audits and aggregate analysis actionable even when raw gateway logs are unavailable.

### Alternatives Considered

#### Alternative 1: Continue reporting only total payload sizes

This was a realistic option because the existing summaries already tracked total input and output sizes and could continue to support coarse comparisons. It was not chosen because totals alone hide whether large usage comes from many small calls or a few oversized calls, which is the concrete gap called out by the PR description and addressed by the added average and maximum fields.

#### Alternative 2: Require raw gateway logs for payload-size analysis

This was considered because raw logs already contain enough information to compute detailed per-call payload statistics without extending compact usage artifacts. It was not chosen because the PR explicitly backfills and normalizes MCP usage from compact usage summaries for historical and usage-only audit flows, showing that raw-log-only analysis does not meet the repository's reporting needs.

### Consequences

#### Positive
- Audit and aggregate reports can show actionable average payload sizes for MCP tools and servers even when only compact usage artifacts are present.
- Historical and legacy records become more comparable because normalization derives missing rounded averages and server maxima from additive totals and tool-level data.
- The change remains privacy-safe because it records size metadata rather than request or response payload content.

#### Negative
- MCP usage aggregation logic becomes more complex because it must normalize averages and maxima across multiple code paths and historical formats.
- Additional schema and test surface area increases the maintenance cost of keeping JavaScript summary generation and Go reporting paths aligned.
- Derived averages and server maxima depend on the accuracy of additive totals and tool summaries, so malformed historical records may still produce imperfect output.

#### Neutral
- Report consumers will see additional payload-size columns in audit and aggregate output and may need to update downstream expectations.
- The implementation supplements existing total-size metrics rather than replacing them.
- Future MCP usage fields will likely need to follow the same normalization approach for backward-compatible reporting.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
