//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestHealthFleetReviewBoundaryAndCohortBounds(t *testing.T) {
	for _, backedBoundary := range []bool{false, true} {
		t.Run(strconv.FormatBool(backedBoundary), func(t *testing.T) {
			path := ".github/workflows/review.lock.yml"
			currentSource, oldSource := strings.Repeat("a", 40), strings.Repeat("b", 40)
			metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "prompt"}
			collection := &HealthCollection{
				Host: "github.com", repoOverride: "org/repo",
				APIRateLimit:    &GitHubAPIRateLimitState{Remaining: 4000},
				revisions:       make(map[string]*workflow.LockMetadata),
				headRevisions:   make(map[string]*workflow.LockMetadata),
				sourceRevisions: make(map[string]string),
			}
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			var history []WorkflowRun
			add := func(id int64, event, conclusion, source string, retain bool) WorkflowRun {
				run := WorkflowRun{DatabaseID: id, WorkflowName: "Review", WorkflowPath: path,
					Repository: "org/repo", Status: "completed", Conclusion: conclusion, Event: event,
					HeadBranch: "main", HeadSha: fmt.Sprintf("%040x", id), Attempt: 1,
					CreatedAt: start.Add(-time.Duration(len(history)) * time.Hour)}
				run.UpdatedAt = run.CreatedAt.Add(time.Minute)
				collection.headRevisions[healthHeadRevisionKey(run)] = metadata
				collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadBranch)] = currentSource
				if retain {
					collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)] = source
					collection.revisions[healthRevisionLookupKey(run)] = metadata
				}
				history = append(history, run)
				return run
			}
			for i := int64(1); i <= healthMinimumRuns; i++ {
				add(i, "schedule", "success", currentSource, true)
			}
			var skipped []WorkflowRun
			for i := int64(10); i < 30; i++ {
				skipped = append(skipped, add(i, "schedule", "failure", currentSource, false))
			}
			jobless := add(40, "schedule", "failure", oldSource, false)
			if backedBoundary {
				add(41, "schedule", "failure", oldSource, false)
			}
			other := add(50, "workflow_dispatch", "failure", currentSource, true)

			original, originalRate := healthRunGH, fetchRateLimitForHostFunc
			t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = original, originalRate })
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Remaining: 4000}, nil
			}
			var jobRequests []string
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				endpoint := args[3]
				if strings.Contains(endpoint, "/jobs?") {
					jobRequests = append(jobRequests, endpoint)
					if strings.Contains(endpoint, "/runs/40/") {
						return []byte(`{"total_count":0,"jobs":[]}`), nil
					}
					return []byte(`{"total_count":1,"jobs":[{"name":"agent","conclusion":"failure"}]}`), nil
				}
				if strings.Contains(endpoint, "/contents/") {
					parsed, err := url.Parse(endpoint)
					require.NoError(t, err)
					ref := parsed.Query().Get("ref")
					if ref == jobless.HeadSha || ref == fmt.Sprintf("%040x", 41) {
						return []byte(oldSource), nil
					}
					return []byte(currentSource), nil
				}
				t.Fatalf("unexpected enrichment request: %s", endpoint)
				return nil, nil
			}
			config := HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
			evidence, err := verifyHealthFleetWorkflow(GitHubWorkflow{Name: "Review", Path: path}, history, config)
			require.NoError(t, err)
			require.Len(t, evidence.Health.Revisions, 2)
			schedule := evidence.Health.Revisions[0]
			require.Equal(t, "schedule", schedule.Event)
			require.Len(t, schedule.Current.RunIDs, healthMinimumRuns)
			if backedBoundary {
				require.Equal(t, "recovery_observed", schedule.Status)
				require.Equal(t, []int64{41}, schedule.Previous.RunIDs)
				require.Equal(t, oldSource, schedule.Previous.SourceSHA)
				require.Empty(t, schedule.Previous.BodyHash, "source-only boundaries must not inherit current metadata")
			} else {
				require.Equal(t, "healthy", schedule.Status)
				require.Nil(t, schedule.Previous)
			}
			require.Equal(t, "failures_observed", evidence.Health.Revisions[1].Status)
			require.Equal(t, other.DatabaseID, evidence.Failures[0].RunID)
			require.Equal(t, "missing", evidence.CacheEvidence[other.DatabaseID].Status)
			collection.CacheEvidence[other.DatabaseID] = HealthCacheEvidence{Status: "changed"}
			require.Equal(t, "missing", evidence.CacheEvidence[other.DatabaseID].Status)
			for _, run := range skipped {
				_, enriched := collection.revisions[healthRevisionLookupKey(run)]
				require.False(t, enriched, "older same-source runs must not resolve lock metadata")
				for _, endpoint := range jobRequests {
					require.NotContains(t, endpoint, fmt.Sprintf("/runs/%d/", run.DatabaseID))
				}
			}
			require.Len(t, jobRequests, 2+boolInt(backedBoundary))
		})
	}
}

func TestHealthFleetReviewBoundaryProbeLimit(t *testing.T) {
	path := ".github/workflows/review.lock.yml"
	source := strings.Repeat("a", 40)
	metadata := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "current"}
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo",
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
		revisions:    make(map[string]*workflow.LockMetadata), headRevisions: make(map[string]*workflow.LockMetadata),
		sourceRevisions: make(map[string]string)}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	history := make([]WorkflowRun, 0, 1201)
	for i := 1; i <= 1200; i++ {
		run := WorkflowRun{DatabaseID: int64(i), WorkflowName: "Review", WorkflowPath: path, Repository: "org/repo",
			Status: "completed", Conclusion: "success", Event: "schedule", HeadBranch: "main",
			HeadSha: fmt.Sprintf("%040x", i), Attempt: 1, CreatedAt: start.Add(-time.Duration(i) * time.Minute)}
		if i <= healthMinimumRuns {
			collection.revisions[healthRevisionLookupKey(run)] = metadata
			collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)] = source
		}
		collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadBranch)] = source
		collection.headRevisions[healthHeadRevisionKey(run)] = metadata
		history = append(history, run)
	}
	other := history[0]
	other.DatabaseID, other.Event, other.Conclusion = 2000, "workflow_dispatch", "failure"
	other.HeadSha = fmt.Sprintf("%040x", other.DatabaseID)
	other.CreatedAt = start.Add(-1201 * time.Minute)
	collection.revisions[healthRevisionLookupKey(other)] = metadata
	collection.sourceRevisions[healthSourceRevisionKey(other, other.HeadSha)] = source
	history = append(history, other)
	original, originalRate := healthRunGH, fetchRateLimitForHostFunc
	t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = original, originalRate })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Remaining: 4000}, nil
	}
	var sourceRequests int
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(args[3], "/jobs?") {
			return []byte(`{"total_count":1,"jobs":[{"name":"agent","conclusion":"failure"}]}`), nil
		}
		require.Contains(t, args[3], "/contents/")
		sourceRequests++
		return []byte(source), nil
	}
	config := HealthConfig{Full: true, Threshold: 80, Checkpoint: "health.json", RepoOverride: "org/repo",
		LogsDir: t.TempDir(), collection: collection}
	evidence, err := verifyHealthFleetWorkflow(GitHubWorkflow{Name: "Review", Path: path}, history, config)
	require.NoError(t, err)
	require.Equal(t, healthFleetMaximumBoundaryProbes, sourceRequests)
	require.True(t, evidence.IncompleteRevisions)
	require.Len(t, evidence.Health.Revisions, 2)
	require.Equal(t, "failures_observed", evidence.Health.Revisions[1].Status)
	require.Equal(t, other.DatabaseID, evidence.Failures[0].RunID)
	serialized, err := json.Marshal(evidence)
	require.NoError(t, err)
	var restored healthFleetWorkflowEvidence
	require.NoError(t, json.Unmarshal(serialized, &restored))
	require.True(t, restored.IncompleteRevisions)
	state := &healthFleetState{Workflows: []GitHubWorkflow{{Name: "Review", Path: path}},
		Verified: map[string]healthFleetWorkflowEvidence{path: restored}}
	report := makeHealthFleetReport(state, config)
	require.True(t, report.Coverage.Partial)
	require.Equal(t, 1, report.Coverage.IncompleteRevisions)
	require.Equal(t, "revision_boundary_incomplete", report.Coverage.Reason)
	if report.NextAction != nil {
		require.NotEqual(t, "resume", report.NextAction.Type)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestHealthFleetReviewPreviousCohortBound(t *testing.T) {
	path := ".github/workflows/review.lock.yml"
	source := strings.Repeat("a", 40)
	current := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "current"}
	previous := &workflow.LockMetadata{FrontmatterHash: "settings", BodyHash: "previous"}
	collection := &HealthCollection{repoOverride: "org/repo",
		revisions:       make(map[string]*workflow.LockMetadata),
		headRevisions:   make(map[string]*workflow.LockMetadata),
		sourceRevisions: make(map[string]string)}
	var history []WorkflowRun
	for i := 1; i <= 20; i++ {
		run := WorkflowRun{DatabaseID: int64(i), WorkflowName: "Review", WorkflowPath: path,
			Repository: "org/repo", Status: "completed", Conclusion: "success",
			Event: "schedule", HeadBranch: "main", HeadSha: fmt.Sprintf("%040x", i), Attempt: 1,
			CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour)}
		collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)] = source
		collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadBranch)] = source
		collection.headRevisions[healthHeadRevisionKey(run)] = current
		if i <= 2 {
			collection.revisions[healthRevisionLookupKey(run)] = current
		} else if i <= 2+healthMinimumRuns {
			collection.revisions[healthRevisionLookupKey(run)] = previous
		}
		history = append(history, run)
	}
	evidence, err := verifyHealthFleetWorkflow(GitHubWorkflow{Name: "Review", Path: path}, history,
		HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection})
	require.NoError(t, err)
	require.Len(t, evidence.Health.Revisions, 1)
	require.Equal(t, 2, evidence.Health.Revisions[0].Current.IndependentRuns)
	require.Equal(t, healthMinimumRuns, evidence.Health.Revisions[0].Previous.IndependentRuns)
	require.Len(t, collection.revisions, 2+healthMinimumRuns)
	require.Zero(t, collection.APIRequests)
}

func TestHealthFleetReviewJoblessBoundaryBeforeCohortFull(t *testing.T) {
	path := ".github/workflows/review.lock.yml"
	currentSource, oldSource := strings.Repeat("a", 40), strings.Repeat("b", 40)
	current := WorkflowRun{DatabaseID: 1, WorkflowName: "Review", WorkflowPath: path, Repository: "org/repo",
		Status: "completed", Conclusion: "success", Event: "schedule", HeadBranch: "main",
		HeadSha: strings.Repeat("1", 40), Attempt: 1, CreatedAt: time.Now().UTC()}
	old := current
	old.DatabaseID, old.HeadSha, old.Conclusion = 2, strings.Repeat("2", 40), "failure"
	old.CreatedAt = current.CreatedAt.Add(-time.Hour)
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo",
		APIRateLimit:  &GitHubAPIRateLimitState{Remaining: 4000},
		revisions:     map[string]*workflow.LockMetadata{healthRevisionLookupKey(current): {BodyHash: "current"}},
		headRevisions: map[string]*workflow.LockMetadata{healthHeadRevisionKey(current): {BodyHash: "current"}},
		sourceRevisions: map[string]string{
			healthSourceRevisionKey(current, current.HeadBranch): currentSource,
			healthSourceRevisionKey(current, current.HeadSha):    currentSource,
			healthSourceRevisionKey(old, old.HeadSha):            oldSource,
		}}
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.Contains(t, args[3], "/jobs?per_page=1")
		return []byte(`{"jobs":[]}`), nil
	}
	evidence, err := verifyHealthFleetWorkflow(GitHubWorkflow{Name: "Review", Path: path}, []WorkflowRun{current, old},
		HealthConfig{Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection})
	require.NoError(t, err)
	require.Len(t, evidence.Health.Revisions, 1)
	require.Equal(t, "insufficient_evidence", evidence.Health.Status)
	require.Nil(t, evidence.Health.Revisions[0].Previous)
}

func TestHealthFleetReviewCacheEvidenceAndRetry(t *testing.T) {
	path := ".github/workflows/review.lock.yml"
	evidence := healthFleetWorkflowEvidence{
		Health:         WorkflowHealth{WorkflowName: "Review", WorkflowPath: path, Status: "failures_observed"},
		IncompleteJobs: true,
		CacheEvidence:  map[int64]HealthCacheEvidence{42: {Status: "stale", ProducerVersion: "old", Attempt: 2}},
	}
	encoded, err := json.Marshal(evidence)
	require.NoError(t, err)
	var restored healthFleetWorkflowEvidence
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.Equal(t, evidence.CacheEvidence, restored.CacheEvidence)
	state := &healthFleetState{Workflows: []GitHubWorkflow{{Name: "Review", Path: path}},
		Verified: map[string]healthFleetWorkflowEvidence{path: restored}}
	config := HealthConfig{Full: true, Checkpoint: "health.json", collection: &HealthCollection{}}
	report := makeHealthFleetReport(state, config)
	require.True(t, report.Coverage.VerificationDone)
	require.True(t, report.Coverage.Partial)
	require.Equal(t, "job_evidence_incomplete", report.Coverage.Reason)
	require.Equal(t, &healthFleetAction{Type: "resume", Checkpoint: config.Checkpoint}, report.NextAction)
	require.Equal(t, evidence.CacheEvidence, report.Details.CacheEvidence)
	data, err := json.Marshal(report)
	require.NoError(t, err)
	require.Contains(t, string(data), `"cache_evidence":{"42":{"status":"stale"`)
	config.Full = false
	require.Nil(t, makeHealthFleetReport(state, config).Details)
	config.Checkpoint = ""
	require.Nil(t, makeHealthFleetReport(state, config).NextAction)
}

func TestHealthFleetReviewRegistryDrift(t *testing.T) {
	path := ".github/workflows/review.lock.yml"
	state := &healthFleetState{RegistryDrift: true, Workflows: []GitHubWorkflow{{Name: "Review", Path: path}},
		Verified: map[string]healthFleetWorkflowEvidence{path: {Health: WorkflowHealth{Status: "healthy"}}}}
	config := HealthConfig{Checkpoint: "health.json", collection: &HealthCollection{stopped: true}}
	report := makeHealthFleetReport(state, config)
	require.False(t, report.Coverage.RegistryComplete)
	require.False(t, report.Coverage.InventoryComplete)
	require.False(t, report.Coverage.VerificationDone)
	require.True(t, report.Coverage.Partial)
	require.Equal(t, "registry_drift", report.Coverage.Reason)
	require.Nil(t, report.NextAction, "permanent registry drift alone cannot be resumed")

	evidence := state.Verified[path]
	evidence.IncompleteJobs = true
	state.Verified[path] = evidence
	require.Equal(t, "resume", makeHealthFleetReport(state, config).NextAction.Type)
	delete(state.Verified, path)
	require.Equal(t, "resume", makeHealthFleetReport(state, config).NextAction.Type)
}

func TestHealthFleetReviewIntentionalFailure(t *testing.T) {
	root, _ := createHealthGitFixture(t)
	t.Chdir(root)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.md"),
		[]byte("---\nfeatures:\n  intentional-failure: true\n---\n"), 0o644))
	_, err := runHealthGitContext(context.Background(), root, "add", ".github/workflows/example.md")
	require.NoError(t, err)
	_, err = runHealthGitContext(context.Background(), root, "-c", "commit.gpgsign=false", "commit", "-qm", "intentional")
	require.NoError(t, err)
	head, err := runHealthGitContext(context.Background(), root, "rev-parse", "HEAD")
	require.NoError(t, err)
	sha := strings.TrimSpace(string(head))
	path := ".github/workflows/example.lock.yml"
	run := WorkflowRun{DatabaseID: 1, WorkflowName: "Example", WorkflowPath: path, Repository: "org/repo",
		Status: "completed", Conclusion: "failure", Event: "schedule", HeadBranch: "main", HeadSha: sha,
		Attempt: 1, CreatedAt: time.Now().UTC()}
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", scanBranch: "main", pinnedBranchSHA: sha,
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.Contains(t, args[3], "/jobs?")
		return []byte(`{"total_count":1,"jobs":[{"name":"agent","conclusion":"failure"}]}`), nil
	}
	config := HealthConfig{Full: true, Threshold: 80, RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
	evidence, err := verifyHealthFleetWorkflow(GitHubWorkflow{Name: "Example", Path: path}, []WorkflowRun{run}, config)
	require.NoError(t, err)
	require.True(t, evidence.Health.IntentionalFailure)
	require.Len(t, evidence.Failures, 1)
	require.True(t, evidence.Failures[0].IntentionalFailure)
	state := &healthFleetState{Workflows: []GitHubWorkflow{{Name: "Example", Path: path}},
		Verified: map[string]healthFleetWorkflowEvidence{path: evidence}}
	report := makeHealthFleetReport(state, config)
	require.Empty(t, report.Details.FailureRepresentatives)
	require.Nil(t, report.NextAction)
}

func TestHealthFleetReviewHistoricalFailureConclusions(t *testing.T) {
	runs := map[int64]WorkflowRun{}
	for i, conclusion := range []string{"failure", "timed_out", "cancelled", "startup_failure", "success", "skipped", "action_required"} {
		runs[int64(i+1)] = WorkflowRun{DatabaseID: int64(i + 1), WorkflowName: "Review",
			WorkflowPath: ".github/workflows/review.lock.yml", Conclusion: conclusion}
	}
	report := makeHealthFleetReport(&healthFleetState{Runs: runs}, HealthConfig{collection: &HealthCollection{}})
	require.Len(t, report.TopFailures, 1)
	require.Equal(t, 4, report.TopFailures[0].Failures)
	require.Len(t, report.Outcomes, len(runs))
}
