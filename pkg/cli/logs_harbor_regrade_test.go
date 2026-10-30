//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogsHarborAdditionalGraderOptions(t *testing.T) {
	workflow := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\n---\nTest.\n")
	cmd := NewLogsCommand()
	require.NoError(t, cmd.ParseFlags([]string{"--harbor", "--additional-grader", workflow + ":loops"}))
	opts, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	require.Equal(t, []string{workflow + ":loops"}, opts.AdditionalGraders)
	stdin, err := loadStdinLogsOptions(cmd)
	require.NoError(t, err)
	require.Equal(t, opts.AdditionalGraders, stdin.AdditionalGraders)
	require.False(t, opts.GradersOnly, "additional graders must not filter runs by original scores")

	for _, args := range [][]string{
		{"--additional-grader", workflow + ":loops"},
		{"--harbor", "--additional-grader", workflow + ":unknown"},
		{"--harbor", "--additional-grader", "invalid"},
	} {
		cmd := NewLogsCommand()
		require.NoError(t, cmd.ParseFlags(args))
		_, err := loadCommonLogsOptions(cmd)
		require.Error(t, err)
	}
}

func TestLogsHarborRegradingPreservesEvidence(t *testing.T) {
	workflow := writeGraderRunWorkflow(t, `---
on: workflow_dispatch
graders:
  recorded-count:
    script: return session.filter(e => e.type === "assistant.message").length;
  absent:
    script: return null;
  broken:
    script: throw new Error("expected failure");
---
Test.
`)
	root := t.TempDir()
	source := filepath.Join(root, "run-123")
	session := sessionTestHeader + sessionTestAgentEvent
	const original = `{"version":2,"results":[{"id":"recorded-count","status":"fail","value":-2,"implementation":{"version":"original"}}]}`
	writeSessionTestFile(t, source, "usage/aw_session.jsonl", session)
	writeSessionTestFile(t, source, "graders/grader_results.json", original)
	runs := []ProcessedRun{harborTestRun(source)}
	missing := harborTestRun(filepath.Join(root, "run-124"))
	missing.Run.DatabaseID = 124
	missing.Run.Attempt = 2
	missing.Run.Conclusion = "failure"
	runs = append(runs, missing)
	selectors := []string{workflow + ":recorded-count", workflow + ":absent", workflow + ":broken"}

	output := filepath.Join(root, "export")
	require.NoError(t, exportLogsHarbor(t.Context(), output, runs, selectors))
	require.Nil(t, runs[0].Regrading, "ordinary report records must remain unchanged")
	firstRecords := make(map[string][]byte)
	for _, name := range harborTestTrials(t, output) {
		dir := filepath.Join(output, harborExportDirectory, name)
		metadata := harborTestJSON(t, filepath.Join(dir, "gh-aw", "metadata.json"))
		require.Equal(t, "gh-aw/regrading.json", metadata["regrading"])
		raw, err := os.ReadFile(filepath.Join(dir, "gh-aw", "regrading.json"))
		require.NoError(t, err)
		firstRecords[name] = raw
		result := harborTestJSON(t, filepath.Join(dir, "result.json"))
		run := metadata["run"].(map[string]any)
		if run["databaseId"] == float64(124) {
			require.Equal(t, "failure", run["conclusion"])
			require.InDelta(t, 2, run["attempt"], 0)
			require.Nil(t, result["verifier_result"])
			continue
		}
		rewards := result["verifier_result"].(map[string]any)["rewards"]
		require.Equal(t, map[string]any{"gh-aw.recorded-count": float64(-2), "gh-aw.regraded.recorded-count": float64(1)}, rewards)
		snapshot, err := os.ReadFile(filepath.Join(dir, "gh-aw", "evidence", "graders", "grader_results.json"))
		require.NoError(t, err)
		require.Equal(t, original, string(snapshot)) //nolint:testifylint // Preserve exact historical evidence bytes, not just JSON equivalence.
		var record struct {
			Results []struct {
				ID             string          `json:"id"`
				Status         string          `json:"status"`
				Implementation json.RawMessage `json:"implementation"`
			} `json:"results"`
		}
		require.NoError(t, json.Unmarshal(raw, &record))
		statuses := map[string]string{}
		for _, r := range record.Results {
			statuses[r.ID] = r.Status
			require.NotEmpty(t, r.Implementation)
		}
		require.Equal(t, map[string]string{"recorded-count": "pass", "absent": "unavailable", "broken": "error"}, statuses)
	}
	require.NoError(t, exportLogsHarbor(t.Context(), output, runs, selectors))
	for name, originalRecord := range firstRecords {
		raw, err := os.ReadFile(filepath.Join(output, harborExportDirectory, name, "gh-aw", "regrading.json"))
		require.NoError(t, err)
		require.JSONEq(t, string(originalRecord), string(raw), "regrading must be deterministic")
	}
	raw, err := os.ReadFile(filepath.Join(source, "usage", "aw_session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, session, string(raw))
}

func TestHarborRegradedRewardCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gh-aw"), 0o700))
	record := &RegradedRun{Results: []json.RawMessage{json.RawMessage(`{"id":"loops","status":"pass","value":1}`)}}
	_, _, err := harborRegradedRewards(dir, record, map[string]any{},
		map[string]json.Number{"gh-aw.regraded.loops": "99"}, map[string]any{"metrics": map[string]any{}})
	require.ErrorContains(t, err, "conflicts with recorded reward")
	record.Results = append(record.Results, json.RawMessage(`{"id":"loops","status":"unavailable","value":null}`))
	_, _, err = harborRegradedRewards(dir, record, map[string]any{}, nil, nil)
	require.ErrorContains(t, err, "duplicate additional grader reward identity")
}
