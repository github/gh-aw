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
	healthReadWorkQueue = func(context.Context, workqueue.Branch) ([]workqueue.QueueCommit, error) {
		t.Fatal("queue must not be read")
		return nil, errors.New("unexpected queue read")
	}
	return collection
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
		_, commits, _ := workDiagnosticsFixture(t)
		reads := 0
		healthReadWorkQueue = func(ctx context.Context, branch workqueue.Branch) ([]workqueue.QueueCommit, error) {
			reads++
			assert.Equal(t, "owner/repo", branch.Remote)
			assert.Equal(t, workqueue.DefaultBranch, branch.Name)
			assert.Equal(t, collection.Host, branch.Host)
			_, bounded := ctx.Deadline()
			assert.True(t, bounded)
			return commits, nil
		}
		collectHealthWorkQueue(collection)
		require.NotNil(t, collection.WorkQueue)
		assert.Equal(t, 1, reads)
		assert.Equal(t, []string{collection.workflows[0].Path, collection.workflows[2].Path}, collection.WorkQueue.Workflows)
		assert.Equal(t, "attention_required", collection.WorkQueue.Status)
		require.NotNil(t, collection.WorkQueue.Stats)
		assert.Equal(t, 1, collection.WorkQueue.Stats.DeliveryByState["failed"])
		assert.Equal(t, 1, collection.WorkQueue.Stats.DeliveryByState["verified"])
		assertWorkDiagnosticsPrivate(t, collection.WorkQueue)

		var output bytes.Buffer
		require.NoError(t, renderHealthCollection(&output, collection))
		assert.Contains(t, output.String(), "[health_work_queue] status=attention_required")
		assert.Contains(t, output.String(), "delivery: pending=0 verified=1 failed=1")
		assert.Contains(t, output.String(), "ledger evidence, not live Actions status")
		assert.Contains(t, output.String(), "verification_exhausted")
		data, err := json.Marshal(collection)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"work_queue":`)
		assert.Contains(t, string(data), `"delivery_by_state":{"failed":1,"pending":0,"verified":1}`)
	})
}

func TestHealthWorkQueueUnavailableEvidence(t *testing.T) {
	for _, tc := range []string{"invalid workflow", "API error", "stopped", "missing ledger", "invalid ledger", "missing policy", "wrong repository"} {
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
				_, commits, _ := workDiagnosticsFixture(t)
				healthReadWorkQueue = func(context.Context, workqueue.Branch) ([]workqueue.QueueCommit, error) {
					switch tc {
					case "missing ledger":
						return nil, errors.New("queue_missing: private detail")
					case "invalid ledger":
						return []workqueue.QueueCommit{{ID: "PAYLOAD-MUST-STAY-PRIVATE"}}, nil
					case "missing policy":
						return nil, nil
					}
					return commits, nil
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
			assertWorkDiagnosticsPrivate(t, collection.WorkQueue)
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
			_, commits, _ := workDiagnosticsFixture(t)
			healthReadWorkQueue = func(context.Context, workqueue.Branch) ([]workqueue.QueueCommit, error) {
				return commits, nil
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

func TestHealthWorkQueueControlsAndBacklog(t *testing.T) {
	for _, control := range []string{"", "admission_paused", "grants_paused"} {
		t.Run("control="+control, func(t *testing.T) {
			collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
			_, commits, _ := workOperatorFixture(t, false)
			if control != "" {
				actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: "owner/repo"}
				request, err := workqueue.NewRequest("pause", "control", actor, workqueue.OperationsParameters{
					Operations: []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{
						"kind": "Control", "control": control, "value": true, "reason": "operator",
					})},
				})
				require.NoError(t, err)
				commits, _, _, err = workqueue.BuildCandidate(commits, actor, request, 3000)
				require.NoError(t, err)
			}
			healthReadWorkQueue = func(context.Context, workqueue.Branch) ([]workqueue.QueueCommit, error) {
				return commits, nil
			}
			collectHealthWorkQueue(collection)
			require.NotNil(t, collection.WorkQueue)
			require.NotNil(t, collection.WorkQueue.Stats)
			assert.Equal(t, 3, collection.WorkQueue.Stats.Available)
			require.Len(t, collection.WorkQueue.Stats.Pools, 1)
			assert.NotNil(t, collection.WorkQueue.Stats.Pools[0].OldestAvailable)
			if control == "" {
				assert.Equal(t, "observed", collection.WorkQueue.Status)
				assert.Empty(t, collection.WorkQueue.Warnings)
				assert.Equal(t, 3, collection.WorkQueue.Stats.Pools[0].SchedulingEligible)
			} else {
				assert.Equal(t, "attention_required", collection.WorkQueue.Status)
				assert.NotEmpty(t, collection.WorkQueue.Warnings)
			}
			var output bytes.Buffer
			require.NoError(t, renderHealthWorkQueue(&output, collection.WorkQueue))
			assert.Contains(t, output.String(), "oldest_work=")
			assertWorkDiagnosticsPrivate(t, collection.WorkQueue)
		})
	}
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
