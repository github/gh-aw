//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthDefaultBranchPaginationTruncationDoesNotCompletePass(t *testing.T) {
	stubHealthFailureCandidates(t)
	original := healthListWorkflowRuns
	t.Cleanup(func() { healthListWorkflowRuns = original })
	runs, collection := healthRevisionFixture()
	collection.DefaultBranch = "main"
	collection.workflows = []GitHubWorkflow{
		{ID: 1, Name: "Example", Path: runs[0].WorkflowPath},
		{ID: 2, Name: "Sparse", Path: ".github/workflows/sparse.lock.yml"},
	}
	defaultPages, laterWorkflows, branchPages := 0, 0, 0
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, _ int, state *HealthCollection) ([]WorkflowRun, int, error) {
		if opts.Ref == "" {
			branchPages++
			assert.False(t, state.DefaultBranchPassComplete)
			return nil, 0, nil
		}
		if opts.WorkflowName == "2" {
			laterWorkflows++
			return nil, 0, nil
		}
		defaultPages++
		*opts.OldestFetchedCreatedAt = runs[0].CreatedAt
		if defaultPages == 1 {
			return runs[:1], opts.Limit, nil
		}
		return nil, opts.Limit, nil
	}
	collected, err := fetchWorkflowRuns("", "2026-10-08", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Len(t, collected, 1, "retain evidence collected before the tied boundary")
	assert.Equal(t, 2*healthMaximumRunSearchResults/(2*healthMinimumRuns), defaultPages)
	assert.Equal(t, 1, laterWorkflows, "pagination truncation must not abort other default workflows")
	assert.Equal(t, 2, branchPages, "pagination truncation must not abort the later branch pass")
	assert.True(t, collection.Partial)
	assert.Equal(t, "pagination_limit", collection.Reason)
	assert.False(t, collection.stopped)
	assert.False(t, collection.DefaultBranchPassComplete)
	assert.True(t, collection.FailurePassComplete, "failure inventory completion is independent of chronological truncation")
}

func TestHealthOtherBranchSamplingReusesDefaultEvidence(t *testing.T) {
	stubHealthFailureCandidates(t)
	original := healthListWorkflowRuns
	t.Cleanup(func() { healthListWorkflowRuns = original })
	runs, collection := healthRevisionFixture()
	collection.DefaultBranch = "main"
	collection.workflows = []GitHubWorkflow{
		{ID: 1, Name: "Example", Path: runs[0].WorkflowPath},
		{ID: 2, Name: "Sparse", Path: ".github/workflows/sparse.lock.yml"},
	}
	var featureRuns []WorkflowRun
	for _, run := range runs[5:] {
		metadata := collection.revisions[healthRevisionLookupKey(run)]
		run.DatabaseID += 100
		run.WorkflowName, run.WorkflowPath, run.HeadBranch = "Sparse", collection.workflows[1].Path, "feature"
		collection.revisions[healthRevisionLookupKey(run)] = metadata
		featureRuns = append(featureRuns, run)
	}
	var calls []string
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, _ *HealthCollection) ([]WorkflowRun, int, error) {
		calls = append(calls, opts.Ref+":"+opts.WorkflowName)
		require.Equal(t, 1, page, "990 default-only runs must not require another exhaustive scan")
		if opts.WorkflowName == "1" {
			if opts.Ref != "" {
				return runs[5:], opts.Limit, nil
			}
			return nil, opts.Limit, nil
		}
		if opts.Ref == "" {
			return featureRuns, len(featureRuns), nil
		}
		return nil, 0, nil
	}
	collected, err := fetchWorkflowRuns("", "2026-10-08", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Equal(t, []string{"main:1", "main:2", ":1", ":2"}, calls)
	assert.Len(t, collected, 2*healthMinimumRuns, "retain default evidence once and visit the other workflow")
	assert.True(t, collection.DefaultBranchPassComplete)
	assert.False(t, collection.Partial)
}

func TestHealthDefaultEvidenceCannotCoverKnownOtherBranchFailureScope(t *testing.T) {
	originalFailure, originalList, originalGH := healthListFailureCandidates, healthListWorkflowRuns, healthRunGH
	t.Cleanup(func() {
		healthListFailureCandidates, healthListWorkflowRuns, healthRunGH = originalFailure, originalList, originalGH
	})
	runs, collection := healthRevisionFixture()
	collection.DefaultBranch = "main"
	collection.Host = "github.com"
	collection.APIRateLimit = &GitHubAPIRateLimitState{Remaining: 4000}
	collection.workflows = []GitHubWorkflow{{ID: 1, Name: "Example", Path: runs[0].WorkflowPath}}
	var featureRuns []WorkflowRun
	for _, run := range runs[5:] {
		metadata := collection.revisions[healthRevisionLookupKey(run)]
		run.DatabaseID += 100
		run.Event, run.HeadBranch = "workflow_dispatch", "feature"
		collection.revisions[healthRevisionLookupKey(run)] = metadata
		featureRuns = append(featureRuns, run)
	}
	failure := featureRuns[0]
	failure.DatabaseID, failure.Conclusion = 99, "failure"
	failure.CreatedAt, failure.UpdatedAt = runs[0].CreatedAt, runs[0].UpdatedAt
	collection.revisions[healthRevisionLookupKey(failure)] = collection.revisions[healthRevisionLookupKey(featureRuns[0])]
	healthListFailureCandidates = func(opts ListWorkflowRunsOptions, _ int, _ string, _ *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		if opts.Ref != "" {
			return nil, time.Time{}, nil
		}
		response := workflowRunAPIResponse{ID: failure.DatabaseID, Name: failure.WorkflowName, Path: failure.WorkflowPath,
			Event: failure.Event, HeadBranch: failure.HeadBranch, HeadSHA: failure.HeadSha, RunAttempt: failure.Attempt,
			Status: failure.Status, Conclusion: failure.Conclusion, CreatedAt: failure.CreatedAt, UpdatedAt: failure.UpdatedAt}
		response.Repository.FullName = failure.Repository
		return []workflowRunAPIResponse{response}, time.Time{}, nil
	}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		assert.Contains(t, args[3], "/runs/99/attempts/1/jobs")
		return []byte(`{"total_count":1,"jobs":[{"id":42}]}`), nil
	}
	var branchPages []int
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, _ *HealthCollection) ([]WorkflowRun, int, error) {
		if opts.Ref != "" {
			return runs[5:], opts.Limit, nil
		}
		branchPages = append(branchPages, page)
		switch page {
		case 1:
			return nil, opts.Limit, nil
		case 2:
			return featureRuns[:3], opts.Limit, nil
		case 3:
			return featureRuns[3:], opts.Limit, nil
		default:
			t.Fatalf("unexpected page %d", page)
			return nil, 0, nil
		}
	}
	collected, err := fetchWorkflowRuns("", "2026-10-08", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, branchPages)
	assert.Len(t, collected, 2*healthMinimumRuns+1)
	assert.True(t, collection.DefaultBranchPassComplete)
	assert.False(t, collection.Partial)
	assert.True(t, healthWorkflowFailureScopesCovered(featureRuns, failure.WorkflowPath, collection))
}

func TestHealthPRNamedDefaultBranchAlwaysUsesLaterPass(t *testing.T) {
	for _, event := range []string{"pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment", "push", "schedule", "workflow_dispatch"} {
		t.Run(event, func(t *testing.T) {
			run := WorkflowRun{HeadBranch: "main", Event: event}
			collection := &HealthCollection{DefaultBranch: "main", scanBranch: "main"}
			isPR := event != "push" && event != "schedule" && event != "workflow_dispatch"
			assert.Equal(t, !isPR, healthRunInScan(run, collection))
			collection.scanBranch = ""
			assert.Equal(t, isPR, healthRunInScan(run, collection))
			run.HeadBranch = "feature"
			assert.True(t, healthRunInScan(run, collection))
			collection.DefaultBranch = ""
			assert.True(t, healthRunInScan(run, collection), "legacy unprioritized sampling retains every branch")
		})
	}
}
