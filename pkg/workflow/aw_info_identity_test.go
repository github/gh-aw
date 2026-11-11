//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwInfoExecutingWorkflowIdentity(t *testing.T) {
	workflowsDir := filepath.Join(testutil.TempDir(t, "aw-info-identity"), ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	worker := compileAndReadLock(t, filepath.Join(workflowsDir, "worker.md"), `---
on: workflow_call
engine: copilot
permissions:
  contents: read
---
# Worker
Handle the caller's request.
`)
	caller := compileAndReadLock(t, filepath.Join(workflowsDir, "caller.md"), `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow: [worker]
---
# Caller
Call the worker.
`)
	assert.Contains(t, caller, "uses: ./.github/workflows/worker.lock.yml")
	assert.Contains(t, worker, "workflow_call:")

	for name, compiled := range map[string]string{"caller": caller, "worker": worker} {
		t.Run(name, func(t *testing.T) {
			metadata, _, err := ExtractMetadataFromLockFile(compiled)
			require.NoError(t, err)
			require.NotNil(t, metadata)
			assert.Equal(t, CurrentSetupRuntimeContractVersion, metadata.SetupRuntimeContractVersion)

			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						ID  string            `yaml:"id"`
						Env map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(compiled), &workflow))
			for _, step := range workflow.Jobs["activation"].Steps {
				if step.ID == "generate_aw_info" {
					assert.Equal(t, "${{ job.workflow_ref }}", step.Env["GH_AW_INFO_WORKFLOW_ID"],
						"run metadata must use the executing job's workflow, not github.workflow_ref (the caller)")
					assert.Equal(t, "${{ job.workflow_sha }}", step.Env["GH_AW_INFO_WORKFLOW_SHA"])
					return
				}
			}

			t.Fatal("generate_aw_info step not found")
		})
	}
}

func TestAwInfoCheckedInLocalSetupMetadata(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	checked := make(map[string]bool)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			content, err := os.ReadFile(file)
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						ID   string            `yaml:"id"`
						Uses string            `yaml:"uses"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &workflow))
			steps := workflow.Jobs["activation"].Steps
			localSetup := false
			for _, step := range steps {
				if step.Uses == "./actions/setup" {
					localSetup = true
				}
			}
			if !localSetup {
				return
			}
			checked[filepath.Base(file)] = true

			manifest, err := ExtractGHAWManifestFromLockFile(string(content))
			require.NoError(t, err)
			require.NotNil(t, manifest, "local setup requires compiler metadata; recompile this workflow")
			assert.NotNil(t, manifest.AgentMetadata, "local setup requires compiler Markdown metadata; recompile this workflow")

			for _, step := range steps {
				if step.ID == "generate_aw_info" {
					assert.Equal(t, "${{ job.workflow_ref }}", step.Env["GH_AW_INFO_WORKFLOW_ID"],
						"metadata must identify the executing workflow, not its caller")
					assert.Equal(t, "${{ job.workflow_sha }}", step.Env["GH_AW_INFO_WORKFLOW_SHA"],
						"local setup requires the immutable executing revision; recompile this workflow")
					return
				}
			}
			t.Fatal("local setup requires a generate_aw_info step")
		})
	}
	require.True(t, checked["release.lock.yml"], "Release must be covered by the local setup metadata contract")
	t.Logf("Checked metadata for %d workflows using local setup", len(checked))
}
