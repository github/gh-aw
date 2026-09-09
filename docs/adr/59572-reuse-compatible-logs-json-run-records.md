# ADR-59572: Reuse compatible logs JSON run records

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command currently recomputes run records by downloading and processing artifacts even when equivalent `logs --json` output from an earlier run already exists. This PR adds a new `--cached-json` input, threads it through both standard and stdin-based log collection flows, and rebuilds report output from a mix of reused cached records and newly processed runs. The diff shows strict cache-safety checks around run identity, completion state, attempt, repository, conclusion, and update time, plus explicit fallbacks when requested filters require artifact-level evidence not preserved in compact JSON. The repository needs a documented decision on whether prior JSON output is an acceptable cache source for logs analysis and reporting.

### Decision

We will allow `gh aw logs` to reuse prior `--json` output as a cache source when a cached record can be proven compatible with the current workflow run and requested analysis mode. The implementation will only reuse completed runs with matching run ID, repository, attempt, conclusion, and update timestamp, and it will disable cached reuse for artifact-dependent filters or analysis modes that require richer evidence than compact JSON retains. When reuse is valid, the command will preserve cached run records in rebuilt JSON output, recompute aggregate totals from the combined cached and newly processed data, and overwrite the file passed to `--cached-json` with the updated JSON response.

### Alternatives Considered

#### Alternative 1: Always re-download and reprocess run artifacts

The command could continue treating every invocation as a fresh collection pass with no reuse of earlier JSON output. This was considered because it is the simplest behavior and avoids the risk of stale cached data. It was not chosen because the PR explicitly adds compatibility checks and tests to avoid unnecessary artifact work for unchanged completed runs.

#### Alternative 2: Reuse cached JSON for all runs and filter modes without validation

Another option would be to accept any previous logs JSON as authoritative and skip most per-run validation and mode checks. This was considered because it would maximize performance improvements and implementation simplicity. It was not chosen because the diff adds explicit guards for repository, attempt, conclusion, updated timestamp, engine filters, and artifact-dependent modes, showing that unvalidated reuse would be unsafe.

#### Alternative 3: Introduce a dedicated internal cache format instead of reusing prior JSON output

The project could have created a separate opaque cache artifact tailored specifically for reuse rather than depending on user-visible JSON output. This was considered because a dedicated format could carry richer evidence and fewer compatibility constraints. It was not chosen in this PR because the implementation intentionally reuses existing `logs --json` output, preserving current user workflows and avoiding an additional cache format.

### Consequences

#### Positive
- Re-running `gh aw logs` can avoid downloading and reprocessing artifacts for unchanged completed runs, reducing cost and latency.
- Cache reuse remains conservative because compatibility checks reject stale or insufficient cached records.
- Rebuilt reports can combine cached and fresh records while preserving existing JSON output structure.
- The cache file is ready for the next invocation without requiring separate output redirection.

#### Negative
- The logs pipeline becomes more complex because download, stdin, filtering, and aggregation paths must all account for cached records.
- Aggregate values may be approximate when compact cached JSON omits detailed evidence that richer artifact processing would have produced.
- Users must understand when `--cached-json` is ignored due to incompatible filters or analysis modes.

#### Neutral
- `LogsDownloadOptions`, `StdinLogsOptions`, and related orchestration types now carry a `CachedJSON` field through multiple entry points.
- Report aggregation now has a separate path for accumulating totals from cached `RunData` values.
- The feature relies on previous JSON output remaining parseable and structurally compatible with the current `LogsData` schema.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
