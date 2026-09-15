# ADR-60951: Track gh aw logs download telemetry

**Date**: 2026-09-15
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes the `gh aw logs` pipeline to capture artifact download duration and downloaded size for each workflow run, persist those values into cached JSONL/log schemas, and print an end-of-run download summary across single-target, multi-target, and stdin-driven entry points. The PR description explains that the command previously did not show how long artifact downloads took or how much data they transferred, which made slow runs and GitHub API usage harder to diagnose. The implementation also derives an approximate per-run GitHub API cost from existing rate-limit reports rather than introducing a separate telemetry source. The architectural question is whether `gh aw logs` should treat download-performance telemetry as first-class run metadata and surface it consistently through its reporting and cache formats.

### Decision

We will record per-run artifact download duration and artifact size inside the `gh aw logs` workflow-run model, propagate those fields into persisted `RunData`/schema outputs, and render an aggregate end-of-run summary for real downloads. We will measure download duration around the existing artifact-download path, compute size from the downloaded artifact directory, and exclude cache hits from the aggregate timing and size metrics so the summary reflects actual transfer work performed during the invocation. We will also estimate GitHub API cost per downloaded run from existing rate-limit snapshots instead of adding a new API accounting mechanism. We chose this because the PR evidence shows the main problem is lack of visibility into download cost, and extending the current logs/reporting pipeline solves that with minimal new architecture.

### Alternatives Considered

#### Alternative 1: Keep download telemetry out of the run model and rely on ad hoc debug logging

The team could have added temporary or verbose-only log lines around artifact downloads without changing `WorkflowRun`, `RunData`, or the JSON schemas. This was considered because it would be a smaller code change and avoid widening cached output contracts. It was not chosen because the PR explicitly updates cached JSONL persistence and schemas, indicating the telemetry needs to survive beyond a single terminal session and be available for downstream analysis.

#### Alternative 2: Report only aggregate command-level download timing

Another option was to compute one total download duration and size for the full command without attaching telemetry to each run. This was considered because it would still improve operator visibility while avoiding new per-run fields. It was not chosen because the PR adds `DownloadDuration` and `DownloadSizeBytes` directly to `WorkflowRun` and `RunData`, showing the intended design is per-run observability that can be aggregated later and reused from cache.

### Consequences

#### Positive
- `gh aw logs` gains concrete visibility into artifact transfer cost, making slow or heavy runs easier to diagnose.
- The telemetry is available both in terminal summaries and persisted JSON/JSONL outputs, enabling later analysis and cache round-tripping.
- Reusing existing rate-limit snapshots provides a lightweight API-cost estimate without introducing a separate tracking subsystem.

#### Negative
- The run/report model and published schemas gain additional fields that maintainers must preserve or evolve carefully.
- Size measurement adds extra filesystem work after downloads and may produce partial observability when directory sizing fails.
- The API cost figure is only an estimate and can understate true usage during rate-limit window resets.

#### Neutral
- Cache hits continue to produce zero download metrics and are intentionally excluded from aggregate download summaries.
- The implementation factors artifact downloading into a helper function to isolate timing and sizing behavior from the rest of run processing.
- All three logs entry points now call the same end-of-run summary renderer, increasing consistency across invocation modes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
