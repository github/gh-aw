package cli

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPagesForWorkflowRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runs  int
		pages int
	}{
		{runs: 0, pages: 0},
		{runs: 1, pages: 1},
		{runs: 100, pages: 1},
		{runs: 101, pages: 2},
		{runs: 1000, pages: 10},
	}
	for _, test := range tests {
		assert.Equal(t, test.pages, pagesForWorkflowRuns(test.runs))
	}
}

func TestForecastWorkflowRunAPIReportsDistribution(t *testing.T) {
	t.Parallel()
	forecast, runTrials, requestTrials := forecastWorkflowRunAPI(30, 120, rand.New(rand.NewSource(1)))
	require.NotNil(t, forecast)
	assert.Len(t, runTrials, monteCarloIterations)
	assert.Len(t, requestTrials, monteCarloIterations)
	assert.Equal(t, 100, forecast.PageSize)
	assert.Equal(t, 1000, forecast.FilteredSearchResultLimit)
	assert.LessOrEqual(t, forecast.ProjectedRuns.P10, forecast.ProjectedRuns.P50)
	assert.LessOrEqual(t, forecast.ProjectedRuns.P50, forecast.ProjectedRuns.P90)
	assert.LessOrEqual(t, forecast.RequestUnits.P10, forecast.RequestUnits.P50)
	assert.LessOrEqual(t, forecast.RequestUnits.P50, forecast.RequestUnits.P90)
	assert.Positive(t, forecast.RequestUnits.Mean)
	assert.Positive(t, forecast.RequestUnits.StdDev)
}

func TestForecastWorkflowRunAPIFlagsResultLimitRisk(t *testing.T) {
	t.Parallel()
	forecast, _, _ := forecastWorkflowRunAPI(30, 1200, rand.New(rand.NewSource(2)))
	require.NotNil(t, forecast)
	assert.True(t, forecast.MayExceedResultLimit)
	assert.Positive(t, forecast.ProbabilityExceedsResultLimit)
}

func TestAggregateWorkflowRunAPIUsesTrialSums(t *testing.T) {
	t.Parallel()
	first, firstRuns, firstRequests := forecastWorkflowRunAPI(30, 80, rand.New(rand.NewSource(3)))
	second, secondRuns, secondRequests := forecastWorkflowRunAPI(30, 90, rand.New(rand.NewSource(4)))
	aggregate := aggregateWorkflowRunAPI([]ForecastWorkflowResult{
		{WorkflowRunAPI: first, apiRunTrials: firstRuns, apiRequestTrials: firstRequests},
		{WorkflowRunAPI: second, apiRunTrials: secondRuns, apiRequestTrials: secondRequests},
	})
	require.NotNil(t, aggregate)
	assert.GreaterOrEqual(t, aggregate.RequestUnits.P50, first.RequestUnits.P50)
	assert.GreaterOrEqual(t, aggregate.RequestUnits.P50, second.RequestUnits.P50)
	assert.GreaterOrEqual(t, aggregate.ProjectedRuns.P50, first.ProjectedRuns.P50)
	assert.GreaterOrEqual(t, aggregate.ProjectedRuns.P50, second.ProjectedRuns.P50)
}
