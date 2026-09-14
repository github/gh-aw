//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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

	for query := range queryCount {
		outputDir := filepath.Join(tempDir, fmt.Sprintf("query-%d", query))
		runLiveLogsCommand(t, cachePattern, outputDir, runsPerQuery)

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
		} else {
			require.ElementsMatch(t, expectedRunIDs, runIDs, "cached queries should return the same runs")
		}
	}

	shards, err := filepath.Glob(cachePattern + ".jsonl")
	require.NoError(t, err)
	cache, err := loadCachedLogsJSONLFiles(shards)
	require.NoError(t, err)
	require.Len(t, cache.runs, runsPerQuery, "the wildcard cache should contain the queried runs")
}

func runLiveLogsCommand(t *testing.T, cachePath, outputDir string, count int) {
	t.Helper()
	cmd := NewLogsCommand()
	cmd.SetArgs([]string{
		"Daily Fact",
		"--repo", "github/gh-aw",
		"--count", fmt.Sprintf("%d", count),
		"--start-date", "-3mo",
		"--end-date", "-1d",
		"--cached-jsonl", cachePath,
		"--output", outputDir,
	})
	require.NoError(t, cmd.Execute())
}
