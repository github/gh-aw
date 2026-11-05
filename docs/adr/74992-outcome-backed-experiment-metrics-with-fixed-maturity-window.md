# ADR-74992: Outcome-Backed Experiment Metrics with a Fixed, Configurable Maturity Window

**Date**: 2026-11-05
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent [TODO: verify]

---

### Context

`gh aw experiments analyze` could only measure per-run values from two sources: grader artifacts and eval answers. The compiler only recognized `eval:` and `grader:` metric references. Graders read only the run's own trace and have no network access, so they cannot see events that happen after the run, such as a pull request being merged. The `daily-harness-experiment-proposer` workflow still told agents to prefer a "labeled/merged PR outcome" as the primary metric. Because the analyzer could not measure that metric, experiments stayed at `EXTEND` and the PROMOTE/REJECT ledger could only learn from signals measured during the run. Deterministic safe-output outcome evaluation already existed (`collectRunOutcomes`, `evalCreatePullRequest`), and ADR-70205 established that outcome evaluation runs only in the CLI, with no scheduled collector. Issue #74988 measured time to decision for gh-aw-authored PRs: the median was 1.7 h, 97.2% were decided within 24 h, 99.9% within 72 h, and the per-workflow p95 ranged from 2.2 h to 48.7 h.

### Decision

We will add two outcome-backed metric references, `outcome:accepted` and `outcome:zero_touch`. They are valid in `metric`, `secondary_metrics`, and `guardrail_metrics`. The `experiments analyze` CLI computes them on demand by running the existing deterministic outcome evaluators over each assigned run in the assignment history. Each mature run contributes one observation to the existing observation-set and statistics pipeline. The value is accepted (or zero-touch) outputs divided by resolved outputs. Resolved outputs are accepted, rejected, and stale-pending outputs; stale-pending outputs count toward the denominator but never the numerator.

Run maturity uses a **fixed, per-experiment `outcome_window`**. It is a positive Go duration that defaults to `72h`, and the compiler validates it. The issue proposed a window learned per workflow with Kaplan–Meier (`outcome_window: auto`); we did not implement it. Runs younger than the window are reported as `immature` and do not count toward `min_samples`. The analyzer never treats incomplete evidence as a negative outcome:
- Unknown or errored outcomes and accepted PRs with incomplete zero-touch evidence are excluded as `unavailable`. To support this, a new `ZeroTouchEvidenceComplete` field on `OutcomeReport` separates "a human touched the PR" from "the evidence is incomplete".
- Runs with no outcome-bearing outputs are excluded as `no_op`, so a variant cannot win by doing less.

### Alternatives Considered

#### Alternative 1: Learn the maturity window per workflow (Kaplan–Meier, `outcome_window: auto`)

Issue #74988 proposed estimating time to decision from the experiment's own pooled outputs. Pending outputs would be treated as censored, and the window would be set at the 95th percentile, clamped to 24 h–30 days, with a 72 h cold-start fallback. This adapts to slow-review repositories and avoids delaying analysis for fast workflows. It was not chosen in this PR. It adds a survival estimator, a minimum-sample fallback, and an analysis-time window that changes as data arrives, so results are harder to reproduce between analyze runs. A fixed 72 h default already covers 99.9% of the measured gh-aw PRs, and slower workflows can set an explicit duration. [TODO: verify whether learned windows are deferred to a follow-up or rejected.]

#### Alternative 2: Measure outcomes with graders or a scheduled background collector

The analyzer could get outcome data from graders or from a periodic job that records outcomes into experiment state. Graders run inside the workflow without network access and finish before merges happen, so they cannot observe post-run outcomes. A scheduled collector would contradict ADR-70205 (CLI-only outcome evaluation). It would also need new storage, permissions, and an operating schedule. Running the existing evaluators on demand at analyze time reuses deterministic code and needs no new infrastructure.

#### Alternative 3: Score per output rather than per run

Each safe output could be treated as an independent observation. This gives more samples. However, runs that create several PRs would be overweighted, and observations would not be independent within a run. That conflicts with the existing one-observation-per-run statistics model and `min_samples` semantics. A per-run ratio keeps the unit of randomization (the run) as the unit of analysis.

### Consequences

#### Positive
- Experiments can use post-run acceptance (`outcome:accepted`) and the stricter `outcome:zero_touch` as primary, secondary, or guardrail metrics. This gives the proposer's PROMOTE/REJECT ledger real outcome signals.
- Excluding incomplete zero-touch evidence, unknown or errored outcomes, and no-op runs avoids biasing results toward variants that do less or that have missing data.
- Per-variant coverage (`immature`, `stale_pending`, `unavailable`, `no_op`) appears in text and `--json` output, including for guardrails, so missing data can be audited.
- The change reuses the existing observation-set type, R-STAT-004 test selection, and outcome evaluators, and it needs no new scheduled infrastructure.

#### Negative
- A single fixed default (72 h) does not fit every workflow. For slow-review repositories, slow-but-good PRs will be scored as stale-pending zeros unless authors raise `outcome_window` by hand. For fast workflows, analysis is delayed longer than needed.
- `experiments analyze` now makes GitHub API calls for every mature assigned run. This increases analysis latency, rate-limit exposure, and dependence on network availability at analyze time.
- Outcome values are re-evaluated each time analyze runs, so a run's observation can change between runs (for example, a stale-pending PR that is merged later). Analyze results are therefore not strictly reproducible.
- Ratios from runs that create several outputs produce fractional values, which force Welch's t-test instead of the two-proportion test and reduce statistical power for those experiments.

#### Neutral
- `ExperimentConfig` gains an `outcome_window` field, and the frontmatter schema accepts it. The compiler now also validates `secondary_metrics` references, which it did not do before.
- `OutcomeReport` JSON now always emits `zero_touch_evidence_complete` (no `omitempty`), which changes the logs and audit JSON schemas.
- The experiments specification (R-STAT-004 and a new "Outcome-backed observations" subsection) and the proposer workflow guidance are updated to allow outcome references.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
