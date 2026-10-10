//go:build !integration

package cli

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func installExperimentFetchFakeGH(t *testing.T, workflowContent, state string) {
	t.Helper()
	binDir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"/branches "*) echo '["experiments/ci-coach"]' ;;
  *"/branches/experiments%%2Fci-coach"*) echo "experiments/ci-coach" ;;
  *"contents/.github/workflows"*) echo '["ci-coach.md"]' ;;
  *"ci-coach.md"*) echo %q ;;
  *"state.jsonl"*) echo %q ;;
  *) exit 1 ;;
esac
`, base64.StdEncoding.EncodeToString([]byte(workflowContent)), base64.StdEncoding.EncodeToString([]byte(state)))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
}

func TestLoadRemoteExperimentConfigsResolvesWorkflowFilename(t *testing.T) {
	installExperimentFetchFakeGH(t, `---
experiments:
  style:
    variants: [concise, detailed]
---
# Workflow
`, "")

	result := loadRemoteExperimentConfigs("octo/repo", "cicoach")

	require.Contains(t, result.ExperimentConfigs, "style")
	assert.Equal(t, []string{"concise", "detailed"}, result.ExperimentConfigs["style"].Variants)
}

func TestFetchRemoteExperimentDetailsLoadsState(t *testing.T) {
	installExperimentFetchFakeGH(t, "", `{"run_id":"1","timestamp":"2026-08-18T12:00:00Z","assignments":{"style":"concise"}}`)

	details, err := fetchRemoteExperimentDetails("octo/repo", "experiments/ci-coach", "ci-coach")

	require.NoError(t, err)
	assert.Equal(t, "ci-coach", details.WorkflowID)
	assert.Equal(t, 1, details.TotalRuns)
	require.Len(t, details.Experiments, 1)
	assert.Equal(t, "style", details.Experiments[0].Name)
	assert.Equal(t, map[string]int{"concise": 1}, details.Experiments[0].Variants)
}
