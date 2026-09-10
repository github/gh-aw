# ADR-59572: Reuse compatible logs JSON run records

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command currently recomputes run records by downloading and processing artifacts even when equivalent cached run data already exists. This PR adds a new `--cached-jsonl` input, threads it through both standard and stdin-based log collection flows, and rebuilds report output from a mix of reused cached records and newly processed runs. The diff shows strict cache-safety checks around run identity, completion state, attempt, repository, conclusion, and update time, plus explicit fallbacks when requested filters require artifact-level evidence not preserved in compact JSONL records. The repository needs a documented decision on whether prior JSONL output is an acceptable cache source for logs analysis and reporting.

### Decision

We will allow `gh aw logs` to reuse `--cached-jsonl` records when the record schema version is supported and the cached record can be proven compatible with the current request. Processed runs require matching run ID, repository, attempt, conclusion, and update timestamp. Each complete `gh run list` response is also appended before filtering or artifact downloads and keyed by its host, repository, and command arguments. Available GitHub API rate-limit reports are appended as separate records. Every record is compacted to exactly one JSON value per line. This preserves every field returned by `gh`, makes undispatched work observable after interruption, and allows an exact future request to reuse the payload. Records from incompatible schema versions are ignored.

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
- Consumers can inspect every discovered run even when artifact processing stops early.

#### Negative
- The logs pipeline becomes more complex because download, stdin, filtering, and aggregation paths must all account for cached records.
- Aggregate values may be approximate when compact cached JSON omits detailed evidence that richer artifact processing would have produced.
- Users must understand when `--cached-jsonl` is ignored due to incompatible filters or analysis modes.
- Reusing an exact discovery request returns the stored point-in-time payload.

#### Neutral
- `LogsDownloadOptions`, `StdinLogsOptions`, and related orchestration types carry a `CachedJSONL` field through multiple entry points.
- Report aggregation now has a separate path for accumulating totals from cached `RunData` values.
- The feature relies on previous JSON output remaining parseable and structurally compatible with the current `LogsData` schema.
- `gh aw json-schema logs-jsonl` describes both processed-run and workflow-run discovery items.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
