//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubHealthFailureCandidates(t *testing.T) {
	t.Helper()
	original := healthListFailureCandidates
	t.Cleanup(func() { healthListFailureCandidates = original })
	healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		return nil, time.Time{}, nil
	}
}

func TestHealthCollectsFailuresBeforeSuccessEvidence(t *testing.T) {
	for _, exhaust := range []bool{false, true} {
		t.Run(strconv.FormatBool(exhaust), func(t *testing.T) {
			originalList, originalGH := healthListWorkflowRuns, healthRunGH
			t.Cleanup(func() { healthListWorkflowRuns, healthRunGH = originalList, originalGH })
			collection := &HealthCollection{
				Host: "github.com", repoOverride: "org/repo",
				APIRateLimit:    &GitHubAPIRateLimitState{Remaining: 4000},
				revisions:       make(map[string]*workflow.LockMetadata),
				headRevisions:   make(map[string]*workflow.LockMetadata),
				sourceRevisions: make(map[string]string),
				workflows: []GitHubWorkflow{
					{ID: 1, Name: "Alpha", Path: ".github/workflows/alpha.lock.yml"},
					{ID: 2, Name: "Zulu", Path: ".github/workflows/zulu.lock.yml"},
				},
			}
			start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
			var failed []workflowRunAPIResponse
			for i := range 2 {
				response := workflowRunAPIResponse{ID: int64(i + 1), Name: "Zulu", Path: collection.workflows[1].Path,
					Status: "completed", Conclusion: "failure", Event: "schedule", HeadBranch: "main",
					HeadSHA: strings.Repeat("a", 40), RunAttempt: 1, CreatedAt: start.Add(-time.Duration(i) * time.Hour)}
				failed = append(failed, response)
				collection.revisions[healthRevisionLookupKey(response.workflowRun())] = &workflow.LockMetadata{
					FrontmatterHash: "settings", BodyHash: "prompt",
				}
				metadata := collection.revisions[healthRevisionLookupKey(response.workflowRun())]
				collection.headRevisions[healthHeadRevisionKey(response.workflowRun())] = metadata
				source := strings.Repeat("c", 40)
				collection.sourceRevisions[healthSourceRevisionKey(response.workflowRun(), response.HeadSHA)] = source
				collection.sourceRevisions[healthSourceRevisionKey(response.workflowRun(), response.HeadBranch)] = source
			}
			failed = append(failed, workflowRunAPIResponse{ID: 99, Path: ".github/workflows/ci.yml", Conclusion: "failure"})
			var jobs []string
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				if strings.Contains(endpoint, "/actions/runs?status=failure") {
					assert.Contains(t, endpoint, "created=")
					out, err := json.Marshal(map[string]any{"workflow_runs": failed})
					require.NoError(t, err)
					return out, nil
				}
				if strings.HasSuffix(endpoint, "/jobs?per_page=1") {
					jobs = append(jobs, endpoint)
				}
				assert.NotContains(t, endpoint, "/runs/99/", "ordinary CI failures must not consume evidence requests")
				if exhaust && len(jobs) == 1 {
					collection.APIRequests = healthMaximumAPIRequests
				}
				return []byte(`{"jobs":[{"id":42}]}`), nil
			}
			var order []string
			healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, _ *HealthCollection) ([]WorkflowRun, int, error) {
				require.False(t, exhaust, "no successful evidence requests after budget exhaustion")
				require.True(t, collection.FailurePassComplete)
				require.Len(t, jobs, 1, "verify one representative per scope before sampling success evidence")
				assert.True(t, collection.FailureListingComplete)
				assert.Equal(t, 2, collection.FailureCandidates)
				assert.Equal(t, 1, collection.FailureCandidateWorkflows)
				require.Equal(t, 1, page)
				order = append(order, opts.WorkflowName)
				if opts.WorkflowName == "2" {
					return []WorkflowRun{failed[0].workflowRun(), failed[1].workflowRun()}, 2, nil
				}
				return nil, 0, nil
			}
			runs, err := fetchWorkflowRuns("", start.Add(-24*time.Hour).Format(time.RFC3339), "org/repo", false, collection)
			require.NoError(t, err)
			config := HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
			_, plan := buildHealthFailureClusters(runs, config)
			require.NotNil(t, plan, "failure evidence survives interruption before successful sampling")
			if exhaust {
				assert.True(t, collection.Partial)
				assert.False(t, collection.FailurePassComplete)
				assert.Len(t, runs, 1)
				assert.Equal(t, 1, plan.UniqueFailedRuns)
			} else {
				assert.Equal(t, []string{"2", "1"}, order, "failing workflows precede alphabetical success-only workflows")
				assert.Len(t, runs, 2, "failure and normal passes must not duplicate runs")
				assert.Equal(t, 2, plan.UniqueFailedRuns)
			}
		})
	}
}

func TestHealthFailureCandidatesPreserveBoundsAndValidateJSON(t *testing.T) {
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	for _, response := range []string{`{"workflow_runs":[]}`, `{}`, `not-json`} {
		collection := &HealthCollection{Host: "github.example.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
		opts := ListWorkflowRunsOptions{Limit: 100, StartDate: "2026-10-19T12:00:00Z", EndDate: "2026-10-20T12:00:00Z"}
		healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			assert.Equal(t, "github.example.com", args[2])
			endpoint, err := url.Parse(args[3])
			require.NoError(t, err)
			assert.Equal(t, "repos/org/repo/actions/runs", endpoint.Path)
			assert.Equal(t, "failure", endpoint.Query().Get("status"))
			assert.Equal(t, "3", endpoint.Query().Get("page"))
			assert.Equal(t, opts.StartDate+".."+opts.EndDate, endpoint.Query().Get("created"))
			return []byte(response), nil
		}
		_, _, err := listHealthFailureCandidates(opts, 3, "github.example.com/org/repo", collection)
		if response == `{"workflow_runs":[]}` {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "failed to parse health failure runs")
		}
	}

}

func TestHealthNamedWorkflowFailureListingIsScoped(t *testing.T) {
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	collection := &HealthCollection{Host: "github.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
		workflows: []GitHubWorkflow{{ID: 42, Path: ".github/workflows/example.lock.yml"}}}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		endpoint, err := url.Parse(args[3])
		require.NoError(t, err)
		assert.Equal(t, "repos/org/repo/actions/workflows/42/runs", endpoint.Path)
		assert.Equal(t, "failure", endpoint.Query().Get("status"))
		assert.Equal(t, "2026-10-19..2026-10-20", endpoint.Query().Get("created"))
		return []byte(`{"workflow_runs":[]}`), nil
	}
	_, _, err := listHealthFailureCandidates(ListWorkflowRunsOptions{Limit: 100, StartDate: "2026-10-19", EndDate: "2026-10-20"}, 1, "org/repo", collection)
	require.NoError(t, err)
	assert.Equal(t, 1, collection.APIRequests)
}

func TestHealthFailurePassSkipsSupersededScopes(t *testing.T) {
	stubHealthFailureCandidates(t)
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	var batch []workflowRunAPIResponse
	collection := &HealthCollection{
		Host: "github.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
		workflows: []GitHubWorkflow{{Path: ".github/workflows/example.lock.yml"}},
		revisions: make(map[string]*workflow.LockMetadata), headRevisions: make(map[string]*workflow.LockMetadata),
		sourceRevisions: make(map[string]string),
	}
	for i := range 10 {
		response := workflowRunAPIResponse{ID: int64(i + 1), Path: collection.workflows[0].Path,
			HeadSHA: strings.Repeat("a", 40), HeadBranch: "main", Event: "schedule", Conclusion: "failure"}
		if i == 1 {
			response.HeadBranch, response.HeadSHA = "feature", strings.Repeat("b", 40)
		}
		run := response.workflowRun()
		metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "prompt"}
		collection.revisions[healthRevisionLookupKey(run)] = metadata
		collection.headRevisions[healthHeadRevisionKey(run)] = metadata
		collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadBranch)] = strings.Repeat("c", 40)
		source := strings.Repeat("d", 40)
		if i == 1 {
			source = strings.Repeat("c", 40)
		}
		collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)] = source
		batch = append(batch, response)
	}
	healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		return batch, time.Time{}, nil
	}
	jobs := 0
	healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
		jobs++
		return []byte(`{"jobs":[{"id":42}]}`), nil
	}
	runs, err := collectHealthFailures("", "org/repo", collection)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "the run that proves a source boundary is not current failure evidence")
	assert.Equal(t, int64(2), runs[0].DatabaseID)
	assert.Equal(t, 3, jobs, "only the current feature representative adds a typed job request")
	assert.True(t, collection.FailurePassComplete)
	require.Len(t, collection.PotentialIssues, 1)
	assert.Equal(t, "superseded", collection.PotentialIssues[0].RepresentativeStatus)
}

func TestHealthFailureListingErrorIsNotSuccess(t *testing.T) {
	stubHealthFailureCandidates(t)
	healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		return nil, time.Time{}, errors.New("failure listing unavailable")
	}
	collection := &HealthCollection{workflows: []GitHubWorkflow{{Path: ".github/workflows/example.lock.yml"}}}
	_, err := fetchWorkflowRuns("", "", "", false, collection)
	require.ErrorContains(t, err, "failure listing unavailable")
	assert.False(t, collection.FailurePassComplete)
}

func TestHealthOptionalJobEnrichmentContinuesUnlessBudgetStops(t *testing.T) {
	originalGH, originalList, originalFailures := healthRunGH, healthListWorkflowRuns, healthListFailureCandidates
	t.Cleanup(func() {
		healthRunGH, healthListWorkflowRuns, healthListFailureCandidates = originalGH, originalList, originalFailures
	})
	for _, response := range []string{"HTTP 502", "malformed JSON", "malformed steps", "request budget", "rate limit"} {
		t.Run(response, func(t *testing.T) {
			path := ".github/workflows/example.lock.yml"
			collection := &HealthCollection{Host: "github.com",
				APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
				workflows:    []GitHubWorkflow{{ID: 1, Path: path}}}
			healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
				return []workflowRunAPIResponse{
					{ID: 1, Path: path, Conclusion: "failure", HeadBranch: "main", RunAttempt: 1},
					{ID: 2, Path: path, Conclusion: "failure", HeadBranch: "feature", RunAttempt: 1},
				}, time.Time{}, nil
			}
			var endpoints []string
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				endpoints = append(endpoints, endpoint)
				switch {
				case strings.HasSuffix(endpoint, "?per_page=1"):
					if response == "request budget" {
						collection.APIRequests = healthMaximumAPIRequests
					}
					return []byte(`{"jobs":[{"steps":"unknown"}]}`), nil
				case strings.Contains(endpoint, "/runs/1/"):
					switch response {
					case "malformed JSON":
						return []byte(`{`), nil
					case "malformed steps":
						return []byte(`{"jobs":[{"steps":"unknown"}]}`), nil
					case "rate limit":
						return []byte("API rate limit exceeded"), errors.New("HTTP 403")
					default:
						return nil, errors.New("HTTP 502")
					}
				default:
					return []byte(`{"total_count":1,"jobs":[{"name":"agent","conclusion":"failure"}]}`), nil
				}
			}
			chronologicalCalls := 0
			healthListWorkflowRuns = func(ListWorkflowRunsOptions, int, *HealthCollection) ([]WorkflowRun, int, error) {
				chronologicalCalls++
				return []WorkflowRun{{DatabaseID: 3, WorkflowPath: path, Conclusion: "success"}}, 1, nil
			}
			runs, err := fetchWorkflowRuns("", "", "org/repo", false, collection)
			require.NoError(t, err)
			assert.True(t, collection.Partial)
			assert.False(t, collection.JobEvidence[1].Complete)
			assert.True(t, collection.jobPresence[healthRunAttempt{id: 1, attempt: 1}])
			if response == "rate limit" || response == "request budget" {
				assert.True(t, collection.stopped)
				assert.Contains(t, []string{"rate_limit", "request_budget"}, collection.Reason)
				assert.False(t, collection.FailurePassComplete)
				require.Len(t, runs, 1, "retain the observed run at the stopping boundary")
				assert.Zero(t, chronologicalCalls)
				assert.LessOrEqual(t, len(endpoints), 2)
			} else {
				assert.False(t, collection.stopped)
				assert.True(t, collection.FailurePassComplete)
				assert.True(t, collection.JobEvidence[2].Complete)
				assert.Equal(t, "job_evidence_incomplete", collection.Reason)
				assert.Contains(t, collection.Warning, "run 1")
				assert.Contains(t, collection.Warning, "continuing")
				assert.Len(t, runs, 3, "retain both failure scopes and continue chronological sampling")
				assert.Equal(t, 1, chronologicalCalls)
				assert.Len(t, endpoints, 4, "one presence and one typed request per scope, with no retries")
			}
			clusters, plan := buildHealthFailureClusters(runs,
				HealthConfig{RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection})
			assert.NotEmpty(t, clusters)
			require.NotNil(t, plan)
			assert.Equal(t, 1, plan.MetadataOnlyRuns)
		})
	}
}

func TestHealthFailurePassIsBoundedAndKeepsHealthyChronologicalSample(t *testing.T) {
	stubHealthFailureCandidates(t)
	originalGH, originalList := healthRunGH, healthListWorkflowRuns
	t.Cleanup(func() { healthRunGH, healthListWorkflowRuns = originalGH, originalList })
	runs, collection := healthRevisionFixture()
	collection.Host = "github.com"
	collection.APIRateLimit = &GitHubAPIRateLimitState{Remaining: 4000}
	collection.workflows = []GitHubWorkflow{{ID: 1, Path: runs[0].WorkflowPath, Name: "Example"}}
	metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "fixed-prompt"}
	var failed []workflowRunAPIResponse
	for i := range 10 {
		response := workflowRunAPIResponse{ID: int64(100 + i), Path: runs[0].WorkflowPath, Name: "Example",
			Status: "completed", Conclusion: "failure", Event: "schedule", HeadBranch: "main", HeadSHA: "older",
			RunAttempt: 1, CreatedAt: runs[0].CreatedAt.Add(-time.Duration(i+1) * time.Hour)}
		response.Repository.FullName = runs[0].Repository
		failed = append(failed, response)
	}
	collection.revisions[healthRevisionLookupKey(failed[0].workflowRun())] = metadata
	for i := range runs {
		collection.revisions[healthRevisionLookupKey(runs[i])] = metadata
	}
	jobs := 0
	healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
		jobs++
		return []byte(`{"jobs":[{"id":42}]}`), nil
	}
	healthListFailureCandidates = func(ListWorkflowRunsOptions, int, string, *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		return failed, time.Time{}, nil
	}
	healthListWorkflowRuns = func(ListWorkflowRunsOptions, int, *HealthCollection) ([]WorkflowRun, int, error) {
		return runs, len(runs), nil
	}
	collected, err := fetchWorkflowRuns("", "", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Equal(t, 2, jobs, "one presence probe and one typed inventory per representative")
	config := HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
	health := calculateCurrentWorkflowHealth("Example", collected, config)
	assert.Equal(t, "healthy", health.Status)
	assert.Equal(t, healthMinimumRuns, health.SuccessCount)
	assert.Zero(t, health.FailureCount, "failure-first ordering must not replace the newest chronological sample")
	clusters, plan := buildHealthFailureClusters(collected, config)
	assert.Empty(t, clusters)
	assert.Nil(t, plan)
}

func TestHealthFailurePassPaginatesBeforeSuccessSampling(t *testing.T) {
	stubHealthFailureCandidates(t)
	collection := &HealthCollection{workflows: []GitHubWorkflow{{Path: ".github/workflows/example.lock.yml"}}}
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	calls := 0
	healthListFailureCandidates = func(opts ListWorkflowRunsOptions, page int, _ string, _ *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		calls++
		assert.Equal(t, 100, opts.Limit)
		if calls <= 10 {
			assert.Equal(t, calls, page)
			assert.Empty(t, opts.BeforeDate)
			return make([]workflowRunAPIResponse, 100), start, nil
		}
		assert.Equal(t, 1, page)
		assert.Equal(t, start.Add(time.Second).Format(time.RFC3339), opts.BeforeDate)
		return nil, time.Time{}, nil
	}
	runs, err := collectHealthFailures("", "org/repo", collection)
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Equal(t, healthMaximumFailureListingPages, calls)
	assert.False(t, collection.FailurePassComplete)
	assert.False(t, collection.FailureListingComplete)
	assert.True(t, collection.Partial)
	assert.Equal(t, "failure_listing_limit", collection.Reason)
}

func TestHealthMergedSamplesPreferChronologicalRuns(t *testing.T) {
	chronological := WorkflowRun{DatabaseID: 1, Conclusion: "success"}
	failure := WorkflowRun{DatabaseID: 1, Conclusion: "failure"}
	merged := mergeHealthRunSamples([]WorkflowRun{chronological}, []WorkflowRun{failure})
	require.Len(t, merged, 1)
	assert.Equal(t, "success", merged[0].Conclusion)
}

func TestHealthBranchPassesShareFailureListingBudget(t *testing.T) {
	originalFailures, originalRuns := healthListFailureCandidates, healthListWorkflowRuns
	t.Cleanup(func() {
		healthListFailureCandidates, healthListWorkflowRuns = originalFailures, originalRuns
	})
	collection := &HealthCollection{
		DefaultBranch: "trunk",
		workflows:     []GitHubWorkflow{{ID: 1, Path: ".github/workflows/example.lock.yml"}},
	}
	listings := 0
	healthListFailureCandidates = func(opts ListWorkflowRunsOptions, _ int, _ string, _ *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
		listings++
		assert.Equal(t, "trunk", opts.Ref, "the default branch consumes the shared listing budget first")
		return make([]workflowRunAPIResponse, opts.Limit), time.Now(), nil
	}
	var branches []string
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, _ int, _ *HealthCollection) ([]WorkflowRun, int, error) {
		branches = append(branches, opts.Ref)
		return nil, 0, nil
	}
	runs, err := fetchWorkflowRuns("", "", "org/repo", false, collection)
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Equal(t, healthMaximumFailureListingPages, listings, "the PR pass must not get another ten listing pages")
	assert.Equal(t, []string{"trunk", ""}, branches, "chronological sampling continues after the listing limit")
	assert.True(t, collection.Partial)
	assert.Equal(t, "failure_listing_limit", collection.Reason)
	assert.False(t, collection.FailureListingComplete)
	assert.False(t, collection.FailurePassComplete)
	assert.False(t, collection.DefaultBranchPassComplete)
}
