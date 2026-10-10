//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/require"
)

func TestExperimentsListOutputStreamContract(t *testing.T) {
	installExperimentFetchFakeGH(t, "", `{"run_id":"1","timestamp":"2026-08-18T12:00:00Z","assignments":{"style":"concise"}}`)
	t.Setenv("DEBUG", "")
	stdout, stderr := captureOutput(t, func() error {
		return RunExperimentsList(ExperimentsListConfig{RepoOverride: "octo/repo", JSONOutput: true})
	})
	require.Empty(t, stderr)
	require.NotContains(t, stdout, "\x1b[")
	var experiments []ExperimentInfo
	require.NoError(t, json.Unmarshal([]byte(stdout), &experiments))
	require.Len(t, experiments, 1)
	require.Equal(t, "ci-coach", experiments[0].WorkflowID)
	require.Equal(t, 1, experiments[0].TotalRuns)

	stdout, stderr = captureOutput(t, func() error {
		return RunExperimentsList(ExperimentsListConfig{RepoOverride: "octo/repo"})
	})
	require.Empty(t, stdout)
	require.Contains(t, stderr, "Found 1 experiment workflow")
	require.Contains(t, stderr, "ci-coach")
	require.NotContains(t, stderr, "\x1b[")
	for line := range strings.SplitSeq(stderr, "\n") {
		require.LessOrEqual(t, lipgloss.Width(line), console.DefaultTableWidth)
	}
}

func TestCompactLogsRetainUnboundedOutput(t *testing.T) {
	t.Parallel()
	data := compactTestData()
	data.Runs[0].EngineID = strings.Repeat("engine", 30)
	for name, render := range map[string]func(io.Writer, LogsData) error{
		"compact": renderLogsCompactToWriter, "verbose": renderLogsCompactVerboseToWriter,
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			require.NoError(t, render(&output, data))
			require.Contains(t, output.String(), data.Runs[0].EngineID)
			require.Greater(t, lipgloss.Width(output.String()), console.DefaultTableWidth)
			require.NotContains(t, output.String(), "\x1b[")
		})
	}
}
