# ADR-60361: Retain Cached JSONL History Within Date Ranges

**Date**: 2026-09-12
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs --cached-jsonl` flow reuses previously stored JSONL records and appends newly collected runs, but the PR description shows that cached run entries outside a requested `--start-date` / `--end-date` window were previously left in place. This made cached output inconsistent with the date-bounded query the user asked for, even though the command continued to preserve non-run metadata and append new results immediately. The diff adds post-collection filtering in both single-target and multi-target log orchestration, plus regression coverage for inclusive date boundaries and file-permission preservation. The architectural question is how cached JSONL state should behave when users request a bounded date range.

### Decision

We will keep `--cached-jsonl` as an append-first cache format, then atomically filter cached run records to the resolved requested date range after collection completes. We decided to preserve non-run, unknown-schema, and undated records while pruning only dated run entries outside the inclusive bounds, because the PR evidence shows the cache must remain reusable and forward-compatible without returning stale run history for date-scoped requests. We will apply the same lifecycle in both single-target and multi-target log collection paths so date-range semantics stay consistent across command modes.

### Alternatives Considered

#### Alternative 1: Leave all previously cached run records untouched

The existing behavior effectively preserved the entire JSONL history and only appended newly collected results. This was considered because it keeps cache writes simple and maximizes retained history. It was not chosen because the PR description and tests show that users asking for a specific date range should not keep seeing stale cached run entries outside that range.

#### Alternative 2: Rewrite the cache to contain only newly fetched records for the current invocation

Another option would be to discard prior cache contents and rebuild the JSONL file solely from the current run's collected records. This was considered because it guarantees strict alignment with the current query window. It was not chosen because the PR evidence explicitly preserves workflow-list, rate-limit, unknown, and undated records, and the existing cache design values incremental append/reuse rather than full replacement.

#### Alternative 3: Filter results only in memory and keep the on-disk cache broader than the requested range

The command could have filtered the final rendered output while leaving the backing JSONL file unchanged. This was considered because it avoids an extra read/filter/write pass on the cache file. It was not chosen because the reported bug is specifically about cached JSONL state retaining out-of-range run history, which would continue to affect later reuse and make the on-disk cache diverge from requested date-range semantics.

### Consequences

#### Positive
- Cached JSONL files now match inclusive `--start-date` and `--end-date` expectations for run records, reducing stale results during later reuse.
- The cache continues to preserve non-run metadata, unknown records, and undated records, maintaining forward compatibility and diagnostic usefulness.
- Single-target and multi-target logs flows share the same post-collection filtering lifecycle, reducing semantic drift between command modes.

#### Negative
- Each date-bounded cached run now incurs an additional full-file read and atomic rewrite step after collection, increasing I/O cost for large cache files.
- Cache lifecycle behavior becomes more complex because correctness depends on append-first collection followed by deferred pruning.
- Date-range correctness now depends on consistent date parsing and boundary resolution, creating more edge cases around timestamps and date-only end bounds.

#### Neutral
- The implementation keeps the JSONL file format unchanged and introduces behavior through orchestration and writer helpers rather than a new cache schema.
- Help text and flag descriptions now document that date-bounded runs prune cached run entries after collection while retaining other record kinds.
- Regression coverage explicitly checks preserved file permissions and retained non-run record types, clarifying the cache contract for future changes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
