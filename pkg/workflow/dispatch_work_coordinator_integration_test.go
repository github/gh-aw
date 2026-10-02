//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchWorkCoordinatorDispatcherWorkerCompilation(t *testing.T) {
	workflowsDir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	const schema = `    schema:
      type: object
      properties:
        task:
          type: string
      required: [task]
      additionalProperties: false`
	dispatcher := `---
on:
  workflow_dispatch:
permissions:
  contents: write
engine: claude
strict: false
tools:
  dispatch-work-coordinator:
    id: integration-queue
    auto-claim: false
` + schema + `
safe-outputs:
  call-workflow:
    workflows: [worker]
    max: 1
---

# Dispatcher

Submit Work and dispatch a worker.
`
	worker := `---
on:
  workflow_call:
permissions:
  contents: write
engine: claude
strict: false
tools:
  dispatch-work-coordinator:
    id: integration-queue
` + schema + `
safe-outputs:
  dispatch-claim-finish:
    max: 1
---

# Worker

Process the trusted Work assignment and finish the Claim.
`

	dispatcherPath := filepath.Join(workflowsDir, "dispatcher.md")
	workerPath := filepath.Join(workflowsDir, "worker.md")
	require.NoError(t, os.WriteFile(dispatcherPath, []byte(dispatcher), 0600))
	require.NoError(t, os.WriteFile(workerPath, []byte(worker), 0600))

	for _, workflowPath := range []string{dispatcherPath, workerPath} {
		compiler := NewCompiler(WithVersion("test-dispatch-work-coordinator"))
		compiler.SetSkipValidation(true)
		require.NoError(t, compiler.CompileWorkflow(workflowPath))
	}

	dispatcherLock, err := os.ReadFile(filepath.Join(workflowsDir, "dispatcher.lock.yml"))
	require.NoError(t, err)
	dispatcherYAML := string(dispatcherLock)
	require.Contains(t, dispatcherYAML, "GH_AW_DISPATCH_WORK_COORDINATOR_ID: integration-queue")
	require.Contains(t, dispatcherYAML, `"GH_AW_DISPATCH_WORK_COORDINATOR_ID": "\\${GH_AW_DISPATCH_WORK_COORDINATOR_ID}"`)
	require.NotContains(t, dispatcherYAML, "Claim Dispatch Work Coordinator assignment")
	require.NotContains(t, dispatcherYAML, "GH_AW_INFO_DISPATCH_WORK_COORDINATOR_ASSIGNMENT")
	require.Contains(t, dispatcherYAML, "workflow: worker")

	workerLock, err := os.ReadFile(filepath.Join(workflowsDir, "worker.lock.yml"))
	require.NoError(t, err)
	workerYAML := string(workerLock)
	require.Contains(t, workerYAML, "Claim Dispatch Work Coordinator assignment")
	require.Contains(t, workerYAML, "GH_AW_DISPATCH_WORK_COORDINATOR_ID: integration-queue")
	require.Contains(t, workerYAML, "GH_AW_INFO_DISPATCH_WORK_COORDINATOR_ASSIGNMENT")
	require.Contains(t, workerYAML, "dispatch_claim_finish")
	require.Contains(t, workerYAML, `"GH_AW_DISPATCH_WORK_COORDINATOR_ID": "\\${GH_AW_DISPATCH_WORK_COORDINATOR_ID}"`)
}
