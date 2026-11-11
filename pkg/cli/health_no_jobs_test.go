//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthSamplingContinuesPastNoJobFailures(t *testing.T) {
	for _, conclusion := range []string{"failure", "timed_out", "cancelled", "startup_failure"} {
		t.Run(conclusion, func(t *testing.T) {
			originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
			t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
			}
			pages, jobChecks := 0, 0
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				switch {
				case strings.HasSuffix(endpoint, "/jobs?per_page=1"):
					jobChecks++
					assert.Contains(t, endpoint, "/attempts/2/jobs", "check the sampled attempt, not the latest one")
					return []byte(`{"total_count":0,"jobs":[]}`), nil
				case strings.Contains(endpoint, "/contents/"):
					assert.NotContains(t, endpoint, "unexecuted")
					if strings.Contains(endpoint, ".md?") {
						return []byte(strings.Repeat("c", 40)), nil
					}
					return []byte(`# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"settings","body_hash":"prompt"}`), nil
				default:
					pages++
					count := 10
					if pages == 2 {
						count = 5
						assert.Contains(t, endpoint, "page=2")
					}
					var runs []workflowRunAPIResponse
					for i := range count {
						run := workflowRunAPIResponse{ID: int64(pages*10 + i), Name: "Example",
							Path: ".github/workflows/example.lock.yml", Status: "completed",
							Conclusion: "success", Event: "schedule", HeadBranch: "main",
							HeadSHA: strings.Repeat("a", 40), RunAttempt: 1}
						if pages == 1 {
							run.Conclusion, run.HeadSHA, run.RunAttempt = conclusion, "unexecuted", 2
						}
						runs = append(runs, run)
					}
					out, err := json.Marshal(map[string]any{"workflow_runs": runs})
					require.NoError(t, err)
					return out, nil
				}
			}
			collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo"}
			runs, err := sampleHealthWorkflow(GitHubWorkflow{ID: 1, Name: "Example",
				Path: ".github/workflows/example.lock.yml"}, "2026-10-08", "org/repo", false, collection)
			require.NoError(t, err)
			assert.Equal(t, 2, pages)
			assert.Equal(t, 10, jobChecks)
			assert.Len(t, runs, 5)
			config := HealthConfig{Threshold: 80, LogsDir: t.TempDir(), RepoOverride: "org/repo", collection: collection}
			health := calculateCurrentWorkflowHealth("Example", runs, config)
			assert.Equal(t, "healthy", health.Status)
			assert.InDelta(t, 100.0, health.SuccessRate, 1e-9)
			clusters, plan := buildHealthFailureClusters(runs, config)
			assert.Empty(t, clusters)
			assert.Nil(t, plan)
		})
	}
}

func TestHealthRunHasJobsRequiresConfirmedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		apiError error
		wantJobs bool
		wantErr  string
	}{
		{name: "no jobs", response: `{"jobs":[]}`},
		{name: "executed failure", response: `{"jobs":[{"id":42}]}`, wantJobs: true},
		{name: "unrecognized job fields", response: `{"total_count":"unknown","jobs":[{"id":"new-format","steps":"unknown"}]}`, wantJobs: true},
		{name: "unrecognized step fields", response: `{"jobs":[{"steps":[{"number":"three","completed_at":{}}]}]}`, wantJobs: true},
		{name: "API unavailable", apiError: errors.New("HTTP 502"), wantErr: "failed to check jobs"},
		{name: "invalid JSON", response: `{`, wantErr: "failed to parse jobs"},
		{name: "null payload", response: `null`, wantErr: "missing jobs array"},
		{name: "missing jobs", response: `{}`, wantErr: "missing jobs array"},
		{name: "null jobs", response: `{"jobs":null}`, wantErr: "missing jobs array"},
		{name: "malformed jobs", response: `{"jobs":{}}`, wantErr: "failed to parse jobs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
			t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
			}
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				assert.Contains(t, args, "github.example.com")
				assert.Contains(t, args, "repos/org/repo/actions/runs/42/attempts/2/jobs?per_page=1")
				return []byte(tc.response), tc.apiError
			}
			collection := &HealthCollection{Host: "github.example.com", repoOverride: "github.example.com/org/repo"}
			hasJobs, err := healthRunHasJobs(WorkflowRun{DatabaseID: 42, Attempt: 2}, collection)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantJobs, hasJobs)
				assert.Empty(t, collection.jobs, "presence observations must not become typed failure evidence")
			}
			assert.Equal(t, 1, collection.APIRequests)
			if tc.wantErr != "" {
				healthRunGH = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
					return []byte(`{"jobs":[{"id":42}]}`), nil
				}
				hasJobs, err = healthRunHasJobs(WorkflowRun{DatabaseID: 42, Attempt: 2}, collection)
				require.NoError(t, err)
				assert.True(t, hasJobs, "an unsuccessful lookup must not be cached as no jobs")
				assert.Equal(t, 2, collection.APIRequests)
			}
		})
	}
}

func TestHealthJobPresenceCacheIsPerAttemptAndCollection(t *testing.T) {
	originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
	t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
	}
	calls := make(map[string]int)
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		assert.Contains(t, args, "github.example.com")
		endpoint := args[3]
		calls[endpoint]++
		if strings.Contains(endpoint, "/attempts/2/") {
			return []byte(`{"jobs":[{"id":42}]}`), nil
		}
		return []byte(`{"jobs":[]}`), nil
	}
	for range 2 {
		collection := &HealthCollection{Host: "github.example.com", repoOverride: "github.example.com/org/repo"}
		for _, attempt := range []int{0, 1, 2} {
			for range 2 {
				hasJobs, err := healthRunHasJobs(WorkflowRun{DatabaseID: 42, Attempt: attempt}, collection)
				require.NoError(t, err)
				assert.Equal(t, attempt == 2, hasJobs)
			}
		}
		assert.Equal(t, 3, collection.APIRequests, "cache both presence and absence within one collection")
	}
	assert.Equal(t, map[string]int{
		"repos/org/repo/actions/runs/42/jobs?per_page=1":            2,
		"repos/org/repo/actions/runs/42/attempts/1/jobs?per_page=1": 2,
		"repos/org/repo/actions/runs/42/attempts/2/jobs?per_page=1": 2,
	}, calls, "each new collection must check jobs again")
}

func TestHealthSamplingWaitingJobsPreservePendingRevision(t *testing.T) {
	for _, hasJobs := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval-only", true: "job-backed"}[hasJobs], func(t *testing.T) {
			originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
			t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
			}
			jobChecks := 0
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				switch {
				case strings.HasSuffix(endpoint, "/jobs?per_page=1"):
					jobChecks++
					assert.Contains(t, endpoint, "/runs/11/attempts/2/jobs?")
					if hasJobs {
						return []byte(`{"jobs":[{"id":42}]}`), nil
					}
					return []byte(`{"jobs":[]}`), nil
				case strings.Contains(endpoint, "/contents/"):
					if strings.Contains(endpoint, ".md?") {
						return []byte(strings.Repeat("c", 40)), nil
					}
					body := "new-prompt"
					if strings.Contains(endpoint, "ref="+strings.Repeat("b", 40)) {
						body = "old-prompt"
					}
					return []byte(`# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"settings","body_hash":"` + body + `"}`), nil
				default:
					var runs []workflowRunAPIResponse
					for i := range 6 {
						run := workflowRunAPIResponse{ID: int64(11 - i), Name: "Example",
							Path: ".github/workflows/example.lock.yml", Status: "completed", Conclusion: "success",
							Event: "schedule", HeadBranch: "main", HeadSHA: strings.Repeat("b", 40), RunAttempt: 1}
						if i == 0 {
							run.Status, run.Conclusion, run.HeadSHA, run.RunAttempt = "waiting", "", strings.Repeat("a", 40), 2
						}
						runs = append(runs, run)
					}
					out, err := json.Marshal(map[string]any{"workflow_runs": runs})
					require.NoError(t, err)
					return out, nil
				}
			}
			collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo"}
			runs, err := sampleHealthWorkflow(GitHubWorkflow{ID: 1, Name: "Example",
				Path: ".github/workflows/example.lock.yml"}, "2026-10-08", "org/repo", false, collection)
			require.NoError(t, err)
			assert.Equal(t, 1, jobChecks)
			health := calculateCurrentWorkflowHealth("Example", runs, HealthConfig{Threshold: 80, collection: collection})
			require.Len(t, health.Revisions, 1)
			if hasJobs {
				assert.Len(t, runs, 6)
				assert.Equal(t, "insufficient_evidence", health.Status)
				assert.Equal(t, 1, health.Revisions[0].Current.Pending)
				assert.Equal(t, []int64{11}, health.Revisions[0].Current.RunIDs)
			} else {
				assert.Len(t, runs, 5)
				assert.Equal(t, "no_current_revision_runs", health.Status)
				assert.Zero(t, health.Revisions[0].Current.Pending)
				assert.Empty(t, health.Revisions[0].Current.RunIDs)
			}
			assert.False(t, collection.Partial)
		})
	}
}

func TestHealthNoJobLookupHonorsRequestBudget(t *testing.T) {
	collection := &HealthCollection{Host: "github.com", APIRequests: healthMaximumAPIRequests}
	_, err := healthRunHasJobs(WorkflowRun{DatabaseID: 42, Attempt: 1}, collection)
	require.ErrorIs(t, err, errHealthAPIBudget)
	assert.True(t, collection.Partial)
	assert.Equal(t, "request_budget", collection.Reason)
}

func TestHealthOnlyNoJobFailuresRemainInsufficient(t *testing.T) {
	originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
	t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
	}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch {
		case args[3] == "repos/org/repo":
			return []byte(`{"full_name":"org/repo","default_branch":"main"}`), nil
		case strings.Contains(args[3], "/contents/.github/workflows?"):
			return []byte(`[{"path":".github/workflows/example.lock.yml","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","type":"file"}]`), nil
		case strings.Contains(args[3], "/git/blobs/"):
			return []byte("jobs: {}"), nil
		case strings.Contains(args[3], "/actions/workflows?"):
			return []byte(`{"workflows":[{"id":1,"name":"Example","path":".github/workflows/example.lock.yml","state":"active"}]}`), nil
		case strings.Contains(args[3], "/actions/runs?status=failure"):
			return []byte(`{"workflow_runs":[{"id":42,"path":".github/workflows/example.lock.yml","status":"completed","conclusion":"failure","run_attempt":1}]}`), nil
		case strings.Contains(args[3], "/actions/workflows/1/runs?"):
			return []byte(`{"workflow_runs":[{"id":42,"name":"Example","status":"completed","conclusion":"failure","run_attempt":1}]}`), nil
		default:
			assert.Contains(t, args[3], "/runs/42/attempts/1/jobs?")
			return []byte(`{"jobs":[]}`), nil
		}
	}
	output := captureAuditGroupStdout(t, func() {
		require.NoError(t, runHealth(context.Background(), HealthConfig{Days: 7, Threshold: 80, JSONOutput: true, RepoOverride: "org/repo"}))
	})
	var report HealthSummary
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Zero(t, report.HealthyWorkflows)
	assert.Zero(t, report.BelowThreshold)
	assert.Equal(t, 1, report.InsufficientWorkflows)
	require.Len(t, report.Workflows, 1)
	assert.Zero(t, report.Workflows[0].TotalRuns)
	assert.Equal(t, "no_recent_runs", report.Workflows[0].Status)
	assert.Empty(t, report.FailureClusters)
}
