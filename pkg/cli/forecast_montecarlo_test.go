//go:build !integration

package cli

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deterministicRNG returns a seeded *rand.Rand for reproducible test results.
func deterministicRNG() *rand.Rand {
	return rand.New(rand.NewSource(42)) //nolint:gosec
}

// TestPoissonSample verifies that the Poisson sampler produces an empirical mean
// and variance close to lambda (within statistical tolerance for 100 000 draws).
func TestPoissonSample(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	const lambda = 10.0 // within Knuth's exact branch (≤15)
	const n = 100_000

	sum := 0.0
	sumSq := 0.0
	for range n {
		v := float64(poissonSample(rng, lambda))
		sum += v
		sumSq += v * v
	}
	mean := sum / n
	variance := sumSq/n - mean*mean

	// Poisson(λ): mean == λ, variance == λ.  Allow 1% relative error.
	assert.InEpsilon(t, lambda, mean, 0.01, "empirical mean should be close to lambda")
	assert.InEpsilon(t, lambda, variance, 0.01, "empirical variance should be close to lambda")
}

// TestPoissonSampleLargeLambda exercises the normal-approximation branch (lambda > 15).
func TestPoissonSampleLargeLambda(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	const lambda = 100.0
	const n = 100_000

	sum := 0.0
	for range n {
		sum += float64(poissonSample(rng, lambda))
	}
	mean := sum / n

	assert.InEpsilon(t, lambda, mean, 0.01, "normal-approximation branch should produce correct mean")
}

// TestPoissonSampleEdgeCases checks boundary conditions.
func TestPoissonSampleEdgeCases(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	assert.Equal(t, 0, poissonSample(rng, 0), "lambda=0 should return 0")
	assert.Equal(t, 0, poissonSample(rng, -5), "negative lambda should return 0")
}

func TestUseNormalApproximationForPoissonThreshold(t *testing.T) {
	t.Parallel()
	assert.False(t, useNormalApproximationForPoisson(poissonNormalApproximationThreshold), "lambda at threshold should use Knuth exact branch")
	assert.True(t, useNormalApproximationForPoisson(poissonNormalApproximationThreshold+0.0001), "lambda above threshold should use Normal approximation")
}

func TestKnuthAlgorithmUsedForLowLambda(t *testing.T) {
	t.Parallel()
	for _, lambda := range []float64{0.1, 1, 5, 15} {
		assert.False(t, useNormalApproximationForPoisson(lambda), "lambda=%v must use Knuth exact branch", lambda)
	}
}

func TestNormalApproximationForHighLambda(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	for _, lambda := range []float64{15.0001, 16, 100} {
		assert.True(t, useNormalApproximationForPoisson(lambda), "lambda=%v must use normal approximation", lambda)
		for range 100 {
			assert.GreaterOrEqual(t, poissonSample(rng, lambda), 0, "normal approximation must not return negative draws")
		}
	}
}

func TestZeroLambdaYieldsZeroSample(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	for range 100 {
		assert.Equal(t, 0, poissonSample(rng, 0), "lambda=0 must always draw zero runs")
	}
}

func TestLambdaCrossoverBoundaryAt15(t *testing.T) {
	t.Parallel()
	assert.False(t, useNormalApproximationForPoisson(15.0), "lambda==15 must use Knuth exact branch")
	assert.True(t, useNormalApproximationForPoisson(math.Nextafter(15.0, math.Inf(1))), "lambda>15 must use normal approximation")
}

// TestPercentileInt checks the int variant of the percentile helper.
func TestPercentileInt(t *testing.T) {
	t.Parallel()
	sorted := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	assert.Equal(t, 10, percentileInt(sorted, 10), "P10")
	assert.Equal(t, 50, percentileInt(sorted, 50), "P50")
	assert.Equal(t, 90, percentileInt(sorted, 90), "P90")
	assert.Equal(t, 0, percentileInt(nil, 50), "empty slice")
}

// TestMeanStdDevInt verifies the mean/stddev helper on a known distribution.
func TestMeanStdDevInt(t *testing.T) {
	t.Parallel()
	// population stddev of {2,4,4,4,5,5,7,9} = 2, mean = 5.
	xs := []int{2, 4, 4, 4, 5, 5, 7, 9}
	mean, stddev := meanStdDevInt(xs)
	assert.Equal(t, 5, mean, "mean")
	assert.InDelta(t, 2.0, stddev, 0.001, "population stddev")

	m0, s0 := meanStdDevInt(nil)
	assert.Equal(t, 0, m0)
	assert.InDelta(t, 0.0, s0, 0)
}

// TestRunMonteCarloNilOnEmpty verifies that runMonteCarlo returns nil for empty inputs.
func TestRunMonteCarloNilOnEmpty(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	assert.Nil(t, runMonteCarlo(nil, 0, 10.0, rng), "nil observations")
	assert.Nil(t, runMonteCarlo([]int{100, 200}, 2, 0.0, rng), "zero lambda")
	assert.Nil(t, runMonteCarlo([]int{100, 200}, 2, -1.0, rng), "negative lambda")
}

// TestRunMonteCarloNonFiniteLambda verifies that runMonteCarlo returns nil for
// non-finite λ inputs (NaN and +Inf) without hanging or panicking.
// Specification reference: R-MC-001 requires graceful handling of degenerate λ values.
func TestRunMonteCarloNonFiniteLambda(t *testing.T) {
	t.Parallel()
	obs := []int{1000, 2000, 3000}

	tests := []struct {
		name   string
		lambda float64
	}{
		{"NaN lambda", math.NaN()},
		{"+Inf lambda", math.Inf(1)},
		{"-Inf lambda", math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rng := deterministicRNG()
			result := runMonteCarlo(obs, len(obs), tt.lambda, rng)
			assert.Nil(t, result, "non-finite λ=%v should return nil (zero-projection fallback)", tt.lambda)
		})
	}
}

// TestRunMonteCarloZeroLambdaFallback verifies the zero-projection fallback behaviour
// (R-MC-001): when λ = 0 (observedRunsPerPeriod = 0), runMonteCarlo MUST return nil
// rather than producing a summary with zero projections, signalling to the caller that
// there are no runs to project.
func TestRunMonteCarloZeroLambdaFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		etObs                 []int
		successCount          int
		observedRunsPerPeriod float64
		wantNil               bool
	}{
		{
			name:                  "zero observedRunsPerPeriod returns nil",
			etObs:                 []int{1000, 2000, 3000},
			successCount:          3,
			observedRunsPerPeriod: 0.0,
			wantNil:               true,
		},
		{
			name:                  "negative observedRunsPerPeriod returns nil",
			etObs:                 []int{1000, 2000, 3000},
			successCount:          3,
			observedRunsPerPeriod: -0.001,
			wantNil:               true,
		},
		{
			name:                  "empty observations returns nil regardless of lambda",
			etObs:                 []int{},
			successCount:          0,
			observedRunsPerPeriod: 5.0,
			wantNil:               true,
		},
		{
			name:                  "positive lambda with observations returns non-nil",
			etObs:                 []int{1000, 2000, 3000},
			successCount:          3,
			observedRunsPerPeriod: 1.0,
			wantNil:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rng := deterministicRNG()
			result := runMonteCarlo(tt.etObs, tt.successCount, tt.observedRunsPerPeriod, rng)
			if tt.wantNil {
				assert.Nil(t, result, "expected nil for λ=%.4f with %d observations", tt.observedRunsPerPeriod, len(tt.etObs))
			} else {
				assert.NotNil(t, result, "expected non-nil for λ=%.4f with %d observations", tt.observedRunsPerPeriod, len(tt.etObs))
			}
		})
	}
}

// TestRunMonteCarloBasicProperties checks that the Monte Carlo summary satisfies
// statistical invariants (P10 ≤ P50 ≤ P90, mean ≥ 0, stddev ≥ 0).
func TestRunMonteCarloBasicProperties(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	// 20 historical runs, all successful, each using ~1 000 tokens.
	etObs := make([]int, 20)
	for i := range etObs {
		etObs[i] = 900 + i*10 // 900–1090
	}

	mc := runMonteCarlo(etObs, len(etObs), 10.0, rng)
	require.NotNil(t, mc)

	assert.Equal(t, monteCarloIterations, mc.Iterations)
	assert.GreaterOrEqual(t, mc.MeanProjectedAIC, 0.0)
	assert.GreaterOrEqual(t, mc.StdDevAIC, 0.0)
	assert.LessOrEqual(t, mc.P10ProjectedAIC, mc.P50ProjectedAIC, "AIC P10 ≤ P50")
	assert.LessOrEqual(t, mc.P50ProjectedAIC, mc.P90ProjectedAIC, "AIC P50 ≤ P90")
}

func TestTrialCountIsTenThousand(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	mc := runMonteCarlo([]int{1_000, 2_000, 3_000}, 3, 4.0, rng)
	require.NotNil(t, mc)
	assert.Equal(t, 10_000, monteCarloIterations)
	assert.Equal(t, 10_000, mc.Iterations)
}

func TestPercentileOrdering(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	mc := runMonteCarlo([]int{500, 1_000, 2_000, 4_000, 8_000}, 5, 12.0, rng)
	require.NotNil(t, mc)
	assert.LessOrEqual(t, mc.P10ProjectedAIC, mc.P50ProjectedAIC)
	assert.LessOrEqual(t, mc.P50ProjectedAIC, mc.P90ProjectedAIC)
}

// TestRunMonteCarloZeroSuccessRate verifies that a 0% success rate produces zero AIC.
func TestRunMonteCarloZeroSuccessRate(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	aicObs := []int{1000, 2000, 3000}
	// successCount = 0 → successRate = 0/3 = 0.
	mc := runMonteCarlo(aicObs, 0, 5.0, rng)
	require.NotNil(t, mc)
	assert.InDelta(t, 0.0, mc.P50ProjectedAIC, 1e-12, "zero success rate → zero AIC")
	assert.InDelta(t, 0.0, mc.P90ProjectedAIC, 1e-12, "zero success rate → zero AIC P90")
}

func TestBernoulliGatesETContribution(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	mc := runMonteCarlo([]int{1_000, 2_000, 3_000}, 0, 5.0, rng)
	require.NotNil(t, mc)
	assert.Zero(t, mc.MeanProjectedAIC)
	assert.Zero(t, mc.P50ProjectedAIC)
	assert.Zero(t, mc.P90ProjectedAIC)
}

func TestHighEffectiveTokensNoOverflow(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	mc := runMonteCarlo([]int{1_000_000, 1_250_000, 1_500_000}, 3, 20.0, rng)
	require.NotNil(t, mc)
	for _, got := range []float64{mc.MeanProjectedAIC, mc.StdDevAIC, mc.P10ProjectedAIC, mc.P50ProjectedAIC, mc.P90ProjectedAIC} {
		assert.False(t, math.IsNaN(got), "Monte Carlo output must not be NaN")
		assert.False(t, math.IsInf(got, 0), "Monte Carlo output must not overflow to infinity")
		assert.GreaterOrEqual(t, got, 0.0, "Monte Carlo output must be non-negative")
	}
}

func TestBootstrapDrawsWithReplacement(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	obs := []int{100, 200, 300}
	counts := map[int]int{}
	repeated := false
	for range 100 {
		draw := bootstrapAICObservation(rng, obs)
		counts[draw]++
		if counts[draw] > 1 {
			repeated = true
		}
	}

	assert.True(t, repeated, "bootstrap sampling with replacement must allow repeated draws")
	for _, observation := range obs {
		assert.Positive(t, counts[observation], "bootstrap sampling must draw from the full historical pool")
	}
}

// TestRunMonteCarloOrderOfMagnitude checks that the simulation mean is within
// 20% of the deterministic point estimate.
func TestRunMonteCarloOrderOfMagnitude(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	etObs := []int{10_000, 12_000, 11_000, 9_500, 10_500}
	successCount := 5
	observedRunsPerPeriod := 20.0

	mc := runMonteCarlo(etObs, successCount, observedRunsPerPeriod, rng)
	require.NotNil(t, mc)

	// Deterministic point estimate (ET).
	var totalAICMilli int
	for _, aic := range etObs {
		totalAICMilli += aic
	}
	avgAICMilli := totalAICMilli / len(etObs)
	pointEstimate := math.Round(observedRunsPerPeriod * (float64(avgAICMilli) / 1000))

	// Simulation mean should be within 20% of point estimate (with 100% success rate
	// and Poisson lambda = 20, the spread should be small).
	assert.InEpsilon(t, pointEstimate, mc.MeanProjectedAIC, 0.20,
		"simulation mean AIC should be close to point estimate")

	// P50 should also be within 20%.
	assert.InEpsilon(t, pointEstimate, mc.P50ProjectedAIC, 0.20,
		"simulation P50 AIC should be close to point estimate")

	// Confidence interval must bracket the mean.
	assert.LessOrEqual(t, mc.P10ProjectedAIC, mc.MeanProjectedAIC)
	assert.GreaterOrEqual(t, mc.P90ProjectedAIC, mc.MeanProjectedAIC)
}

// TestRunMonteCarloSortedOutputs verifies CI ordering holds across many random seeds.
func TestRunMonteCarloSortedOutputs(t *testing.T) {
	t.Parallel()
	etObs := []int{5_000, 7_000, 6_000, 4_500}
	for seed := range 5 {
		rng := rand.New(rand.NewSource(int64(seed))) //nolint:gosec
		mc := runMonteCarlo(etObs, len(etObs), 12.0, rng)
		require.NotNil(t, mc)
		assert.LessOrEqual(t, mc.P10ProjectedAIC, mc.P50ProjectedAIC)
		assert.LessOrEqual(t, mc.P50ProjectedAIC, mc.P90ProjectedAIC)
	}
}

// TestRunMonteCarloDistributionShape verifies that the ET distribution is roughly
// unimodal by checking that the mean lies between P10 and P90.
func TestRunMonteCarloDistributionShape(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	etObs := make([]int, 50)
	for i := range etObs {
		etObs[i] = 8_000 + i*40
	}
	mc := runMonteCarlo(etObs, len(etObs), 30.0, rng)
	require.NotNil(t, mc)

	assert.GreaterOrEqual(t, mc.MeanProjectedAIC, mc.P10ProjectedAIC, "mean ≥ P10")
	assert.LessOrEqual(t, mc.MeanProjectedAIC, mc.P90ProjectedAIC, "mean ≤ P90")
}

// TestGammaSampleMeanVariance verifies that gammaSample produces the expected mean
// (= shape) and variance (= shape) for a Gamma(shape, scale=1) distribution.
func TestGammaSampleMeanVariance(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	const shape = 5.5 // typical value: n+0.5 for n=5 observed runs
	const n = 200_000

	var sum, sumSq float64
	for range n {
		v := gammaSample(rng, shape)
		sum += v
		sumSq += v * v
	}
	mean := sum / n
	variance := sumSq/n - mean*mean

	// Gamma(shape, scale=1): mean = shape, variance = shape.  Allow 1% relative error.
	assert.InEpsilon(t, shape, mean, 0.01, "gamma empirical mean should equal shape")
	assert.InEpsilon(t, shape, variance, 0.01, "gamma empirical variance should equal shape")
}

// TestGammaSampleSmallShape verifies the shape < 1 reduction path for multiple
// fractional shape values (0.3, 0.5, 0.8) to ensure the recursive identity
// Gamma(shape) = Gamma(shape+1) × U^(1/shape) is exercised correctly.
func TestGammaSampleSmallShape(t *testing.T) {
	t.Parallel()
	const n = 200_000
	for _, shape := range []float64{0.3, 0.5, 0.8} {
		rng := deterministicRNG()
		var sum float64
		for range n {
			sum += gammaSample(rng, shape)
		}
		mean := sum / n
		assert.InEpsilon(t, shape, mean, 0.01,
			"gamma mean should equal shape for shape=%v", shape)
	}
}

// TestGammaSampleEdgeCases checks boundary and degenerate inputs.
func TestGammaSampleEdgeCases(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()
	assert.InDelta(t, 0.0, gammaSample(rng, 0), 0, "shape=0 → 0")
	assert.InDelta(t, 0.0, gammaSample(rng, -1), 0, "shape<0 → 0")
}

// TestRunMonteCarloIsReliable verifies that IsReliable reflects the minimum
// observation threshold.
func TestRunMonteCarloIsReliable(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()

	// Below threshold: 3 observations < minObservationsForReliableForecast (10).
	smallObs := []int{1000, 1500, 1200}
	mcSmall := runMonteCarlo(smallObs, len(smallObs), 4.0, rng)
	require.NotNil(t, mcSmall)
	assert.False(t, mcSmall.IsReliable, "fewer than 10 observations → IsReliable=false")

	// At threshold: exactly minObservationsForReliableForecast observations.
	atThreshold := []int{1000, 1100, 1200, 1300, 1400, 1500, 1600, 1700, 1800, 1900}
	mcAt := runMonteCarlo(atThreshold, len(atThreshold), 4.0, rng)
	require.NotNil(t, mcAt)
	assert.True(t, mcAt.IsReliable, "exactly 10 observations → IsReliable=true")

	// Well above threshold.
	largeObs := make([]int, 20)
	for i := range largeObs {
		largeObs[i] = 1000 + i*50
	}
	mcLarge := runMonteCarlo(largeObs, len(largeObs), 10.0, rng)
	require.NotNil(t, mcLarge)
	assert.True(t, mcLarge.IsReliable, "20 observations → IsReliable=true")
}

// TestRunMonteCarloGammaPoissonWiderCI verifies that the Gamma–Poisson compound model
// produces wider confidence intervals for small samples compared to a scenario where
// the rate is well-estimated (large sample).  With small n the posterior Gamma has
// higher relative variance, so the simulated ET distribution should be broader.
func TestRunMonteCarloGammaPoissonWiderCI(t *testing.T) {
	t.Parallel()
	// Same observed rate (λ = 10) but different sample sizes.
	etVal := 1_000 // constant ET to isolate run-count variability
	const lambda = 10.0

	// Small sample: 3 runs observed → high relative uncertainty in λ.
	smallObs := []int{etVal, etVal, etVal}
	rngSmall := rand.New(rand.NewSource(7)) //nolint:gosec
	mcSmall := runMonteCarlo(smallObs, len(smallObs), lambda, rngSmall)
	require.NotNil(t, mcSmall)

	// Large sample: 100 runs observed → low relative uncertainty in λ.
	largeObs := make([]int, 100)
	for i := range largeObs {
		largeObs[i] = etVal
	}
	rngLarge := rand.New(rand.NewSource(7)) //nolint:gosec
	mcLarge := runMonteCarlo(largeObs, len(largeObs), lambda, rngLarge)
	require.NotNil(t, mcLarge)

	ciSmall := mcSmall.P90ProjectedAIC - mcSmall.P10ProjectedAIC
	ciLarge := mcLarge.P90ProjectedAIC - mcLarge.P10ProjectedAIC

	assert.Greater(t, ciSmall, ciLarge,
		"small-sample CI (P90-P10=%d) should be wider than large-sample CI (%d)", ciSmall, ciLarge)
}

// TestRunMonteCarloFullEpisodePath is a smoke test that exercises runMonteCarlo
// with a realistic setup and validates ET percentile ordering.
func TestRunMonteCarloFullEpisodePath(t *testing.T) {
	t.Parallel()
	rng := deterministicRNG()

	// Simulate 30 completed runs with varied token counts.
	etObs := make([]int, 30)
	successCount := 0
	for i := range etObs {
		etObs[i] = 5_000 + i*200
		if i%5 != 0 { // 4 out of every 5 runs succeed → 80% success rate
			successCount++
		}
	}

	mc := runMonteCarlo(etObs, successCount, 8.0, rng)
	require.NotNil(t, mc)
	assert.Equal(t, monteCarloIterations, mc.Iterations)
	assert.Greater(t, mc.P90ProjectedAIC, mc.P10ProjectedAIC, "P90 > P10 for non-trivial inputs")

	// AIC percentiles should already be in ascending order.
	aics := []float64{mc.P10ProjectedAIC, mc.P50ProjectedAIC, mc.P90ProjectedAIC}
	sorted := make([]float64, len(aics))
	copy(sorted, aics)
	sort.Float64s(sorted)
	assert.Equal(t, aics, sorted, "AIC percentiles should already be in ascending order")
}

func TestResolveForecastWorkflowsFromRemote_RateLimitFallsBackToPartialResults(t *testing.T) {
	originalFetch := forecastFetchGitHubWorkflows
	originalSleep := forecastRateLimitSleep
	t.Cleanup(func() {
		forecastFetchGitHubWorkflows = originalFetch
		forecastRateLimitSleep = originalSleep
	})

	attempts := 0
	var backoffs []time.Duration
	forecastFetchGitHubWorkflows = func(_ context.Context, repoOverride string, verbose bool) (map[string]*GitHubWorkflow, error) {
		attempts++
		return nil, errors.New("API rate limit exceeded")
	}
	stderrReader, stderrWriter, err := os.Pipe()
	require.NoError(t, err, "Should create stderr pipe")
	originalStderr := os.Stderr
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		os.Stderr = originalStderr
	})

	forecastRateLimitSleep = func(_ context.Context, delay time.Duration) error {
		backoffs = append(backoffs, delay)
		return nil
	}

	names, err := resolveForecastWorkflowsFromRemote(context.Background(), []string{"ci-doctor", "daily-planner"}, "owner/repo", true)
	require.NoError(t, err, "T-FC-030 should return caller-supplied partial results after rate-limit retries")
	assert.Equal(t, []string{"ci-doctor", "daily-planner"}, names, "Should preserve caller-supplied workflow order")
	assert.Equal(t, forecastRateLimitMaxAttempts, attempts, "Should retry discovery until the retry budget is exhausted")
	assert.Equal(t, []time.Duration{
		forecastRateLimitBackoffDuration(1),
		forecastRateLimitBackoffDuration(2),
	}, backoffs, "Should back off between retry attempts")

	require.NoError(t, stderrWriter.Close(), "Should close stderr writer after the test call")
	stderrBytes, readErr := io.ReadAll(stderrReader)
	require.NoError(t, readErr, "Should read captured stderr")
	assert.Contains(t, string(stderrBytes), "partial results", "Should warn that discovery returned partial results")
}

func TestMonteCarloFixtureVariantsAreAvailable(t *testing.T) {
	t.Parallel()
	t.Run("minimal fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_minimal.json")
		run, ok := fixture["run"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "success", run["conclusion"])
	})

	t.Run("zero ET fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_zero_et.json")
		usage, ok := fixture["token_usage_summary"].(map[string]any)
		require.True(t, ok)
		totalEffectiveTokens, ok := usage["total_effective_tokens"].(float64)
		require.True(t, ok)
		assert.Zero(t, totalEffectiveTokens)
	})

	t.Run("failed run fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_failed.json")
		run, ok := fixture["run"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "failure", run["conclusion"])
	})

	t.Run("high ET fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_high_et.json")
		usage, ok := fixture["token_usage_summary"].(map[string]any)
		require.True(t, ok)
		totalEffectiveTokens, ok := usage["total_effective_tokens"].(float64)
		require.True(t, ok)
		assert.GreaterOrEqual(t, totalEffectiveTokens, 1_000_000.0)
	})

	t.Run("cancelled run fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_cancelled.json")
		run, ok := fixture["run"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "cancelled", run["conclusion"])
	})

	t.Run("partial ET fixture", func(t *testing.T) {
		t.Parallel()
		fixture := loadFixture(t, "run_summary_partial_et.json")
		run, ok := fixture["run"].(map[string]any)
		require.True(t, ok)
		assert.Empty(t, run["conclusion"])

		usage, ok := fixture["token_usage_summary"].(map[string]any)
		require.True(t, ok)
		totalEffectiveTokens, ok := usage["total_effective_tokens"].(float64)
		require.True(t, ok)
		assert.Positive(t, totalEffectiveTokens)
	})
}
