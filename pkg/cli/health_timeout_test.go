//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthTimeoutFlag(t *testing.T) {
	cmd := NewHealthCommand()
	timeout, err := cmd.Flags().GetDuration("timeout")
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, timeout)
	require.NoError(t, cmd.ParseFlags([]string{"--timeout=0"}))
	timeout, err = cmd.Flags().GetDuration("timeout")
	require.NoError(t, err)
	assert.Zero(t, timeout)
	require.ErrorContains(t, RunHealth(HealthConfig{Timeout: -time.Second}), "--timeout must not be negative")
}

func TestHealthTimeBudgetCancelsInFlightRequests(t *testing.T) {
	for _, quota := range []bool{false, true} {
		name := "api"
		if quota {
			name = "quota"
		}
		t.Run(name, func(t *testing.T) {
			originalGH, originalRate := healthRunGH, fetchRateLimitForHostFunc
			t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = originalGH, originalRate })
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			collection := &HealthCollection{Host: "github.com", ctx: ctx}
			if !quota {
				collection.APIRateLimit = &GitHubAPIRateLimitState{Remaining: 4000}
			}
			calls := 0
			healthRunGH = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
				calls++
				<-ctx.Done()
				// exec.CommandContext can return a process error rather than ctx.Err().
				return []byte("response omitted"), errors.New("signal: killed")
			}
			fetchRateLimitForHostFunc = func(ctx context.Context, _ string) (rateLimitResource, error) {
				<-ctx.Done()
				return rateLimitResource{}, ctx.Err()
			}
			start := time.Now()
			_, err := healthAPI(collection, "repos/org/repo/actions/runs")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Less(t, time.Since(start), time.Second)
			assert.True(t, collection.Partial)
			assert.True(t, collection.stopped)
			assert.Equal(t, "time_budget", collection.Reason)
			assert.Contains(t, collection.Warning, "--timeout")
			expectedCalls := 1
			if quota {
				expectedCalls = 0
			}
			assert.Equal(t, expectedCalls, calls)
			_, err = healthAPI(collection, "repos/org/repo")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Equal(t, expectedCalls, calls, "expiry must prevent further API calls")
		})
	}
}

func TestHealthTimeBudgetReturnsPartialJSON(t *testing.T) {
	for _, stage := range []string{"registry", "failure_listing", "representative_jobs", "sampling"} {
		t.Run(stage, func(t *testing.T) {
			originalGH, originalRate := healthRunGH, fetchRateLimitForHostFunc
			t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = originalGH, originalRate })
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
			}
			healthRunGH = func(ctx context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				currentStage := "sampling"
				response := `{"workflow_runs":[]}`
				switch {
				case strings.Contains(endpoint, "/actions/workflows?"):
					currentStage = "registry"
					response = `{"workflows":[{"id":1,"name":"Example","path":".github/workflows/example.lock.yml","state":"active"}]}`
				case strings.Contains(endpoint, "status=failure"):
					currentStage = "failure_listing"
					response = `{"workflow_runs":[{"id":42,"name":"Example","path":".github/workflows/example.lock.yml","status":"completed","conclusion":"failure","event":"pull_request","run_attempt":1}]}`
				case strings.Contains(endpoint, "/jobs?per_page=1"):
					currentStage = "job_presence"
					if strings.Contains(endpoint, "per_page=100") {
						currentStage = "representative_jobs"
					}
					response = `{"jobs":[{"id":7,"name":"agent","status":"completed","conclusion":"failure"}],"total_count":1}`
				}
				if currentStage == stage {
					<-ctx.Done()
					return nil, errors.New("signal: killed")
				}
				return []byte(response), nil
			}
			start := time.Now()
			output := captureAuditGroupStdout(t, func() {
				require.NoError(t, RunHealth(HealthConfig{
					Days: 7, Threshold: 80, RepoOverride: "org/repo",
					JSONOutput: true, LogsDir: t.TempDir(), Timeout: 30 * time.Millisecond,
				}))
			})
			assert.Less(t, time.Since(start), time.Second)
			var report struct {
				Collection HealthCollection `json:"collection"`
				Workflows  []WorkflowHealth `json:"workflows"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &report))
			assert.True(t, report.Collection.Partial)
			assert.Equal(t, "time_budget", report.Collection.Reason)
			assert.Contains(t, report.Collection.Warning, "time budget exhausted")
			assert.InDelta(t, 0.03, report.Collection.TimeoutSeconds, 0.001)
			assert.GreaterOrEqual(t, report.Collection.ElapsedSeconds, 0.03)
			assert.Equal(t, stage == "sampling", report.Collection.FailurePassComplete)
			if stage == "representative_jobs" || stage == "sampling" {
				assert.Equal(t, 1, report.Collection.FailureCandidates)
				assert.Len(t, report.Collection.PotentialIssues, 1)
				require.Len(t, report.Workflows, 1)
				assert.Equal(t, 1, report.Workflows[0].TotalRuns, "collected runs must survive deadline expiry")
			}
		})
	}
}

func TestHealthTimeBudgetPropagatesToWorkQueueAndGit(t *testing.T) {
	original := healthReadWorkQueue
	t.Cleanup(func() { healthReadWorkQueue = original })
	healthReadWorkQueue = func(ctx context.Context, _ workqueue.Branch) ([]workqueue.QueueCommit, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	collection := &HealthCollection{Host: "github.com", ctx: ctx}
	start := time.Now()
	_, err := readHealthWorkQueueStats("org/repo", collection)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "time_budget", collection.Reason)
	_, err = runHealthGitContext(ctx, t.TempDir(), "rev-parse", "HEAD")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestHealthTimeBudgetPreservesAcceptedRunsFromInterruptedPage(t *testing.T) {
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	collection := &HealthCollection{
		Host: "github.com", repoOverride: "org/repo", ctx: ctx,
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
	}
	healthRunGH = func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(args[3], "/jobs?") {
			<-ctx.Done()
			return nil, errors.New("signal: killed")
		}
		return []byte(`{"workflow_runs":[
			{"id":1,"event":"pull_request","status":"completed","conclusion":"success"},
			{"id":2,"event":"pull_request","status":"completed","conclusion":"failure"}
		]}`), nil
	}
	runs, err := sampleHealthWorkflow(GitHubWorkflow{ID: 1, Name: "Example", Path: ".github/workflows/example.lock.yml"},
		"2026-10-24", "org/repo", false, collection)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(1), runs[0].DatabaseID)
	assert.Equal(t, "success", runs[0].Conclusion)
	assert.Equal(t, "time_budget", collection.Reason)
}

func TestHealthTimeBudgetSkipsNewCacheReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collection := &HealthCollection{ctx: ctx, startedAt: time.Now()}
	run := WorkflowRun{DatabaseID: 42}
	summary, _ := loadHealthRunSummary(run, HealthConfig{LogsDir: t.TempDir(), collection: collection})
	assert.Nil(t, summary)
	assert.Equal(t, "canceled", collection.CacheEvidence[42].Status)
	finishHealthCollection(collection)
	assert.Greater(t, collection.ElapsedSeconds, float64(0))
	assert.Equal(t, "canceled", collection.Reason)
}

func TestHealthCancellationIsNotTimeBudgetExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collection := &HealthCollection{ctx: ctx}
	_, err := healthAPI(collection, "repos/org/repo")
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "canceled", collection.Reason)
	assert.True(t, collection.Partial)
	assert.Zero(t, collection.APIRequests)
}
