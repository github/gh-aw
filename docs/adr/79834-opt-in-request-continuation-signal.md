# ADR-79834: Record Remaining Work via Opt-In Request-Continuation Signal

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify other reviewers]

---

### Context

Agentic workflows run under budgets such as turn limits, safe-output `max` counts, and engine continuation settings. An agent sometimes finishes its useful work and still sees more remaining, but there is no structured way to say so. The existing signals do not fit: `noop` means no work remains, `report_incomplete` means a blocking failure, and Copilot autopilot (`engine.max-continuations`) is engine-specific. Operators need to count and attribute these "more work remains" requests in `audit`, `logs`, and `health` without mistaking them for reruns or completions.

### Decision

We will add an opt-in `request_continuation` safe output (`safe-outputs.request-continuation`, default `max: 1`). It records a bounded, nonempty `message` describing the remaining work. The signal is advisory metadata only: it does not dispatch workflows, resume engine sessions, or prove task completion. Its counts and provenance are surfaced in `audit`, `logs`, and `health`, and missing cached evidence is reported as unavailable rather than as zero. The primary driver is observability and cost control: continuation intent is kept separate from completion semantics.

### Alternatives Considered

#### Alternative 1: Automatic re-run or re-dispatch on request

Have the agent's continuation request trigger another run or resume the session automatically. This was considered because it would finish the remaining work without human involvement. It was rejected because it creates unbounded cost and loop risk, removes any checkpoint for a human to review, and conflates intent with completion. It would also overlap with the Copilot autopilot and durable-session behavior that this PR leaves unchanged.

#### Alternative 2: Reuse `noop` or `report_incomplete`

This was the closest alternative, since it adds no new safe-output type. It was rejected because `noop` is a terminal outcome meaning no work remains, and `report_incomplete` signals a blocking failure. Overloading either would distort their analytics and their terminal-outcome semantics.

#### Alternative 3: Parse free-text intent from the final message or logs

Infer continuation intent by scanning the agent's final message or logs. This was rejected because it provides no structured provenance (run, workflow, experiment variant), is brittle across engines, and would be hard to bound.

### Consequences

#### Positive
- A structured, bounded, opt-in signal with provenance (run, workflow, and experiment variant) that is engine-agnostic because it uses the safe-output path.
- Analytics separate requested work from completion, and `health` reports unavailable evidence separately from zero requests.
- Requests are excluded from GitHub mutation manifests and activity notifications, like `noop`, so they add no GitHub side effects.

#### Negative
- The new safe-output type spans the compiler, schemas, handlers, tool definitions, documentation, and lock files, which gives it a wide blast radius (76 files changed).
- Requests are advisory and nothing acts on them automatically, so they will accumulate unused unless a consumer is added.
- Analytics depend on cached evidence, so older or uncached runs report "unavailable" until cache backfill covers them.
- It adds code to already large logs, audit, and health modules (for example `logs_report.go`, `audit_report_types.go`, and `logs_models.go`).

#### Neutral
- The feature is enabled in Package Specification Extractor, Contribution Check, and Matt Pocock Skills Reviewer, and their `.lock.yml` files are recompiled.
- Existing Copilot autopilot (`engine.max-continuations`) and durable-session behavior is unchanged.
- The handler logs a fixed string and does not echo agent content into Actions logs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
