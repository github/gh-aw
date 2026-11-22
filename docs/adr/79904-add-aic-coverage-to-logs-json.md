# ADR-79904: Add Explicit AI Credit Coverage to `logs --json`

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: Peli de Halleux (PR #79904 author); other reviewers not recorded [TODO: verify]

---

### Context

`gh aw logs --json` reported runs without recorded AI credit (AIC) cost as `"aic": 0`, the same value as a run measured at zero cost. `summary.total_aic` used `omitempty`, so absence and zero collided there too. Issue #79253 found 10 of 21 runs in one sample reporting `aic: 0` while `total_aic` was absent, which understates summed spend and makes per-output cost ratios look better than they are. Consumers need to tell "not measured" from "measured zero" without changing the type of the existing numeric fields. Cached records loaded through `--cached-jsonl` must also keep measured zeros.

### Decision

We will add an `aic_coverage` field to each run (`available` or `unavailable`) and to the summary (`available`, `partial`, or `unavailable`), and keep `aic` and `total_aic` numeric. A run is `available` when its usage evidence records a cost, including zero, and consumers must ignore `aic` when coverage is `unavailable`. The primary driver is accurate cost accounting without a breaking type change to existing fields. Cached token usage also persists `AICFound` as `aic_found` so measured zeros survive cache hits, and older cache records are treated as `unavailable` unless their AIC is positive.

### Alternatives Considered

#### Alternative 1: Make `aic` nullable (`*float64`, `null` when unknown)

This was the first option proposed in issue #79253. It puts the absence signal in the value itself, so consumers cannot miss it. It was not chosen because it changes the JSON type of an existing numeric field, which breaks consumers that parse `aic` as a number and requires regenerating the checked-in schemas. It would also change `total_aic` the same way. The PR does not record why this was rejected; this reasoning is inferred from the diff and the issue [TODO: verify]. It was a close call.

#### Alternative 2: Keep `0` as the unknown value and document it

Leaving `aic: 0` unchanged and documenting that zero may mean "unknown" needs no schema or cache change. It was rejected because the defect is in machine-readable output: automation that sums `runs[].aic` keeps understating cost, and documentation alone cannot make a zero unambiguous.

### Consequences

#### Positive
- Consumers can separate measured zero from unmeasured cost, per run and in the summary.
- `summary.total_aic` sums only recorded costs, and `summary.aic_coverage` shows whether the total is complete (`available`), incomplete (`partial`), or absent (`unavailable`).
- Existing numeric fields keep their types, so the run-level and summary-level changes are additive.
- Cache hits keep measured zeros, so cached and fresh reports agree on coverage.

#### Negative
- Output changes: `summary.total_aic` is now always emitted, including zero, and `aic_coverage` is added to runs and the summary.
- Consumers that ignore `aic_coverage` still read `aic: 0` for unavailable runs.
- Cached token-usage records gain a persisted `aic_found` field, which extends the cache record format.
- Legacy cache records with zero cost are classified `unavailable` even when zero was measured, because they predate `aic_found`.

#### Neutral
- `docs/src/content/docs/setup/cli.md` documents the new fields.
- `schemas/logs.schema.json`, `schemas/logs-jsonl.schema.json`, and `schemas/audit.schema.json` reference `total_aic` or `aic_coverage`, but this PR does not regenerate them. Whether regeneration is required under the schema workflow from ADR-59720 is [TODO: verify].
- The `partial` rule compares covered runs with the number of runs passed to the summary. [TODO: verify] that this is the intended denominator when runs are filtered or cached and fresh records are mixed.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
