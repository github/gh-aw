//go:build integration

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHarborConformance is deliberately opt-in via its build tag, but never skips:
// the dedicated CI target must fail if the pinned external consumer is unavailable.
func TestHarborConformance(t *testing.T) {
	python := os.Getenv("GH_AW_HARBOR_PYTHON")
	if python == "" {
		python = "python3"
	}
	root, err := os.MkdirTemp(".", ".harbor-conformance-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	t.Setenv("TMPDIR", root)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("HARBOR_TELEMETRY", "0")

	source := filepath.Join(root, "original")
	output := filepath.Join(root, "export")
	require.NoError(t, os.MkdirAll(output, 0o700))
	fixtures := []string{
		"copilot-routed-gpt-responses",
		"claude-awf-selected-messages",
		"pi-claude-two-subagents",
	}
	var runs []ProcessedRun
	for index, fixture := range fixtures {
		runID := int64(101 + index)
		runDir := filepath.Join(source, fmt.Sprintf("run-%d", runID))
		fixtureDir := filepath.Join("testdata", "model_routing_golden", fixture)
		require.NoError(t, os.CopyFS(runDir, os.DirFS(fixtureDir)))
		experimentDir := filepath.Join(runDir, "experiment")
		require.NoError(t, os.MkdirAll(experimentDir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(experimentDir, "state.json"), []byte(
			`{"counts":{"strategy":{"control":1,"candidate":2}},"runs":[{"assignments":{"strategy":"candidate"}}]}`,
		), 0o600))
		if index < 2 {
			gradersDir := filepath.Join(runDir, "graders")
			require.NoError(t, os.MkdirAll(gradersDir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(gradersDir, "grader_results.json"), fmt.Appendf(nil,
				`{"version":2,"results":[`+
					`{"id":"quality","name":"Quality","status":"%s","value":%g,"passed":%t},`+
					`{"id":"broken","status":"error","value":999},`+
					`{"id":"unavailable","status":"unavailable","value":123},`+
					`{"id":"missing-value","status":"pass","value":null},`+
					`{"id":"boolean-value","status":"pass","value":true,"passed":true},`+
					`{"id":"duplicate","status":"pass","value":0.1},`+
					`{"id":"duplicate","status":"fail","value":0.9}]}`,
				[]string{"pass", "fail"}[index], []float64{0.75, 0.25}[index], index == 0,
			), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(gradersDir, "grader_manifest.json"), []byte(
				`{"version":2,"graders":[{"id":"quality","name":"Quality","direction":"maximize","threshold":0.5}]}`,
			), 0o600))
		}
		start := time.Date(2026, 10, 9, 18, index, 0, 0, time.UTC)
		runs = append(runs, ProcessedRun{Run: WorkflowRun{
			DatabaseID: runID, Attempt: 1, Repository: "example-org/routing-sandbox",
			URL:          fmt.Sprintf("https://github.com/example-org/routing-sandbox/actions/runs/%d", runID),
			WorkflowName: "Harbor conformance", WorkflowPath: ".github/workflows/test.md",
			Status: "completed", Conclusion: "success",
			CreatedAt: start, StartedAt: start, UpdatedAt: start.Add(time.Minute),
			Duration: time.Minute, LogsPath: runDir,
		}})
	}
	require.NoError(t, exportHarborJob(t.Context(), output, runs))
	// Delete the original artifacts and move the export before asking Harbor to read
	// it: neither an original absolute path nor GitHub access may be required.
	require.NoError(t, os.RemoveAll(source))
	relocated := filepath.Join(root, "relocated")
	require.NoError(t, os.Rename(output, relocated))
	job := filepath.Join(relocated, harborExportDirectory)
	require.FileExists(t, filepath.Join(job, "config.json"))
	command := exec.CommandContext(t.Context(), python, "../../scripts/test_harbor_conformance.py", job)
	result, err := command.CombinedOutput()
	require.NoError(t, err, "native Harbor conformance failed (install harbor==0.25.0):\n%s", result)
	t.Logf("%s", result)
}
