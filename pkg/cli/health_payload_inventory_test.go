//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthPayloadSignalsSurviveInventoryBudgetExhaustion(t *testing.T) {
	stubHealthFailureCandidates(t)
	path := ".github/workflows/example.lock.yml"
	collection := &HealthCollection{workflows: []GitHubWorkflow{{Path: path}, {Path: ".github/workflows/other.lock.yml"}}}
	healthListFailureCandidates = func(opts ListWorkflowRunsOptions, page int, _ string, state *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		if page == 2 {
			stopHealthCollection(state, errHealthAPIBudget)
			return nil, time.Time{}, errHealthAPIBudget
		}
		batch := make([]workflowRunAPIResponse, opts.Limit)
		for i := range batch {
			batch[i] = workflowRunAPIResponse{ID: int64(i + 1), Path: path, Conclusion: "failure"}
		}
		return batch, time.Time{}, nil
	}
	runs, err := fetchWorkflowRuns("", "", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Empty(t, runs, "unverified payloads must not become confirmed health evidence")
	assert.Equal(t, healthMinimumRuns, collection.FailureCandidates)
	require.Len(t, collection.PotentialIssues, 1)
	assert.Equal(t, "unverified", collection.PotentialIssues[0].RepresentativeStatus)
	assert.True(t, collection.Partial)
	assert.False(t, collection.FailureListingComplete)
	assert.False(t, collection.FailurePassComplete)
	assert.Zero(t, collection.RevisionLookups)
}

func TestHealthJoblessCandidateDoesNotHideExecutedRepresentative(t *testing.T) {
	stubHealthFailureCandidates(t)
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	path := ".github/workflows/example.lock.yml"
	collection := &HealthCollection{Host: "github.com", workflows: []GitHubWorkflow{{Path: path}},
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		return []workflowRunAPIResponse{{ID: 1, Path: path, Conclusion: "failure"}, {ID: 2, Path: path, Conclusion: "failure"}}, time.Time{}, nil
	}
	jobs := 0
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.True(t, collection.FailureListingComplete, "complete the cheap inventory before any verification")
		jobs++
		if jobs == 1 {
			return []byte(`{"jobs":[]}`), nil
		}
		return []byte(`{"jobs":[{"id":42}]}`), nil
	}
	runs, err := collectHealthFailures("", "org/repo", collection)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(2), runs[0].DatabaseID)
	assert.Equal(t, 3, jobs, "two presence probes and one typed representative inventory")
	assert.Equal(t, "jobless", collection.PotentialIssues[0].RepresentativeStatus)
	assert.Equal(t, 2, collection.FailureCandidates)
}
