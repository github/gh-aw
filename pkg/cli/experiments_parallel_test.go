//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExperimentsParallelEvidence(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) string {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		if content != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "parallel.json"), []byte(content), 0o600))
		}
		return dir
	}
	first := write("run-1/parallel-experiment-1-entry-1-agent", `{
		"version":1,"identity":{"run_id":"100","run_attempt":"1","job":"experiments","matrix_id":"entry-1"},
		"configured":{"id":"entry-1","experiment":"style","variant":"short","model":"requested"},
		"resolved":{"model":null},"state":"failed","admitted":true,"executed":true,"completed":false,"failed":true,
		"usage":{"aic":0,"input_tokens":10,"output_tokens":null},"missing_evidence":["output_tokens"]
	}`)
	write("run-1/parallel-experiment-2-entry-1-agent", `{
		"version":1,"identity":{"run_id":"100","run_attempt":"2","job":"experiments","matrix_id":"entry-1"},
		"configured":{"id":"entry-1","experiment":"style","variant":"short"},
		"state":"skipped","admitted":false,"executed":false,"usage":null
	}`)
	write("run-1/parallel-experiment-2-entry-2-agent", "")
	write("run-1/agent", `{"version":1}`)

	report, err := collectParallelExperimentReport([]string{root, first})
	require.NoError(t, err)
	require.Len(t, report.Artifacts, 2)
	require.Len(t, report.EvidenceGaps, 1)
	assert.Contains(t, report.Coverage, "unknown")
	assert.Equal(t, "not evaluated", report.Evaluation)
	m := report.Artifacts[0].Manifest
	assert.Equal(t, "failed", m.State)
	require.NotNil(t, m.Usage)
	require.NotNil(t, m.Usage.AIC)
	assert.Zero(t, *m.Usage.AIC)
	assert.Nil(t, m.Usage.OutputTokens)
	assert.JSONEq(t, `{"model":null}`, string(m.Resolved))
	assert.Nil(t, report.Artifacts[1].Manifest.Usage)

	cmd := NewExperimentsCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"parallel", root, "--json"})
	require.NoError(t, cmd.Execute())
	var decoded parallelExperimentReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
	expected, err := json.Marshal(report)
	require.NoError(t, err)
	assert.JSONEq(t, string(expected), output.String())

	output.Reset()
	cmd = NewExperimentsCommand()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"parallel", root})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "not evaluated")
	assert.Contains(t, output.String(), `"output_tokens":null`)
	assert.Contains(t, output.String(), "Evidence gap")
}

func TestExperimentsParallelInvalidAndDuplicateEvidence(t *testing.T) {
	root := t.TempDir()
	manifest := `{"version":1,"identity":{"run_id":"1","run_attempt":"1","job":"experiments","matrix_id":"entry-1"},"configured":{"id":"entry-1","experiment":"e","variant":"v"}}`
	create := func(parent, name, content string) {
		dir := filepath.Join(root, parent, name)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "parallel.json"), []byte(content), 0o600))
	}
	create("a", "parallel-experiment-1-entry-1-agent", manifest)
	create("a", "parallel-experiment-1-entry-2-agent", manifest)
	create("a", "parallel-experiment-1-entry-3-agent", `{"version":2}`)
	create("a", "parallel-experiment-1-entry-4-agent", `{`)
	report, err := collectParallelExperimentReport([]string{filepath.Join(root, "a")})
	require.NoError(t, err)
	assert.Len(t, report.Artifacts, 1)
	assert.Len(t, report.EvidenceGaps, 3)

	create("b", "parallel-experiment-1-entry-1-agent", manifest)
	_, err = collectParallelExperimentReport([]string{root})
	require.ErrorContains(t, err, "duplicate parallel experiment identity")
}

func TestExperimentsParallelSymlinks(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "parallel-experiment-1-entry-1-agent")
	require.NoError(t, os.Mkdir(artifact, 0o700))
	require.NoError(t, os.Symlink(filepath.Join(root, "other"), filepath.Join(artifact, "parallel.json")))
	report, err := collectParallelExperimentReport([]string{root})
	require.NoError(t, err)
	require.Len(t, report.EvidenceGaps, 1)
	assert.Empty(t, report.Artifacts)
	assert.Contains(t, report.EvidenceGaps[0].Reason, "unavailable")
}
