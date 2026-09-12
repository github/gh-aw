//go:build integration

package cli

import (
	"fmt"
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
	cachePath := filepath.Join(tempDir, "logs.jsonl")
	firstOutputDir := filepath.Join(tempDir, "first")
	runLiveLogsCommand(t, cachePath, firstOutputDir)

	cache, err := loadCachedLogsJSONL(cachePath)
	require.NoError(t, err)
	require.Len(t, cache.runs, 1, "the live call should cache exactly one run")

	var runID int64
	for runID = range cache.runs {
		break
	}
	require.DirExists(t, filepath.Join(firstOutputDir, fmt.Sprintf("run-%d", runID)),
		"the first call should download the live run")

	secondOutputDir := filepath.Join(tempDir, "second")
	runLiveLogsCommand(t, cachePath, secondOutputDir)

	require.NoDirExists(t, filepath.Join(secondOutputDir, fmt.Sprintf("run-%d", runID)),
		"the second call should reuse the JSONL record instead of downloading the run")
}

func runLiveLogsCommand(t *testing.T, cachePath, outputDir string) {
	t.Helper()
	cmd := NewLogsCommand()
	cmd.SetArgs([]string{
		"Daily Fact",
		"--repo", "github/gh-aw",
		"--count", "1",
		"--end-date", "-1d",
		"--cached-jsonl", cachePath,
		"--output", outputDir,
	})
	require.NoError(t, cmd.Execute())
}
