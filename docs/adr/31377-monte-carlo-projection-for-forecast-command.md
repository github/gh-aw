# ADR-31377: Monte Carlo Projection for `gh aw forecast` Command

**Date**: 2026-05-10
**Status**: Draft
**Deciders**: Unknown (PR authored by `app/copilot-swe-agent`; human deciders TBD)


---

## Part 1 — Narrative (Human-Friendly)

### Context

Users of `gh-aw` want to project the future cost and yield of their agentic workflows before scheduling them at higher cadence or rolling them out organization-wide. Historical run data is highly variable: per-run AI Credit usage can vary by an order of magnitude depending on agent decisions, runs per period follow a counting process, and not every run succeeds. A naive point estimate (e.g. `avg(tokens) × avg(runs/period)`) hides this uncertainty and tends to under-state tail risk. The command must also integrate with existing analysis infrastructure (episode classification, A/B experiment variant tracking, JSON output for agent consumers) and remain useful on small samples (≤30 days of history).

### Decision

We will introduce a new **experimental** `gh aw forecast` CLI command that projects per-workflow AI Credit usage using **Monte Carlo simulation** (10 000 trials) rather than a single point estimate. Each trial composes three independent sources of uncertainty — Poisson-distributed run counts, bootstrap-resampled per-run AI Credits, and Bernoulli-distributed success — and the aggregated trials yield P10/P50/P90 confidence intervals. The command lives in `pkg/cli/forecast*.go`, reuses the existing `buildEpisodeData` engine from `logs_episode.go` for episode analysis, supports remote repositories via `--repo`, and is gated as experimental (stderr warning + `(experimental)` short description) because the interface and statistical assumptions may change.

### Alternatives Considered

#### Alternative 1: Point estimates from historical averages

Compute `mean(aic) × mean(runs_per_period) × success_rate` and report a single projected number per workflow. Simple, deterministic, and cheap. Rejected because it hides variance, gives users no way to reason about tail risk (which is the operationally interesting question for cost budgeting), and makes side-by-side comparisons across workflows misleading when their variance profiles differ.

#### Alternative 2: Closed-form analytical distribution (e.g. compound Poisson)

Model run count as Poisson(λ) and per-run tokens as a parametric distribution (lognormal, gamma) and derive percentiles analytically. More elegant and faster than simulation. Rejected because the historical token distribution is typically multi-modal (different agent paths produce qualitatively different cost profiles) and ill-suited to a single parametric family; bootstrap resampling preserves the empirical shape without forcing a fit. Closed form also makes per-variant A/B splits and success-rate composition awkward.

#### Alternative 3: Reuse the existing `audit` command and add a `--forecast` flag

Extend the audit command instead of creating a new top-level command. Rejected because forecasting has a different mental model from auditing (forward projection vs. retrospective analysis), a different input shape (workflow IDs vs. run IDs), and different output structure (per-period projections vs. per-run metrics). Bundling them would muddy both commands' interfaces.

### Consequences

#### Positive
- Users get P10/P50/P90 intervals, exposing tail risk that point estimates would hide.
- Bootstrap resampling preserves the empirical token distribution without imposing a parametric model.
- JSON output (`monte_carlo` field) gives downstream agents structured access to the full distribution summary.
- Reuse of `buildEpisodeData` avoids duplicating episode-classification logic and keeps semantics consistent with `logs`/`audit`.
- Experimental gating lets us iterate on the statistical model (e.g. switching distributions, adjusting trial count) without a stability commitment.

#### Negative
- Monte Carlo introduces nondeterminism in output — two consecutive runs on the same data produce slightly different P50/P10/P90 values unless a seed is pinned. This complicates regression testing and snapshot comparisons.
- 10 000 trials × N workflows × bootstrap sampling adds CPU cost; the Poisson sampler has two regimes (Knuth exact for λ ≤ 15, Normal approximation otherwise) to stay within ~10 ms/workflow, but this adds complexity vs. a closed-form approach.
- Episode counts for orchestrator-style workflows are a lower-bound estimate because `AwContext` (dispatch/workflow_call) lineage is unavailable without artifact downloads, which the command intentionally skips for speed.
- Remote-repo mode (`--repo`) degrades frontmatter metadata to empty since Markdown source is local-only, creating a subtle behavior split between local and remote forecasts.
- Adds three new files in `pkg/cli/` (forecast_command.go, forecast.go, forecast_montecarlo.go) plus tests, increasing maintenance surface in an already large package.

#### Neutral
- The `--days` flag is capped at 30, which is a deliberate sampling-window choice; longer windows would require pagination changes in `gh run list`.
- The W3C-style specification at `docs/src/content/docs/specs/forecast-specification.md` (sidebar order 1355) commits us to keeping spec and implementation in sync while the command is experimental.
- Trial count (10 000) is currently hardcoded; making it configurable is a future option but not part of this decision.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Projection Algorithm

1. The `forecast` command **MUST** project per-workflow AI Credit usage using Monte Carlo simulation, not a single point estimate.
2. The simulation **MUST** run at least 10 000 independent trials per workflow per forecast invocation.
3. Each trial **MUST** compose three independent random variables: run count drawn from a Poisson process, per-run AI Credits drawn by bootstrap resampling of historical observations, and per-run success drawn as a Bernoulli with the historical success rate.
4. The Poisson sampler **MUST** use Knuth's exact algorithm when λ ≤ 15 and **MUST** use a Normal approximation when λ > 15.
5. The command **MUST** report P10, P50, and P90 AI Credits percentiles in both the console table and JSON output.
6. The command **MUST NOT** emit only a point estimate without accompanying P10/P90 bounds.

### Command Interface

1. The command **MUST** be registered in the `analysis` command group as `gh aw forecast`.
2. The command **MUST** be marked experimental: its Cobra short description **MUST** include the literal substring `(experimental)`, and it **MUST** print an experimental warning to stderr at runtime.
3. The `--days` flag **MUST** accept only the values `7` and `30`; values outside this set **MUST** be rejected with a clear error.
4. The `--json` flag **MUST** emit the full `ForecastResult` struct including a `monte_carlo` object with `mean_projected_aic`, `std_dev_aic`, and P10/P50/P90 fields.
5. The command **MAY** accept multiple workflow IDs as positional arguments; when omitted, it **MUST** forecast all agentic workflows discoverable in the target repository.
6. When `--repo owner/repo` is supplied, workflow discovery **MUST** use the GitHub API (`fetchGitHubWorkflows`) and **MUST NOT** read local `.lock.yml` files for that invocation.
7. Workflow ID matching against remote repositories **MUST** be case-insensitive against both display names and file-path basenames.

### Episode Analysis

1. Episode grouping **MUST** reuse `buildEpisodeData` and `classifyEpisode` from `logs_episode.go`; it **MUST NOT** reimplement episode classification.
2. Because no artifacts are downloaded, episode linkage **MUST** rely only on GitHub Actions API fields (`event`, `headSha`, `headBranch`) and **MUST** gracefully degrade when `AwContext` is unavailable.
3. The console output **SHOULD** display an episode breakdown table only when `runs/episode > 1` (i.e. orchestrator-style workflows).

### Frontmatter and Variants

1. When forecasting local workflows, the command **MUST** surface active trigger types and concurrency configuration from each workflow's Markdown frontmatter.
2. When forecasting via `--repo`, frontmatter-derived fields **MAY** be empty without causing the forecast to fail.
3. When a workflow defines A/B experiment variants, run counts and fractions **MUST** be reported per variant in both console and JSON output.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25642964043) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
