# ADR-66645: Reconstruct Model-Routing Observability Client-Side from `model-routing.jsonl`

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

AWF's model router already emits routing classifications, model selections, and per-request deviations, but `gh aw audit` and `gh aw logs` had no way to surface those decisions. Operators could see a run's total cost but could not tell which model was selected, why, or how much of the spend came from the classifier versus the agent's actual traffic (#66638). The routing evidence exists only as a per-run artifact (`api-proxy-logs/model-routing.jsonl`) alongside token-usage records, and older AWF releases (before v0.28.39) recorded endpoint-only differences as `deviated`, so historical runs are not directly comparable to new ones.

### Decision

We will reconstruct model-routing observability entirely on the client side in the CLI, by parsing `api-proxy-logs/model-routing.jsonl` from the run artifact and joining it to token-usage records on `request_id`. A new `pkg/cli/model_routing.go` builds a single `ModelRoutingSummary` (status, objective, labels, classifier, selected model/effort, ranked choices, router metadata, outcome counts, deviations, failures) plus three cost buckets — classifier, selected-model, and deviated traffic — that the audit text/JSON renderers, audit comparison, cross-run logs aggregation, and the unified session view all consume. Legacy endpoint-only deviations are normalized into selected-model traffic at parse time and flagged via `endpoint_only_deviation_normalized` so cross-version comparisons stay meaningful. The primary driver is that routing decisions are already fully recorded in the artifact: no new runtime emission, server, or schema-producing component is needed, only a reader and a shared summary type.

### Alternatives Considered

#### Alternative 1: Emit a precomputed routing summary from the workflow runtime

Have the router or the workflow's log-collection step write an aggregated routing summary artifact that the CLI simply renders. This would make the CLI trivial and avoid duplicated aggregation logic. It was rejected because it would require a compiler/runtime change shipped to every workflow before any data appears, would leave all existing runs unreadable, and would freeze the summary shape at emission time — whereas client-side aggregation can be improved and re-run against artifacts already stored.

#### Alternative 2: Treat routing as just another token-usage dimension

Extend the existing token-usage model with a few routing fields (selected model, effort) instead of introducing a dedicated routing summary and cost buckets. This was a close call since both data sets are joined on `request_id` anyway. It was rejected because routing has record kinds that have no token-usage counterpart (classification attempts, ranked choices, routing failures, deviations), and because cost attribution needs classifier traffic held *separately* from agent traffic — collapsing them into one model would have made that split impossible to express.

#### Alternative 3: Drop pre-v0.28.39 runs from routing reports

Rather than normalizing endpoint-only deviations, simply refuse to summarize artifacts produced by older AWF versions. Rejected because it would make routing comparisons useless for any repository that has not fully rolled over to the newest AWF, which is the exact population that most needs cost visibility.

### Consequences

#### Positive
- Routing decisions, deviations, and failures become visible in `gh aw audit` (text and JSON) and in cross-run `gh aw logs` aggregates without any workflow change.
- Cost is attributable: classifier spend is separated from selected-model and deviated traffic, so routing overhead can be measured against its savings.
- Works retroactively on artifacts already collected, including runs from older AWF versions, thanks to the endpoint-only deviation normalization.
- Audit comparison can flag regressions in model, effort, mode, or router version between two runs.

#### Negative
- Aggregation semantics now live in the CLI, so they are versioned with the CLI rather than with the run: two CLI versions can render the same artifact differently.
- The normalization rule for pre-v0.28.39 endpoint-only deviations is a compatibility shim that must be carried indefinitely (or until a deprecation decision is made) and is easy to forget when the deviation taxonomy changes.
- The `request_id` join silently drops attribution when the routing log and the token-usage log disagree or when either is truncated; such traffic falls into the deviated bucket by default.
- Adds a sizeable surface in `pkg/cli` (~420 lines plus renderers) and three regenerated JSON schemas that must be kept in sync with the Go types.

#### Neutral
- `firewall.model_routing` events are now included in unified sessions and routing logs are added to the fallback artifact, enlarging artifacts slightly.
- Reference docs for routing, audit, cost management, and artifacts were updated together with the output schemas, so schema regeneration becomes part of the routine change cycle for this feature.
- The summary shape is expressed as exported Go types, which makes the JSON output an effective public contract even though it was not declared as one.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
