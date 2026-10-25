# ADR-71886: Stage-Aware Partial Results for MCP Tool Timeouts

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: Not formally recorded; pending maintainer review of PR #71886.

---

### Context

The `gh aw` MCP server exposes `compile` and `audit` tools that run CLI subprocesses under caller-provided deadlines. When a call exhausted that deadline, callers received either nothing or a generic failure, with no indication of which stage (waiting for a subprocess slot, Docker image preparation, compilation, audit analysis) consumed the time and no usable partial output. The `audit` tool already reserved response time with its own deadline helper, but `compile` did not, and the two tools reported timeouts inconsistently. Reports in #71845 also suggested that diagnostics might leak between concurrent calls, although this was not reproduced.

### Decision

Both tools reserve five seconds before an inherited caller deadline, or use a 55-second budget when no deadline is supplied. Stage-specific failures are wrapped in a typed `mcpStageError`. On timeout, both tools return a tool error envelope with `partial: true`, `complete: false`, a named `stage`, and available structured `partial_results`. Compile writes atomic per-workflow progress snapshots so results completed before later stages stall can be returned; raw subprocess diagnostics are withheld from timeout envelopes because they may contain sensitive or untrusted output. Docker probes use the request deadline, while started image pulls continue under a separate five-minute bound. Audit treats `exec.ErrWaitDelay` as a pipe-drain timeout and preserves any decoded report. Subprocesses set `WaitDelay` so inherited stdout/stderr pipes cannot hold the response indefinitely.

### Alternatives Considered

#### Alternative 1: Increase the MCP tool deadline

Raise or remove the default response budget so that slow compile/audit runs can finish. Instead, a caller-provided deadline is honored with only a five-second response margin; the 55-second fallback applies only when no caller deadline exists.

#### Alternative 2: Return a generic timeout error without partial results

Keep a single "timed out" error string and discard collected output. This is simpler, but gives agents no indication of where time was spent and throws away completed validation results and decoded audit reports.

#### Alternative 3: Per-tool, ad hoc deadline and timeout handling

Copy the existing audit-specific deadline logic into the compile handler. This avoids a new shared abstraction, but duplicates logic and lets the two tools' timeout envelopes drift apart; the PR instead consolidates audit's constants and helper onto the shared implementation.

### Consequences

#### Positive
- Callers can distinguish timeout stages (`subprocess_slot`, `docker_preparation`, `compilation`, `audit`, or a CLI-reported audit stage) and choose a targeted recovery.
- Completed compile results and decoded audit reports are preserved when available.
- Timeout envelopes do not expose potentially sensitive or instruction-bearing subprocess diagnostics.
- `compile` and `audit` share one deadline policy and a consistent `partial`/`complete`/`stage` envelope shape.
- `WaitDelay` bounds hangs caused by child processes holding inherited pipes open.

#### Negative
- Usable work time per call is reduced by the reserved 5-second response margin.
- The timeout envelope is a new response contract (fields `stage`, `partial_results`, and safe diagnostic summaries) that clients and documentation must track; compile timeouts no longer return the usual validation array.
- Stage classification depends on wrapping errors at every subprocess boundary; new stages must remember to use `mcpStageError` or will fall back to a generic stage name.

#### Neutral
- The default 60-second RPC deadline is intentionally left unchanged.
- Per-subprocess diagnostic buffers were retained; concurrency leakage was not reproduced, and regression tests for overlapping compile/logs calls were added rather than a new isolation mechanism.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
