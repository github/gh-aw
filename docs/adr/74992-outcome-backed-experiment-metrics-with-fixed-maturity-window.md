# ADR-74992: Outcome-Backed Experiment Metrics with a Learned Maturity Window

**Date**: 2026-11-05
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent [TODO: verify]

---

### Context

`gh aw experiments analyze` could only measure per-run values from two sources: grader artifacts and eval answers. The compiler only recognized `eval:` and `grader:` metric references. Graders read only the run's own trace and have no network access, so they cannot see events that happen after the run, such as a pull request being merged. The `daily-harness-experiment-proposer` workflow still told agents to prefer a "labeled/merged PR outcome" as the primary metric. Because the analyzer could not measure that metric, experiments stayed at `EXTEND` and the PROMOTE/REJECT ledger could only learn from signals measured during the run. Deterministic safe-output outcome evaluation already existed (`collectRunOutcomes`, `evalCreatePullRequest`), and ADR-70205 established that outcome evaluation runs only in the CLI, with no scheduled collector. Issue #74988 measured time to decision for gh-aw-authored PRs: the median was 1.7 h, 97.2% were decided within 24 h, 99.9% within 72 h, and the per-workflow p95 ranged from 2.2 h to 48.7 h.

### Decision

We will add two outcome-backed metric references, `outcome:accepted` and `outcome:zero_touch`. They are valid in `metric`, `secondary_metrics`, and `guardrail_metrics`. The `experiments analyze --outcome` CLI computes them on demand by running the existing deterministic outcome evaluators over each assigned run in the assignment history. Collection is opt-in because it makes additional GitHub API requests. Without `--outcome`, analysis of any outcome-backed metric returns an actionable error before collecting metric evidence. Each mature run contributes one observation to the existing observation-set and statistics pipeline. The value is accepted (or zero-touch) outputs divided by resolved outputs. Resolved outputs are accepted, rejected, and stale-pending outputs; stale-pending outputs count toward the denominator but never the numerator.

Run maturity uses a **per-workflow `outcome_window`**. It defaults to `auto`, which estimates the pooled 95th-percentile time to decision across variants using Kaplan–Meier estimation. Still-open outputs are right-censored at their current age. The estimated window is clamped to 24 hours–30 days and falls back to `72h` until at least 20 outputs have known decision times. An explicit positive Go duration overrides learning. Runs younger than the effective window are reported as `immature` and do not count toward `min_samples`. The analyzer never treats incomplete evidence as a negative outcome:
- Unknown or errored outcomes and accepted PRs with incomplete zero-touch evidence are excluded as `unavailable`. To support this, a new `ZeroTouchEvidenceComplete` field on `OutcomeReport` separates "a human touched the PR" from "the evidence is incomplete".
- Runs with no outcome-bearing outputs are excluded as `no_op`, so a variant cannot win by doing less.

### Alternatives Considered

#### Alternative 1: Use a fixed 72-hour default

One fixed 72-hour default is simple and covered 99.9% of the measured gh-aw PRs, but the per-workflow p95 ranged from 2.2 h to 48.7 h. That delays analysis for fast workflows and can score slow-but-good outputs as pending. Explicit durations remain available when a repository needs stable policy, while the learned default adapts to observed decision latency.

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
- Learned windows can change as new outcomes arrive, so analyses may use different maturity cutoffs over time. Explicit durations provide a stable alternative.
- `experiments analyze --outcome` makes GitHub API calls for assigned runs, including recent runs used as censored observations. This increases analysis latency, rate-limit exposure, and dependence on network availability only when explicitly enabled.
- Outcome values are re-evaluated each time analyze runs, so a run's observation can change between runs (for example, a stale-pending PR that is merged later). Analyze results are therefore not strictly reproducible.
- Ratios from runs that create several outputs produce fractional values, which force Welch's t-test instead of the two-proportion test and reduce statistical power for those experiments.

#### Neutral
- `ExperimentConfig` gains an `outcome_window` field, and the frontmatter schema accepts it. The compiler now also validates `secondary_metrics` references, which it did not do before.
- `OutcomeReport` JSON now always emits `zero_touch_evidence_complete` (no `omitempty`). The logs and audit JSON schemas keep the field optional for historical records; absence does not establish complete zero-touch evidence.
- The experiments specification (R-STAT-004 and a new "Outcome-backed observations" subsection) and the proposer workflow guidance are updated to allow outcome references.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
