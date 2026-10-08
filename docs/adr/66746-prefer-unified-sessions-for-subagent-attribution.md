# ADR-66746: Prefer Structured Unified Sessions over Stdio Heuristics for Subagent Model Attribution

**Date**: 2026-10-08
**Status**: Proposed
**Deciders**: pelikhan

---

### Context

Copilot subagent model attribution in `gh aw audit`/`gh aw logs` was derived by scanning `agent-stdio.log` for `name(model)` pairs with a loose regular expression. That heuristic matched any parenthesised token anywhere on a line, so startup noise such as `[INFO] container(s)`, `len(d)`, or printed source like `class RoutingProfile(StrictModel)` was reported as a subagent dispatch, while current CLI dispatch formats (notably `Name (model: model)`) were missed entirely. Meanwhile the Copilot CLI already emits structured lifecycle evidence — `subagent.started`, `subagent.configured`, `subagent.completed`, and a `session.shutdown.agentMetrics` accounting snapshot — that the unified session pipeline was discarding. Attribution therefore disagreed with the engine's own accounting, and per-agent request counts could not be reconciled with run cost. The fix must remain backward compatible with runs whose artifacts contain only `agent-stdio.log`. Addresses the session-source and persistence portion of #66740.

### Decision

We will treat structured unified sessions as the authoritative source for Copilot subagent model attribution, reading `usage/aw_session.jsonl` first and falling back to the canonical `agent-session.jsonl` trace, and only using legacy stdio inference when no structured conclusion evidence exists. To make this possible, the unified session projection preserves Copilot subagent identity, parentage, model, reasoning effort, execution mode, model-selection source, first dispatched model, and assistant request-correlation metadata (`apiCallId`, `interactionId`, `turnId`, `parentToolCallId`), and exposes `session.shutdown.data.agentMetrics` unchanged as `session.result.data.agentMetrics` — an exact per-agent snapshot that is deliberately *not* added into aggregate session usage. The primary driver is correctness: the engine already knows which agent ran on which model, so attribution should read that fact rather than re-derive it from prose. When structured evidence is present but malformed, we emit a warning and fall back; when a structured session legitimately has no subagents, heuristic inference is suppressed rather than allowed to invent agents. The legacy path is additionally narrowed to CLI dispatch-marker lines (`● Name(model)` and `● Name (model: model)`).

### Alternatives Considered

#### Alternative 1: Keep stdio parsing and only tighten the regular expression

Retain `agent-stdio.log` as the single source and simply anchor the pattern to the `●` dispatch marker and add the `(model: ...)` layout. This was a close call — it is a far smaller change and is exactly the narrowing this PR also applies to the fallback path. It was rejected as the *primary* mechanism because stdio remains a human-readable rendering that the engine may reformat at any time, it carries no parentage, effort, execution mode, or per-agent token/credit accounting, and it can never be reconciled against the engine's own numbers. Tightening the regex removes false positives but still cannot produce correct per-agent actuals.

#### Alternative 2: Derive subagent actuals from whole-run usage or proxy request logs

Attribute subagent spend by subtracting main-agent usage from the run total, or by joining per-request proxy records to agent identity. Rejected for this change: whole-run subtraction silently misattributes router/classifier traffic to subagents (the tests explicitly assert that missing per-agent accounting yields *empty* actuals rather than a guess), and per-request proxy joins require a correlation key that is only now being persisted. Per-request proxy joins and routing-cost splits are deliberately left to follow-up work on #66740.

#### Alternative 3: Emit a precomputed subagent attribution artifact from the runtime

Have the workflow runtime write an aggregated subagent attribution file that the CLI renders directly. Rejected because it would require a compiler/runtime rollout before any data appears, would leave existing runs unreadable, and would freeze the summary shape at emission time — the same reasoning recorded in ADR-66645 for client-side routing aggregation.

### Consequences

#### Positive
- Attribution is grounded in engine-emitted lifecycle events, eliminating false "agents" synthesised from log noise and printed code.
- Per-agent request counts, token counts, and nano-AIU credits become available verbatim from `agentMetrics`, so subagent accounting can be reconciled against the engine rather than estimated.
- Subagent parentage, reasoning effort, execution mode, model-selection source, and request-correlation IDs are now persisted, unlocking the per-request joins and per-agent reporting planned for #66740.
- Older runs still work: the narrowed stdio fallback keeps the existing heuristic warning and now recognises both current dispatch formats.

#### Negative
- The unified session schema, TypeScript declarations, and generated JSON schemas gain new fields (`reasoningEffort`, `agentMetrics`, `UnifiedMessageData`), widening the public artifact contract that must be kept in sync with the Go/TS types.
- `agentMetrics` is typed as an opaque `JsonValue` passthrough, so engine-side shape changes will not be caught by validation and may surface only as downstream rendering bugs.
- Unified session artifacts grow: assistant messages now carry correlation metadata and the shutdown snapshot is retained in full.
- Two attribution code paths (structured and heuristic) must be maintained and tested in parallel for as long as legacy artifacts are supported.

#### Neutral
- `agentMetrics` is an authoritative snapshot, not additive usage; any consumer must be careful never to sum it into session totals.
- Private fields such as `agentDescription` are explicitly dropped from projected `subagent.started` events, so the unified artifact is narrower than the native one in that respect.
- Attribution semantics now live in the CLI (`pkg/cli/token_usage_subagent_session.go`) and are versioned with the CLI rather than with the run.
- The spec (`docs/src/content/docs/specs/unified-agent-session-specification.md`) and the generated schemas are updated in the same change, making schema regeneration part of the routine cycle for this feature.

---

*Proposed for maintainer review; acceptance is not implied by implementation.*
