---
title: Forecast Command Specification
description: Formal W3C-style specification for the gh aw forecast command — Monte Carlo token-usage projection, episode analysis, workflow discovery, and output formats for GitHub Agentic Workflows
sidebar:
  order: 1355
---

# Forecast Command Specification

**Version**: 1.0.0  
**Status**: Draft  
**Latest Version**: [forecast-specification](/gh-aw/specs/forecast-specification/)  
**Editor**: GitHub Agentic Workflows Team

## Abstract

This specification defines the `gh aw forecast` command for the GitHub Agentic Workflows project. The command performs historical sampling of completed agentic workflow runs and applies a Monte Carlo simulation engine to project future AI Credits (AIC) consumption over a configurable time horizon. The specification covers workflow discovery (local and remote modes), data sampling via the GitHub Actions API, the Poisson–bootstrap Monte Carlo projection algorithm, episode-level analysis, and both console-table and machine-readable JSON output formats. Implementations conforming to this specification provide operators with probabilistic cost forecasts suitable for capacity planning, cost estimation, and budget governance.

---

## Status of This Document

This section describes the status of this document at the time of publication. This is a **Draft** specification. The `gh aw forecast` command is a stable, generally available command; forecasts it produces are probabilistic estimates and may be inaccurate.

This document is governed by the GitHub Agentic Workflows project specifications process.

Feedback should be filed as GitHub issues against the `github/gh-aw` repository.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Terminology](#3-terminology)
4. [Command Interface](#4-command-interface)
5. [Workflow Discovery](#5-workflow-discovery)
6. [Data Sampling](#6-data-sampling)
7. [Monte Carlo Projection Engine](#7-monte-carlo-projection-engine)
8. [Episode Analysis](#8-episode-analysis)
9. [Output Formats](#9-output-formats)
10. [Error Handling](#10-error-handling)
11. [Implementation Requirements](#11-implementation-requirements)
12. [Compliance Testing](#12-compliance-testing)
13. [Norms](#13-norms)
14. [Sync Notes](#14-sync-notes)
15. [Appendices](#15-appendices)
16. [References](#16-references)
17. [Change Log](#17-change-log)

---

## 1. Introduction

### 1.1 Purpose

The `gh aw forecast` command addresses the operational need to predict future Large Language Model (LLM) token expenditure for agentic workflows managed by gh-aw. Token consumption is a primary cost driver for agentic systems; the ability to project future usage from historical observations enables:

- **Capacity Planning**: Anticipating token demand before budget thresholds are reached.
- **Cost Governance**: Providing P10/P50/P90 confidence intervals for financial planning.
- **Workflow Comparison**: Ranking workflows by projected token cost across a shared time period.
- **Experiment Evaluation**: Measuring the token impact of A/B experiment variants.

The command combines empirical bootstrapping of historical token observations with a Poisson-distributed run-count model to produce statistically sound projections without requiring parametric distribution assumptions on token usage.

### 1.2 Scope

This specification covers:

- Command-line interface: flags, positional arguments, and invocation modes
- Workflow discovery in local (`.github/workflows/`) and remote (`--repo`) modes
- Historical run sampling and per-run metric derivation
- The Monte Carlo simulation algorithm producing P10, P50, P90 percentile estimates
- Episode grouping and episode-level metric computation
- Console table output format
- Machine-readable JSON output schema (`--json`)
- Error conditions and graceful-degradation behavior

This specification does NOT cover:

- The AI Credits (AIC) computation algorithm (defined in the [AI Credits Specification](/gh-aw/specs/ai-credits-specification/))
- The `aw_info.json` artifact schema
- A/B experiment frontmatter schema (defined in the [A/B Experiments Specification](/gh-aw/experimental/experiments-specification/))
- Billing, pricing, or financial modeling beyond token projections
- Streaming or real-time token consumption reporting

### 1.3 Design Goals

A conforming `gh aw forecast` implementation MUST be designed for:

- **Empirical Accuracy**: Projections derived from observed historical data rather than assumed distributions.
- **Probabilistic Reporting**: P10/P50/P90 uncertainty bounds communicated to callers.
- **Graceful Degradation**: Missing data (no runs, no artifacts, no frontmatter) MUST produce partial results rather than failures.
- **Dual Modes**: Both local-repository and remote-repository operation without requiring a checkout.
- **Interoperability**: JSON output schema stable enough for machine consumption by downstream tooling.

---

## 2. Conformance

### 2.1 Conformance Classes

A **conforming forecast implementation** is one that satisfies all MUST, REQUIRED, and SHALL requirements in this specification.

A **partially conforming forecast implementation** is one that satisfies all MUST requirements in Sections 4, 5, 6, and 7 but MAY lack support for optional features such as episode analysis (Section 8), experiment variant reporting, or verbose diagnostics.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations MUST support:

- **Level 1 (Required)**: Command invocation, workflow discovery, historical data sampling, and Monte Carlo projection with console output.
- **Level 2 (Standard)**: JSON output (`--json`), episode analysis, remote-repository mode (`--repo`), and experiment variant reporting.
- **Level 3 (Complete)**: All optional features including `--verbose` diagnostics, concurrency limit reporting, and frontmatter metadata enrichment.

---

## 3. Terminology

### 3.1 AI Credits (AIC)

A normalized unit of LLM token consumption defined in the [AI Credits Specification](/gh-aw/specs/ai-credits-specification/). AIC accounts for token class weights and model multipliers to produce a single comparable scalar across heterogeneous LLM invocations.

### 3.2 Workflow Run

A single execution of a GitHub Actions workflow. A run has a unique numeric run ID, an event type, a status (`completed`, `in_progress`, `queued`), a conclusion (`success`, `failure`, `cancelled`, etc.), and a head commit SHA.

### 3.3 Historical Window

The time interval `[now − days, now]` used to bound the set of completed and in-progress runs eligible for sampling. Controlled by the `--days` flag.

### 3.4 Sample

The subset of completed and in-progress workflow runs within the historical window selected for metric derivation. The maximum sample size per workflow is controlled by the `--sample` flag.

### 3.5 Monte Carlo Trial

A single independent simulation that draws stochastic values for run count, per-run token usage, and per-run success, combining them to produce one projected AI Credit total for the projection period.

### 3.6 Projection Period

The future time interval for which token consumption is projected. Controlled by the `--period` flag; either one calendar week (`week`) or one calendar month (`month`).

### 3.7 Observed Runs Per Period

The rate of workflow runs observed in the historical window, extrapolated to the projection period length:

```
observed_runs_per_period = (sampled_run_count / history_days) × period_days
```

Where `period_days` is 7 for `week` and 30 for `month`.

### 3.8 Episode

A logical grouping of one or more workflow runs that collectively represent a single task attempt. Episodes are identified by grouping runs sharing the same `headSha` and `headBranch`, or by `workflow_dispatch`/`workflow_call` linkage where available.

### 3.9 Yield

The effective throughput rate: the expected number of successful runs per projection period, computed as the product of the observed run frequency and the historical success rate:

```
yield = observed_runs_per_period × success_rate
```

Where `success_rate = successful_run_count / total_sampled_run_count`.

Example:

If `successful_run_count = 18`, `total_sampled_run_count = 24`, and
`observed_runs_per_period = 20`, then:

```
success_rate = 18 / 24 = 0.75
yield = observed_runs_per_period × success_rate = 20 × 0.75 = 15
```

### 3.10 Bootstrap Resampling

An empirical resampling technique where individual observations are drawn with replacement from the observed sample. Used in Section 7 to model per-run token usage without parametric distribution assumptions.

### 3.11 Lock File

A `.lock.yml` file located in `.github/workflows/` that declares a compiled agentic workflow and its associated metadata. Lock files are the authoritative source of workflow identifiers in local mode.

---

## 4. Command Interface

### 4.1 Synopsis

```
gh aw forecast [workflow_id...] [flags]
```

Security and operational safeguards for this command interface are defined in §10.7.

### 4.2 Positional Arguments

| Argument | Type | Required | Description |
|---|---|---|---|
| `workflow_id` | string (repeatable) | No | Zero or more workflow identifiers to forecast. If omitted, all discovered agentic workflows are forecasted. |

Workflow identifiers MUST be matched case-insensitively against:
1. The workflow display name
2. The workflow file-path basename (without extension)

If a provided `workflow_id` does not match any discovered workflow, the implementation MUST emit an error message identifying the unmatched identifier and MUST exit with a non-zero status code.

### 4.3 Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--days` | int | `30` | Length of the historical sampling window in days. Permitted values: `7`, `30`. |
| `--period` | string | `"month"` | Projection period length. Permitted values: `"week"`, `"month"`. |
| `--sample` | int | `100` | Maximum number of runs to sample per workflow. MUST be ≥ 1. |
| `--max-age` | int | `90` | Maximum age in days for historical runs eligible for sampling. Implementations SHOULD discard runs older than this bound unless the caller overrides it. MUST be ≥ 1. |
| `--repo` | string | (none) | Target a repository other than the current working directory, in `owner/repo` format. Enables remote mode. |
| `--json` | bool | `false` | Emit machine-readable JSON output instead of console tables. |
| `--verbose` | bool | `false` | Emit verbose diagnostic output to stderr during processing. |

### 4.4 Flag Validation

Implementations MUST validate all flag values before beginning any API calls or file system operations:

- **R-CLI-001**: If `--days` is not one of `{7, 30}`, the implementation MUST exit with a non-zero status and an error message specifying the permitted values.
- **R-CLI-002**: If `--period` is not one of `{"week", "month"}`, the implementation MUST exit with a non-zero status and an error message specifying the permitted values.
- **R-CLI-003**: If `--sample` is less than 1, the implementation MUST exit with a non-zero status.
- **R-CLI-004**: If `--repo` is provided, it MUST match the pattern `owner/repo` (two non-empty components separated by `/`). An invalid format MUST produce a non-zero exit with a descriptive error.
- **R-CLI-005**: If `--max-age` is provided and is less than 1, the implementation MUST exit with a non-zero status and a descriptive error.

### 4.5 Exit Codes

| Code | Meaning |
|---|---|
| `0` | Forecast completed successfully. |
| `1` | Usage error (invalid flags, unmatched workflow IDs). |
| `2` | GitHub API authentication failure. |
| `3` | No workflows discovered. |

### 4.6 Example Invocations

```sh
# Forecast all agentic workflows in the current repository for the next month
gh aw forecast

# Forecast two specific workflows and compare
gh aw forecast ci-doctor daily-planner

# Use a 7-day window and project over the next week
gh aw forecast --period week --days 7

# Emit machine-readable JSON
gh aw forecast --json

# Forecast workflows in a remote repository
gh aw forecast --repo owner/repo

# Forecast a specific workflow in a remote repository
gh aw forecast --repo owner/repo ci-doctor

# Ignore historical runs older than 90 days (default)
gh aw forecast --max-age 90
```

---

## 5. Workflow Discovery

### 5.1 Modes

The forecast command operates in one of two discovery modes, determined by the presence of the `--repo` flag:

- **Local Mode**: `--repo` is absent; workflows are discovered from the current repository's `.github/workflows/` directory.
- **Remote Mode**: `--repo` is present; workflows are discovered via the GitHub Actions API.

### 5.2 Local Mode Discovery

In local mode, the implementation MUST:

1. **R-DISC-001**: Enumerate all files matching `*.lock.yml` within `.github/workflows/` of the current working repository.
2. **R-DISC-002**: Parse each lock file to extract the workflow identifier and display name.
3. **R-DISC-003**: If the `.github/workflows/` directory does not exist or contains no lock files, the implementation MUST emit an informational message and exit with code `3`.

The implementation MAY additionally read frontmatter metadata from corresponding workflow source files to enrich per-workflow records with:

- Active trigger types (`active_triggers`)
- Concurrency configuration (`concurrency_limit`)
- A/B experiment variant declarations (`experiment_variants`)

Frontmatter enrichment is OPTIONAL; absence of a corresponding source file MUST NOT prevent discovery or projection of the workflow.

### 5.3 Remote Mode Discovery

In remote mode (when `--repo owner/repo` is specified), the implementation MUST:

1. **R-DISC-010**: Call the GitHub Actions API (`GET /repos/{owner}/{repo}/actions/workflows`) to enumerate workflows in the target repository. If workflow discovery hits a primary or secondary GitHub API rate limit, the implementation SHOULD back off and retry before failing.
2. **R-DISC-011**: Filter the returned workflows to those identified as agentic (e.g., by inspecting file-path conventions, labels, or other implementation-defined heuristics).
3. **R-DISC-012**: Match any caller-supplied `workflow_id` positional arguments against workflow display names and file-path basenames using case-insensitive string comparison.
4. **R-DISC-013**: If rate-limit exhaustion occurs after at least one caller-supplied workflow identifier can still be attempted, the implementation MUST continue with that subset as a partial result set and MUST emit a warning identifying the degraded discovery mode.
5. **R-DISC-014**: Implementations MUST tolerate workflow discovery race conditions where a workflow is renamed, disabled, or deleted after enumeration but before run sampling. The affected workflow MUST be reported as a per-workflow partial failure without aborting the overall forecast.

Remote workflow discovery race-condition mitigation:

- Capture a stable snapshot of discovered workflow IDs from the initial listing call.
- During per-workflow run sampling, treat HTTP 404/410 for a previously listed workflow as a
  recoverable per-workflow partial failure.
- Emit a warning that identifies the workflow and the race condition class (renamed, removed, or
  inaccessible at sample time).

In remote mode, frontmatter metadata (triggers, concurrency, experiment variants) is UNAVAILABLE because the workflow source files are not accessible. The implementation MUST degrade gracefully: fields that depend on frontmatter MUST be omitted from output or reported as their zero/empty values rather than causing an error.

### 5.4 Workflow ID Matching

Workflow ID matching MUST be case-insensitive. A caller-supplied identifier matches a discovered workflow if and only if it equals (ignoring case) either:

- The workflow's display name, OR
- The basename of the workflow's file path (without file extension)

Matching MUST be performed after discovery is complete; partial prefix matches are NOT sufficient for conformance.

---

## 6. Data Sampling

### 6.1 Sampling Procedure

For each discovered workflow (or each workflow in the filtered set), the implementation MUST perform the following sampling procedure:

1. **R-SAMP-001**: Query workflow runs within the historical window using the equivalent of `gh run list --workflow <id> --limit <sample> --created >=<cutoff>`, retaining completed runs and in-progress runs with partial usage snapshots.
2. **R-SAMP-002**: Limit the returned run set to at most `--sample` runs.
3. **R-SAMP-003**: Implementations SHOULD discard historical runs older than 90 days by default, even when a broader sampling window is requested, and SHOULD expose this bound through a `--max-age` flag so operators can opt in to older samples when needed.
4. **R-SAMP-004**: For each run in the sample, derive the per-run metrics defined in Section 6.2.
5. **R-SAMP-005**: Record the count of runs with a successful conclusion separately from the total sampled count.

If the historical window yields zero sampled runs for a workflow, the implementation MUST:

- **R-SAMP-006**: Return `nil` (or a sentinel empty result) for that workflow's Monte Carlo projection.
- **R-SAMP-007**: Include the workflow in output with `sampled_runs: 0` and all projection fields set to zero.
- **R-SAMP-008**: SHOULD emit a warning indicating that no historical data is available for the workflow.

### 6.2 Per-Run Metric Derivation

For each sampled run, the implementation MUST derive:

| Metric | Source | Description |
|---|---|---|
| `aic` | `aw_info.json` artifact | Total AIC for this run as defined in the AI Credits Specification. |
| `duration_seconds` | Run start/end timestamps | Wall-clock duration of the run in seconds. |
| `success` | Run conclusion field | `true` if conclusion is `"success"`, `false` otherwise. |

#### 6.2.1 AI Credit Retrieval

AI Credits are obtained from locally-cached run summaries when available. The `gh aw logs` command stores a `run_summary.json` file for each processed run under `{output_dir}/run-{run_id}/`. During forecasting the implementation:

- **R-SAMP-010**: MUST attempt to load the cached `run_summary.json` for each sampled run using the default logs output directory (`.github/aw/logs`).
- **R-SAMP-011**: MUST extract the `TotalAIC` field from the cached `TokenUsage` summary when present.
- **R-SAMP-012**: If no cached summary exists or the AIC field is zero, the run's AIC contribution MUST be treated as zero and the run MUST still be counted in `sampled_runs`.  The implementation SHOULD log a debug-level warning.
- **R-SAMP-013**: When no `run_summary.json` is available, the implementation SHOULD consult a forecast-specific `forecast_aic.json` cache in the run directory before downloading the usage artifact, and after computing a positive AIC from a freshly downloaded artifact it SHOULD persist that value to `forecast_aic.json` (version-gated by CLI version) so repeated forecast runs reuse the cached AIC without re-scanning or re-parsing artifacts. A dedicated file is used because `run_summary.json` is a "fully processed" marker for `gh aw logs`/`audit`.

This lightweight approach avoids re-downloading artifacts while still providing accurate AIC observations for runs that have already been processed locally by `gh aw logs`.

#### 6.2.2 Duration Derivation

Duration MUST be computed as:

```
duration_seconds = run.updated_at − run.started_at
```

Both timestamps MUST be sourced from the GitHub Actions API run object. If either timestamp is zero or unavailable, the run's duration contribution SHOULD be treated as zero.

### 6.3 Observed Rate Computation

After sampling, the implementation MUST compute:

```
observed_runs_per_period = (sampled_run_count / history_days) × period_days
```

Where:
- `history_days` is the value of `--days`
- `period_days` is `7` for `"week"` and `30` for `"month"`

---

## 7. Monte Carlo Projection Engine

### 7.1 Overview

The Monte Carlo engine runs **10,000 independent simulation trials** per workflow to produce a probability distribution over projected AI Credit consumption in the next projection period. The engine models three independent sources of uncertainty per trial.

Implementations MUST use exactly 10,000 trials. The trial count is a normative requirement to ensure consistency of P10/P50/P90 estimates across implementations.

### 7.2 Uncertainty Sources

Each trial draws independently from three stochastic components:

#### 7.2.1 Run Count (Poisson Model)

The number of runs in the projection period is modeled as a Poisson random variable with rate parameter:

```
λ = observed_runs_per_period
```

The implementation MUST use:

- **Knuth's exact algorithm** when `λ ≤ 15`:

  ```
  L ← e^(−λ)
  k ← 0; p ← 1
  repeat:
    k ← k + 1
    p ← p × Uniform(0, 1)
  until p ≤ L
  return k − 1
  ```

- **Normal approximation** when `λ > 15`:

  ```
  k ← round(Normal(μ=λ, σ=sqrt(λ)))
  k ← max(0, k)
  ```

- **R-MC-001**: For `λ = 0`, the implementation MUST return a projected token total of 0 for that trial without invoking either algorithm.
- **R-FC-060**: Implementations MUST use `λ = 15` as the crossover threshold: Knuth's exact algorithm for `λ ≤ 15`, and Normal approximation only for `λ > 15`. Implementations MUST NOT raise this threshold above 15 without a specification revision, because the documented error and comparability assumptions are calibrated to this crossover.
- **R-MC-002**: `λ` MUST be derived from `observed_runs_per_period` using the formula in §3.7 and
  MUST be reused unchanged for every trial of the same workflow forecast. Implementations MUST NOT
  recalculate or modify `λ` within a single forecast run.
- **R-MC-003**: `λ` MUST be treated as a real-valued rate parameter. Implementations MUST NOT round,
  floor, or ceil `λ` before selecting the Poisson branch or before drawing the projected run count.
- **R-MC-004**: If the computed `λ` is negative, `NaN`, or otherwise non-finite, implementations
  MUST replace it with `0`, emit a warning, and continue in the same zero-projection mode required
  by **R-MC-001**.

#### 7.2.2 Per-Run Token Usage (Bootstrap Resampling)

Token usage per run is modeled empirically using bootstrap resampling:

- **R-MC-010**: For each run in a trial, the implementation MUST draw one observation uniformly at random **with replacement** from the set of historical AIC observations in the sample.
- **R-MC-011**: If the sample contains zero AIC observations (all runs had missing artifacts), the per-run token draw MUST return 0.

This non-parametric approach preserves the empirical distribution of token usage, including multi-modal distributions and heavy tails, without imposing a parametric form.

#### 7.2.3 Per-Run Success (Bernoulli Model)

Whether a given run in the trial succeeds is modeled as a Bernoulli draw:

```
P(success) = success_rate = successful_run_count / total_sampled_run_count
```

- **R-MC-020**: Each run in a trial MUST independently draw from `Bernoulli(success_rate)`.
- **R-MC-021**: Only successful runs contribute their token draw to the trial's projected total. Failed runs contribute zero tokens to the projection.
- **R-MC-022**: If `total_sampled_run_count = 0`, `success_rate` MUST be treated as 0. The implementation MUST return a zero projection for all trials.

### 7.3 Trial Aggregation

For a given trial with `k` drawn runs:

```
trial_tokens = Σ_{i=1}^{k} (success_i × token_draw_i)
```

Where:
- `success_i` is `1` if the Bernoulli draw for run `i` succeeds, `0` otherwise
- `token_draw_i` is the bootstrapped AIC observation for run `i`

### 7.4 Output Statistics

After completing all 10,000 trials, the implementation MUST compute and report:

| Statistic | Definition |
|---|---|
| `mean_projected_aic` | Arithmetic mean of all trial totals |
| `std_dev_aic` | Population or sample standard deviation of all trial totals |
| `p10_projected_aic` | 10th percentile of trial totals (lower bound of 80% CI) |
| `p50_projected_aic` | 50th percentile of trial totals (median projection) |
| `p90_projected_aic` | 90th percentile of trial totals (upper bound of 80% CI) |

Percentile computation MUST use the nearest-rank method or an equivalent method that produces results consistent with a 10,000-element sorted array.

The `projected_aic` top-level field MUST equal `p50_projected_aic`.

### 7.5 Nil Projection Condition

If no historical runs are available for a workflow, the implementation MUST return a nil (empty/zero) projection for that workflow. Nil projections MUST be represented in JSON output as zero values for all numeric Monte Carlo fields. The implementation MUST NOT run trials when the sample is empty.

### 7.6 Minimum Sample Size for Percentile Validity

The P10 and P90 estimates produced by the Monte Carlo engine are only statistically reliable when the bootstrap sample contains a sufficient number of distinct AIC observations.

- **R-MC-030**: Implementations SHOULD require a minimum of **10** AIC observations (i.e., runs with non-zero `aic`) before treating P10 and P90 as reliable estimates. When `n < 10`, implementations SHOULD emit a warning to stderr indicating that the confidence interval may be unreliable due to insufficient sample size. _Rationale: Bootstrap resampling with fewer than 10 observations produces percentile estimates that are highly sensitive to individual outliers. With n < 10, the P10 and P90 bounds collapse toward the single minimum and maximum observations, making the 80% confidence interval misleadingly precise. The threshold of 10 is consistent with standard statistical practice for non-parametric bootstrapping._
- **R-MC-031**: Implementations MUST still run the Monte Carlo simulation and return P10/P50/P90 values even when `n < 10`. The simulation MUST NOT be suppressed solely on the basis of sample size; the warning in **R-MC-030** is advisory only.
- **R-MC-032**: When `n = 0` (no AIC observations in the sample), the **Nil Projection Condition** in §7.5 applies and the simulation MUST NOT run. This is a separate condition from the low-sample warning.

---

## 8. Episode Analysis

### 8.1 Purpose

An **episode** is a logical grouping of one or more workflow runs that collectively represent a single task attempt. Episode analysis computes per-episode metrics to reveal how many runs, on average, are required to complete a task successfully.

### 8.2 Episode Construction

The implementation MUST group sampled runs into episodes using the `buildEpisodeData` and `classifyEpisode` engine:

- **R-EP-001**: Runs sharing the same `headSha` and `headBranch` MUST be grouped into the same episode.
- **R-EP-002**: Runs linked by `workflow_dispatch` or `workflow_call` relationships (reconstructed from cached run summaries) SHOULD be merged into the triggering run's episode.

#### 8.2.1 Limitations in Forecast Context

During forecasting, full artifact data may not be available for all sampled runs. When cached summary data is unavailable:

- **R-EP-010**: `workflow_dispatch`/`workflow_call` linkage MUST be omitted from episode construction.
- **R-EP-011**: The resulting `sampled_episodes` count MUST be treated as a **lower-bound estimate**. Implementations MUST communicate this limitation in output (e.g., via a note in console output or a boolean `episode_count_is_lower_bound` field in JSON).

For orchestrator workflows that primarily receive `workflow_call` triggers, the episode count underestimate may be significant. Implementations SHOULD emit a warning when the dominant trigger type is `workflow_call` or `workflow_dispatch`.

### 8.3 Episode Metrics

For each workflow, the implementation MUST compute:

| Metric | Definition |
|---|---|
| `sampled_episodes` | Count of distinct episodes identified in the sample |
| `runs_per_episode` | `sampled_run_count / sampled_episodes` |
| `avg_aic_per_episode` | Mean AIC summed across all runs within each episode |
| `observed_episodes_per_period` | `(sampled_episodes / history_days) × period_days` |

### 8.4 Episode Table Display

The implementation MUST display the episode analysis table in console output when any workflow in the result set has `runs_per_episode > 1.0`. The table SHOULD be omitted when all workflows have `runs_per_episode = 1.0` (one run per episode is the baseline and adds no additional information).

---

## 9. Output Formats

### 9.1 Console Table Output

When `--json` is not specified, the implementation MUST render a formatted console table to stderr with the following columns:

| Column | Description |
|---|---|
| `Workflow` | Workflow display name or identifier |
| `Sampled Runs` | Count of completed and in-progress runs included in the sample |
| `Success Rate` | Fraction of sampled runs concluding with `success`, formatted as a percentage; `N/A` when no runs were sampled |
| `Yield/Period` | Effective throughput rate (`success_rate × observed_runs_per_period`) formatted to one decimal place |
| `Avg AIC` | `avg_aic` formatted as K/M abbreviations (e.g. `12.5K`, `1.20M`); `-` when zero |
| `Proj. AIC (P50)` | Median projected AI Credits from Monte Carlo (P50), formatted as K/M abbreviations |
| `80% CI (P10–P90)` | Confidence interval range `p10–p90`, both formatted as K/M abbreviations |
| `Triggers` | Comma-separated list of active trigger event names from frontmatter (up to 3, remainder shown as `+N`) |

#### 9.1.1 Table Formatting Requirements

- **R-OUT-001**: Column widths MUST be auto-fitted to the widest value in each column.
- **R-OUT-002**: AIC values MUST be formatted as K/M abbreviations (e.g. `12.5K`, `1.20M`); raw integer values of zero MUST be rendered as `-`.
- **R-OUT-003**: Rows MUST be sorted by Monte Carlo P50 projected AI Credits in descending order; when Monte Carlo data is unavailable, sort by `projected_aic`.
- **R-OUT-004**: A workflow with zero sampled runs MUST appear in the table with `-` in projection columns and `N/A` in rate columns.
- **R-OUT-005**: When episode analysis is applicable (Section 8.4), a second table with episode metrics MUST be printed below the main table, separated by a blank line.

#### 9.1.2 Example Console Output

```
Workflow          Sampled Runs  Success Rate  Yield/Period  Avg AIC  Proj. AIC (P50)  80% CI (P10–P90)  Triggers
ci-doctor                   42         92%          35.4   12.5K         480.0K       430.0K–535.0K   pull_request, workflow_dispatch
daily-planner               18         89%          14.4    8.2K         131.0K       105.0K–158.0K   schedule
```

### 9.2 JSON Output Schema

When `--json` is specified, the implementation MUST emit a single JSON object to stdout conforming to the following schema. No additional content (banners, progress indicators, or table output) MUST be emitted to stdout. Diagnostic messages MAY be emitted to stderr.

#### 9.2.1 Root Object

```json
{
  "period": "<string>",
  "as_of": "<RFC 3339 timestamp>",
  "workflows": [ <WorkflowForecast>, ... ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `period` | string | MUST | Projection period: `"week"` or `"month"`. |
| `as_of` | string | MUST | ISO 8601 / RFC 3339 UTC timestamp at which the forecast was computed. |
| `workflows` | array | MUST | Ordered array of per-workflow forecast objects. MUST be sorted by `projected_aic` (P50) descending. |

#### 9.2.2 WorkflowForecast Object

```json
{
  "workflow_id": "<string>",
  "period": "<string>",
  "sampled_runs": <integer>,
  "history_days": <integer>,
  "observed_runs_per_period": <number>,
  "success_rate": <number>,
  "yield": <number>,
  "avg_aic": <number>,
  "avg_duration_seconds": <number>,
  "projected_aic": <number>,
  "active_triggers": [ "<string>", ... ],
  "concurrency_limit": <integer>,
  "monte_carlo": { <MonteCarlo> },
  "episode_analysis": { <EpisodeAnalysis> },
  "experiment_variants": [ <ExperimentVariant>, ... ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `workflow_id` | string | MUST | Workflow identifier as used in discovery. |
| `period` | string | MUST | Mirrors the root `period` field. |
| `sampled_runs` | integer | MUST | Number of runs included in the sample. |
| `history_days` | integer | MUST | Value of `--days` used for this forecast. |
| `observed_runs_per_period` | number | MUST | Extrapolated run rate for the projection period. |
| `success_rate` | number | MUST | Fraction of sampled runs that concluded successfully, in `[0.0, 1.0]`. |
| `yield` | number | MUST | Effective throughput rate: `success_rate × observed_runs_per_period`. |
| `avg_aic` | number | MUST | Mean AIC per sampled run. `0` when no AIC data is available. |
| `avg_duration_seconds` | number | MUST | Mean wall-clock duration per sampled run in seconds. |
| `projected_aic` | number | MUST | P50 Monte Carlo projection. Equals `monte_carlo.p50_projected_aic`. |
| `active_triggers` | array of strings | SHOULD | Trigger event types from workflow frontmatter. Empty array when frontmatter is unavailable. |
| `concurrency_limit` | integer | SHOULD | Concurrency group limit from frontmatter. `0` indicates unlimited or unavailable. |
| `monte_carlo` | object | MUST | Monte Carlo simulation results. See Section 9.2.3. |
| `episode_analysis` | object | SHOULD | Episode analysis results. See Section 9.2.4. |
| `experiment_variants` | array | MAY | A/B experiment variant breakdown. See Section 9.2.5. Empty array when frontmatter is unavailable or no experiments are configured. |

#### 9.2.3 MonteCarlo Object

```json
{
  "iterations": 10000,
  "mean_projected_aic": <number>,
  "std_dev_aic": <number>,
  "p10_projected_aic": <number>,
  "p50_projected_aic": <number>,
  "p90_projected_aic": <number>
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `iterations` | integer | MUST | Always `10000`. |
| `mean_projected_aic` | number | MUST | Arithmetic mean of trial totals. |
| `std_dev_aic` | number | MUST | Standard deviation of trial totals. |
| `p10_projected_aic` | number | MUST | 10th percentile of trial totals. |
| `p50_projected_aic` | number | MUST | 50th percentile (median) of trial totals. |
| `p90_projected_aic` | number | MUST | 90th percentile of trial totals. |

When `sampled_runs = 0`, all numeric fields in this object MUST be `0` and `iterations` MUST be `0`.

#### 9.2.4 EpisodeAnalysis Object

```json
{
  "sampled_episodes": <integer>,
  "episode_count_is_lower_bound": <boolean>,
  "runs_per_episode": <number>,
  "avg_aic_per_episode": <number>,
  "observed_episodes_per_period": <number>
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `sampled_episodes` | integer | MUST | Distinct episode count. Lower-bound estimate when artifact linkage is unavailable. |
| `episode_count_is_lower_bound` | boolean | SHOULD | `true` when episode linkage data is incomplete (for example, remote mode without artifacts); otherwise `false`. |
| `runs_per_episode` | number | MUST | Mean runs per episode. |
| `avg_aic_per_episode` | number | MUST | Mean AIC per episode. |
| `observed_episodes_per_period` | number | MUST | Extrapolated episode rate for the projection period. |

#### 9.2.5 ExperimentVariant Object

```json
{
  "experiment_name": "<string>",
  "variant": "<string>",
  "run_count": <integer>,
  "fraction": <number>
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `experiment_name` | string | MUST | Name of the A/B experiment from frontmatter. |
| `variant` | string | MUST | Variant identifier (e.g., `"control"`, `"treatment"`). |
| `run_count` | integer | MUST | Number of sampled runs assigned to this variant. |
| `fraction` | number | MUST | `run_count / sampled_runs` for this workflow; fraction in `[0.0, 1.0]`. |

#### 9.2.6 Complete JSON Example

```json
{
  "period": "month",
  "as_of": "2026-05-10T22:00:00Z",
  "workflows": [
    {
      "workflow_id": "ci-doctor",
      "period": "month",
      "sampled_runs": 42,
      "history_days": 30,
      "observed_runs_per_period": 38.5,
      "success_rate": 0.92,
      "yield": 0.92,
      "avg_aic": 12500,
      "avg_duration_seconds": 145.3,
      "projected_aic": 480000,
      "active_triggers": ["pull_request", "workflow_dispatch"],
      "concurrency_limit": 0,
      "monte_carlo": {
        "iterations": 10000,
        "mean_projected_aic": 481250,
        "std_dev_aic": 32000.5,
        "p10_projected_aic": 430000,
        "p50_projected_aic": 480000,
        "p90_projected_aic": 535000
      },
      "episode_analysis": {
        "sampled_episodes": 40,
        "episode_count_is_lower_bound": true,
        "runs_per_episode": 1.05,
        "avg_aic_per_episode": 13100,
        "observed_episodes_per_period": 36.7
      },
      "experiment_variants": [
        {
          "experiment_name": "model-selection",
          "variant": "control",
          "run_count": 21,
          "fraction": 0.5
        },
        {
          "experiment_name": "model-selection",
          "variant": "treatment",
          "run_count": 21,
          "fraction": 0.5
        }
      ]
    }
  ]
}
```

### 9.3 Output Ordering

- **R-OUT-010**: In both console and JSON output, workflows MUST be ordered by
  `projected_aic` (P50 value) in descending order.
- **R-OUT-011**: Workflows with zero projected tokens MUST appear after all workflows with non-zero projections.
- **R-OUT-012**: Among workflows with equal projected tokens, the ordering SHOULD be deterministic (e.g., alphabetical by workflow ID).
- **R-OUT-013**: JSON output SHOULD disclose episode lower-bound semantics by including
  `episode_analysis.episode_count_is_lower_bound` for each workflow. Console output SHOULD include
  a note when this field is `true`.

---

## 10. Error Handling

### 10.1 Authentication Errors

If the GitHub API returns an authentication error (HTTP 401 or 403):

- **R-ERR-001**: The implementation MUST emit a descriptive error message to stderr indicating the authentication failure and guidance on re-authenticating with `gh auth login`.
- **R-ERR-002**: The implementation MUST exit with code `2`.

### 10.2 API Rate Limiting

If the GitHub API returns a rate-limit response (HTTP 429 or a `X-RateLimit-Remaining: 0` header):

- **R-ERR-010**: The implementation SHOULD retry the request after the period indicated by the `X-RateLimit-Reset` header.
- **R-ERR-011**: The implementation MUST emit a warning to stderr when entering a rate-limit wait state.
- **R-ERR-012**: If retry is not feasible, the implementation MUST exit with a non-zero status and a message indicating the rate limit condition.

### 10.3 Partial Failures

When one or more workflows in the discovery set encounter individual errors (e.g., artifact download failure, API timeout for a specific workflow):

- **R-ERR-020**: The implementation MUST continue processing the remaining workflows rather than aborting the entire forecast.
- **R-ERR-021**: Workflows that encountered individual errors MUST appear in output with `sampled_runs: 0` and all projection fields zeroed.
- **R-ERR-022**: The implementation MUST emit a warning to stderr for each workflow that encountered an individual error.

### 10.4 No Workflows Discovered

If workflow discovery yields zero workflows:

- **R-ERR-030**: The implementation MUST emit a message to stderr indicating that no agentic workflows were found and describing the discovery mode used.
- **R-ERR-031**: The implementation MUST exit with code `3`.

### 10.5 Verbose Diagnostics

When `--verbose` is specified, the implementation SHOULD emit the following additional diagnostic information to stderr:

- The list of discovered workflows and their identifiers
- The number of runs fetched per workflow
- The number of runs with valid AIC data versus missing artifacts
- The computed `λ` (Poisson rate) for each workflow
- Timing information for API calls and simulation execution

### 10.6 Safeguards for API Rate-Limit During Sampling

When the GitHub API returns HTTP 429 or HTTP 403 (with a `X-RateLimit-Remaining: 0` header)
during `gh api` sampling calls (i.e., while fetching run lists or artifact data for individual
workflows):

- **R-ERR-040**: The implementation MUST apply an exponential-backoff retry strategy: the first
  retry MUST wait at least the number of seconds indicated by the `Retry-After` or
  `X-RateLimit-Reset` header (whichever is present and non-zero). If neither header is present,
  the implementation MUST wait at least 60 seconds before the first retry attempt.
- **R-ERR-041**: The implementation MUST retry the failed request at least once before treating
  the workflow as a partial failure. Implementations SHOULD retry up to 3 times with increasing
  backoff intervals.
- **R-ERR-042**: The implementation MUST emit a warning to stderr before each backoff wait
  period, including the workflow identifier, the HTTP status code received, and the estimated
  wait duration.
- **R-ERR-043**: If all retry attempts are exhausted and the request still fails, the
  implementation SHOULD fall back to partial-result mode: the affected workflow MUST be included
  in output with `sampled_runs: 0` and all projection fields set to zero, consistent with
  **R-ERR-021**. The implementation MUST NOT abort the entire forecast run due to a single
  workflow's rate-limit failure.
- **R-ERR-044**: When operating in partial-result mode due to rate-limit exhaustion, the
  implementation SHOULD include a `rate_limit_skipped` boolean field set to `true` in the
  workflow's JSON output entry so that callers can distinguish rate-limit-induced zero projections
  from genuine zero-activity workflows. This field is an **additive optional extension** first
  defined in Section 10.6; callers MUST treat its absence as equivalent to `false` (per
  §11.5 / **R-IMPL-041**, unknown fields in JSON output MUST be treated as ignorable).

### 10.7 Safeguards

#### 10.7.1 Threat Model

- **Credential scope abuse**: Over-scoped credentials could allow unauthorized repository access.
- **Artifact privacy leakage**: `aw_info.json` artifacts may contain operationally sensitive AIC
  metadata and prompt-adjacent context.
- **Rate-limit abuse**: Aggressive polling or unrestricted retries can amplify API pressure and
  trigger organizational throttling.

#### 10.7.2 Required Mitigations

- **Credential scope**: The forecast command accesses the GitHub Actions API using `gh` CLI
  credentials. Token permissions MUST include only the minimum required scope (`actions:read` for
  target repositories).
- **Artifact privacy**: Implementations MUST NOT log raw artifact payloads at default verbosity and
  SHOULD redact prompt-adjacent fields in diagnostic output.
- **Rate-limit abuse controls**: Implementations MUST implement bounded retry/backoff behavior and
  MUST stop retrying when the retry budget is exhausted.
- **Remote repository access**: When `--repo` targets a repository the caller does not own, the
  caller MUST have explicit read access. Implementations MUST NOT bypass repository access controls.
- **JSON output handling**: The JSON schema can expose model and usage topology; operators SHOULD
  treat it as internal data and apply least-privilege access controls.

#### 10.7.3 Residual Risk

Even with these safeguards, operators with valid read access can still infer workload intensity
from forecast outputs. This residual risk is accepted and MUST be managed through repository
visibility and access-governance controls.

---

## 11. Implementation Requirements

### 11.1 Randomness

- **R-IMPL-001**: The Monte Carlo engine MUST use a cryptographically seeded pseudorandom number generator (PRNG). Implementations MUST NOT use a fixed seed unless in test mode.
- **R-IMPL-002**: The PRNG MUST be seeded independently per forecast invocation to ensure different results on repeated calls.

### 11.2 Performance

- **R-IMPL-010**: The 10,000-trial simulation for a single workflow MUST complete within 500 milliseconds on a single CPU core with a sample size of 100 runs.
- **R-IMPL-011**: Multiple workflows SHOULD be forecasted concurrently where the runtime environment supports parallelism.
- **R-IMPL-012**: API calls for data sampling SHOULD be made concurrently across workflows, subject to GitHub API rate limit constraints.

### 11.3 Deterministic Output

- **R-IMPL-020**: Given a fixed sample and fixed PRNG seed (in test mode), the Monte Carlo output MUST be reproducible. This requirement applies to test and validation scenarios only; production invocations MUST use random seeds (R-IMPL-001).

### 11.4 Numeric Precision

- **R-IMPL-030**: All intermediate AIC computations MUST use 64-bit floating-point arithmetic (IEEE 754 double precision).
- **R-IMPL-031**: JSON serialization of numeric fields MUST NOT produce non-finite values (`NaN`, `+Inf`, `-Inf`). If a computation produces a non-finite value, it MUST be replaced with `0` and a warning MUST be emitted.
- **R-IMPL-032**: Implementations MUST NOT round projected AIC values in intermediate computations; rounding for display purposes MUST occur only at serialization time.

### 11.5 Accuracy Disclosure Behavior

Because forecasts are probabilistic estimates:

- **R-IMPL-040**: The implementation MUST emit a note to stderr on every non-JSON invocation indicating that all forecasts are estimates derived from historical samples and may be inaccurate (JSON callers are assumed to be automated pipelines that handle notices separately).
- **R-IMPL-041**: The JSON output schema MAY have new fields added in minor versions without notice. Callers MUST treat unknown fields as ignorable.

---

## 12. Compliance Testing

### 12.1 Test Suite Requirements

Test fixtures for the compliance tests are located in `specs/forecast-compliance-fixtures/`.
See `specs/forecast-compliance-fixtures/README.md` for instructions on running the test suite
and adding new fixtures.

#### 12.1.1 Command Interface Tests

- **T-FC-001**: Invocation with invalid `--days` value exits non-zero with descriptive error.
- **T-FC-002**: Invocation with invalid `--period` value exits non-zero with descriptive error.
- **T-FC-003**: Invocation with `--sample < 1` exits non-zero.
- **T-FC-004**: Invocation with invalid `--repo` format exits non-zero.
- **T-FC-005**: Unmatched `workflow_id` positional argument exits non-zero with identification of the unmatched value.

#### 12.1.2 Workflow Discovery Tests

- **T-FC-010**: Local mode: discovers workflows from `.github/workflows/*.lock.yml`.
- **T-FC-011**: Local mode: no lock files found exits with code `3`.
- **T-FC-012**: Remote mode: calls GitHub Actions API and matches workflow IDs case-insensitively.
- **T-FC-013**: Remote mode: missing frontmatter fields default to zero/empty without error.
- **T-FC-030**: Remote mode: on GitHub API rate-limit exhaustion during workflow discovery, the implementation backs off and emits a warning before continuing with caller-supplied workflow IDs as partial results.

#### 12.1.3 Data Sampling Tests

- **T-FC-020**: Sampling respects `--sample` limit.
- **T-FC-021**: Sampling respects `--days` historical window cutoff.
- **T-FC-022**: Run with missing `aw_info.json` artifact contributes zero AIC and is still counted in `sampled_runs`.
- **T-FC-023**: Workflow with zero sampled runs produces nil projection with zero fields.
- **T-FC-024**: An in-progress run with a non-zero token usage snapshot is represented as a partial observation.
- **T-AIC-006**: A run with total AI Credits of at least 1,000,000 is handled without overflow.

#### 12.1.4 Monte Carlo Engine Tests

- **T-FC-031**: With `λ ≤ 15`, Knuth's algorithm is used for Poisson draw (verifiable by seeded PRNG in test mode).
- **T-FC-032**: With `λ > 15`, Normal approximation is used; drawn value is non-negative.
- **T-FC-033**: With `λ = 0`, projected tokens is exactly `0` for all trials.
- **T-FC-034**: Bootstrap resampling draws with replacement from historical AIC observations.
- **T-FC-035**: Only successful Bernoulli draws contribute AIC to the trial total.
- **T-FC-036**: 10,000 trials are executed per workflow.
- **T-FC-037**: P10 ≤ P50 ≤ P90 for all non-zero projections.
- **T-FC-038**: `projected_aic` equals `p50_projected_aic`.
- **T-FC-039**: Boundary crossover: `λ = 15` uses Knuth's exact branch.
- **T-FC-040**: Boundary crossover: `λ > 15` uses Normal approximation branch.

#### 12.1.5 Episode Analysis Tests

- **T-FC-041**: Runs sharing `headSha` and `headBranch` are grouped into the same episode.
- **T-FC-042**: `runs_per_episode` equals `sampled_run_count / sampled_episodes`.
- **T-FC-043**: Episode table is printed in console output when any workflow has `runs_per_episode > 1`.
- **T-FC-044**: Episode table is suppressed when all workflows have `runs_per_episode = 1.0`.

#### 12.1.6 Output Format Tests

- **T-FC-050**: Console output contains all required columns.
- **T-FC-051**: JSON output is valid JSON conforming to the schema in Section 9.2.
- **T-FC-052**: JSON `as_of` field is a valid RFC 3339 UTC timestamp.
- **T-FC-053**: JSON `workflows` array is sorted by `projected_aic` descending.
- **T-FC-054**: No stdout output (other than JSON) when `--json` is specified.
- **T-FC-055**: Accuracy note emitted to stderr unless `--json` is specified.

### 12.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|---|---|---|---|
| Flag validation | T-FC-001–005 | 1 | Required |
| Local workflow discovery | T-FC-010–011 | 1 | Required |
| Remote workflow discovery | T-FC-012–013 | 2 | Required |
| Remote discovery rate-limit backoff and partial results | T-FC-030 | 2 | Required |
| Data sampling with limit and window | T-FC-020–021 | 1 | Required |
| Missing artifact graceful handling | T-FC-022 | 1 | Required |
| Nil projection for empty sample | T-FC-023 | 1 | Required |
| Partial observation for in-progress run | T-FC-024 | 1 | Required |
| Knuth Poisson algorithm (λ ≤ 15) | T-FC-031 | 1 | Required |
| Normal approximation (λ > 15) | T-FC-032 | 1 | Required |
| Zero-λ projection | T-FC-033 | 1 | Required |
| Bootstrap resampling | T-FC-034 | 1 | Required |
| Bernoulli success filtering | T-FC-035 | 1 | Required |
| 10,000 trial count | T-FC-036 | 1 | Required |
| Percentile ordering | T-FC-037 | 1 | Required |
| P50 field consistency | T-FC-038 | 1 | Required |
| λ crossover threshold enforcement | T-FC-039–040 | 1 | Required |
| Episode grouping | T-FC-041–042 | 2 | Required |
| Episode table display logic | T-FC-043–044 | 2 | Required |
| Console output columns | T-FC-050 | 1 | Required |
| JSON schema conformance | T-FC-051–054 | 2 | Required |
| Accuracy disclosure note | T-FC-055 | 1 | Required |

---

## 13. Norms

This section defines cross-cutting normative requirements that apply across sampling, projection, and
consumer-facing output behavior.

- **N-FC-001 (Minimum sample-size floor)**: Implementations MUST apply the minimum sample-size floor
  in §7.6 before presenting percentile projections as valid planning signals. If the floor is not
  met, implementations MUST still return output but MUST include the validity warning described in
  §7.6.
- **N-FC-002 (Projection immutability)**: Once a projection result is computed for a given input set
  and random seed policy, the emitted P10/P50/P90 values MUST be immutable within that command
  invocation and MUST be reused consistently across console and JSON output paths.
- **N-FC-003 (Caller percentile obligations)**: Callers consuming forecast output MUST treat P10, P50,
  and P90 as a set. Callers MUST NOT present P50 alone as a complete risk estimate and SHOULD include
  percentile spread when using forecast data for budgeting or promotion decisions.

---

## 14. Sync Notes

This section maps normative forecast requirements to implementation files.

| Normative Area | Implementation File(s) |
|---|---|
| §4.5 Exit codes | `pkg/cli/forecast_command.go` |
| §6 Data Sampling | `pkg/cli/forecast.go` |
| §7 Monte Carlo Projection Engine | `pkg/cli/forecast_montecarlo.go` |
| Monte Carlo engine (Poisson/Bootstrap/Bernoulli) | `pkg/cli/forecast_montecarlo.go` |
| Forecast command orchestration and output fields | `pkg/cli/forecast.go`, `pkg/cli/forecast_command.go` |
| Workflow discovery, rate-limit backoff, and run sampling | `pkg/cli/forecast.go` |
| Forecast compliance tests (including rate-limit backoff and λ thresholds) | `pkg/cli/forecast_montecarlo_test.go` |

Sync procedure:
1. Update this specification when changing projection algorithms or thresholds.
2. Update corresponding Go implementation/tests in the files above in the same change.
3. Re-run forecast tests to verify normative parity.

Sync follow-up tasks:

- **[Open]** Add and verify explicit AIC-primary / AIC-legacy migration assertions for forecast
  reporting paths so that AIC-facing surfaces remain compatibility-only where applicable.
- **[Open]** Track promotion-gate evidence until three confirmed production runs are documented in
  the Promotion Tracking table (§Status of This Document, criterion 1).
- **[Open]** Close the forecast compliance-test gap for promotion criterion 3 by recording sustained
  CI evidence covering both local and remote discovery paths over a full promotion review window.
- **[Resolved]** Expand forecast fixtures to cover invalid/non-finite `λ` derivation paths and
  zero-projection fallback behavior. Resolved in `pkg/cli/forecast_montecarlo_test.go` via
  `TestRunMonteCarloNonFiniteLambda` and `TestRunMonteCarloZeroLambdaFallback`.
- **[Resolved]** Add an implementation-level assertion that verbose diagnostics and JSON output are
  derived from the same `λ` value used by the Monte Carlo engine. Resolved in
  `pkg/cli/forecast_test.go` via `TestForecastWorkflow_LambdaConsistencyAcrossOutputFormats` and
  the existing `TestObservedRunsPerPeriodConsistency`. Closes
  [#31984](https://github.com/github/gh-aw/issues/31984).
- **[Resolved]** Re-review Appendix B whenever the Poisson branch threshold or
  `observed_runs_per_period` calculation changes. Resolved by adding a spec-cross-reference
  comment on `poissonNormalApproximationThreshold` in `pkg/cli/forecast_montecarlo.go` directing
  maintainers to Appendix B and R-FC-060. Closes
  [#31985](https://github.com/github/gh-aw/issues/31985).
- **[Resolved 2026-07-29]** R-IMPL-040 accuracy-note sync: The forecast command no longer
  emits an experimental-status warning. `pkg/cli/forecast_render.go` (`renderForecastTable`)
  emits an accuracy note — "All forecasts are estimates derived from historical samples and
  may be inaccurate." — via `console.FormatWarningMessage` on every non-JSON invocation. The
  note is suppressed for `--json` output (per R-IMPL-040).

---

## 15. Appendices

### Appendix A: Worked Example

#### A.1 Scenario

A workflow named `ci-doctor` has the following historical sample over 30 days:

- 42 completed runs
- 5 runs missing `aw_info.json` (treated as 0 AIC)
- AIC observations (for the 37 runs with artifacts): range from 8,000 to 18,000, mean ≈ 12,500
- 38 successful runs (yield = 38/42 ≈ 0.905)
- Projection period: `month` (30 days)

#### A.2 Observed Rate

```
observed_runs_per_period = (42 / 30) × 30 = 42.0
λ = 42.0
```

Since λ > 15, Normal approximation is used: `Normal(μ=42, σ=√42 ≈ 6.48)`.

#### A.3 Single Trial

Draw `k ~ round(Normal(42, 6.48)) = 44` (example).

For each of the 44 runs:
1. Draw success: `Bernoulli(0.905)` → say 40 succeed.
2. For each of the 40 successful runs, draw one AIC observation from the 37-item historical pool (bootstrap).
3. Sum the 40 AIC draws.

One trial might yield: 40 × 12,200 (average draw) ≈ 488,000 AIC.

#### A.4 After 10,000 Trials

Sorted trial totals (example summary):

```
P10 ≈ 415,000   (10th percentile — lower bound of 80% CI)
P50 ≈ 479,000   (median — headline projection)
P90 ≈ 545,000   (90th percentile — upper bound of 80% CI)
mean ≈ 481,000
std_dev ≈ 40,000
```

### Appendix B: Poisson Algorithm Selection Rationale

Knuth's exact Poisson algorithm is used for small λ (≤ 15) because it produces exact integer draws from the Poisson distribution without bias. For large λ, the Poisson distribution converges to a Normal distribution (`N(λ, λ)`), making the Normal approximation computationally efficient and sufficiently accurate.

The threshold of λ = 15 is chosen as the crossover point where Normal approximation error is below 1% for the tails relevant to P10/P90 computation. This fixed crossover is mandated by **R-FC-060** and MUST NOT be changed without a specification revision.

### Appendix C: Bootstrap Resampling Rationale

Traditional projection models assume a parametric distribution (e.g., log-normal) for per-run token usage. Agentic workflow token usage is frequently multi-modal (e.g., simple tasks versus complex multi-step tasks) and exhibits heavy tails due to recursive sub-agent chains. Bootstrap resampling avoids distributional misspecification by directly sampling from the empirical distribution, preserving these characteristics faithfully. The tradeoff is that projections are bounded by observed extremes; extrapolation beyond observed maximum AIC requires explicit assumption and is out of scope for this specification.

### Appendix D: Episode Count Lower-Bound Semantics

For orchestrator workflows that primarily use `workflow_call` or `workflow_dispatch` triggers, episodes are initiated by calls from another workflow rather than directly by GitHub events. These cross-workflow links are embedded in `aw_info.json` artifacts and are unavailable during forecasting when artifacts cannot be retrieved. As a result, each received `workflow_call` is counted as a separate episode, causing the episode count to overcount episodes and undercount the linkage. This means `runs_per_episode` may appear closer to `1.0` than its true value. Callers MUST treat `sampled_episodes` as a lower-bound estimate in this scenario and SHOULD note this limitation in any capacity planning documents.

### Appendix E: Workflow Discovery Race Conditions

Remote discovery is inherently eventually consistent. Between the workflow listing call and
subsequent run/artifact sampling calls, any workflow may be renamed, disabled, or deleted.

Conforming implementations SHOULD:

1. Use workflow IDs from the listing response as the stable key for subsequent requests.
2. Treat per-workflow 404/410 responses as recoverable partial failures.
3. Continue processing unaffected workflows and emit a warning for each raced workflow.

### Appendix F: Safeguards (Moved)

Safeguard requirements for this specification are now defined in §10.7.

---

## 16. References

### Normative References

- **[RFC 2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997. <https://www.ietf.org/rfc/rfc2119.txt>
- **[RFC 3339]** Klyne, G. and Newman, C., "Date and Time on the Internet: Timestamps", RFC 3339, July 2002. <https://www.ietf.org/rfc/rfc3339.txt>
- **[AIC-SPEC]** GitHub Agentic Workflows Team, "AI Credits Specification". [AI Credits-specification](/gh-aw/specs/ai-credits-specification/)
- **[EXP-SPEC]** GitHub Agentic Workflows Team, "A/B Experiments Specification". [experiments-specification](/gh-aw/experimental/experiments-specification/)

### Informative References

- **[KNUTH-TAOCP]** Knuth, D.E., "The Art of Computer Programming, Volume 2: Seminumerical Algorithms", 3rd edition. Section 3.4.1 (Poisson distribution generation algorithm).
- **[BOOTSTRAP]** Efron, B. and Tibshirani, R., "An Introduction to the Bootstrap", Chapman & Hall, 1993.
- **[GH-ACTIONS-API]** GitHub, "GitHub Actions REST API Reference". <https://docs.github.com/en/rest/actions>

---

## 17. Change Log

### Version 1.0.0 (Draft)

- Promoted the `gh aw forecast` command out of experimental status
- Removed the experimental-status stderr warning and `[EXPERIMENTAL]` command labels
- Replaced the experimental warning (R-IMPL-040) with an accuracy-disclosure note: "All forecasts are estimates derived from historical samples and may be inaccurate."
- Centralized console output explanations in the footer and removed user-facing "Monte Carlo" wording

### Version 0.1.0 (Experimental Draft)

- Updated remote discovery requirements with workflow-race mitigation guidance (R-DISC-014)
- Added optional JSON lower-bound disclosure field `episode_count_is_lower_bound` and recommendation R-OUT-013 (without reassigning existing R-OUT-010..012 semantics)
- Added Appendix F safeguards format (threat model, mitigations, residual risk)
- Initial specification for `gh aw forecast` command
- Defined command interface: flags `--days`, `--period`, `--sample`, `--repo`, `--json`, `--verbose`
- Defined local and remote workflow discovery modes
- Defined data sampling procedure and per-run metric derivation
- Defined Monte Carlo projection engine with Poisson + bootstrap algorithm
- Defined episode analysis with lower-bound semantics for orchestrator workflows
- Defined console table output format
- Defined JSON output schema (Sections 9.2.1–9.2.6)
- Defined error handling and exit codes
- Defined compliance test suite (T-FC-001 through T-FC-055)
- Added appendices: worked example, algorithm rationale, and safeguards

---

*Copyright © 2026 GitHub Agentic Workflows Team. All rights reserved.*
