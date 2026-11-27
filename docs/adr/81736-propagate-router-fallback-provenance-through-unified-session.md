# ADR-81736: Propagate Router Fallback Selection Provenance Through the Unified Session

**Date**: 2026-11-27
**Status**: Draft
**Deciders**: Not stated in PR evidence (PR author: rabeckett)
**Related**: [PR #81736](https://github.com/github/gh-aw/pull/81736), [Issue #81717](https://github.com/github/gh-aw/issues/81717) (closing reference in PR body), [Issue #80508](https://github.com/github/gh-aw/issues/80508) (parent), AWF [github/gh-aw-firewall#12035](https://github.com/github/gh-aw-firewall/issues/12035)

## Context

AWF selects the model for a routed run through a router. When the router cannot choose or is unhealthy, AWF falls back to an eligible model through a fallback walk. The AWF selection record now carries three fields: `selection_source` (`"router"` or `"fallback"`), `fallback_reason` (the routing failure code, or `null`), and `fallback_tier` (`"standard"`, `"premium"`, `"unclassified"`, `"lightweight"`, `"none"`, or `null`). In a fallback selection, `router` and `ranked_choices` are also `null`.

Before this PR, gh-aw dropped those three fields when it built `usage/aw_session.jsonl`. As a result, `gh aw audit`, `gh aw logs`, and the routing smoke assertions could not tell whether a run's model came from the router or from the fallback walk. Issue #81717 notes that if the router silently stopped choosing, runs would still succeed through fallback and no one reading audit or logs would notice.

Constraints from the linked issue and the PR:

- All fallback provenance read by audit, logs, and smoke checks must come from `usage/aw_session.jsonl`. It must not come from engine-native files or side logs such as `model-routing.jsonl`.
- Runs from older AWF releases omit these fields. They must keep parsing and displaying with no behavior change.
- The PR does not bump the default AWF version or change `model: auto` compilation. That follows once the new AWF release is published.

## Decision

Carry the three AWF selection-provenance fields through the unified session as the only read path, and make every consumer read them from there:

1. **Session ingestion.** `actions/setup/js/unified_session_payload.cjs` projects `selectionSource`, `fallbackReason`, and `fallbackTier` into both `firewall.model_routing` selection events and `workflow.info.modelRouting`. Invalid values are dropped. Explicit `null` values for `fallbackReason`, `fallbackTier`, and `router` are preserved, and `rankedChoices: null` is omitted. `model_attribution.cjs` (`recordModelRouting`) and `parse_token_usage.cjs` carry the same fields through runner metadata.
2. **Schemas.** `docs/public/schemas/unified-session.schema.json`, `schemas/audit.schema.json`, `schemas/logs.schema.json`, and `schemas/logs-jsonl.schema.json` are regenerated. The enum values are constrained, and the fields stay optional so that older runs still validate.
3. **CLI.** The Go modules under `pkg/cli/` (`model_routing_session.go`, `model_routing.go`, `logs_models.go`, `logs_report.go`, `audit_comparison.go`, `audit_report_render.go`, `audit_cross_run_render.go`) read provenance only from `usage/aw_session.jsonl`.
   - `gh aw audit` keeps router picks concise and shows `selection_source`, `fallback_reason`, and `fallback_tier` only for fallback picks. The JSON summary exposes all three fields.
   - `gh aw logs` keeps fallback buckets separate from router-picked runs. Omitted (pre-fallback AWF) and explicit `"router"` sources are grouped the same way.
4. **Shared smoke assertions.** `actions/setup/js/smoke_model_routing_assertions.cjs` accepts `expectedSelectionSource`, `expectedFallbackReason`, `expectedFallbackTier`, and `expectedClassifierRequests`.
   - When `expectedSelectionSource` is `"router"` or `"fallback"`, `selectionSource` must be present and must match.
   - When it is omitted, an absent field is allowed, but any present value other than `"router"` fails.
   - A classifier count other than 1 is allowed only when `expectedSelectionSource` is `"fallback"`. The default stays at 1 for router-picked smokes.
5. **Fixtures and docs.** A golden fixture is added at `pkg/cli/testdata/model_routing_golden/copilot-routed-fallback-no-route/`, `logs_expected.json` is updated, and the unified session specification and audit reference document the fields.

## Alternatives Considered

These alternatives are inferred from the issue text and the PR diff. The PR does not record a separate design discussion.

1. **Read fallback provenance from the AWF side log** (`sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl`) in the CLI and smoke checks. Rejected. The issue requires `usage/aw_session.jsonl` to be the only source. The PR adds a test in which a contradictory side log is ignored, which shows the two sources can disagree.
2. **Make the three fields required on every selection record** and reject records that omit them. Rejected. Runs from older AWF releases would stop parsing, and the issue requires them to keep working. The chosen design keeps the fields optional in the schema and makes absence an error only when a caller explicitly asks for a source.
3. **Infer fallback from `router: null` or `ranked_choices: null`** instead of carrying explicit fields. Rejected. Inference cannot recover `fallback_reason` or `fallback_tier`. It also cannot tell a pre-fallback AWF record from a fallback record that lost its explicit field, which is the silent failure this change is meant to expose.

## Consequences

**Positive**

- Fallback runs are visible in `gh aw audit` and `gh aw logs`, and they are separated from router picks. A router that silently stops choosing no longer hides behind successful fallback runs.
- Routed smoke tests can require router selection explicitly. A dropped `selectionSource` fails loudly instead of looking like an older AWF release.
- A single read path (`usage/aw_session.jsonl`) removes the chance that the CLI and the side logs disagree.
- Runs from older AWF releases parse and display as before.

**Negative**

- Four schemas and the golden fixtures change together. Any future change to these fields requires regenerating all of them and keeping `logs_expected.json` in sync.
- The compatibility rules (omitted, explicit `"router"`, and `null` are treated differently in different places) must be maintained until pre-fallback AWF releases are no longer relevant.
- The unified session is now the only source of truth. If a projection drops these fields again, runs fail silently unless a smoke test explicitly requests the expected source. Issue #81717 describes the projection gap this PR fixes, which shows that this risk is real.
- The smoke assertion surface gains four expectation keys. The rule that `expectedClassifierRequests` other than 1 is valid only for fallback smokes is easy for callers to misread.
