# ADR-77304: Stream JSONL Output from the Logs Command

**Date**: 2026-11-13
**Status**: Accepted

---

## Context

`gh aw logs` collects runs from three entry points: workflow discovery, multiple workflow targets, and `--stdin` run IDs or URLs. Its only machine-readable output, `--json`, is written after collection and cross-run analysis finish, so CLI consumers cannot show progress or process individual runs during a long collection. Collection is concurrent across targets and download workers, so any streamed output must be serialized, and stdout must stay parseable because human diagnostics go to stderr. A failed write to a consumer's pipe should also stop collection instead of downloading runs that no one receives. The `--cached-jsonl` file already appends some records as they are processed, but it is a schema-versioned reuse cache, not a consumer-facing event stream.

## Decision

Use `gh aw logs --format jsonl` as the sole selector for streaming JSON Lines output; there is no standalone `--jsonl` flag. Reject combinations with `--json` or `--report-file`, while retaining compatibility with `--cached-jsonl`. Existing output modes remain unchanged.

The discovery, multi-target, and stdin collection paths share a stream that writes one JSON object per line to stdout. Events use a `type` discriminator:

| Type | Meaning |
| --- | --- |
| `progress` | Collection starts, runs are discovered, or downloads finish, fail, or are skipped. |
| `run` | An accepted run is available for incremental consumption. |
| `summary` | The final report, using the same `LogsData` representation and verbosity rules as `--json`. |
| `warning` | A recoverable collection issue, including a skipped workflow target. |
| `error` | Collection terminates with an error. |
| `complete` | Collection terminates successfully. |

Emit events during collection rather than waiting for the entire batch to finish. Serialize concurrent producers through a mutex-protected JSON encoder so their objects cannot interleave within a line. Preserve the first write error, cancel the shared collection context, and return the write error to the caller instead of continuing collection for an unreadable stream.

Incremental `run` events are provisional: cross-run analysis and live outcomes are available only in the authoritative final `summary`. Concurrent targets can interleave events; the stream does not promise a global run order. Human diagnostics remain on stderr.

Once collection starts, attempt a terminal `complete` or `error` event when it returns. Successful empty collections also produce a summary. Failed collections are not guaranteed to produce a summary, and a summary alone does not establish success: later finalization can still fail. Validation failures before stream initialization, process interruption, and output write failures can prevent terminal delivery; consumers must also check the command's exit status.

## Alternatives Considered

The following are design alternatives and tradeoffs, not a record of historical proposals.

#### Alternative 1: Add a standalone `--jsonl` flag

A dedicated flag could improve discoverability, but would add another output selector alongside `--format` and the existing `--json` flag. Using `--format jsonl` extends the existing format selection mechanism without adding another boolean mode.

#### Alternative 2: Tail the `--cached-jsonl` file

The cache file already receives records as they are processed, so consumers could read it during collection. However, it is a schema-versioned reuse format, not a progress and completion protocol. Reusing it as the public stdout contract would couple interactive consumers to cache internals and persistence behavior.

#### Alternative 3: Emit only the final `--json` report

Keeping final-report-only output avoids a new event contract and concurrency rules, but consumers must wait for full collection before receiving any run data. It does not satisfy the incremental-consumption requirement.

## Consequences

### Benefits

Consumers can display progress and process accepted runs before collection finishes, while reusing existing final-report types for the summary. Streaming is opt-in and separate from the reuse cache. Write-error cancellation limits unnecessary work when the downstream consumer stops reading.

### Costs and limitations

The event contract adds a public compatibility surface without a schema-version field. Consumers should tolerate unknown event types and additional fields. The `logs-jsonl` JSON schema describes cached records, not this stdout event stream.

Run data can appear both incrementally and in the summary, so consumers must reconcile provisional records rather than count both as distinct results. The stream does not replace the in-memory collection needed for final analysis or guarantee lower memory use.

Serialized writes introduce backpressure from slow consumers and require all collection entry points to share stream initialization, cancellation, and finalization. Cancellation is cooperative; already-running operations may take time to stop.

Consumers must parse stdout separately from stderr, allow cross-target interleaving, and handle incomplete streams. A `complete` event reports successful command collection, not that every workflow run succeeded or every target was collected without warnings.
