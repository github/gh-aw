# ADR-79904: Add Explicit AI Credit Coverage to `logs --json`

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: Peli de Halleux (PR #79904 author); other reviewers not recorded [TODO: verify]

---

### Context

`gh aw logs --json` reported runs without recorded AI credit (AIC) cost as `"aic": 0`, the same value as a run measured at zero cost. `summary.total_aic` used `omitempty`, so absence and zero collided there too. Issue #79253 found 10 of 21 runs in one sample reporting `aic: 0` while `total_aic` was absent, which understates summed spend and makes per-output cost ratios look better than they are. Consumers need to tell "not measured" from "measured zero". Cached records loaded through `--cached-jsonl` must also keep measured zeros.

### Decision

Emit `null` for an unknown run `aic` and for `summary.total_aic` when any returned run has unknown cost or there are no runs. A measured zero remains numeric. Retain `aic_coverage` per run (`available` or `unavailable`) and summary (`available`, `partial`, or `unavailable`) so clients can distinguish partial from absent evidence. Cached token usage persists `AICFound` as `aic_found` so measured zeros survive cache hits; older cache records without coverage are unavailable unless their AIC is positive.

### Alternatives Considered

#### Alternative 1: Keep numeric fields and add coverage

Keeping unknown costs as zero avoids changing the JSON type, but clients that ignore coverage can mistake missing data for a measured zero or a partial subtotal for a complete total.

#### Alternative 2: Keep `0` as the unknown value and document it

Leaving `aic: 0` unchanged and documenting that zero may mean "unknown" needs no schema or cache change. It was rejected because the defect is in machine-readable output: automation that sums `runs[].aic` keeps understating cost, and documentation alone cannot make a zero unambiguous.

### Consequences

#### Positive
- Consumers can separate measured zero from unmeasured cost, per run and in the summary.
- `summary.total_aic` is numeric only when the total is complete, and `summary.aic_coverage` distinguishes partial from unavailable evidence.
- Cache hits keep measured zeros, so cached and fresh reports agree on coverage.

#### Negative
- Consumers must handle `null` in the previously numeric run and summary cost fields.
- Cached token-usage records gain a persisted `aic_found` field, which extends the cache record format.
- Legacy cache records with zero cost are classified `unavailable` even when zero was measured, because they predate `aic_found`.

#### Neutral
- `docs/src/content/docs/setup/cli.md` documents the new fields.
- The generated logs schemas declare nullable cost fields.
- The `partial` rule compares covered runs with the number of returned runs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
