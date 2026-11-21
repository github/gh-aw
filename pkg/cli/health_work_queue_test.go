//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthWorkflowUsesWorkQueue(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		enabled       bool
		invalid       bool
	}{
		{"none", "jobs: {}", false, false},
		{"root", "env:\n  GH_AW_WORK_QUEUE_ENABLED: 'true'", true, false},
		{"job", "jobs:\n  agent:\n    env:\n      GH_AW_WORK_QUEUE_ENABLED: 'true'", true, false},
		{"step", "jobs:\n  activation:\n    steps:\n      - env:\n          GH_AW_WORK_QUEUE_ENABLED: true", true, false},
		{"disabled", "env:\n  GH_AW_WORK_QUEUE_ENABLED: 'false'", false, false},
		{"comment", "# GH_AW_WORK_QUEUE_ENABLED: true\njobs: {}", false, false},
		{"prompt", "jobs:\n  agent:\n    steps:\n      - run: |\n          GH_AW_WORK_QUEUE_ENABLED: true", false, false},
		{"invalid", "jobs: [", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, err := healthWorkflowUsesWorkQueue([]byte(tc.content))
			assert.Equal(t, tc.enabled, enabled)
			assert.Equal(t, tc.invalid, err != nil)
		})
	}
}

func healthWorkQueueFixture(t *testing.T, definitions ...string) *HealthCollection {
	t.Helper()
	originalGH, originalRead := healthRunGH, healthReadWorkQueue
	t.Cleanup(func() { healthRunGH, healthReadWorkQueue = originalGH, originalRead })
	collection := &HealthCollection{
		Host: "github.example.com", repoOverride: "github.example.com/owner/repo",
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 5000}, localGitChecked: true,
		fileSnapshots: map[string]healthFileSnapshot{"trunk": {files: map[string]string{}}},
	}
	contents := map[string]string{}
	for index, content := range definitions {
		workflowPath := fmt.Sprintf(".github/workflows/worker-%d.lock.yml", index)
		sha := fmt.Sprintf("%040x", index+1)
		collection.workflows = append(collection.workflows, GitHubWorkflow{Name: fmt.Sprintf("worker-%d", index), Path: workflowPath, State: "active"})
		collection.fileSnapshots["trunk"].files[workflowPath] = sha
		contents["repos/owner/repo/git/blobs/"+sha] = content
	}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.GreaterOrEqual(t, len(args), 4)
		require.Equal(t, []string{"api", "--hostname", "github.example.com"}, args[:3])
		if args[3] == "repos/owner/repo" {
			return []byte(`{"full_name":"owner/repo","default_branch":"trunk"}`), nil
		}
		if content, ok := contents[args[3]]; ok {
			require.Equal(t, []string{"--jq", ".content | @base64d"}, args[4:])
			return []byte(content), nil
		}
		t.Fatalf("unexpected health API request: %v", args)
		return nil, errors.New("unexpected request")
	}
	healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
		t.Fatal("queue must not be read")
		return workqueue.Snapshot{}, errors.New("unexpected queue read")
	}
	return collection
}

func healthWQSnapshot(log *workQueueTestLog) workqueue.Snapshot {
	return workqueue.Snapshot{Head: "abc", State: log.state()}
}

// healthWQExhaustedFixture releases the running dispatch of the mixed fixture and
// cancels its Work for exhausted attempts in the same release commit.
func healthWQExhaustedFixture(t *testing.T) *workQueueTestLog {
	t.Helper()
	log, ids := workQueueMixedFixture(t)
	dispatchID := ids["running_dispatch"]
	claimID := log.state().Dispatches[dispatchID].Claims[0]
	log.append(workQueueTestAdmin, "release", workqueue.ReleaseParameters{DispatchID: dispatchID, Reason: "worker_terminated"},
		workqueue.ReleaseOperation{Kind: "Release", DispatchID: dispatchID, Reason: "worker_terminated"},
		workqueue.WorkCancellationOperation{Kind: "WorkCancellation", WorkID: ids["running"], ClaimID: claimID, Reason: "attempts_exhausted"})
	return log
}

func TestHealthWorkQueueConditionalCollection(t *testing.T) {
	t.Run("no workflows", func(t *testing.T) {
		collection := healthWorkQueueFixture(t)
		collectHealthWorkQueue(collection)
		assert.Nil(t, collection.WorkQueue)
		assert.Zero(t, collection.APIRequests)
	})
	t.Run("no queue usage", func(t *testing.T) {
		collection := healthWorkQueueFixture(t, "jobs: {}", "env:\n  GH_AW_WORK_QUEUE_ENABLED: 'false'")
		collectHealthWorkQueue(collection)
		assert.Nil(t, collection.WorkQueue)
		assert.Equal(t, 3, collection.APIRequests)
	})
	t.Run("queue shared by workflows without runs", func(t *testing.T) {
		collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: 'true'", "jobs: {}", "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
		snapshot := healthWQSnapshot(healthWQExhaustedFixture(t))
		reads := 0
		healthReadWorkQueue = func(ctx context.Context, branch workqueue.Branch) (workqueue.Snapshot, error) {
			reads++
			assert.Equal(t, "owner/repo", branch.Remote)
			assert.Equal(t, workqueue.DefaultBranch, branch.Name)
			assert.Equal(t, collection.Host, branch.Host)
			_, bounded := ctx.Deadline()
			assert.True(t, bounded)
			return snapshot, nil
		}
		collectHealthWorkQueue(collection)
		require.NotNil(t, collection.WorkQueue)
		assert.Equal(t, 1, reads)
		assert.Equal(t, []string{collection.workflows[0].Path, collection.workflows[2].Path}, collection.WorkQueue.Workflows)
		assert.Equal(t, "attention_required", collection.WorkQueue.Status)
		require.NotNil(t, collection.WorkQueue.Stats)
		assert.Equal(t, 1, collection.WorkQueue.Stats.WorkCancellationReasons["attempts_exhausted"])
		assert.Equal(t, 1, collection.WorkQueue.Stats.ClaimCancellationReasons["worker_terminated"])
		assert.Equal(t, []string{"work-queue cancelled 1 Work after exhausting worker attempts"}, collection.WorkQueue.Warnings)

		var output bytes.Buffer
		require.NoError(t, renderHealthCollection(&output, collection))
		text := output.String()
		assert.Contains(t, text, "[health_work_queue] status=attention_required")
		assert.Contains(t, text, "Work cancellations: ")
		assert.Contains(t, text, "attempts_exhausted")
		assert.Contains(t, text, "ledger records, not live Actions status")
		assert.Less(t, strings.Index(text, "Work cancellations: "), strings.Index(text, "Warning: work-queue cancelled 1 Work"))
		data, err := json.Marshal(collection)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"work_queue":`)
		assert.Contains(t, string(data), `"work_cancellation_reasons":{"attempts_exhausted":1,"operator":1}`)
	})
}

func TestHealthWorkQueueUnavailableEvidence(t *testing.T) {
	for _, tc := range []string{"invalid workflow", "API error", "stopped", "missing ledger", "invalid ledger", "empty ledger", "wrong repository"} {
		t.Run(tc, func(t *testing.T) {
			collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
			switch tc {
			case "invalid workflow":
				collection = healthWorkQueueFixture(t, "jobs: [")
			case "API error":
				healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
					return nil, errors.New("API unavailable")
				}
			case "stopped":
				collection.stopped = true
			default:
				log, _ := workQueueMixedFixture(t)
				healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
					switch tc {
					case "missing ledger":
						return workqueue.Snapshot{}, errors.New("queue_missing: private detail")
					case "invalid ledger":
						return workqueue.Snapshot{}, &workqueue.ProtocolError{Code: "request_invalid", Message: "private detail"}
					case "empty ledger":
						return workqueue.Snapshot{}, nil
					}
					return healthWQSnapshot(log), nil
				}
				if tc == "wrong repository" {
					originalGH := healthRunGH
					healthRunGH = func(ctx context.Context, message string, args ...string) ([]byte, error) {
						out, err := originalGH(ctx, message, args...)
						return bytes.ReplaceAll(out, []byte(`"full_name":"owner/repo"`), []byte(`"full_name":"other/repo"`)), err
					}
				}
			}
			collectHealthWorkQueue(collection)
			require.NotNil(t, collection.WorkQueue)
			assert.Equal(t, "unavailable", collection.WorkQueue.Status)
			assert.Nil(t, collection.WorkQueue.Stats)
			require.NotEmpty(t, collection.WorkQueue.Warnings)
			assert.NotContains(t, strings.Join(collection.WorkQueue.Warnings, " "), "private detail")
		})
	}
}

func TestRunHealthWorkQueueWithoutRuns(t *testing.T) {
	for _, name := range []string{"", "worker-0"} {
		t.Run("workflow="+name, func(t *testing.T) {
			fixture := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
			originalDiscover, originalList := healthDiscoverWorkflows, healthListWorkflowRuns
			t.Cleanup(func() { healthDiscoverWorkflows, healthListWorkflowRuns = originalDiscover, originalList })
			healthDiscoverWorkflows = func(collection *HealthCollection) ([]GitHubWorkflow, error) {
				collection.localGitChecked = true
				collection.fileSnapshots = fixture.fileSnapshots
				collection.APIRateLimit = fixture.APIRateLimit
				return fixture.workflows, nil
			}
			healthListWorkflowRuns = func(ListWorkflowRunsOptions, int, *HealthCollection) ([]WorkflowRun, int, error) {
				return nil, 0, nil
			}
			originalGH := healthRunGH
			healthRunGH = func(ctx context.Context, message string, args ...string) ([]byte, error) {
				if strings.Contains(args[3], "/runs?") {
					return []byte(`{"workflow_runs":[]}`), nil
				}
				return originalGH(ctx, message, args...)
			}
			snapshot := healthWQSnapshot(healthWQExhaustedFixture(t))
			healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
				return snapshot, nil
			}
			output := captureAuditGroupStdout(t, func() {
				require.NoError(t, runHealth(context.Background(), HealthConfig{
					WorkflowName: name, Days: 7, Threshold: 80, JSONOutput: true, RepoOverride: fixture.repoOverride,
				}))
			})
			var report struct {
				Collection *HealthCollection `json:"collection"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &report))
			require.NotNil(t, report.Collection)
			require.NotNil(t, report.Collection.WorkQueue)
			assert.Equal(t, "attention_required", report.Collection.WorkQueue.Status)
			require.NotNil(t, report.Collection.WorkQueue.Stats)
		})
	}
}

func TestHealthWorkQueueBacklogObserved(t *testing.T) {
	collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
	log, ids := workQueueMixedFixture(t)
	snapshot := healthWQSnapshot(log)
	healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
		return snapshot, nil
	}
	collectHealthWorkQueue(collection)
	require.NotNil(t, collection.WorkQueue)
	require.NotNil(t, collection.WorkQueue.Stats)
	assert.Equal(t, "observed", collection.WorkQueue.Status, "operator cancellations alone do not require attention")
	assert.Empty(t, collection.WorkQueue.Warnings)
	stats := collection.WorkQueue.Stats
	assert.Equal(t, 1, stats.Available)
	assert.Equal(t, 1, stats.DispatchesByState["bound"])
	require.Len(t, stats.Pools, 1)
	assert.Equal(t, []string{ids["waiting"]}, stats.Pools[0].NextEligible)
	var output bytes.Buffer
	require.NoError(t, renderHealthWorkQueue(&output, collection.WorkQueue))
	assert.Contains(t, output.String(), "[health_work_queue] status=observed workflows=1")
	assert.Contains(t, output.String(), "pool: default available=1")
	assert.NotContains(t, output.String(), "Warning:")
}

func TestHealthWorkQueueDetectionSharesAPIBudget(t *testing.T) {
	collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
	collection.APIRequests = healthMaximumAPIRequests
	collectHealthWorkQueue(collection)
	require.NotNil(t, collection.WorkQueue)
	assert.Equal(t, "unavailable", collection.WorkQueue.Status)
	assert.True(t, collection.Partial)
	assert.Equal(t, "request_budget", collection.Reason)
	assert.Equal(t, healthMaximumAPIRequests, collection.APIRequests)
}
