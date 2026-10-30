package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func regradeEpisodeFixture(t *testing.T, root string, attempt int, collection string) string {
	t.Helper()
	attemptDir := filepath.Join(root, "episodes", "github.com", "owner", "repo", "run-77", "attempt-"+strconv.Itoa(attempt))
	source := filepath.Join(attemptDir, collection)
	regradeTestFile(t, source, ".episode-artifacts.json", `["../20-usage"]`)
	regradeTestFile(t, source, "aw_info.json", fmt.Sprintf(`{"run_id":77,"run_attempt":"%d","repository":"owner/repo","engine_id":"copilot"}`, attempt))
	regradeTestFile(t, attemptDir, "20-usage/.episode-complete", "v1\n")
	regradeTestFile(t, attemptDir, "20-usage/aw_session.jsonl", regradeTestEvent)
	regradeTestFile(t, attemptDir, "20-usage/grader_results.json", `{"version":2,"results":[{"id":"recorded","status":"pass","value":2}]}`)
	return source
}

func TestRegradeEpisodeCollectionInventorySession(t *testing.T) {
	root := t.TempDir()
	source := regradeEpisodeFixture(t, root, 2, "collection-a")
	processed := ProcessedRun{Run: WorkflowRun{DatabaseID: 77, Attempt: 2, Repository: "owner/repo",
		URL: "https://github.com/owner/repo/actions/runs/77", LogsPath: source}}
	results, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{processed}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	for _, result := range results {
		assert.Equal(t, "recorded", result.SessionStatus)
		assert.Equal(t, filepath.Join(filepath.Dir(source), "20-usage", "aw_session.jsonl"), result.SessionPath)
		assert.Equal(t, regradeDigest([]byte(regradeTestEvent)), result.SessionDigest)
		assert.NotEmpty(t, result.OriginalGraders)
		assert.NotEmpty(t, result.OriginalEvidence)
	}
}

func TestOfflineGradersEpisodeAttemptDiscovery(t *testing.T) {
	root := t.TempDir()
	first := regradeEpisodeFixture(t, root, 2, "collection-a")
	second := regradeEpisodeFixture(t, root, 3, "collection-c")
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	paths := map[int]string{}
	for _, run := range runs {
		paths[run.Run.Attempt] = run.Run.LogsPath
		assert.Equal(t, "owner/repo", run.Run.Repository)
		assert.Equal(t, "https://github.com/owner/repo/actions/runs/77", run.Run.URL)
	}
	assert.Equal(t, first, paths[2])
	assert.Equal(t, second, paths[3])
	results, err := regradeProcessedRuns(t.Context(), root, runs, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.Equal(t, "recorded", result.SessionStatus)
		assert.Empty(t, result.IdentityMissing)
	}
}

func TestOfflineGradersRejectsDuplicateEpisodeIdentities(t *testing.T) {
	root := t.TempDir()
	regradeEpisodeFixture(t, root, 2, "collection-a")
	regradeEpisodeFixture(t, root, 2, "collection-b")
	_, err := loadOfflineGradingRuns(root)
	require.ErrorContains(t, err, "duplicate episode collection identity")
}

func TestOfflineGradersEpisodeSummaryAttemptConflict(t *testing.T) {
	root := t.TempDir()
	source := regradeEpisodeFixture(t, root, 2, "collection-a")
	summary, err := json.Marshal(map[string]any{"runs": []map[string]any{{
		"run_id": 77, "repository": "owner/repo", "run_attempt": "3",
		"url": "https://github.com/owner/repo/actions/runs/77", "logs_path": source,
	}}})
	require.NoError(t, err)
	regradeTestFile(t, root, "summary.json", string(summary))
	_, err = loadOfflineGradingRuns(root)
	require.ErrorContains(t, err, "conflicting attempt")
}

func TestRegradeEpisodeInvalidInventoryIsExplicit(t *testing.T) {
	root := t.TempDir()
	source := regradeEpisodeFixture(t, root, 2, "collection-a")
	regradeTestFile(t, source, ".episode-artifacts.json", `["../../escape"]`)
	record := RegradedRun{SessionStatus: "missing"}
	_, err := regradeSessionPayload(t.Context(), source, &record)
	require.ErrorContains(t, err, "invalid episode artifact inventory")
	assert.Equal(t, "invalid", record.SessionStatus)
	assert.Empty(t, record.SessionDigest)
}

func TestOfflineGradersEpisodeRejectsMissingCompletionAndHarbor(t *testing.T) {
	for _, marker := range []string{"incomplete", "Harbor"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			source := regradeEpisodeFixture(t, root, 2, "collection-a")
			artifact := filepath.Join(filepath.Dir(source), "20-usage")
			if marker == "incomplete" {
				require.NoError(t, os.Remove(filepath.Join(artifact, ".episode-complete")))
			} else {
				regradeTestFile(t, artifact, "gh-aw-export.json", `{"format":"gh-aw-harbor"}`)
			}
			_, err := loadOfflineGradingRuns(root)
			require.Error(t, err)
		})
	}
}
