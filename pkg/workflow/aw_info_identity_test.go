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
