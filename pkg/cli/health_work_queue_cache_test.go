//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthWorkQueueSharesCompleteLockSnapshots(t *testing.T) {
	for _, first := range []string{"metadata", "queue"} {
		for _, header := range []struct {
			name, content string
			valid         bool
		}{
			{"valid", healthSnapshotMetadata, true},
			{"missing body hash", `# gh-aw-metadata: {"frontmatter_hash":"settings"}`, false},
			{"missing frontmatter hash", `# gh-aw-metadata: {"body_hash":"prompt"}`, false},
			{"invalid metadata", `# gh-aw-metadata: {`, false},
			{"metadata only in body", "# generated workflow\n" + healthSnapshotMetadata, false},
		} {
			t.Run(first+"/"+header.name, func(t *testing.T) {
				content := header.content + "\nenv:\n  GH_AW_WORK_QUEUE_ENABLED: true\njobs: {}\n"
				collection := healthWorkQueueFixture(t, content)
				run := WorkflowRun{WorkflowPath: collection.workflows[0].Path}
				requests := 0
				originalGH := healthRunGH
				healthRunGH = func(ctx context.Context, message string, args ...string) ([]byte, error) {
					requests++
					return originalGH(ctx, message, args...)
				}
				readMetadata := func() {
					metadata, err := readHealthLockMetadata(run, "trunk", collection)
					if header.valid {
						require.NoError(t, err)
						assert.Equal(t, "settings", metadata.FrontmatterHash)
						assert.Equal(t, "prompt", metadata.BodyHash)
					} else {
						require.Error(t, err)
					}
				}
				readQueue := func() {
					actual, err := healthWorkQueueWorkflowContent(run.WorkflowPath, "trunk", collection)
					require.NoError(t, err, "invalid revision metadata must not suppress queue detection")
					assert.Equal(t, content, string(actual))
					enabled, err := healthWorkflowUsesWorkQueue(actual)
					require.NoError(t, err)
					assert.True(t, enabled, "queue detection needs the complete YAML, not only its header")
				}
				if first == "metadata" {
					readMetadata()
					readQueue()
				} else {
					readQueue()
					readMetadata()
				}
				readMetadata()
				readQueue()
				assert.Equal(t, 1, requests, "both directions must reuse the same complete blob")
				assert.Equal(t, 1, collection.RevisionLookups)
			})
		}
	}
}

func TestHealthWorkQueueDoesNotTreatMetadataOnlySnapshotAsCompleteYAML(t *testing.T) {
	collection := healthWorkQueueFixture(t, healthSnapshotMetadata+"\nenv:\n  GH_AW_WORK_QUEUE_ENABLED: true\n")
	run := WorkflowRun{WorkflowPath: collection.workflows[0].Path}
	sha := collection.fileSnapshots["trunk"].files[run.WorkflowPath]
	collection.lockSnapshots = map[string]healthLockSnapshot{
		sha: {metadata: &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "prompt"}},
	}
	content, err := healthWorkQueueWorkflowContent(run.WorkflowPath, "trunk", collection)
	require.NoError(t, err)
	enabled, err := healthWorkflowUsesWorkQueue(content)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 1, collection.APIRequests)
	_, err = readHealthLockMetadata(run, "trunk", collection)
	require.NoError(t, err)
	assert.Equal(t, 1, collection.APIRequests)
}

func TestHealthWorkQueueCachesDefaultBranchDirectorySnapshot(t *testing.T) {
	collection := healthWorkQueueFixture(t, "jobs: {}", "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
	directory := collection.fileSnapshots["trunk"]
	collection.fileSnapshots = nil
	originalGH := healthRunGH
	directories := 0
	healthRunGH = func(ctx context.Context, message string, args ...string) ([]byte, error) {
		if args[3] == "repos/owner/repo/contents/.github/workflows?ref=trunk" {
			directories++
			var entries []map[string]string
			for path, sha := range directory.files {
				entries = append(entries, map[string]string{"path": path, "sha": sha, "type": "file"})
			}
			return json.Marshal(entries)
		}
		return originalGH(ctx, message, args...)
	}
	for range 2 {
		for _, entry := range collection.workflows {
			_, err := healthWorkQueueWorkflowContent(entry.Path, "trunk", collection)
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, directories)
	assert.Equal(t, 3, collection.APIRequests, "one directory and two distinct complete blobs")
}

func TestRunHealthRetainsActionsEvidenceWhenWorkQueueExhaustsBudget(t *testing.T) {
	for _, name := range []string{"", "Example"} {
		t.Run("workflow="+name, func(t *testing.T) {
			originalDiscover, originalList, originalFailures := healthDiscoverWorkflows, healthListWorkflowRuns, healthListFailureCandidates
			originalGH, originalRate := healthRunGH, fetchRateLimitForHostFunc
			t.Cleanup(func() {
				healthDiscoverWorkflows, healthListWorkflowRuns, healthListFailureCandidates = originalDiscover, originalList, originalFailures
				healthRunGH, fetchRateLimitForHostFunc = originalGH, originalRate
			})
			run := WorkflowRun{DatabaseID: 42, Attempt: 1, WorkflowName: "Example", WorkflowPath: ".github/workflows/example.lock.yml",
				Repository: "org/repo", Status: "completed", Conclusion: "failure", Event: "schedule", HeadBranch: "main",
				HeadSha: strings.Repeat("a", 40), CreatedAt: time.Now().UTC().Add(-time.Hour)}
			var order []string
			healthDiscoverWorkflows = func(collection *HealthCollection) ([]GitHubWorkflow, error) {
				metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "prompt"}
				collection.localGitChecked = true
				collection.revisions = map[string]*workflow.LockMetadata{healthRevisionLookupKey(run): metadata}
				collection.headRevisions = map[string]*workflow.LockMetadata{healthHeadRevisionKey(run): metadata}
				collection.sourceRevisions = map[string]string{
					healthSourceRevisionKey(run, run.HeadSha):    strings.Repeat("b", 40),
					healthSourceRevisionKey(run, run.HeadBranch): strings.Repeat("b", 40),
				}
				key := healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}
				collection.jobPresence = map[healthRunAttempt]bool{key: true}
				collection.jobs = map[healthRunAttempt]*healthJobs{key: {complete: true}}
				collection.fileSnapshots = map[string]healthFileSnapshot{
					"main": {files: map[string]string{run.WorkflowPath: strings.Repeat("c", 40)}},
				}
				return []GitHubWorkflow{{ID: 1, Name: run.WorkflowName, Path: run.WorkflowPath, State: "active"}}, nil
			}
			healthListFailureCandidates = func(_ ListWorkflowRunsOptions, _ int, _ string, _ *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
				order = append(order, "failure")
				response := workflowRunAPIResponse{ID: run.DatabaseID, RunAttempt: run.Attempt, Name: run.WorkflowName, Path: run.WorkflowPath,
					Status: run.Status, Conclusion: run.Conclusion, Event: run.Event, HeadBranch: run.HeadBranch,
					HeadSHA: run.HeadSha, CreatedAt: run.CreatedAt}
				response.Repository.FullName = run.Repository
				return []workflowRunAPIResponse{response}, time.Time{}, nil
			}
			healthListWorkflowRuns = func(_ ListWorkflowRunsOptions, _ int, collection *HealthCollection) ([]WorkflowRun, int, error) {
				order = append(order, "sampling")
				require.True(t, collection.FailurePassComplete)
				collection.APIRequests = healthMaximumAPIRequests - 1
				return []WorkflowRun{run}, 1, nil
			}
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
			}
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				order = append(order, "queue")
				require.Contains(t, order, "sampling", "queue must never displace primary Actions evidence")
				require.Equal(t, "repos/org/repo", args[3])
				return []byte(`{"full_name":"org/repo","default_branch":"main"}`), nil
			}
			output := captureAuditGroupStdout(t, func() {
				require.NoError(t, runHealth(context.Background(), HealthConfig{
					WorkflowName: name, Days: 7, JSONOutput: true, RepoOverride: "org/repo", LogsDir: ".health-unused-cache",
				}))
			})
			var report struct {
				Collection           *HealthCollection     `json:"collection"`
				FailureInvestigation *FailureInvestigation `json:"failure_investigation"`
				Workflows            []WorkflowHealth      `json:"workflows"`
				FailureCount         int                   `json:"failure_count"`
				Status               string                `json:"status"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &report))
			require.NotNil(t, report.Collection)
			assert.True(t, report.Collection.FailurePassComplete)
			assert.Equal(t, 1, report.Collection.FailureCandidates)
			assert.True(t, report.Collection.JobEvidence[run.DatabaseID].Complete)
			assert.Equal(t, "request_budget", report.Collection.Reason)
			require.NotNil(t, report.Collection.WorkQueue)
			assert.NotEmpty(t, report.Collection.WorkQueue.Warnings)
			failures, status := report.FailureCount, report.Status
			if name == "" {
				require.Len(t, report.Workflows, 1)
				failures, status = report.Workflows[0].FailureCount, report.Workflows[0].Status
			}
			assert.Equal(t, 1, failures)
			assert.Equal(t, "failures_observed", status)
			require.NotNil(t, report.FailureInvestigation)
			assert.Equal(t, 1, report.FailureInvestigation.UniqueFailedRuns)
			assert.Equal(t, []string{"sampling", "queue"}, order[len(order)-2:])
		})
	}
}
