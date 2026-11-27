# ADR-81744: Preserve routing verdict evidence and adaptive receipts in agent-output-fallback

**Status:** Draft
**Date:** 2026-11-27
**PR:** [#81744](https://github.com/github/gh-aw/pull/81744)
**Linked issue:** #81725 (related: #79102)

## Context

When a routed workflow's large `agent` artifact upload times out or fails, the `conclusion` job recovers from the small `agent-output-fallback` artifact. Before this PR, that fallback carried safe-output and accounting files (`token-usage.jsonl`, `model-routing.jsonl`) but not the evidence needed to classify a routed run's `routing.observation` verdict. Three gaps followed:

- Workflows that opt into `model-routing.learning` without explicit `safe-outputs:` (or with only auto-injected issue creation) did not emit `agent-output-fallback`, so no recovery path existed.
- The fallback omitted the firewall's upstream HTTP error status (for example 429 and 503) and the main-agent `assistant.refusal` signal. Without them, the conclusion collector cannot tell provider rate limits, 5xx outages, and refusals apart.
- Live adaptive routing writes selection receipts to `adaptive-selection.jsonl`. Omitting that file turns a recovered live run into `receipt_missing` instead of counting it under its bound receipt.

The PR body's scenario table shows the effect on fallback-only recovery:

| Scenario | Full `agent` artifact | Fallback-only, before |
| --- | --- | --- |
| `report_incomplete` with an AWF-only HTTP 429 | `skip` (`rate_limited`) | `failure` (charged to the model) |
| Failed agent, no terminal output, main-agent `assistant.refusal` | `failure` | `skip` (`unknown_evidence`) |
| Live adaptive run with a bound receipt | Counted under the bound receipt | `receipt_missing` |

The linked issue #81725 describes the problem and the proposed behavior. It does not contain alternatives or consequences, so this ADR draws those from PR evidence.

The gate trigger is the diff itself: 125 added lines under `pkg/` (five files in `pkg/workflow/`), above the 100-line default threshold. The PR does not carry the `implementation` label.

## Decision

Use `agent-output-fallback` as the recovery channel for routing verdict evidence. Emit it whenever routing learning is enabled, not only when safe outputs exist, and include the minimal evidence the conclusion collector needs.

Concretely:

1. **Emission gate.** `generateAgentOutputFallbackUpload` in `pkg/workflow/compiler_yaml_artifacts.go` emits the upload when `data.RoutingLearningMode` is set, in addition to the existing safe-output condition.
2. **Added recovery files, in all three firewall log layouts (including ARC/DinD path rewriting):**
   - `adaptive-selection.jsonl`, beside `model-routing.jsonl` and `token-usage.jsonl`, when present.
   - `agent/upstream-errors-fallback.jsonl`: allowlisted lines carrying only `event`, `request_id`, and `status`. It is a separate file so a merged download cannot overwrite the full `upstream-errors.jsonl`.
   - `agent/routing-refusal.json`: content-free `{"refused":true}`, written only for top-level `assistant.refusal` events.
   - `agent-errors.jsonl`: classified `agent.execution` categories and codes, produced before secret redaction.
3. **Validation.** `actions/setup/js/validate_recovery_envelopes.cjs` checks canonical allowlisted bytes and rejects directories, symlinks, or tampered payloads at fallback paths before upload. Secret redaction and recovery-envelope validation remain required gates.

Why: the conclusion job should reach the same `routing.observation` verdict and `runs.jsonl` row whether it recovers from the full artifact or the fallback. The allowlist supplies the verdict inputs without headers, response bodies, or conversation content.

Evidence:

- `pkg/workflow/compiler_yaml_artifacts.go`: the early return changes from `SafeOutputs == nil || AutoInjectedCreateIssue` to also require `RoutingLearningMode == ""`. The fallback path list gains `agent-errors.jsonl`, `agent/routing-refusal.json`, `agent/upstream-errors-fallback.jsonl`, and `adaptive-selection.jsonl` entries for each layout.
- Test files changed: `pkg/workflow/compiler_artifacts_test.go` (+87), `pkg/workflow/routing_learning_test.go` (+13), `actions/setup/js/routing_observation.test.cjs` (+161), `actions/setup/js/push_routing_history.test.cjs` (+242), and `actions/setup/js/validate_recovery_envelopes.test.cjs` (+77).
- `actions/setup/js/validate_recovery_envelopes.cjs` (+182 / -1) carries the envelope validation logic.

## Alternatives Considered

The first two alternatives are rejected in the PR body or its scenario table. The third is inferred from the diff; the author did not record it.

1. **Keep fallback-only recovery as it was (status quo).** Rejected: the scenario table shows wrong verdicts for HTTP 429, main-agent refusal, and live receipts.
2. **Copy the full `upstream-errors.jsonl` and conversation-level refusal evidence into the fallback.** Rejected: the PR body requires that the fallback exclude headers and response bodies. The allowlisted record supplies the status and refusal signal without the diagnostic content.
3. **Add a separate routing-evidence artifact instead of extending `agent-output-fallback`.** Inferred, not recorded by the author. A new artifact would need its own upload, download, and validation path in `conclusion`. Extending the existing fallback reuses the redaction and envelope gates already applied to it.

## Consequences

**Positive**

- Fallback-only recovery yields the same `routing.observation` verdicts as full-artifact recovery for the HTTP 429, HTTP 503, main-agent refusal, and bound live receipt cases in the PR table.
- Opted-in adaptive workflows without explicit `safe-outputs:` gain a recovery path when the `agent` upload fails.
- The fallback contains no raw upstream headers, response bodies, or conversation content. The distinct file name prevents overwrites in merged downloads.

**Negative**

- More workflows upload `agent-output-fallback`: every workflow with `model-routing.learning`, not only those with safe outputs.
- The recovery allowlist is larger and spans three firewall log layouts, including ARC/DinD path rewriting. If the compiler path lists and `validate_recovery_envelopes.cjs` drift apart, evidence is dropped silently, so they must stay in sync.
- 329 files under `.github/workflows/` change in the diff, mostly from recompilation. Reviewers should confirm that the lock changes reflect only the new gating, as the PR body states.
