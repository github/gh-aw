# ADR-81744: Preserve routing verdict evidence and adaptive receipts in agent-output-fallback

**Status:** Accepted
**Date:** 2026-11-27
**Deciders:** rabeckett (PR author)
**PR:** [#81744](https://github.com/github/gh-aw/pull/81744)
**Linked issue:** #81725 (related: #79102)

## Context

When a routed workflow's large `agent` artifact upload times out or fails, the `conclusion` job recovers from the small `agent-output-fallback` artifact. Before this change, that fallback carried safe-output and accounting files (`token-usage.jsonl`, `model-routing.jsonl`) but omitted key evidence that `conclusion` uses to classify a routed run's `routing.observation` verdict:

- Workflows that opt into `model-routing.learning` without explicit `safe-outputs:` (or with only auto-injected issue creation) did not emit `agent-output-fallback`, leaving no recovery path when the full `agent` artifact failed to upload.
- The fallback omitted AWF's upstream HTTP error status evidence (for example HTTP 429 and 503), the main-agent `assistant.refusal` signal, and classified execution error categories (`agent-errors.jsonl`). Without them, fallback-only recovery could misclassify rate limits, provider outages, timeouts, and content refusals.
- Live adaptive routing writes selection receipts to `adaptive-selection.jsonl`. Omitting that file from the fallback manifest turns a recovered live run into `receipt_missing` instead of counting it under its bound receipt.

| Scenario | Full `agent` artifact | Fallback-only recovery before |
| --- | --- | --- |
| `report_incomplete` with an AWF-only HTTP 429 | `skip` (`rate_limited`) | `failure` (counted against the model) |
| Failed agent, no terminal output, AWF-only HTTP 503 | `skip` (`infrastructure`) | `skip` (`unknown_evidence`) |
| Failed agent, no terminal output, main-agent `assistant.refusal` | `failure` | `skip` (`unknown_evidence`) |
| Failed agent with `agentic_engine_timeout` in `agent-errors.jsonl` | `skip` (`infrastructure`) | `skip` (`unknown_evidence`) |
| Live adaptive run with a bound receipt | Receipt preserved for reducer | `receipt_missing` |

## Decision

Use `agent-output-fallback` as the recovery channel for routing verdict evidence and adaptive receipts, emitting it whenever `model-routing.learning` is enabled in addition to explicit safe-output workflows.

1. **Emission gate.** `generateAgentOutputFallbackUpload` in `pkg/workflow/compiler_yaml_artifacts.go` emits `agent-output-fallback` when `data.RoutingLearningMode` is set, keeping existing secret redaction and recovery-envelope validation gates.
2. **Allowlisted recovery files across all three firewall log layouts (including ARC/DinD path rewriting):**
   - `adaptive-selection.jsonl` beside `model-routing.jsonl` and `token-usage.jsonl` in each layout when present.
   - `agent/upstream-errors-fallback.jsonl`: allowlisted lines carrying only `event` (`UPSTREAM_ERROR_RESPONSE`), a T-UAS-082-bounded `request_id` (`^[A-Za-z0-9_.:-]{1,64}$`), and HTTP `status` (100-599). Kept distinct from `upstream-errors.jsonl` so merged downloads never overwrite or duplicate the full diagnostic, and ignored whenever `upstream-errors.jsonl` is present or protected session capture is active.
   - `agent/routing-refusal.json`: content-free `{"refused":true}`, written only for top-level `assistant.refusal` events (never nested subagent refusals) and read only when no agent conversation source is present.
   - `agent-errors.jsonl`: classified `agent.execution` categories and codes already written before secret redaction.
3. **Fail-closed pre-upload validation and cleanup.** `actions/setup/js/protected_command_output.cjs` shares the symlink-safe sidecar write/remove helpers and discards untrusted fallback files before protected capture. `actions/setup/js/validate_recovery_envelopes.cjs` validates canonical allowlisted bytes (requiring `O_NOFOLLOW` on POSIX and open-and-verify `lstat`/`fstat` identity checks on Windows) and removes directories, symlinks, or tampered payloads at those fallback paths before upload.

## Alternatives Considered

1. **Keep fallback-only recovery unchanged (status quo).** Rejected because fallback-only recovery flips routed verdicts for HTTP 429, HTTP 503, execution timeouts, and main-agent refusals, and drops live adaptive receipts.
2. **Upload raw `upstream-errors.jsonl` and full conversation logs in `agent-output-fallback`.** Rejected because `agent-output-fallback` must stay small and exclude sanitized upstream headers, response bodies, and conversation content, and must not overwrite full diagnostics in merged downloads.
3. **Create a separate routing-evidence artifact instead of extending `agent-output-fallback`.** Rejected because a new artifact would duplicate upload, download, ARC/DinD consolidation, redaction, and recovery-envelope gating already wired into `agent-output-fallback` and `conclusion`.

## Consequences

**Positive**

- Fallback-only recovery matches full-artifact `routing.observation` verdicts and observe-mode `runs.jsonl` rows across all three firewall log layouts, and preserves `adaptive-selection.jsonl` for live-mode reducer parity.
- Opted-in adaptive workflows without explicit `safe-outputs:` gain a reliable recovery path when the full `agent` artifact upload fails.
- No raw upstream headers, response bodies, or conversation prose enter `agent-output-fallback`, and merged downloads preserve the full `agent` artifact's primary sources without duplication.

**Negative**

- Workflows that opt into `model-routing.learning` without explicit `safe-outputs:` now emit the small `agent-output-fallback` upload step.
- The compiler's fallback path list, producers, collector, and `validate_recovery_envelopes.cjs` must remain aligned on the fallback file paths and canonical schemas.

---
*Reviewed by the PR author.*
