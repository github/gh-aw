---
name: ADR-31398 sync AWF v0.25.43 schema and wire apiProxy.modelMultipliers
description: Records the decision to track the gh-aw-firewall config schema in lockstep and to source apiProxy.modelMultipliers from existing engine.token-weights.multipliers rather than a new frontmatter field
type: project
---

# ADR-31398: Sync AWF v0.25.43 schema and wire `apiProxy.modelMultipliers` from existing `engine.token-weights.multipliers`

**Date**: 2026-05-11
**Status**: Superseded
**Superseded**: 2026-07-11
**Deciders**: pelikhan (PR author), to be confirmed

---

## Part 1 — Narrative (Human-Friendly)

### Context

`gh-aw` embeds the `gh-aw-firewall` (AWF) JSON schema at `pkg/workflow/schemas/awf-config.schema.json` and generates a populated AWF config object at compile time from workflow frontmatter. The embedded schema was last synced against firewall `v0.25.38`; since then, upstream firewall `v0.25.43` introduced new config surface — `apiProxy.modelMultipliers`, `apiProxy.maxRuns`, `apiProxy.auth.*` (GitHub-OIDC → Azure/AWS/GCP exchange), and `container.dockerHostPathPrefix`. Workflow frontmatter already carries a `engine.token-weights.multipliers` map that captures per-model AI Credits weights, but the generated AWF config was not emitting it because the schema field did not yet exist. Without a sync, valid AWF capabilities are silently unreachable from `gh-aw`, and `specs/awf-config-sources-spec.md` (which tracks "known drift") goes out of date.

### Decision

We will sync the embedded AWF schema to the firewall `main` snapshot covering `v0.25.43`, and we will wire the newly available `apiProxy.modelMultipliers` field from the **existing** `engine.token-weights.multipliers` workflow input rather than introducing a new frontmatter key. Emission is conditional: when the multipliers map is absent or empty, the generated AWF config **omits** the field entirely (relying on AWF's per-model default of `1`). The drift-tracking spec (`specs/awf-config-sources-spec.md`) is updated in the same PR so the newly surfaced config paths are explicitly recorded.

### Alternatives Considered

#### Alternative 1: Introduce a new dedicated frontmatter field for AWF model multipliers

Add a new `firewall.api-proxy.model-multipliers` (or similar) frontmatter block dedicated to AWF enforcement, distinct from `engine.token-weights.multipliers`. Rejected because the multiplier values represent the same domain concept (per-model AI Credits weighting), and splitting them into two configuration surfaces would force authors to keep the two in sync manually, with no benefit. The existing `engine.token-weights.multipliers` field is already typed as `map[string]float64` and is structurally identical to what the AWF schema requires.

#### Alternative 2: Defer the schema sync until a user requests one of the new fields

Leave the embedded schema pinned at `v0.25.38` and only sync when a concrete user need surfaces. Rejected because schema drift accumulates silently — each missed sync makes the next one larger and increases the chance that `gh-aw` validates a config that the live firewall would reject (or vice versa). Tracking drift via `awf-config-sources-spec.md` only works when the schema is kept current.

#### Alternative 3: Sync the schema but do not wire `modelMultipliers` in this PR

Land the schema update only and treat `modelMultipliers` emission as a follow-up. Rejected as a close call: the wire-up is small (≈18 lines of Go plus a helper) and is the highest-value field in the sync because `gh-aw` already carries the data. Splitting it out would leave the field visible-but-unused in the schema and require a second PR to deliver value users can already configure.

### Consequences

#### Positive
- `gh-aw` no longer silently drops per-model token weights when firewall enforcement is enabled — the multipliers configured under `engine.token-weights.multipliers` now reach the API proxy and influence its 429 budget enforcement.
- Schema drift is eliminated for `v0.25.43`, restoring the invariant that `gh-aw`'s embedded schema and the live firewall accept the same documents.
- The drift-tracking spec records the four newly surfaced config paths, making the next sync's diff easier to reason about.
- Authors get the new feature without learning a new frontmatter key.

#### Negative
- `engine.token-weights.multipliers` now has two distinct effects (internal token accounting **and** AWF proxy-side hard enforcement of AI Credits budgets), so changing this map can produce 429 responses at runtime rather than purely affecting accounting. This dual semantics is not obvious from the field name.
- Embedded-schema sync coupling: `gh-aw` releases now have a soft dependency on the firewall spec cadence — every firewall schema change requires a corresponding `gh-aw` PR to stay drift-free.
- The schema sync adds substantial new surface (`apiProxy.auth.*` OIDC config with conditional `if/then/else` validation for Azure/AWS/GCP) that is **not** yet wired through from frontmatter; readers of the schema may incorrectly assume those paths are reachable via `gh-aw`.

#### Neutral
- Conditional emission (omit when empty) preserves backward compatibility with workflows that do not set multipliers — generated AWF configs are byte-for-byte identical to the pre-PR output in that case.
- Two new test cases (emission when configured, omission when empty) anchor the behavior; future changes to `extractModelMultipliers` will trip them.
- A future ADR may be needed when `apiProxy.maxRuns`, `apiProxy.auth.*`, or `container.dockerHostPathPrefix` are wired through from frontmatter — those decisions are deferred and not in scope here.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Embedded schema sync

1. `pkg/workflow/schemas/awf-config.schema.json` **MUST** track the published `gh-aw-firewall` schema for the version (or snapshot) that `gh-aw` is integrated against.
2. The schema's `$id` **MUST** point to a stable, fetchable URL identifying the upstream source of truth (a release artifact URL or the `main`-branch raw URL is acceptable).
3. When a firewall schema change adds new config paths, `specs/awf-config-sources-spec.md` **MUST** be updated in the same PR to record those paths under its "known drift" coverage table.
4. Schema syncs **MUST NOT** narrow the accepted document set in ways that reject configurations the previous embedded schema accepted, unless the upstream firewall also rejects them.

### `apiProxy.modelMultipliers` wire-up

1. When the workflow's `engine.token-weights.multipliers` map is non-empty, `BuildAWFConfigJSON` **MUST** emit those entries verbatim as `apiProxy.modelMultipliers` in the generated AWF config.
2. When `engine.token-weights.multipliers` is absent, `nil`, or empty, `BuildAWFConfigJSON` **MUST** omit the `apiProxy.modelMultipliers` key entirely from the generated AWF config (the `omitempty` JSON tag is required on the struct field).
3. The implementation **MUST NOT** introduce a separate AWF-specific frontmatter field for per-model multipliers; `engine.token-weights.multipliers` is the single source of truth.
4. The multiplier values **MUST** be passed through unchanged; the implementation **MUST NOT** apply normalization, scaling, or default-injection on the path between frontmatter and AWF config.
5. The helper that extracts multipliers **MUST** be nil-safe with respect to `WorkflowData`, `EngineConfig`, and `TokenWeights` so that workflows without an engine configuration do not panic.

### Test coverage

1. `pkg/workflow/awf_config_test.go` **MUST** include a test that asserts `apiProxy.modelMultipliers` is emitted with the configured values when `engine.token-weights.multipliers` is non-empty.
2. `pkg/workflow/awf_config_test.go` **MUST** include a test that asserts `apiProxy.modelMultipliers` is **not** present in the generated JSON when `engine.token-weights.multipliers` is empty.
3. Tests **SHOULD** assert on the raw generated JSON string (e.g., `assert.Contains` / `assert.NotContains` on a marshalled config) rather than the in-memory struct, to detect regressions in `omitempty` handling.

### Out of scope for this ADR

1. Wire-up of `apiProxy.maxRuns`, `apiProxy.auth.*`, and `container.dockerHostPathPrefix` from frontmatter is **NOT REQUIRED** by this ADR and **MAY** be addressed in subsequent ADRs.
2. Validation that frontmatter multiplier values fall within a sensible numeric range is **NOT REQUIRED** by this ADR; the AWF schema's `exclusiveMinimum: 0` constraint is the authoritative validator at the firewall layer.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

## Superseded note

This ADR is superseded by the removal of authored `engine.token-weights` frontmatter in this PR. The `apiProxy.modelMultipliers` wire-up requirements in this document are historical and are no longer the repository's current design contract.

---
