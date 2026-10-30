//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func harborTestRun(source string) ProcessedRun {
	return ProcessedRun{Run: WorkflowRun{
		DatabaseID: 123, Attempt: 1, Repository: "owner/repo",
		URL:    "https://github.com/owner/repo/actions/runs/123",
		Status: "completed", Conclusion: "success", WorkflowName: "Example", LogsPath: source,
	}}
}

func harborTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	return document
}

func harborTestTrials(t *testing.T, output string) []string {
	t.Helper()
	result := harborTestJSON(t, filepath.Join(output, harborExportDirectory, "result.json"))
	trials := result["trial_results"].([]any)
	names := make([]string, 0, len(trials))
	for _, trial := range trials {
		names = append(names, trial.(map[string]any)["trial_name"].(string))
	}
	return names
}

func TestExportHarborMissingSessionUnknownRewards(t *testing.T) {
	for _, conclusion := range []string{"success", "failure", "cancelled", ""} {
		t.Run(conclusion, func(t *testing.T) {
			output := t.TempDir()
			run := harborTestRun(filepath.Join(output, "not-downloaded"))
			run.Run.Conclusion = conclusion
			require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
			trials := harborTestTrials(t, output)
			require.Len(t, trials, 1)
			trial := filepath.Join(output, harborExportDirectory, trials[0])
			result := harborTestJSON(t, filepath.Join(trial, "result.json"))
			require.Contains(t, result, "verifier_result")
			require.Nil(t, result["verifier_result"])
			require.Nil(t, result["agent_result"])
			require.Equal(t, "", result["task_checksum"])
			require.Nil(t, result["started_at"])
			require.Nil(t, result["finished_at"])
			require.Equal(t, map[string]any{"name": "gh-aw", "version": "unknown"}, result["agent_info"])
			require.NoFileExists(t, filepath.Join(trial, "verifier", "reward.json"))
			require.NoFileExists(t, filepath.Join(trial, "agent", "trajectory.json"))
			metadata := harborTestJSON(t, filepath.Join(trial, "gh-aw", "metadata.json"))
			require.Equal(t, "missing", metadata["session_status"])
			require.Nil(t, metadata["reward_mapping"])
			require.Nil(t, metadata["graders"])
			require.Empty(t, metadata["evidence_files"])
			require.Equal(t, conclusion, metadata["run"].(map[string]any)["conclusion"])
			config := harborTestJSON(t, filepath.Join(trial, "config.json"))
			require.NotEmpty(t, config["agent"].(map[string]any)["import_path"])
			require.Nil(t, config["environment"].(map[string]any)["type"])
			require.DirExists(t, filepath.Join(trial, config["task"].(map[string]any)["path"].(string)))
		})
	}
}

func TestExportHarborRecordedSessionAndEvidence(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	session := sessionTestHeader + sessionTestAgentEvent
	graders := `{"version":1,"results":[{"id":"quality","status":"pass","value":42,"passed":true}]}`
	manifest := `{"graders":[{"id":"quality","direction":"maximize","threshold":10}],"customGraders":["quality"]}`
	state := "{\"run_id\":\"123\",\"timestamp\":\"2026-10-01T00:00:00Z\",\"assignments\":{\"prompt\":\"B\"},\"baseline_counts\":{\"prompt\":{\"A\":2,\"B\":1}}}\n"
	fixtures := map[string]string{
		"usage/aw_session.jsonl": session, "graders/grader_results.json": graders,
		"graders/grader_manifest.json": manifest, "experiment/state.jsonl": state,
		"aw_info.json": `{"engine_id":"copilot","model":"recorded-model"}`,
		"run.json":     `{"run_attempt":1,"id":123}`, "jobs.json": `{"jobs":[]}`,
		"usage/activity/summary.json": `{"experiments":{"assignments":{"prompt":"B"}}}`,
	}
	for path, content := range fixtures {
		writeSessionTestFile(t, source, path, content)
	}
	writeSessionTestFile(t, source, "custom-grader.sh", "#!/bin/sh\nexit 99\n")
	writeSessionTestFile(t, source, "source-code.go", "not evidence")
	run := harborTestRun(source)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	assertATIFFile(t, filepath.Join(trial, "agent", "trajectory.json"))
	metadata := harborTestJSON(t, filepath.Join(trial, "gh-aw", "metadata.json"))
	require.Equal(t, "recorded", metadata["session_status"])
	require.Contains(t, metadata["agent_identity_provenance"], "ATIF converter")
	require.Equal(t, map[string]any{
		"name": "copilot", "version": "unknown", "model_info": map[string]any{"name": "recorded-model"},
	}, harborTestJSON(t, filepath.Join(trial, "result.json"))["agent_info"])
	require.Equal(t, "gh-aw/evidence", metadata["run"].(map[string]any)["LogsPath"])
	require.NotContains(t, metadata["run"].(map[string]any)["LogsPath"], source)
	require.Equal(t, "gh-aw/evidence/experiment/state.jsonl", metadata["experiment_state"])
	require.Contains(t, metadata["experiment_assignment_provenance"], "ledger")
	require.Equal(t, "123", metadata["experiment_assignment_run_id"])
	require.Equal(t, map[string]any{"prompt": "B"}, metadata["experiments"].(map[string]any)["assignments"])
	grader := metadata["graders"].(map[string]any)["results"].([]any)[0].(map[string]any)
	require.Equal(t, float64(42), grader["value"])
	require.Equal(t, true, grader["passed"])
	require.Equal(t, "maximize", grader["direction"])
	require.Equal(t, map[string]any{"rewards": map[string]any{"gh-aw.quality": float64(42)}},
		harborTestJSON(t, filepath.Join(trial, "result.json"))["verifier_result"])
	require.Equal(t, map[string]any{"gh-aw.quality": float64(42)},
		harborTestJSON(t, filepath.Join(trial, "verifier", "reward.json")))
	mapping := metadata["reward_mapping"].(map[string]any)["metrics"].(map[string]any)["gh-aw.quality"].(map[string]any)
	require.Equal(t, "gh-aw/evidence/graders/grader_results.json", mapping["source_file"])
	require.Equal(t, "none; original numeric JSON value", mapping["transformation"])
	require.Equal(t, "pass", mapping["status"])
	require.Equal(t, "maximize", mapping["direction"])
	require.Equal(t, float64(10), mapping["threshold"])
	for path, content := range fixtures {
		original, err := os.ReadFile(filepath.Join(source, path))
		require.NoError(t, err)
		copied, err := os.ReadFile(filepath.Join(trial, "gh-aw", "evidence", path))
		require.NoError(t, err)
		require.Equal(t, content, string(original))
		require.Equal(t, original, copied)
	}
	require.NoFileExists(t, filepath.Join(trial, "gh-aw", "evidence", "custom-grader.sh"))
	require.NoFileExists(t, filepath.Join(trial, "gh-aw", "evidence", "source-code.go"))
	require.NoFileExists(t, filepath.Join(source, "usage", "aw_session.atif"))
	require.NoError(t, os.RemoveAll(source))
	assertATIFFile(t, filepath.Join(trial, "agent", "trajectory.json"))
	snapshot := filepath.Join(trial, "gh-aw", "evidence")
	require.NotNil(t, extractGradersData(snapshot))
	require.NotNil(t, extractExperimentData(snapshot))
}

func TestExportHarborFallbackPathAndOverwrite(t *testing.T) {
	output := t.TempDir()
	source := resolveAuditOutputDir(output, 123)
	writeSessionTestFile(t, source, "aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
	writeSessionTestFile(t, output, "unrelated.txt", "keep")
	run := harborTestRun("")
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	assertATIFFile(t, filepath.Join(trial, "agent", "trajectory.json"))
	writeSessionTestFile(t, trial, "stale.txt", "remove on replacement")
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	require.NoFileExists(t, filepath.Join(trial, "stale.txt"))
	require.FileExists(t, filepath.Join(output, "unrelated.txt"))
	require.FileExists(t, filepath.Join(source, "aw_session.jsonl"))
	assertATIFFile(t, filepath.Join(trial, "agent", "trajectory.json"))
	require.NoError(t, exportHarborJob(t.Context(), output, nil))
	require.Empty(t, harborTestTrials(t, output))
	require.NoDirExists(t, trial)
}

func TestExportHarborStableRepositoryHostAttemptIDs(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	first := harborTestRun(source)
	repository := harborTestRun(source)
	repository.Run.Repository = "another/repo"
	repository.Run.URL = "https://github.com/another/repo/actions/runs/123"
	host := harborTestRun(source)
	host.Run.URL = "https://github.example.com/owner/repo/actions/runs/123"
	attempt := harborTestRun(source)
	attempt.Run.Attempt = 2
	runs := []ProcessedRun{first, repository, host, attempt, first}
	require.NoError(t, exportHarborJob(t.Context(), output, runs))
	names := harborTestTrials(t, output)
	require.Len(t, names, 4)
	sort.Strings(names)
	for _, name := range names {
		require.NotContains(t, name, "/")
		require.NotContains(t, name, "..")
	}
	firstJobID := harborTestJSON(t, filepath.Join(output, harborExportDirectory, "result.json"))["id"]
	runs = []ProcessedRun{attempt, host, repository, first}
	require.NoError(t, exportHarborJob(t.Context(), output, runs))
	reordered := harborTestTrials(t, output)
	sort.Strings(reordered)
	require.Equal(t, names, reordered)
	require.Equal(t, firstJobID, harborTestJSON(t, filepath.Join(output, harborExportDirectory, "result.json"))["id"])
	withoutRepository := first
	withoutRepository.Run.Repository = ""
	require.Equal(t, harborRunIdentity(first), harborRunIdentity(withoutRepository))
	withoutAttempt := first
	withoutAttempt.Run.Attempt = 0
	require.Equal(t, harborRunIdentity(first), harborRunIdentity(withoutAttempt))
}

func TestExportHarborFailureAndCancellationPreserveExport(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	run := harborTestRun(source)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	resultPath := filepath.Join(output, harborExportDirectory, "result.json")
	before, err := os.ReadFile(resultPath)
	require.NoError(t, err)
	writeSessionTestFile(t, source, "aw_session.jsonl", "malformed JSONL\n")
	require.Error(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	after, err := os.ReadFile(resultPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, exportHarborJob(ctx, output, []ProcessedRun{run}), context.Canceled)
	after, err = os.ReadFile(resultPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	entries, err := os.ReadDir(output)
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), ".harbor-"))
	}
}

func TestExportHarborRejectsUnownedDestination(t *testing.T) {
	output := t.TempDir()
	writeSessionTestFile(t, output, "harbor/unrelated.txt", "keep")
	require.ErrorContains(t, exportHarborJob(t.Context(), output, nil), "unowned")
	require.FileExists(t, filepath.Join(output, "harbor", "unrelated.txt"))
	writeSessionTestFile(t, output, "harbor/gh-aw-export.json", `{"format":"somebody-else","version":1}`)
	require.ErrorContains(t, exportHarborJob(t.Context(), output, nil), "unowned")
	require.NoError(t, os.RemoveAll(filepath.Join(output, harborExportDirectory)))
	writeSessionTestFile(t, output, "harbor", "not a directory")
	require.ErrorContains(t, exportHarborJob(t.Context(), output, nil), "not a directory")
}

func TestExportHarborRejectsSymlinks(t *testing.T) {
	for _, location := range []string{"source", "session", "graders", "output", "destination", "ancestor"} {
		t.Run(location, func(t *testing.T) {
			output, source, outside := t.TempDir(), t.TempDir(), t.TempDir()
			writeSessionTestFile(t, outside, "aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
			writeSessionTestFile(t, outside, "grader_results.json", `{"results":[]}`)
			var link, target string
			switch location {
			case "source":
				link, target = filepath.Join(source, "linked"), outside
				source = link
			case "session":
				link, target = filepath.Join(source, "aw_session.jsonl"), filepath.Join(outside, "aw_session.jsonl")
			case "graders":
				link, target = filepath.Join(source, "graders"), outside
			case "output":
				link, target = filepath.Join(output, "linked"), outside
				output = link
			case "destination":
				link, target = filepath.Join(output, harborExportDirectory), outside
			case "ancestor":
				link, target = filepath.Join(output, "linked"), outside
				output = filepath.Join(link, "child")
			}
			require.NoError(t, os.Symlink(target, link))
			require.ErrorContains(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}), "symbolic link")
			require.FileExists(t, filepath.Join(outside, "aw_session.jsonl"))
			require.NoFileExists(t, filepath.Join(outside, "config.json"))
		})
	}
}

func TestExportHarborRunningRunIsNotFinished(t *testing.T) {
	output := t.TempDir()
	run := harborTestRun(t.TempDir())
	run.Run.Status = "in_progress"
	run.Run.StartedAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	run.Run.UpdatedAt = run.Run.StartedAt.Add(time.Minute)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{run}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	result := harborTestJSON(t, filepath.Join(trial, "result.json"))
	require.Equal(t, "2026-10-01T00:00:00Z", result["started_at"])
	require.Nil(t, result["finished_at"])
	require.Nil(t, result["exception_info"])
}

func TestExportHarborLegacyExperimentInferenceIsLabeled(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	writeSessionTestFile(t, source, "experiment/state.json", `{"counts":{"prompt":{"A":3,"B":1}}}`)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	metadata := harborTestJSON(t, filepath.Join(trial, "gh-aw", "metadata.json"))
	require.Equal(t, "legacy_max_count_heuristic", metadata["experiment_assignment_provenance"])
	require.Nil(t, metadata["reward_mapping"])
	require.Nil(t, harborTestJSON(t, filepath.Join(trial, "result.json"))["verifier_result"])
}

func TestExportHarborOnlyRecordedNumericRewards(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	recorded := `{"version":1,"results":[
		{"id":"zero","status":"pass","value":0,"passed":false},
		{"id":"negative","status":"fail","value":-7.25,"passed":true},
		{"id":"integer","status":"pass","value":9007199254740993},
		{"id":"boolean","status":"pass","value":true},
		{"id":"false-boolean","status":"fail","value":false},
		{"id":"passed-only","status":"pass","passed":true},
		{"id":"null","status":"pass","value":null},
		{"id":"string","status":"pass","value":"1"},
		{"id":"overflow","status":"pass","value":1e400},
		{"id":"error","status":"error","value":12},
		{"id":"unavailable","status":"unavailable","value":13},
		{"id":"ok-status","status":"ok","value":14},
		{"id":"unknown-status","status":"unknown","value":15},
		{"id":"","status":"pass","value":16},
		{"id":"duplicate","status":"pass","value":1},
		{"id":"duplicate","status":"fail","value":2}
	]}`
	writeSessionTestFile(t, source, "graders/grader_results.json", recorded)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	rewardsPath := filepath.Join(trial, "verifier", "reward.json")
	raw, err := os.ReadFile(rewardsPath)
	require.NoError(t, err)
	var rewards map[string]json.Number
	require.NoError(t, json.Unmarshal(raw, &rewards))
	require.Equal(t, map[string]json.Number{
		"gh-aw.zero": "0", "gh-aw.negative": "-7.25", "gh-aw.integer": "9007199254740993",
	}, rewards)
	resultRaw, err := os.ReadFile(filepath.Join(trial, "result.json"))
	require.NoError(t, err)
	require.Contains(t, string(resultRaw), "9007199254740993")
	require.NotContains(t, string(raw), `"reward"`)
	original, err := os.ReadFile(filepath.Join(trial, "gh-aw", "evidence", "graders", "grader_results.json"))
	require.NoError(t, err)
	require.Equal(t, recorded, string(original))
}

func TestExportHarborNoEligibleMetricsMeansUnknownRewards(t *testing.T) {
	for _, result := range []string{
		`{"id":"flag","status":"pass","value":true,"passed":true}`,
		`{"id":"flag","status":"fail","passed":true}`,
		`{"id":"flag","status":"error","value":3}`,
		`{"id":"flag","status":"unavailable","value":3}`,
		`{"id":"flag","status":"pass","value":null}`,
		`{"id":"flag","status":"pass","value":1e400}`,
		`{"id":"flag","status":"ok","value":3}`,
	} {
		t.Run(result, func(t *testing.T) {
			output, source := t.TempDir(), t.TempDir()
			writeSessionTestFile(t, source, "grader_results.json", `{"results":[`+result+`]}`)
			require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}))
			trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
			require.Nil(t, harborTestJSON(t, filepath.Join(trial, "result.json"))["verifier_result"])
			require.Nil(t, harborTestJSON(t, filepath.Join(trial, "gh-aw", "metadata.json"))["reward_mapping"])
			require.NoFileExists(t, filepath.Join(trial, "verifier", "reward.json"))
		})
	}
}

func TestExportHarborAgentIdentityFromAwInfoWithoutSession(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	writeSessionTestFile(t, source, "aw_info.json", `{"engine_id":"codex","agent_version":"0.99.0","version":"gh-aw-not-agent","model":"recorded-model"}`)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	result := harborTestJSON(t, filepath.Join(trial, "result.json"))
	require.Equal(t, map[string]any{
		"name": "codex", "version": "0.99.0", "model_info": map[string]any{"name": "recorded-model"},
	}, result["agent_info"])
	metadata := harborTestJSON(t, filepath.Join(trial, "gh-aw", "metadata.json"))
	require.Contains(t, metadata["agent_identity_provenance"], "aw_info.json")
	require.Equal(t, "missing", metadata["session_status"])
}

func TestExportHarborAgentIdentityPrefersRecordedTrajectory(t *testing.T) {
	output, source := t.TempDir(), t.TempDir()
	writeSessionTestFile(t, source, "aw_info.json", `{"engine_id":"codex","agent_version":"0.99.0","model":"different-engine"}`)
	init := `{"type":"session.start","data":{"sourceEngine":"copilot","sourceEngineVersion":"1.0.0","model":"actual-model"},"provenance":{"component":"agent"}}` + "\n"
	writeSessionTestFile(t, source, "aw_session.jsonl", sessionTestHeader+init+sessionTestAgentEvent)
	require.NoError(t, exportHarborJob(t.Context(), output, []ProcessedRun{harborTestRun(source)}))
	trial := filepath.Join(output, harborExportDirectory, harborTestTrials(t, output)[0])
	require.Equal(t, map[string]any{
		"name": "copilot", "version": "1.0.0", "model_info": map[string]any{"name": "actual-model"},
	}, harborTestJSON(t, filepath.Join(trial, "result.json"))["agent_info"])
}
