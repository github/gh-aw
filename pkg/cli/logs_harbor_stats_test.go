//go:build !integration

package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExportHarborJobStatsRecordedRewards(t *testing.T) {
	output, firstSource, secondSource := t.TempDir(), t.TempDir(), t.TempDir()
	writeSessionTestFile(t, firstSource, "grader_results.json",
		`{"results":[{"id":"quality","status":"pass","value":1},{"id":"latency","status":"fail","value":-4}]}`)
	writeSessionTestFile(t, secondSource, "grader_results.json",
		`{"results":[{"id":"quality","status":"fail","value":1.0},{"id":"missing","status":"unavailable"}]}`)
	first, second, missing := harborTestRun(firstSource), harborTestRun(secondSource), harborTestRun(t.TempDir())
	second.Run.DatabaseID = 124
	missing.Run.DatabaseID = 125
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{first, second, missing}))
	job := harborTestJSON(t, filepath.Join(output, harborExportDirectory, "result.json"))
	stats := job["stats"].(map[string]any)
	require.EqualValues(t, 3, stats["n_completed_trials"])
	require.EqualValues(t, 0, stats["n_errored_trials"])
	evals := stats["evals"].(map[string]any)
	require.Len(t, evals, 1)
	group := evals["gh-aw__adhoc"].(map[string]any)
	require.EqualValues(t, 2, group["n_trials"])
	require.Empty(t, group["metrics"])
	require.Empty(t, group["pass_at_k"])
	require.Empty(t, group["exception_stats"])
	rewards := group["reward_stats"].(map[string]any)
	require.Len(t, rewards, 2)
	quality := rewards["gh-aw.quality"].(map[string]any)
	require.Len(t, quality, 1)
	require.Len(t, quality["1"], 2)
	latency := rewards["gh-aw.latency"].(map[string]any)
	require.Len(t, latency["-4"], 1)
	require.NotContains(t, rewards, "reward")
	require.NotContains(t, rewards, "success")
}

func TestHarborRewardStatsNumericGrouping(t *testing.T) {
	for original, expected := range map[string]string{
		"1": "1", "1.0": "1", "1e0": "1",
		"-0": "0", "-0.0": "0", "0e1": "0",
		"-7.25": "-7.25", "0.1": "0.1",
		"9007199254740993":   "9007199254740993",
		"9007199254740993.0": "9007199254740992",
		"1e20":               "100000000000000000000",
	} {
		t.Run(original, func(t *testing.T) {
			require.Equal(t, expected, harborRewardStatsKey(json.Number(original)))
		})
	}
}
