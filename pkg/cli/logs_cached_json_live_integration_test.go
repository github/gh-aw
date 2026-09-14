//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLogsCachedJSONLLiveCaching(t *testing.T) {
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		t.Skip("GitHub authentication is required for the live logs cache integration test")
	}

	tempDir := t.TempDir()
	cachePattern := filepath.Join(tempDir, "logs-*")
	const queryCount = 3
	const runsPerQuery = 2
	var expectedRunIDs []int64
	startDate, endDate, err := resolveLogsDateRange("-3mo", "-1d", time.Now())
	require.NoError(t, err)

	for query := range queryCount {
		outputDir := filepath.Join(tempDir, fmt.Sprintf("query-%d", query))
		runLiveLogsCommand(t, cachePattern, outputDir, runsPerQuery, startDate, endDate)

		shards, err := filepath.Glob(cachePattern + ".jsonl")
		require.NoError(t, err)
		require.Len(t, shards, query+1, "each query should create a new JSONL cache shard")

		summaryData, err := os.ReadFile(filepath.Join(outputDir, "summary.json"))
		require.NoError(t, err)
		var summary LogsData
		require.NoError(t, json.Unmarshal(summaryData, &summary))
		require.Len(t, summary.Runs, runsPerQuery)

		runIDs := make([]int64, 0, len(summary.Runs))
		for _, run := range summary.Runs {
			runIDs = append(runIDs, run.RunID)
		}
		if query == 0 {
			expectedRunIDs = runIDs
			for _, runID := range expectedRunIDs {
				require.DirExists(t, filepath.Join(outputDir, fmt.Sprintf("run-%d", runID)),
					"the first call should download the live run")
			}
		} else {
			require.ElementsMatch(t, expectedRunIDs, runIDs, "cached queries should return the same runs")
			for _, runID := range expectedRunIDs {
				require.NoDirExists(t, filepath.Join(outputDir, fmt.Sprintf("run-%d", runID)),
					"cached queries should reuse JSONL records instead of downloading runs")
			}
		}
	}

	shards, err := filepath.Glob(cachePattern + ".jsonl")
	require.NoError(t, err)
	cache, err := loadCachedLogsJSONLFiles(shards)
	require.NoError(t, err)
	require.Len(t, cache.runs, runsPerQuery, "the wildcard cache should contain the queried runs")
	for _, runID := range expectedRunIDs {
		require.Contains(t, cache.runs, runID, "the wildcard cache should contain each queried run")
	}
}

func runLiveLogsCommand(t *testing.T, cachePattern, outputDir string, count int, startDate, endDate string) {
	t.Helper()
	cmd := NewLogsCommand()
	cmd.SetArgs([]string{
		"Daily Fact",
		"--repo", "github/gh-aw",
		"--count", fmt.Sprintf("%d", count),
		"--start-date", startDate,
		"--end-date", endDate,
		"--cached-jsonl", cachePattern,
		"--output", outputDir,
	})
	require.NoError(t, cmd.Execute())
}
