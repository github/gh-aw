# ADR-63664: Surface AWF steering counters in compact usage data

**Date**: 2026-09-26
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR adds support for reading AWF API proxy steering events from multiple log file layouts and schema variants, then backfills those counts into compact usage artifacts and audit output. Today, detailed steering behavior requires raw firewall logs, while compact usage summaries and usage-only audits do not expose per-event steering totals. The changed files span JavaScript artifact generation, Go audit backfill logic, schema updates, tests, and documentation, which indicates a cross-cutting product decision rather than a localized bug fix. The decision is how gh-aw should represent steering behavior when only compact usage artifacts are available.

### Decision

We will aggregate AWF steering events by normalized event name inside the compact usage activity summary and propagate those counters into audit token-usage output when detailed firewall analysis is unavailable. We will support the log filename and schema variants visible in the PR evidence, including `events.jsonl`, `event-logs.jsonl`, and top-level or nested event-name fields. We chose this because it preserves steering visibility for `gh aw audit --artifacts usage` and related usage-only flows without requiring raw firewall log downloads.

### Alternatives Considered

#### Alternative 1: Keep steering analysis only in raw firewall log processing

This was a realistic option because gh-aw already derives detailed steering information from firewall artifacts when those logs are present. It was not chosen because the PR evidence explicitly adds steering data to `usage/activity/summary.json` and backfills audit results from compact usage artifacts, showing that raw-log-only visibility is insufficient for the intended audit workflows.

#### Alternative 2: Publish only a single total steering-event count

This was considered because earlier code paths already tracked `total_steering_events`, and a single aggregate is simpler to compute and document. It was not chosen because the PR updates both JS and Go paths to preserve per-event counters such as `token_steering` and `timeout_steering`, which provides more actionable inspection of AWF behavior than a single total.

### Consequences

#### Positive
- Usage-only audit flows can report steering behavior even when raw firewall logs are unavailable.
- Operators gain per-event steering counters, which makes it easier to distinguish token, timeout, model, or other steering causes.
- The implementation becomes more robust across AWF versions by recognizing multiple log filenames and event-name field variants.

#### Negative
- Steering parsing logic now exists in both JavaScript artifact-generation code and Go audit-analysis code, which increases maintenance overhead.
- Supporting multiple historical file layouts and schema variants adds complexity and ongoing compatibility expectations.
- Compact usage artifacts and schemas become broader, which increases documentation and regression-test surface area.

#### Neutral
- Audit consumers now need to understand both `total_steering_events` and `steering_event_counts` fields in token-usage output.
- The change does not replace detailed firewall analysis; it supplements it when only usage artifacts are available.
- Additional tests are required to keep the JS and Go aggregation paths aligned as AWF logging evolves.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
