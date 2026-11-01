# ADR-73886: Scope Evals Summaries to Audited Attempts

**Date**: 2026-11-01
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

`gh aw audit` builds an evals summary by aggregating `evals.jsonl` files found under downloaded artifact prefixes. Reusable-workflow reruns can leave artifacts from multiple run attempts in the same download, while older flattened single-file evals artifacts can expose a root `evals.jsonl` alongside a prefixed copy. Evals result rows identify the workflow run but do not identify the attempt, so summing every discovered file can count the same questions more than once or mix answers from different attempts. The audit must summarize the audited attempt without losing concurrent eval invocations from that attempt.

### Decision

We will select eval result files for the audited run attempt before aggregating their summaries, counting each artifact prefix once. The audit will use eval-job upload prefixes from cached job logs when complete, fall back to `run_attempt` in per-invocation execution evidence when necessary, and conservatively return no aggregate when a rerun's multiple invocations cannot be attributed. Root-level flattened `evals.jsonl` files will be assigned to their single missing logged prefix when possible, and otherwise will not be counted as an additional invocation beside prefixed copies.

### Alternatives Considered

#### Alternative 1: Aggregate every discovered `evals.jsonl` file

This preserves the existing simple directory scan and requires no attribution logic. It was rejected because reusable-workflow reruns and flattened artifacts can contain duplicate representations or results from different attempts, producing inflated question counts and misleading audit findings.

#### Alternative 2: Attribute results only from the evals rows' run IDs

This would reuse the identity already present in each result row and avoid reading job logs or execution evidence. It was rejected because result rows do not carry a run attempt, so the same run ID cannot distinguish a rerun from the audited attempt or identify which artifact prefix produced it.

#### Alternative 3: Require execution evidence for every invocation

This would provide a direct attempt value and make selection deterministic. It was rejected because older or partially downloaded artifacts may not contain usable execution evidence; the cached eval-job logs provide stronger evidence when available, while a conservative no-summary result is safer than silently mixing attempts when neither source can establish attribution.

### Consequences

#### Positive
- Audit summaries no longer double-count eval questions from reusable-workflow reruns or flattened artifact copies.
- Concurrent eval invocations within the audited attempt remain aggregatable, while results from other attempts are excluded.
- When attribution is unavailable for a rerun, the audit avoids publishing an inflated summary instead of presenting uncertain data as fact.

#### Negative
- Audit summary generation now depends on cached job logs and execution-evidence files in addition to `evals.jsonl` contents.
- Incomplete historical artifacts can produce a missing summary rather than a partial or best-effort aggregate, which may require investigation by operators.
- The artifact naming and flattening rules must remain aligned with the assignment logic as eval uploads evolve.

#### Neutral
- The existing per-invocation summarization and findings path are retained; only the set of files passed to the aggregator changes.
- First attempts without attribution evidence continue to include all discovered invocations because there cannot be a prior rerun to mix in.
- The behavior is covered by fixtures for job-log attribution, execution-evidence fallback, flattened artifacts, and unresolvable reruns.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
