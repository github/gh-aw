# ADR-60136: Fix Daily AIC Snapshot Completeness and Quota Handling

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Daily AIC accounting in gh-aw reused incomplete historical observations, repeatedly fetched old artifacts after failures, and could report partial totals as under budget when quota-limited reads failed. This pull request updates workflow generation and runtime support across many compiled workflows, plus supporting scripts, to make the activation phase publish a complete versioned scan snapshot and to treat fresh workflow history as the authoritative accounting source. The PR description also calls out independent validation of agent, detection, and evals usage, preservation of valid usage across reruns, and stopping historical reads after the first failure. The architectural question is how daily AIC usage state should be persisted and reused so budget enforcement remains correct under reruns, malformed data, and API quota failures.

### Decision

We will replace the conclusion-time cache-based daily AIC persistence flow with activation-published, versioned scan observation artifacts that contain complete resolved-run snapshots and are revalidated before reuse. We decided to derive authoritative totals from fresh workflow history, stop historical recovery after the first failed read, and prevent incomplete scans from producing an `under_budget` result because the PR evidence shows correctness of accounting is more important than reusing partial state. We will also collect and validate agent, detection, and evals usage independently so overlapping summaries and malformed numeric values do not distort the daily total.

### Alternatives Considered

#### Alternative 1: Keep the conclusion-only Actions cache model

The existing design restored a daily AIC cache and appended new observations during workflow conclusion, then reused that cached state later. This was considered because it already existed and avoided changing many compiled workflows. It was not chosen because the PR evidence shows it could reuse incomplete observations, double-count overlapping usage, and preserve partial totals after quota failures, which makes budget decisions unreliable.

#### Alternative 2: Recompute all daily usage from scratch on every run without persisted scan observations

Another option would be to discard reuse entirely and rebuild the daily accounting view from raw workflow history for every invocation. This was considered because it would minimize trust in prior state and simplify correctness reasoning. It was not chosen because the PR clearly introduces reusable versioned scan snapshots, indicating the system still wants bounded historical recovery and resumable scan state rather than full repeated recomputation.

#### Alternative 3: Continue reusing historical observations even after partial fetch failures

The system could continue processing whatever historical artifacts were available and mark the workflow under budget when the partial total remained below the limit. This was considered because it maximizes availability and allows workflows to proceed under degraded API conditions. It was not chosen because the PR explicitly stops after the first failed historical read and prevents incomplete scans from yielding `under_budget`, showing that false-safe budget results are less acceptable than a blocked or incomplete accounting result.

### Consequences

#### Positive
- Daily budget decisions become more trustworthy because reused observations must come from complete versioned scan snapshots and are revalidated before contributing to totals.
- Quota and API failures no longer silently degrade into false `under_budget` outcomes based on partial history.
- Separating agent, detection, and evals usage validation reduces double-counting and makes accounting inputs easier to reason about across reruns.

#### Negative
- The activation path and generated workflows become more complex because snapshot publication, restoration, validation, and failure handling now happen earlier and in more places.
- The system is less tolerant of partial historical data, so some runs that previously continued may now fail or refuse to declare budget safety.
- Regenerating many workflow lock files increases the blast radius of the change and makes future maintenance of this accounting flow more expensive.

#### Neutral
- Workflow artifacts now include a versioned `aic-usage-scan-v2` snapshot instead of relying on the prior conclusion-time cache write pattern.
- Evals token usage is collected and uploaded alongside existing eval artifacts, extending the accounting surface covered by the workflows.
- Repository contributors must continue recompiling generated workflow lock files whenever the underlying workflow markdown or runtime behavior changes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
