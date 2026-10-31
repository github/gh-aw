package cli

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegradeValidateRecordedIdentityAttemptBounds(t *testing.T) {
	for _, test := range []struct {
		name, attempt, message string
	}{
		{"missing", "", ""},
		{"null", "null", ""},
		{"one", "1", ""},
		{"native maximum", strconv.FormatInt(math.MaxInt, 10), ""},
		{"quoted native maximum", strconv.Quote(strconv.FormatInt(math.MaxInt, 10)), ""},
		{"32-bit maximum", "2147483647", ""},
		{"zero", "0", "must be a positive integer"},
		{"negative", "-1", "must be a positive integer"},
		{"native minimum", strconv.FormatInt(math.MinInt, 10), "must be a positive integer"},
		{"below int64 minimum", "-9223372036854775809", "must be a positive integer"},
		{"above int64 maximum", "9223372036854775808", "must be a positive integer"},
		{"fraction", "1.5", "must be a positive integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"run_id":77}`)
			if test.attempt != "" {
				raw = json.RawMessage(`{"run_id":77,"run_attempt":` + test.attempt + `}`)
			}
			err := regradeValidateRecordedIdentity(WorkflowRun{DatabaseID: 77}, raw)
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
			} else {
				require.NoError(t, err)
			}
		})
	}

	for _, attempt := range []string{"2147483648", "4294967297", "9223372036854775807"} {
		for _, recorded := range []string{attempt, strconv.Quote(attempt)} {
			t.Run("32-bit overflow/"+recorded, func(t *testing.T) {
				raw := json.RawMessage(`{"run_id":77,"run_attempt":` + recorded + `}`)
				err := regradeValidateRecordedIdentity(WorkflowRun{DatabaseID: 77, Attempt: 1}, raw)
				if strconv.IntSize == 32 {
					require.ErrorContains(t, err, "recorded run attempt is outside the supported range")
				} else {
					require.ErrorContains(t, err, "conflicting attempt metadata")
				}
			})
		}
	}
}

func TestRegradeValidateRecordedIdentityConflicts(t *testing.T) {
	expected := WorkflowRun{DatabaseID: 77, Attempt: 2, Repository: "owner/repo", URL: "https://github.com/owner/repo/actions/runs/77"}
	for _, test := range []struct {
		name, metadata, message string
	}{
		{"matching", `{"run_id":77,"run_attempt":2,"repository":"owner/repo","server_url":"https://github.com"}`, ""},
		{"aliases", `{"runId":"77","runAttempt":"2","repo":"OWNER/REPO","serverUrl":"https://GITHUB.COM"}`, ""},
		{"run ID", `{"run_id":88,"run_attempt":2}`, "conflicting run IDs"},
		{"attempt", `{"run_id":77,"run_attempt":3}`, "conflicting attempt metadata"},
		{"repository", `{"run_id":77,"run_attempt":2,"repository":"other/repo"}`, "conflicting repository metadata"},
		{"host", `{"run_id":77,"run_attempt":2,"server_url":"https://ghe.example"}`, "does not match selected host"},
		{"URL", `{"run_id":77,"run_attempt":2,"url":"https://github.com/owner/repo/actions/runs/88"}`, "URL conflicts with recorded run ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := regradeValidateRecordedIdentity(expected, json.RawMessage(test.metadata))
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

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
