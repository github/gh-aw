# ADR-60702: Support Wildcard Cached Logs Files

**Date**: 2026-09-13
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes the `gh aw logs` cache behavior in `pkg/cli/` so callers can pass a trailing wildcard cache prefix such as `logs-*` instead of a single JSONL file. The implementation now merges multiple matching cache shards, chooses a collision-resistant output shard name, and prunes wildcard source files that contain only out-of-range dated run records when a date filter is applied. The PR also adds a `--cached-logs` CLI alias, updates user-facing help text, and extends tests around wildcard resolution, shard ordering, and pruning. The architectural question is how the logs command should represent reusable cached run data when repeated collections produce multiple partial cache files over time.

### Decision

We will treat cached logs JSONL inputs as either a single file or a trailing-wildcard shard prefix, and in wildcard mode we will load all matching `.jsonl` shards as the starting cache while writing fresh results to a new uniquely named shard. We decided to sort matching shards deterministically, let newer shards override duplicate cached run and workflow-list records, and prune fully out-of-range dated shards after collection when a date range is requested. This approach was chosen because the PR evidence shows a need to reuse accumulated cache history safely without overwriting existing shards or keeping obviously stale wildcard cache files forever.

### Alternatives Considered

#### Alternative 1: Keep a single append-only cached JSONL file

The logs command could continue requiring one explicit cache file and append every new record into that same file. This was considered because it is the simplest mental model and avoids wildcard resolution, duplicate handling, and shard cleanup logic. It was not chosen because the PR adds unique shard output names and wildcard loading specifically to avoid collisions and let multiple cache fragments be reused together.

#### Alternative 2: Support arbitrary glob patterns for cache discovery

Another option would be to accept any glob expression for cache inputs rather than only a trailing prefix wildcard. This was considered because it would give users more flexibility in how they organize cache files. It was not chosen because the implementation intentionally rejects non-trailing wildcard patterns, which keeps discovery rules predictable and allows the writer to derive a safe output prefix for new shard creation.

#### Alternative 3: Merge wildcard sources and rewrite one consolidated cache file

The command could read several cache shards, combine them in memory, and then rewrite a single consolidated JSONL file as the new cache state. This was considered because it would leave users with one canonical cache artifact after each run. It was not chosen because the diff explicitly preserves existing shards, writes only newly downloaded data to a fresh file, and prunes only shards proven irrelevant to the requested date range.

### Consequences

#### Positive
- Users can reuse multiple cached logs shards in one invocation, which makes incremental log collection more resilient across repeated runs.
- New cache output files avoid name collisions and preserve prior cache artifacts instead of overwriting them.
- Date-range pruning removes wildcard shards that contain only out-of-range dated run records, reducing stale cache buildup.

#### Negative
- Cache handling becomes more complex because the command now resolves wildcard prefixes, merges shards, sorts them, and warns on duplicate records.
- Duplicate cached runs or workflow-list payloads are resolved by last-wins behavior, which can hide older conflicting data behind warning messages.
- Wildcard pruning relies on record structure and timestamps, so unusual or metadata-only files are intentionally preserved and may still accumulate.

#### Neutral
- The CLI surface grows by one alias, `--cached-logs`, while preserving `--cached-jsonl` compatibility.
- The implementation extends existing JSONL cache mechanisms rather than introducing a new cache format or storage backend.
- Additional tests now codify shard naming, wildcard validation, deterministic ordering, and date-range cleanup behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
