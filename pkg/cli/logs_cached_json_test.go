//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
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

func TestLoadCachedLogsJSONReportsFoundFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runs":[]}`), 0o600))

	_, stderr := captureOutput(t, func() error {
		_, err := loadCachedLogsJSON(path)
		return err
	})

	assert.Contains(t, stderr, "Found cached logs JSON file: "+path)
}

func TestLoadCachedLogsJSONIgnoresMissingFile(t *testing.T) {
	runs, err := loadCachedLogsJSON(filepath.Join(t.TempDir(), "missing.json"))

	require.NoError(t, err)
	assert.Nil(t, runs)
}

func TestLoadCachedLogsJSONReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	_, stderr := captureOutput(t, func() error {
		_, err := loadCachedLogsJSON(path)
		return err
	})

	assert.Contains(t, stderr, "Cached logs JSON file not found: "+path)
}

func TestLoadCachedLogsJSONRejectsInvalidInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"summary":{}}`), 0o600))

	_, err := loadCachedLogsJSON(path)

	require.ErrorContains(t, err, "missing runs array")
}

func TestLoadCachedLogsJSONRejectsInvalidRunAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runs":[{"run_id":42,"run_attempt":"bogus"}]}`), 0o600))

	_, err := loadCachedLogsJSON(path)

	require.ErrorContains(t, err, "invalid run_attempt")
}

func TestWriteCachedLogsJSONUpdatesFileInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runs":[{"run_id":1}]}`), 0o600))
	data := LogsData{
		Runs: []RunData{{
			RunID:             2,
			WorkflowName:      "updated-workflow",
			TokenUsageSummary: &TokenUsageSummary{TotalInputTokens: 100},
		}},
	}

	data.Summary.TotalRuns = 1

	require.NoError(t, writeCachedLogsJSON(path, data, false))

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	var result LogsData
	require.NoError(t, json.Unmarshal(updated, &result))
	require.Len(t, result.Runs, 1)
	assert.Equal(t, int64(2), result.Runs[0].RunID)
	assert.Equal(t, "updated-workflow", result.Runs[0].WorkflowName)
	assert.Nil(t, result.Runs[0].TokenUsageSummary, "cached output should match the compact JSON response")
}

func TestWriteCachedLogsJSONReportsWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")

	_, stderr := captureOutput(t, func() error {
		return writeCachedLogsJSON(path, LogsData{Runs: []RunData{}}, false)
	})

	assert.Contains(t, stderr, "Writing cached logs JSON file: "+path)
}

func TestDownloadWorkflowLogsFromEmptyStdinUpdatesCachedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runs":[{"run_id":1}]}`), 0o600))

	err := DownloadWorkflowLogsFromStdin(context.Background(), StdinLogsOptions{
		OutputDir:  t.TempDir(),
		CachedJSON: path,
	})
	require.NoError(t, err)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	var result LogsData
	require.NoError(t, json.Unmarshal(updated, &result))
	assert.Empty(t, result.Runs)
	assert.Equal(t, "No runs found. No run IDs or URLs were provided on stdin.", result.Message)
}

func TestPrepareLogsDataUpdatesCachedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runs":[]}`), 0o600))

	_, err := prepareLogsData([]ProcessedRun{{
		Run: WorkflowRun{
			DatabaseID:   42,
			WorkflowName: "updated-workflow",
			Status:       "completed",
		},
	}}, renderLogsOutputOptions{
		outputDir:  t.TempDir(),
		cachedJSON: path,
	})
	require.NoError(t, err)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	var result LogsData
	require.NoError(t, json.Unmarshal(updated, &result))
	require.Len(t, result.Runs, 1)
	assert.Equal(t, int64(42), result.Runs[0].RunID)
	assert.Equal(t, "updated-workflow", result.Runs[0].WorkflowName)
}

func TestCachedLogsLookupHonorsRepositoryAndFilters(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {RunID: 42, Repository: "github/gh-aw", EngineID: "copilot", Status: "completed", Conclusion: "success", RunAttempt: "1", UpdatedAt: updatedAt},
	}
	run := WorkflowRun{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt}

	_, ok := runs.lookup(run, runFilterOpts{engine: "copilot"})
	assert.True(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{engine: "claude"})
	assert.False(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{runtime: "gvisor"})
	assert.False(t, ok)
	_, ok = runs.lookup(WorkflowRun{DatabaseID: 42, Repository: "other/repo", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt}, runFilterOpts{})
	assert.False(t, ok)
}

func TestCachedLogsLookupRejectsChangedRun(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {
			RunID:      42,
			Status:     "completed",
			Conclusion: "success",
			RunAttempt: "1",
			UpdatedAt:  updatedAt,
		},
	}

	tests := []WorkflowRun{
		{DatabaseID: 42, Status: "in_progress", Conclusion: "", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "failure", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 2, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt.Add(time.Minute)},
	}
	for _, run := range tests {
		_, ok := runs.lookup(run, runFilterOpts{})
		assert.False(t, ok)
	}
}

func TestCachedLogsLookupRejectsUnknownIdentity(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {RunID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", RunAttempt: "1", UpdatedAt: updatedAt},
		43: {RunID: 43, Status: "completed", Conclusion: "success", UpdatedAt: updatedAt},
	}

	tests := []WorkflowRun{
		{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 43, Status: "completed", Conclusion: "success", UpdatedAt: updatedAt},
	}
	for _, run := range tests {
		_, ok := runs.lookup(run, runFilterOpts{})
		assert.False(t, ok)
	}
}

func TestCachedJSONCanSatisfy(t *testing.T) {
	usageFilter := []string{constants.UsageArtifactName.String()}
	assert.True(t, cachedJSONCanSatisfy(usageFilter, false, false, false, false))
	assert.False(t, cachedJSONCanSatisfy(nil, false, false, false, false))
	assert.False(t, cachedJSONCanSatisfy(usageFilter, true, false, false, false))
	assert.False(t, cachedJSONCanSatisfy(usageFilter, false, true, false, false))
	assert.False(t, cachedJSONCanSatisfy(usageFilter, false, false, true, false))
	assert.False(t, cachedJSONCanSatisfy(usageFilter, false, false, false, true))
}

func TestDownloadRunArtifactsConcurrentReusesCachedJSONRecord(t *testing.T) {
	cached := RunData{
		RunID:        42,
		WorkflowName: "cached-workflow",
		Repository:   "github/gh-aw",
		Status:       "completed",
		Conclusion:   "success",
		RunAttempt:   "1",
		UpdatedAt:    time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		LogsPath:     "/previous/run-42",
	}

	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: cached.UpdatedAt}}, runArtifactsConcurrentOptions{
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
		RunID:                      42,
		WorkflowName:               "cached-workflow",
		Status:                     "completed",
		Conclusion:                 "success",
		Duration:                   "2m0s",
		TokenUsageSummary:          &TokenUsageSummary{TotalSteeringEvents: 2},
		TokenUsage:                 1200,
		Turns:                      4,
		ErrorCount:                 1,
		WarningCount:               2,
		GitHubAPICalls:             3,
		TemporaryIDMappings:        4,
		ChainedTargetCount:         1,
		ChainedFollowupActionCount: 2,
		DelegatedTempTargetCount:   1,
		TemporaryIDMapStatus:       temporaryIDMapStatusMissing,
		EngineID:                   "copilot",
		CreatedAt:                  time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		LogsPath:                   "/previous/run-42",
		Classification:             "normal",
		IntentionalFailure:         true,
	}

	processedRun := processedRunFromCachedData(cached)
	assert.Empty(t, processedRun.Run.LogsPath)

	data := buildLogsData([]ProcessedRun{processedRun}, t.TempDir(), nil)

	require.Equal(t, []RunData{cached}, data.Runs)
	assert.Equal(t, 1, data.Summary.TotalRuns)
	assert.Equal(t, "2.0m", data.Summary.TotalDuration)
	assert.Equal(t, 2, data.Summary.TotalSteeringEvents)
	assert.Equal(t, 1200, data.Summary.TotalTokens)
	assert.Equal(t, 4, data.Summary.TotalTurns)
	assert.Equal(t, 3, data.Summary.TotalGitHubAPICalls)
	assert.Equal(t, 1, data.Summary.RunsWithTemporaryIDChains)
	assert.Equal(t, 1, data.Summary.RunsWithDelegatedTempTargets)
	assert.Equal(t, 1, data.Summary.RunsWithMissingTemporaryIDMap)
	assert.Equal(t, 4, data.Summary.TotalTemporaryIDMappings)
	assert.Equal(t, 1, data.Summary.TotalChainedTargets)
	assert.Equal(t, 2, data.Summary.TotalChainedFollowupActions)
	assert.Equal(t, map[string]int{"copilot": 1}, data.Summary.EngineCounts)
	assert.Equal(t, 1, data.Summary.IntentionalFailureRuns)
}
