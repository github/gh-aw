# ADR-77326: Separate Authored Pricing Overrides from Reporting Costs

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: [TODO: verify] Workflow actor lpcox; implementation by copilot-swe-agent; reviewers not yet identified

---

### Context

The compiler fills in pricing for a workflow's model when frontmatter and the embedded catalog do not provide it (`resolveModelPricingIfMissing`, `resolveFallbackModelPricing`). Those results were merged into the same `WorkflowData.ModelCosts` map that holds authored `models.providers` pricing, and `apiProxy.providers` in the AWF config was built from that map. AWF treats any pricing supplied in `apiProxy.providers` as an operator override, so compiler-injected defaults could outrank AWF's maintained pricing and flatten richer facts such as long-context tiers and promotions (see [github/gh-aw#77325](https://github.com/github/gh-aw/issues/77325)). ADR-48107 chose to embed `models.providers` overlays in `apiProxy.providers`, but because authored and automatic prices shared one map, that decision could not tell them apart.

### Decision

We will keep two maps with different authority. `WorkflowData.ModelCosts` holds all pricing used for reporting, including compiler-resolved defaults, and continues to feed `GH_AW_INFO_MODEL_COSTS`. `WorkflowData.ModelCostOverrides` holds only explicit frontmatter and imported `models.providers` pricing, and it is the sole source for `apiProxy.providers`, including in threat-detection runs. The primary driver is pricing authority: AWF stays the authority for any price the author did not set explicitly.

### Alternatives Considered

#### Alternative 1: Tag Automatic Entries with a Source Marker

Each automatically resolved entry would carry a marker such as `source: compiler-default`, and `extractModelCostProviders` would filter those entries out. This keeps a single map and a single shape. It was a close call. It was not chosen because the marker would live inside a free-form `map[string]any` that is serialized and merged in several places, so one missed copy or filter would silently re-enable injection. A separate map makes the boundary a field-level fact rather than a convention.

#### Alternative 2: Stop Resolving Automatic Pricing

Removing `resolveModelPricingIfMissing` and the fallback resolver would eliminate the authority inversion at its source. It was not chosen because `GH_AW_INFO_MODEL_COSTS` reporting and estimates depend on catalog pricing for models that frontmatter does not price, and removing it would leave those reports without cost data. Reporting can move to AWF-owned data later, as planned in #77325.

#### Alternative 3: Adopt AWF-Owned Pricing Data in This Change

Replacing the embedded catalog with a revision-pinned AWF pricing snapshot would address the root cause in one step. It was not chosen for this change because it needs a consumer integration with integrity checks, caching, and update handling. That is the second step in #77325. This change is the smaller first step that removes the injection.

### Consequences

#### Positive
- Automatically resolved catalog prices can no longer become AWF operator overrides, so AWF's maintained pricing governs any model the author did not price.
- Explicit zero rates in authored frontmatter still reach AWF, and tests guard that behavior.
- Reporting in `GH_AW_INFO_MODEL_COSTS` keeps compiler-resolved prices, so cost visibility does not regress.
- Regression tests cover the boundary at three points: the compiler build (`buildInitialWorkflowData`), AWF config generation (`extractModelCostProviders`), and detection-run propagation.

#### Negative
- Two maps must stay in sync. Any new code path that copies `ModelCosts` into a new `WorkflowData` must also copy `ModelCostOverrides`, or authored overrides are silently dropped from the AWF config. Today the only such path is `buildThreatDetectionWorkflowData`.
- Models that gh-aw's embedded catalog prices but AWF's built-in table does not may now reach AWF's unknown-model AI-credits path unless the author adds explicit `models.providers` pricing. This is the failure mode ADR-48107 was written to prevent. [TODO: verify coverage of resolved models against AWF's current pricing table]
- Reporting prices and AWF's charged prices can differ, and the report may not reflect what AWF enforces until #77325 is complete.

#### Neutral
- The new `ModelCostOverrides` field is internal to the Go `WorkflowData` struct. It adds no user-facing schema.
- ADR-48107 remains in Draft. Its decision text ("embed `models.providers` overlays from `WorkflowData.ModelCosts`") is narrowed by this ADR. A reviewer should mark ADR-48107 as superseded or amend it when this ADR is finalized.
- Detection runs now copy `ModelCosts` and `ModelCostOverrides` after construction rather than in the struct literal.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
