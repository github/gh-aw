package cli

import (
	"math"
	"math/rand"
	"sort"
)

const (
	forecastWorkflowRunsPageSize    = 100
	forecastWorkflowRunsResultLimit = 1000
)

func forecastWorkflowRunAPI(observationCount int, expectedRuns float64, rng *rand.Rand) (*ForecastWorkflowRunAPI, []int, []int) {
	if observationCount == 0 || expectedRuns <= 0 {
		runTrials := make([]int, monteCarloIterations)
		requestTrials := make([]int, monteCarloIterations)
		for i := range requestTrials {
			requestTrials[i] = 1
		}
		return &ForecastWorkflowRunAPI{
			PageSize:                  forecastWorkflowRunsPageSize,
			FilteredSearchResultLimit: forecastWorkflowRunsResultLimit,
			ProjectedRuns:             summarizeForecastCountDistribution(runTrials),
			RequestUnits:              summarizeForecastCountDistribution(requestTrials),
		}, runTrials, requestTrials
	}

	runTrials := make([]int, monteCarloIterations)
	requestTrials := make([]int, monteCarloIterations)
	gammaShape := float64(observationCount) + 0.5
	gammaScale := expectedRuns / float64(observationCount)
	exceeds := 0
	for i := range monteCarloIterations {
		runs := poissonSample(rng, gammaSample(rng, gammaShape)*gammaScale)
		runTrials[i] = runs
		requestTrials[i] = pagesForWorkflowRuns(runs)
		if runs > forecastWorkflowRunsResultLimit {
			exceeds++
		}
	}

	projectedRuns := summarizeForecastCountDistribution(runTrials)
	return &ForecastWorkflowRunAPI{
		PageSize:                      forecastWorkflowRunsPageSize,
		FilteredSearchResultLimit:     forecastWorkflowRunsResultLimit,
		ProjectedRuns:                 projectedRuns,
		RequestUnits:                  summarizeForecastCountDistribution(requestTrials),
		ProbabilityExceedsResultLimit: float64(exceeds) / float64(monteCarloIterations),
		MayExceedResultLimit:          projectedRuns.P90 > forecastWorkflowRunsResultLimit,
	}, runTrials, requestTrials
}

func pagesForWorkflowRuns(runs int) int {
	if runs <= 0 {
		return 1
	}
	return (runs + forecastWorkflowRunsPageSize - 1) / forecastWorkflowRunsPageSize
}

func summarizeForecastCountDistribution(values []int) ForecastDistribution {
	if len(values) == 0 {
		return ForecastDistribution{}
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	mean, stddev := meanStdDevInt(sorted)
	return ForecastDistribution{
		Mean:   math.Round(float64(mean)*1000) / 1000,
		StdDev: math.Round(stddev*1000) / 1000,
		P10:    percentileInt(sorted, 10),
		P50:    percentileInt(sorted, 50),
		P90:    percentileInt(sorted, 90),
	}
}

func aggregateWorkflowRunAPI(results []ForecastWorkflowResult) *ForecastWorkflowRunAPI {
	runTrials := make([]int, monteCarloIterations)
	requestTrials := make([]int, monteCarloIterations)
	hasData := false
	for _, result := range results {
		if len(result.apiRequestTrials) == 0 {
			continue
		}
		hasData = true
		for i := range result.apiRequestTrials {
			runTrials[i] += result.apiRunTrials[i]
			requestTrials[i] += result.apiRequestTrials[i]
		}
	}
	if !hasData {
		return nil
	}

	probability := 0.0
	mayExceed := false
	for _, result := range results {
		if result.WorkflowRunAPI == nil {
			continue
		}
		probability = 1 - (1-probability)*(1-result.WorkflowRunAPI.ProbabilityExceedsResultLimit)
		mayExceed = mayExceed || result.WorkflowRunAPI.MayExceedResultLimit
	}
	return &ForecastWorkflowRunAPI{
		PageSize:                      forecastWorkflowRunsPageSize,
		FilteredSearchResultLimit:     forecastWorkflowRunsResultLimit,
		ProjectedRuns:                 summarizeForecastCountDistribution(runTrials),
		RequestUnits:                  summarizeForecastCountDistribution(requestTrials),
		ProbabilityExceedsResultLimit: probability,
		MayExceedResultLimit:          mayExceed,
	}
}
