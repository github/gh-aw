# ADR-71886: Stage-Aware Partial Results for MCP Tool Timeouts

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: Not formally recorded; pending maintainer review of PR #71886.

---

### Context

The `gh aw` MCP server exposes `compile` and `audit` tools that run CLI subprocesses under a default 60-second RPC deadline imposed by the MCP client. When a call exhausted that deadline, callers received either nothing or a generic failure, with no indication of which stage (waiting for a subprocess slot, Docker image preparation, compilation, audit analysis) consumed the time and no usable partial output. The `audit` tool already reserved response time with its own deadline helper, but `compile` did not, and the two tools reported timeouts inconsistently. Reports in #71845 also suggested that diagnostics might leak between concurrent calls, although this was not reproduced.

### Decision

We will reserve response time inside the existing 60-second deadline for both `compile` and `audit` through a shared helper (`mcpToolResponseDeadline`: 55s default, 5s margin before any inherited context deadline), and wrap stage-specific failures in a typed `mcpStageError`. On timeout, both tools return a tool error envelope with `partial: true`, `complete: false`, a named `stage`, and any available structured `partial_results` (plus bounded per-call `diagnostics` for compile and recovery `suggestions` for audit), instead of a successful-looking or opaque result. Subprocesses set `WaitDelay` so inherited stdout/stderr pipes cannot hold the response past the deadline. The 60-second deadline itself remains unchanged because representative dry-runs showed no evidence that a longer deadline was needed.

### Alternatives Considered

#### Alternative 1: Increase the MCP tool deadline

Raise or remove the 60-second limit so that slow compile/audit runs can finish. This was considered because it addresses timeouts directly, but the deadline is set by MCP clients rather than by the server, representative strict dry-runs completed in under a second, and a longer deadline would still provide no stage diagnostics when it was eventually exceeded.

#### Alternative 2: Return a generic timeout error without partial results

Keep a single "timed out" error string (as `audit` previously did) and discard collected output. This is simpler and avoids defining a partial-result contract, but it gives agents no actionable information about where time was spent and throws away validation results and diagnostics that were already produced.

#### Alternative 3: Per-tool, ad hoc deadline and timeout handling

Copy the existing audit-specific deadline logic into the compile handler. This avoids a new shared abstraction, but duplicates logic and lets the two tools' timeout envelopes drift apart; the PR instead consolidates audit's constants and helper onto the shared implementation.

### Consequences

#### Positive
- Callers can distinguish timeout stages (`subprocess_slot`, `docker_preparation`, `compilation`, `audit`, or a CLI-reported audit stage) and choose a targeted recovery.
- Partial structured results and bounded diagnostics are preserved instead of being lost on timeout.
- `compile` and `audit` share one deadline policy and a consistent `partial`/`complete`/`stage` envelope shape.
- `WaitDelay` bounds hangs caused by child processes holding inherited pipes open.

#### Negative
- Usable work time per call is reduced by the reserved 5-second response margin.
- The timeout envelope is a new response contract (fields `stage`, `partial_results`, `diagnostics`) that clients and documentation must track; compile timeouts no longer return the usual validation array.
- Stage classification depends on wrapping errors at every subprocess boundary; new stages must remember to use `mcpStageError` or will fall back to a generic stage name.

#### Neutral
- The default 60-second RPC deadline is intentionally left unchanged.
- Per-subprocess diagnostic buffers were retained; concurrency leakage was not reproduced, and regression tests for overlapping compile/logs calls were added rather than a new isolation mechanism.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
