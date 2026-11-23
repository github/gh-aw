//go:build !integration

package cli

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func healthRevisionFixture() ([]WorkflowRun, *HealthCollection) {
	collection := &HealthCollection{revisions: make(map[string]*workflow.LockMetadata)}
	var runs []WorkflowRun
	start := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	for i := range 10 {
		run := WorkflowRun{DatabaseID: int64(i + 1), WorkflowName: "Example",
			WorkflowPath: ".github/workflows/example.lock.yml", Repository: "org/repo",
			Status: "completed", Attempt: 1, Event: "schedule", HeadBranch: "main",
			HeadSha: strconv.Itoa(i + 1), CreatedAt: start.Add(time.Duration(i) * time.Hour),
			Conclusion: "failure"}
		run.UpdatedAt = run.CreatedAt.Add(time.Minute)
		metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "old-prompt"}
		if i >= 5 {
			run.Conclusion = "success"
			metadata.BodyHash = "fixed-prompt"
		}
		runs = append(runs, run)
		collection.revisions[healthRevisionLookupKey(run)] = metadata
	}
	return runs, collection
}

func TestHealthRevisionRecoveryAndCurrentClusters(t *testing.T) {
	runs, collection := healthRevisionFixture()
	config := HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
	health := calculateCurrentWorkflowHealth("Example", runs, config)
	assert.Equal(t, "recovery_observed", health.Status)
	assert.Equal(t, 5, health.TotalRuns)
	assert.InDelta(t, 100.0, health.SuccessRate, 1e-9)
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, 5, health.Revisions[0].Previous.Failures)
	clusters, plan := buildHealthFailureClusters(runs, config)
	assert.Empty(t, clusters, "historical failures must not populate the current investigation plan")
	assert.Nil(t, plan)
	slices.Reverse(runs)
	assert.Equal(t, health, calculateCurrentWorkflowHealth("Example", runs, config))
}

func TestHealthFailureStatusPrecedesInsufficientRevisionEvidenceInEitherOrder(t *testing.T) {
	for _, statuses := range [][]string{
		{"insufficient_evidence", "failures_observed"},
		{"failures_observed", "insufficient_evidence"},
	} {
		current := "no_recent_runs"
		for _, candidate := range statuses {
			if higherPriorityHealthStatus(current, candidate) {
				current = candidate
			}
		}
		assert.Equal(t, "failures_observed", current)
	}
	assert.False(t, higherPriorityHealthStatus("failures_observed", "insufficient_evidence"))
}

func TestHealthStatusPrecedence(t *testing.T) {
	priorities := []string{
		"no_recent_runs", "healthy", "recovery_observed", "insufficient_evidence",
		"no_current_revision_runs", "revision_unknown", "not_collected", "failures_observed",
	}
	for i, higher := range priorities {
		t.Run(higher, func(t *testing.T) {
			assert.False(t, higherPriorityHealthStatus(higher, higher))
			for _, lower := range priorities[:i] {
				for _, order := range [][]string{{lower, higher}, {higher, lower}} {
					current := "no_recent_runs"
					for _, candidate := range order {
						if higherPriorityHealthStatus(current, candidate) {
							current = candidate
						}
					}
					assert.Equal(t, higher, current, "order: %v", order)
				}
			}
		})
	}
}

func TestHealthSupersededSourceRetainsOnlyPreviousFailureEvidence(t *testing.T) {
	for _, currentRuns := range []int{healthMinimumRuns - 1, healthMinimumRuns} {
		t.Run(strconv.Itoa(currentRuns), func(t *testing.T) {
			runs, collection := healthRevisionFixture()
			oldCommit, currentCommit := strings.Repeat("a", 40), strings.Repeat("b", 40)
			oldSource, currentSource := strings.Repeat("c", 40), strings.Repeat("d", 40)
			metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "same-lock"}
			collection.revisions = make(map[string]*workflow.LockMetadata)
			collection.headRevisions = map[string]*workflow.LockMetadata{healthHeadRevisionKey(runs[0]): metadata}
			collection.sourceRevisions = map[string]string{
				healthSourceRevisionKey(runs[0], oldCommit):     oldSource,
				healthSourceRevisionKey(runs[0], currentCommit): currentSource,
				healthSourceRevisionKey(runs[0], "main"):        currentSource,
			}
			for i := range runs {
				runs[i].HeadSha = oldCommit
				if i >= healthMinimumRuns {
					runs[i].HeadSha = currentCommit
					collection.revisions[healthRevisionLookupKey(runs[i])] = metadata
				}
			}
			// Retain one stale first-pass failure as the boundary, without its skipped lock metadata.
			runs = runs[healthMinimumRuns-1 : healthMinimumRuns+currentRuns]
			config := HealthConfig{Threshold: 80, RepoOverride: "org/repo", collection: collection}
			health := calculateCurrentWorkflowHealth("Example", runs, config)
			expected := "insufficient_evidence"
			if currentRuns == healthMinimumRuns {
				expected = "recovery_observed"
			}
			assert.Equal(t, expected, health.Status)
			assert.Equal(t, currentRuns, health.TotalRuns)
			assert.Zero(t, health.FailureCount)
			require.Len(t, health.Revisions, 1)
			assert.Equal(t, currentRuns, health.Revisions[0].Current.IndependentRuns)
			require.NotNil(t, health.Revisions[0].Previous)
			assert.Equal(t, 1, health.Revisions[0].Previous.Failures)
			assert.Equal(t, oldSource, health.Revisions[0].Previous.SourceSHA)
			assert.NotContains(t, health.Revisions[0].Previous.key, "unknown:")
			clusters, plan := buildHealthFailureClusters(runs, config)
			assert.Empty(t, clusters)
			assert.Nil(t, plan)
		})
	}
}

func TestHealthIgnoresNonDispatchedConclusions(t *testing.T) {
	for _, blocked := range []WorkflowRun{
		{Status: "waiting", Conclusion: "action_required"},
		{Status: "completed", Conclusion: "action_required"},
		{Status: "completed", Conclusion: "skipped"},
	} {
		t.Run(blocked.Status+"/"+blocked.Conclusion, func(t *testing.T) {
			runs, collection := healthRevisionFixture()
			config := HealthConfig{Threshold: 80, collection: collection}
			expected := calculateCurrentWorkflowHealth("Example", runs, config)
			expectedMetrics := CalculateWorkflowHealth("Example", runs, 80)
			run := runs[len(runs)-1]
			run.DatabaseID, run.HeadSha = 11, "unexecuted"
			run.Status, run.Conclusion = blocked.Status, blocked.Conclusion
			run.CreatedAt = run.CreatedAt.Add(time.Hour)
			run.Duration, run.TokenUsage = time.Hour, 100000
			run.HeadBranch, run.Event = "unapproved", "pull_request"
			runs = append([]WorkflowRun{run}, runs...)
			assert.Equal(t, expected, calculateCurrentWorkflowHealth("Example", runs, config))
			assert.Equal(t, expectedMetrics, CalculateWorkflowHealth("Example", runs, 80))
			assert.Equal(t, run, runs[0], "health calculation must not mutate caller data")
		})
	}
}

func TestHealthRetainsAmbiguousWaitingEvidenceWithoutAPI(t *testing.T) {
	runs, collection := healthRevisionFixture()
	waiting := runs[len(runs)-1]
	waiting.DatabaseID, waiting.HeadSha = 11, "pending"
	waiting.Status, waiting.Conclusion = "waiting", ""
	waiting.CreatedAt = waiting.CreatedAt.Add(time.Hour)
	collection.revisions[healthRevisionLookupKey(waiting)] = &workflow.LockMetadata{
		FrontmatterHash: "settings", BodyHash: "new-prompt",
	}
	runs = append([]WorkflowRun{waiting}, runs...)
	health := calculateCurrentWorkflowHealth("Example", runs, HealthConfig{Threshold: 80, collection: collection})
	assert.Equal(t, "insufficient_evidence", health.Status)
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, 1, health.Revisions[0].Current.Pending)
	assert.Equal(t, []int64{11}, health.Revisions[0].Current.RunIDs)
	metrics := CalculateWorkflowHealth("Example", runs, 80)
	assert.Equal(t, 11, metrics.TotalRuns, "waiting alone cannot prove a run never executed")
	assert.Equal(t, waiting, runs[0], "health calculation must not mutate caller data")
}

func TestHealthRevisionsDoNotClaimUnsupportedRecovery(t *testing.T) {
	for _, scenario := range []string{"same-content", "one-success", "retry-only", "unknown-revision", "still-failing", "overlapping-runs", "other-branch", "other-event"} {
		t.Run(scenario, func(t *testing.T) {
			runs, collection := healthRevisionFixture()
			for i := range runs {
				run := &runs[i]
				switch scenario {
				case "same-content":
					collection.revisions[healthRevisionLookupKey(*run)].BodyHash = "same-prompt"
				case "one-success":
					if run.DatabaseID > 6 {
						run.Status, run.Conclusion = "in_progress", ""
					}
				case "retry-only":
					if run.DatabaseID > 5 {
						run.Attempt = 2
					}
				case "unknown-revision":
					delete(collection.revisions, healthRevisionLookupKey(*run))
				case "still-failing":
					if run.DatabaseID == 10 {
						run.Conclusion = "failure"
					}
				case "overlapping-runs":
					if run.DatabaseID == 5 {
						run.UpdatedAt = run.CreatedAt.Add(20 * time.Hour)
					}
				case "other-branch":
					if run.DatabaseID < 6 {
						run.HeadBranch = "feature"
					}
				case "other-event":
					if run.DatabaseID < 6 {
						run.Event = "push"
					}
				}
			}
			for _, status := range healthRevisionStatuses(runs, collection) {
				assert.NotEqual(t, "recovery_observed", status.Status)
			}
		})
	}
}

func TestHealthRevisionRollbackDoesNotMergeHistoricalCohorts(t *testing.T) {
	runs, collection := healthRevisionFixture()
	latest := runs[len(runs)-1]
	latest.DatabaseID, latest.HeadSha = 11, "rollback"
	latest.CreatedAt, latest.UpdatedAt = latest.CreatedAt.Add(time.Hour), latest.UpdatedAt.Add(time.Hour)
	collection.revisions[healthRevisionLookupKey(latest)] = &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "old-prompt"}
	statuses := healthRevisionStatuses(append(runs, latest), collection)
	require.Len(t, statuses, 1)
	assert.Equal(t, []int64{11}, statuses[0].Current.RunIDs)
	assert.Equal(t, "insufficient_evidence", statuses[0].Status)
}

func TestHealthSummaryKeepsSparseAndSameNameWorkflowsSeparate(t *testing.T) {
	runs, collection := healthRevisionFixture()
	target := GitHubWorkflow{Name: "Example", Path: ".github/workflows/example.lock.yml"}
	other := GitHubWorkflow{Name: "Example", Path: ".github/workflows/other.lock.yml"}
	config := HealthConfig{Full: true, Threshold: 80, collection: collection}
	evidence := calculateCurrentWorkflowHealth(target.Name, runs, config)
	empty := calculateCurrentWorkflowHealth(other.Name, nil, config)
	empty.WorkflowPath = other.Path
	report := makeHealthFleetReport(&healthFleetState{
		Workflows: []GitHubWorkflow{target, other},
		Verified:  map[string]healthFleetWorkflowEvidence{target.Path: {Health: evidence}, other.Path: {Health: empty}},
	}, config)
	require.Len(t, report.Details.Workflows, 2)
	assert.Equal(t, target.Path, report.Details.Workflows[0].WorkflowPath)
	assert.Equal(t, other.Path, report.Details.Workflows[1].WorkflowPath)
	assert.Equal(t, 1, report.StatusCounts["recovery_observed"])
	assert.Equal(t, 1, report.StatusCounts["no_recent_runs"])
}

func TestHealthNewlyPushedRevisionHasNoCurrentEvidence(t *testing.T) {
	runs, collection := healthRevisionFixture()
	collection.headRevisions = map[string]*workflow.LockMetadata{
		healthHeadRevisionKey(runs[0]): {FrontmatterHash: "settings", BodyHash: "just-pushed"},
	}
	health := calculateCurrentWorkflowHealth("Example", runs, HealthConfig{Threshold: 80, collection: collection})
	assert.Equal(t, "no_current_revision_runs", health.Status)
	assert.Zero(t, health.TotalRuns)
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, "just-pushed", health.Revisions[0].Current.BodyHash)
	assert.Equal(t, "fixed-prompt", health.Revisions[0].Previous.BodyHash)
	assert.True(t, healthWorkflowEvidenceEnough(runs, collection), "older runs cannot establish health for an unexecuted revision")
}
