# Forecast Compliance Fixtures

This directory contains fixture files for the [Forecast Specification's §12 Compliance Testing](../../docs/src/content/docs/specs/forecast-specification.md#12-compliance-testing).

Forecast is a run-level AIC projection command. Episode analysis and yield were removed in
[#34750](https://github.com/github/gh-aw/pull/34750); the former episode requirements and tests
T-FC-041–T-FC-044 are retired, not missing fixture coverage. No episode fixtures are required.
The former P4 yield-formula predicate is replaced by
`TestFormal_P4_RunLevelOutputContract` (T-FC-056), which checks the serialized forecast
shape rather than a formula absent from the command. Console exclusions are checked by
`TestForecastTableOmitsRetiredMetrics`.

## Fixture Files

### `run_summary_minimal.json`

A minimal `run_summary.json` fixture conforming to the `RunSummary` schema used by `pkg/cli/`.
This fixture represents a single successful workflow run (`daily-report`) with:

- `conclusion: "success"` — the run is counted as successful in Bernoulli sampling
- `token_usage_summary.total_aic: 0.0054` — the AIC observation used in bootstrap resampling
- `run.updatedAt` and `run.startedAt` — used to compute `duration_seconds`

Use this fixture as the baseline for Monte Carlo engine compliance tests (**T-FC-031** through
**T-FC-040**) by loading it as a cached run summary.

## Formal Model

The forecast sample is the ordered prefix, capped by `--sample` when positive, of workflow runs
whose `startedAt` timestamp is within the `--days` historical window and whose status is either
`completed` or `in_progress`, excluding non-dispatched conclusions (`skipped`,
`action_required`). Missing usage artifacts are represented as zero-valued observations and remain
in the sample. In-progress runs with partial usage snapshots are included in the sample but do not
increment the Bernoulli success count unless their conclusion is `success`.

For a non-empty sample, the Monte Carlo engine runs exactly 10,000 trials. Each trial draws a run
count from `Poisson(λ)`, where `λ = sampled_runs / history_days × period_days`; the Poisson sampler
uses Knuth's exact algorithm for `λ ≤ 15` and a non-negative normal approximation for `λ > 15`.
Each simulated run gates contribution through a Bernoulli draw parameterized by the historical
success rate, then bootstraps an AIC observation with replacement from the sampled historical pool.
The output percentiles satisfy `P10 ≤ P50 ≤ P90`, and the top-level projected AIC is the Monte Carlo
P50.

When the sample is empty, the projection is nil and top-level projection fields remain zero.
High-valued observations (including AIC/AIC fixture values at or above the 1,000,000-token boundary)
must not produce NaN, infinity, or integer overflow in the simulation result.

## Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| `SampleLimit` (T-FC-020) | `TestSampleLimitRespected` | Table-driven check that `--sample` caps, no-ops when above total, and no-ops when zero |
| `DateWindowCutoff` (T-FC-021) | `TestDateWindowCutoffRespected` | Confirms only runs within the `--days` window are retained |
| `MissingArtifactZeroAIC` (T-FC-022) | `TestMissingArtifactContributesZeroAIC` | Edge case: missing artifact run counted in sample, contributes zero AIC |
| `EmptySampleNilProjection` (T-FC-023) | `TestEmptySampleProducesNilProjection` | Zero observations yields a nil Monte Carlo projection |
| `PartialObservation` (T-FC-024) | `TestInProgressRunIsPartialObservation` | Edge case: in-progress run with non-zero AIC is partial, not a Bernoulli success |
| `HighAICNoOverflow` (T-AIC-006) | `TestHighAICNoOverflow` | Edge case: high AIC/AIC observations do not panic, return NaN, or overflow to Inf |
| `KnuthForLowLambda` (T-FC-031) | `TestKnuthAlgorithmUsedForLowLambda` | lambda in {0.1,1,5,15} selects Knuth's exact algorithm |
| `NormalForHighLambda` (T-FC-032) | `TestNormalApproximationForHighLambda` | lambda > 15 selects normal approximation; sampled draws are non-negative |
| `ZeroLambdaZeroTokens` (T-FC-033) | `TestZeroLambdaYieldsZeroSample` | lambda=0 always yields exactly 0 |
| `BootstrapWithReplacement` (T-FC-034) | `TestBootstrapDrawsWithReplacement` | Bootstrap draws can repeat and cover the full historical pool |
| `BernoulliGatesET` (T-FC-035) | `TestBernoulliGatesETContribution` | successRate=0 forces mean projected AIC to exactly 0 |
| `TrialCountIs10000` (T-FC-036) | `TestTrialCountIsTenThousand` | Engine always executes exactly 10,000 trials |
| `PercentileOrdering` (T-FC-037) | `TestPercentileOrdering` | P10 <= P50 <= P90 holds for a non-trivial observation set |
| `P50FieldConsistency` (T-FC-038) | `TestProjectedTokensEqualsP50` | projected AIC is defined as, and equal to, Monte Carlo P50 |
| `LambdaCrossoverAt15` (T-FC-039/040) | `TestLambdaCrossoverBoundaryAt15` | Boundary: lambda==15 uses Knuth; lambda>15 uses normal approximation |
| `RunLevelOutputContract` (T-FC-056) | `TestFormal_P4_RunLevelOutputContract`, `TestForecastTableOmitsRetiredMetrics` | JSON excludes `yield` and `episode_analysis`; console excludes yield and episode tables |

## How to Run Compliance Tests

The forecast compliance tests are located in `pkg/cli/forecast_montecarlo_test.go`,
`pkg/cli/forecast_test.go`, and `pkg/cli/forecast_compliance_fixtures_formal_test.go`.

To run the full forecast compliance test suite:

```bash
go test -v -run "TestForecast|TestMonteCarlo|TestFormal" ./pkg/cli/
```

To run only the Monte Carlo engine tests (covering T-FC-031–T-FC-040):

```bash
go test -v -run "TestMonteCarlo" ./pkg/cli/
```

To run with the race detector (recommended for CI):

```bash
go test -race -run "TestForecast|TestMonteCarlo" ./pkg/cli/
```

To run the formal predicates listed above:

```bash
go test -v -run "TestSampleLimit|TestDateWindow|TestMissingArtifact|TestEmptySample|TestInProgressRun|TestHighAIC|TestKnuthAlgorithm|TestNormalApproximation|TestZeroLambda|TestBootstrapDraws|TestBernoulliGates|TestTrialCount|TestPercentileOrdering|TestProjectedTokens|TestLambdaCrossover" ./pkg/cli/
```

## Fixture Schema Reference

The `run_summary_minimal.json` fixture follows the `RunSummary` struct defined in
`pkg/cli/logs_models.go`. Key fields used by the forecast command:

| JSON Field | Go Field | Forecast Usage |
|---|---|---|
| `run.conclusion` | `Run.Conclusion` | Bernoulli success probability |
| `run.updatedAt` | `Run.UpdatedAt` | Duration computation |
| `run.startedAt` | `Run.StartedAt` | Duration computation |
| `token_usage_summary.total_aic` | `TokenUsage.TotalAIC` | Bootstrap AIC sample |
| `run_id` | `RunID` | Run identification |

## Adding New Fixtures

To add a fixture covering a specific compliance scenario:

1. Copy `run_summary_minimal.json` and modify the relevant fields.
2. Name the fixture descriptively (e.g., `run_summary_zero_aic.json` for T-FC-022).
3. Document the fixture purpose and the test IDs it covers in this README.

### Available Additional Fixtures

| Fixture Name | Purpose | Test IDs |
|---|---|---|
| `run_summary_zero_aic.json` | Run with missing/zero AIC (artifact not downloaded) | [T-FC-022](../../docs/src/content/docs/specs/forecast-specification.md#1213-data-sampling-tests) |
| `run_summary_failed.json` | Run with `conclusion: "failure"` for Bernoulli sampling | [T-FC-035](../../docs/src/content/docs/specs/forecast-specification.md#1214-monte-carlo-engine-tests) |
| `run_summary_high_aic.json` | Run with very high AIC (≥ 1,000,000) for overflow checks | [T-AIC-006](../../docs/src/content/docs/specs/forecast-specification.md#1213-data-sampling-tests) |
| `run_summary_cancelled.json` | Run with `conclusion: "cancelled"` (included in sample but not a Bernoulli success; AIC is zero because the run did not complete) | [T-FC-035](../../docs/src/content/docs/specs/forecast-specification.md#1214-monte-carlo-engine-tests) |
| `run_summary_partial_aic.json` | In-progress run with a non-zero token usage snapshot | [T-FC-024](../../docs/src/content/docs/specs/forecast-specification.md#1213-data-sampling-tests) |

Sync note: `T-FC-022`, `T-FC-024`, `T-FC-035`, and `T-AIC-006` still point to the canonical forecast specification anchors for §12.1.3 Data Sampling Tests and §12.1.4 Monte Carlo Engine Tests. Their fixture assertions are covered by `TestMonteCarloFixtureVariantsAreAvailable` in `pkg/cli/forecast_montecarlo_test.go`, alongside the `TestForecast*` command-path coverage in `pkg/cli/forecast_test.go`. `TestFormal_ForecastSpecSyncNoteAnchorsExist` in `pkg/cli/forecast_compliance_fixtures_formal_test.go` mechanically fails if either anchor heading moves or is renamed in `forecast-specification.md`, so this note no longer needs manual re-dating.

## Norms

Fixture filenames use `run_summary_<scenario>.json` with a concise, descriptive scenario name. Keep each fixture deterministic and limited to the fields needed by its test. Every fixture's test ID and behavioral claim must link to the governing [Forecast Specification §12](../../docs/src/content/docs/specs/forecast-specification.md#12-compliance-testing), using the subsection anchor when one is defined.

## Safeguards

Fixtures must contain synthetic, non-identifying values only. Never include credentials, tokens, personal information, production run data, or copied live workflow logs. Use fabricated run identifiers and repository/workflow names when a test requires them.

## Change Log

### 2026-11-04

- Added fixture naming and data-safety norms and linked the fixture index directly to the governing forecast compliance section.
