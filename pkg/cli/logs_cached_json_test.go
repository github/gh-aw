//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCachedLogsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	data, err := json.Marshal(LogsData{Runs: []RunData{
		{RunID: 42, WorkflowName: "cached-workflow"},
		{RunID: 0, WorkflowName: "invalid"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	runs, err := loadCachedLogsJSON(path)

	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "cached-workflow", runs[42].WorkflowName)
}

func TestLoadCachedLogsJSONRejectsInvalidInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"summary":{}}`), 0o600))

	_, err := loadCachedLogsJSON(path)

	require.ErrorContains(t, err, "missing runs array")
}

func TestCachedLogsLookupHonorsRepositoryAndFilters(t *testing.T) {
	runs := cachedLogsRuns{
		42: {RunID: 42, Repository: "github/gh-aw", EngineID: "copilot"},
	}
	run := WorkflowRun{DatabaseID: 42, Repository: "github/gh-aw"}

	_, ok := runs.lookup(run, runFilterOpts{engine: "copilot"})
	assert.True(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{engine: "claude"})
	assert.False(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{runtime: "gvisor"})
	assert.False(t, ok)
	_, ok = runs.lookup(WorkflowRun{DatabaseID: 42, Repository: "other/repo"}, runFilterOpts{})
	assert.False(t, ok)
}

func TestDownloadRunArtifactsConcurrentReusesCachedJSONRecord(t *testing.T) {
	cached := RunData{
		RunID:        42,
		WorkflowName: "cached-workflow",
		LogsPath:     "/previous/run-42",
	}

	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{DatabaseID: 42}}, runArtifactsConcurrentOptions{
		outputDir:    t.TempDir(),
		maxRuns:      1,
		cachedRuns:   cachedLogsRuns{42: cached},
		storageLimit: newLogsStorageLimit(t.TempDir(), 0, false),
	})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].CachedRun)
	assert.True(t, results[0].Cached)
	assert.Equal(t, cached, *results[0].CachedRun)
}

func TestBuildLogsDataPreservesCachedRunRecord(t *testing.T) {
	cached := RunData{
		RunID:              42,
		WorkflowName:       "cached-workflow",
		Status:             "completed",
		Conclusion:         "success",
		Duration:           "2m0s",
		TokenUsage:         1200,
		Turns:              4,
		ErrorCount:         1,
		WarningCount:       2,
		GitHubAPICalls:     3,
		EngineID:           "copilot",
		CreatedAt:          time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		LogsPath:           "/previous/run-42",
		Classification:     "normal",
		IntentionalFailure: true,
	}

	data := buildLogsData([]ProcessedRun{processedRunFromCachedData(cached)}, t.TempDir(), nil)

	require.Equal(t, []RunData{cached}, data.Runs)
	assert.Equal(t, 1, data.Summary.TotalRuns)
	assert.Equal(t, "2.0m", data.Summary.TotalDuration)
	assert.Equal(t, 1200, data.Summary.TotalTokens)
	assert.Equal(t, 4, data.Summary.TotalTurns)
	assert.Equal(t, 3, data.Summary.TotalGitHubAPICalls)
	assert.Equal(t, map[string]int{"copilot": 1}, data.Summary.EngineCounts)
	assert.Equal(t, 1, data.Summary.IntentionalFailureRuns)
}
