package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOfflineGradersRejectsSummaryIdentityConflicts(t *testing.T) {
	for _, test := range []struct {
		name, host, repository, attempt, message string
	}{
		{"host", "ghe.example", "owner/repo", "2", "host"},
		{"repository", "github.com", "other/repo", "2", "repository"},
		{"attempt", "github.com", "owner/repo", "3", "attempt"},
		{"invalid attempt", "github.com", "owner/repo", "invalid", "invalid recorded attempt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			regradeTestFile(t, root, "run-77/run.json", `{"id":77,"run_attempt":2,"html_url":"https://github.com/owner/repo/actions/runs/77","repository":{"full_name":"owner/repo"}}`)
			summary, err := json.Marshal(map[string]any{"runs": []map[string]any{{
				"run_id": 77, "repository": test.repository, "run_attempt": test.attempt,
				"url": "https://" + test.host + "/" + test.repository + "/actions/runs/77", "logs_path": "run-77",
				"graders": map[string]any{"results": []map[string]any{{"id": "recorded", "value": 99}}},
			}}})
			require.NoError(t, err)
			regradeTestFile(t, root, "summary.json", string(summary))
			_, err = loadOfflineGradingRuns(root)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestOfflineGradersRejectsRawAndRunSummaryAttemptConflict(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "run-77/run.json", `{"id":77,"run_attempt":2,"html_url":"https://github.com/owner/repo/actions/runs/77","repository":{"full_name":"owner/repo"}}`)
	regradeTestFile(t, root, "run-77/run_summary.json", `{"run_id":77,"run":{"databaseId":77,"attempt":3}}`)
	_, err := loadOfflineGradingRuns(root)
	require.ErrorContains(t, err, "conflicting attempt")
}

func TestOfflineGradersCompatibleIdentityMetadata(t *testing.T) {
	expected := WorkflowRun{DatabaseID: 77, Attempt: 2, Repository: "OWNER/REPO", URL: "https://GITHUB.COM/owner/repo/actions/runs/77"}
	actual := WorkflowRun{DatabaseID: 77, Attempt: 2, Repository: "owner/repo", URL: "https://github.com/owner/repo/actions/runs/77"}
	require.NoError(t, validateOfflineRunIdentity(expected, actual))
	require.NoError(t, validateOfflineRunIdentity(expected, WorkflowRun{DatabaseID: 77}))
	expected.URL = "https://github.com/owner/repo/actions/runs/88"
	require.ErrorContains(t, validateOfflineRunIdentity(expected, actual), "run ID")
}

func TestOfflineGradersMissingCollectionSourceRetainsMetadata(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "summary.json", `{"runs":[{"run_id":77,"repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/77","run_attempt":"4","status":"completed","conclusion":"failure","logs_path":"run-77"}]}`)
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	results, err := regradeProcessedRuns(t.Context(), root, runs, []graderRunDefinition{{ID: "loops", Source: "builtin"}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	for _, result := range results {
		assert.Equal(t, "github.com/owner/repo/77/4", result.Identity)
		assert.Equal(t, "completed", result.Run.Status)
		assert.Equal(t, "failure", result.Run.Conclusion)
		assert.Equal(t, "missing", result.SessionStatus)
		assert.Equal(t, filepath.Join(root, "run-77"), result.SourceDirectory)
		for _, raw := range result.Results {
			var record map[string]any
			require.NoError(t, json.Unmarshal(raw, &record))
			assert.Equal(t, "unavailable", record["status"])
			assert.Nil(t, record["value"])
		}

	}
}

func TestOfflineGradersRejectsConflictingOriginalScores(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "summary.json", `{"runs":[{"run_id":77,"repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/77","run_attempt":"1","graders":{"results":[{"id":"quality","value":1}]}},{"run_id":77,"repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/77","run_attempt":"1","graders":{"results":[{"id":"quality","value":2}]}}]}`)
	run := ProcessedRun{Run: WorkflowRun{DatabaseID: 77, Repository: "owner/repo", Attempt: 1}}
	_, err := offlineGradingOriginals(root, []ProcessedRun{run, run})
	require.ErrorContains(t, err, "conflicting recorded grader evidence")
}

func TestOfflineGradersOriginalsMatchSummaryByIdentityNotOrder(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "summary.json", `{"runs":[{"run_id":1,"repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/1","run_attempt":"1","graders":{"results":[{"id":"quality","value":1}]}},{"run_id":2,"repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/2","run_attempt":"2","graders":{"results":[{"id":"quality","value":2}]}}]}`)
	first := ProcessedRun{Run: WorkflowRun{DatabaseID: 1, Repository: "owner/repo", Attempt: 1, URL: "https://github.com/owner/repo/actions/runs/1"}}
	second := ProcessedRun{Run: WorkflowRun{DatabaseID: 2, Repository: "owner/repo", Attempt: 2, URL: "https://github.com/owner/repo/actions/runs/2"}}
	originals, err := offlineGradingOriginals(root, []ProcessedRun{second, first})
	require.NoError(t, err)
	assert.JSONEq(t, `{"results":[{"id":"quality","value":1}]}`, string(originals[harborRunIdentity(first)].Data))
	assert.JSONEq(t, `{"results":[{"id":"quality","value":2}]}`, string(originals[harborRunIdentity(second)].Data))
}
