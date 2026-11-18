# Typed evidence and window policies

Use the installed `gh aw value` command and workflow-owned declarations. No
central registration, gh-aw source checkout, Go toolchain, JS, or query runtime
is required. Preserve independent observation provenance; an adapter does not
verify that supplied numbers correctly measure the goal.

## Evidence contract

`evidence-number-v1` consumes `--evidence PATH`, not implicit Git/session data.
It can replay outside a full checkout; pass an explicit definition and repo.
Its config declares `scope` (`git_snapshot`, `run`, `benchmark`, `object_state`),
`aggregation` (`scalar`, `median`, `excess_lines`), `minimum_samples` (1–10000),
and `require_population`. Unknown config keys fail.

`scalar` requires exactly one independently measured number and minimum 1.
`median` uses repeated comparable raw measurements, not agent duration or grades.
Benchmark scope requires median aggregation and a nonempty comparable `context`.
`excess_lines` requires Git scope, population, nonnegative `maximum_lines`, and
one nonnegative integer newline count per file. It sums the excess over that
budget. Verified absences may be zero; missing files/evidence may not.

Bundle shape:

```json
{
  "version": 1,
  "repository": "github.com/owner/repo",
  "observations": [{
    "id": "benchmark-parent",
    "workflow": "performance",
    "metric": "compile-time-v1",
    "scope": "benchmark",
    "subject": "experiment:fix-123",
    "run_id": 123,
    "at": "2026-10-01T12:00:00Z",
    "observed_at": "2026-10-02T12:00:00Z",
    "phase": "baseline",
    "coverage": "available",
    "population": ["fixture:compile-v1"],
    "context": "runner/toolchain/fixture/protocol-v1",
    "references": ["https://github.com/owner/repo/actions/runs/123"],
    "values": [101, 100, 103, 99, 102]
  }]
}
```

Declare the matching definition ID/unit/direction and config separately.
Bundles are strict single-object JSON, at most 512 KiB and 1024 observations.
Require unique observation IDs, selected HOST/OWNER/REPO, positive run IDs,
source references, finite numbers, and ordered event/collection clocks.
Available Git observations require a full commit `sha`; this records identity,
not authenticated ancestry. Population IDs are unique nonempty strings.

Coverage is `available`, `partial`, or `unavailable`. The latter two require a
reason and no numeric values. Missing population or too few samples yields
unavailable, not zero. Filter by declared workflow/metric/scope and event-time
report bounds before evaluation; retain source and collection clocks.

Pair at most one `baseline` and one `outcome` per subject/experiment. `sample`
records form a descriptive series. Changed population/context prevents change
calculation. Do not cherry-pick among multiple endpoints. Preserve negative
improvements. A scope-specific independent producer must verify correctness,
eligibility, validation/race checks, and benchmark conditions before supplying
an available number. Scalars are transports, not ten bespoke goal validators.

Imported measurements never synthesize native delivery, human acceptance,
survival, or attributed value. Evidence-only reports always leave attribution
null, including when a native issue has been accepted. `window_mature` means
only that time has elapsed by the report cutoff; `sampling_coverage` remains
`not_evaluated`. Do not call it full-window evidence.

## Versioned recipe policy

Pin `window.version: 1` and all consequential fields before future collection:

| Field | Default / constraint |
| --- | --- |
| `baseline` | `exact_parent` for Git replay; `paired_observation` for imported evidence. They are not interchangeable. |
| `before_days`, `after_days` | 7, 7; each 1–365. |
| `outcome_lag_days` | 0; 0–365. |
| `durability_days` | 7; 1–365, at least the lag. |
| `sample_every_hours` | 24; positive divisor of 24. |
| `trend_every_days` | 7; 1–365. |
| `minimum_before_samples`, `minimum_after_samples` | 7, 7; positive, attainable; at most 1024 total slots. |
| `isolation_before_days`, `isolation_after_days` | 7, 7; each 0–365. |
| `overlap` | `workflow` or opt-in `changed_files`. |

Defaults preserve the slides, not a statistically justified universal policy.
Choose horizons from workflow cadence, expected response delay, reversibility,
and goal evidence. Benchmarks also need repeated controlled samples; extending
calendar time cannot manufacture those. Label retrospective policies honestly.
Keep the slide recipe's explicitly pinned 7/7-day, 14-slot policy unchanged.

Git replay samples before adoption and from adoption plus lag; it compares
the exact parent to the lagged outcome, and independently checks changed files
through durability. Wait until lag+after, durability, and forward isolation
have all elapsed, even if minimum samples were already met. Delayed changes
remain descriptive: attribution requires subject linkage not yet supported
for delayed aggregate outcomes.

Workflow overlap excludes neighboring producer-linked merges. Changed-file
overlap instead requires exact file evidence and rejects intersections; missing
metadata stays unavailable. Neither rule establishes exclusive causality.
Do not lengthen outcome horizons and silently widen global exclusions.
Imported replay records the policy but does not execute Git sampling, overlap,
or survival checks. It rejects PR author/batch/witness selectors and `--ref`.

## Reevaluation discipline

Reuse a fixed cohort and original evidence; publish a separate result rather
than rewriting the first experiment. Distinguish configured transports,
available independent measurements, mature horizons, and actual attribution.
Nine unavailable goals plus one real outcome is not ten validated functions.
No-op, missing identity, missing denominator, and absent independent references
are not goal scores. Do not fix them by backdating contracts or dispatching runs.
