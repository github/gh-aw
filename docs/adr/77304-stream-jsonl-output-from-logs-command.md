# ADR-77304: Stream JSONL Output from the Logs Command

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers [TODO: verify]

---

### Context

`gh aw logs` collects runs from three entry points: workflow discovery, multiple workflow targets, and `--stdin` run IDs or URLs. Its only machine-readable output, `--json`, is written after collection and cross-run analysis finish, so CLI consumers cannot show progress or process individual runs during a long collection. Collection is concurrent across targets and download workers, so any streamed output must be serialized, and stdout must stay parseable because human diagnostics go to stderr. A failed write to a consumer's pipe should also stop collection instead of downloading runs that no one receives. The `--cached-jsonl` file already appends some records as they are processed, but it is a schema-versioned reuse cache, not a consumer-facing event stream.

### Decision

We will add `jsonl` as a value of the existing `--format` flag, not as a new flag. When it is set, `gh aw logs` writes typed JSON Lines events to stdout without buffering: `progress`, `run`, `warning`, and `summary` events, followed by a terminal `complete` or `error` event. The `summary` event carries the same final report as `--json` and is authoritative. The primary driver is letting consumers act on progress and run data during collection while leaving the existing output modes unchanged.

### Alternatives Considered

#### Alternative 1: Add a standalone `--jsonl` flag

A dedicated flag reads more naturally and makes the streaming mode easier to discover. It was considered but not chosen because it adds a second output selector that must be checked against `--format`, `--json`, and `--report-file`. The PR keeps one selector and rejects conflicting combinations during validation, and its tests assert that no `--jsonl` flag exists. This was a close call, since discoverability favors the dedicated flag.

#### Alternative 2: Tail the `--cached-jsonl` file

The cache file already receives run records as they are processed, so consumers could read it while collection runs without any new output path. It was not chosen because the cache is a reuse format with different semantics. It contains discovery payloads, `safe_output_item` lines, and rate-limit reports, has no progress or terminal events, and does not report errors or warnings. Making it a stdout contract would couple consumers to cache internals.

#### Alternative 3: Emit only the final `--json` report

Keeping the status quo has the smallest surface and avoids any ordering or interleaving rules. It was not chosen because consumers would still wait for the full collection before seeing any run data, which is the problem this change addresses.

### Consequences

#### Positive
- Consumers can display progress and parse each processed run as soon as it is accepted. A test that blocks a second download confirms that the first `run` event arrives before the batch finishes.
- Existing `--json`, table, Markdown, and cache behavior is unchanged, and the new mode is opt-in.
- A write failure, such as a closed pipe, cancels collection, so the CLI does not keep downloading artifacts that nobody can read.
- The final `summary` uses the same report as `--json`, so consumers that already parse `--json` can reuse their types.

#### Negative
- The event contract is a new public compatibility surface. Events have no schema version field, unlike cached JSONL records, so changes must be additive, and consumers must ignore unknown `type` values.
- Streamed `run` records are partial: they omit cross-run analysis and live outcomes. Consumers must wait for `summary` for authoritative aggregates, and run data appears twice in the stream.
- Concurrent targets interleave events. Only per-target discovery order is preserved, so consumers that assume a global order will break.
- An interrupted process or broken pipe may end without a terminal event, so consumers must treat end-of-stream without `complete` or `error` as a failure.
- The event shapes are documented in prose only. `gh aw json-schema` does not generate a schema for the stdout stream; `logs-jsonl` covers cached items only.
- The streaming state is carried through `context` and optional fields on `LogsDownloadOptions` and `renderLogsOutputOptions`. Each entry point must call `startLogsJSONL` and `finish`, and any new entry point must follow the same pattern.

#### Neutral
- Human diagnostics stay on stderr, so consumers should parse stdout alone and not merge the two streams.
- Empty results still emit `summary` and `complete`, so consumers can rely on a terminal event for empty results and errors.
- Non-`jsonl` paths use nil-receiver no-op methods, so they pay only a nil check.
- Failed multi-target workflows emit a `warning` event as well as the existing stderr warning.
- The CLI reference (`docs/src/content/docs/setup/cli.md`) gains an event table, and the `--format` help text and examples list `jsonl`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
