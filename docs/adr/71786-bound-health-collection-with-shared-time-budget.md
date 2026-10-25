# ADR-71786: Bound Health Collection with a Shared Time Budget and Partial Results

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify reviewers]

---

### Context

`gh aw health` collects evidence across every active workflow in a repository by making serial GitHub API calls, local Git reads, cache reads, and work-queue ledger reads. Each individual request had its own deadline (10s/30s `context.WithTimeout` rooted in `context.Background()`), but there was no overall budget, and JSON output was only emitted on completion. On a repository with 356 active workflows the command could run for many minutes and looked hung, even though debug logging showed it was progressing. The health command is also exposed as an MCP tool, where unbounded runtime is especially costly for calling agents.

### Decision

We will give health collection a single, shared deadline (`--timeout`, default `2m`, `0` disables it, negative values rejected), implemented as a parent `context.Context` stored on `HealthCollection` and threaded into quota checks, `gh api` subprocesses, local Git revision reads, and work-queue ledger reads. Per-request timeouts are now derived from this parent context rather than `context.Background()`. When the budget expires we will not fail the command; instead we retain already-collected runs and candidate counts and emit valid JSON marked `collection.partial=true`, `reason=time_budget`, with `elapsed_seconds`, `timeout_seconds`, a `time_budget` capability, and an actionable warning. The same duration option is exposed through the health MCP tool, and privacy-safe request progress/timing logs (endpoint path only, no bodies or headers) are added under `DEBUG=cli:health`.

### Alternatives Considered

#### Alternative 1: Keep only per-request timeouts (status quo)

Each API/Git call already had a bounded deadline, so no single call could hang forever. This was rejected because total runtime still scales linearly with the number of workflows and pages, so large repositories can take many minutes with no output, which is the reported problem.

#### Alternative 2: Fail hard (non-zero exit / error) when an overall deadline expires

A global timeout that aborts with an error is simpler and avoids any risk of a consumer mistaking incomplete data for a full report. It was rejected because it discards all evidence gathered so far (e.g., failure candidates already found), which is the most useful output for triage; returning explicitly labeled partial results preserves that value while still signalling incompleteness.

#### Alternative 3: Parallelize or stream collection instead of bounding it

Concurrent API requests or incremental/streamed JSON output would reduce or hide latency. This was not chosen in this PR [TODO: verify rationale] — it would increase rate-limit pressure and change the single-document JSON contract, whereas a time budget is a smaller, contract-compatible change.

### Consequences

#### Positive
- `gh aw health` and the health MCP tool return within a predictable time by default (~2 minutes), instead of appearing hung on large repositories.
- Evidence collected before expiry is retained, including runs accepted earlier in a page whose later entry timed out.
- Consumers can detect incompleteness programmatically via `collection.partial`, `reason`, and the `time_budget` capability.
- `DEBUG=cli:health` gives per-request progress and timing for diagnosing slow collections without logging response bodies.

#### Negative
- The default behavior changes: runs that previously completed after more than two minutes now return partial results unless `--timeout` is raised or set to `0`.
- Partial reports do not establish fleet health; consumers that ignore the `partial` flag may draw wrong conclusions.
- The deadline is not strictly enforced end to end: an in-progress local cache read and report rendering may finish after the collection deadline.
- Storing a `context.Context` on the `HealthCollection` struct and checking the budget at many call sites adds coupling and new code paths to maintain and test.

#### Neutral
- `RunHealth` now delegates to a context-aware `runHealth(ctx, config)`, and local Git helpers gain a `runHealthGitContext` variant.
- The JSON schema gains `timeout_seconds` (omitted when unbounded) and `elapsed_seconds`; `ReportSchemaVersion` remains `1`.
- A user cancellation of the parent context is reported as `reason=canceled`, distinct from `time_budget`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
