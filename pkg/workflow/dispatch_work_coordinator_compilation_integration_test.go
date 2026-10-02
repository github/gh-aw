//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestDispatchCoordinatorReconciliationPreservesStagedConfiguration(t *testing.T) {
	compiler := NewCompiler()
	staged := TemplatableBool("${{ inputs.staged }}")
	data := &WorkflowData{Tools: map[string]any{"work-queue": true}, SafeOutputs: &SafeOutputsConfig{Staged: &staged}}
	step := strings.Join(compiler.buildDispatchClaimReconciliationStep(data), "")
	require.Contains(t, step, "GH_AW_SAFE_OUTPUTS_STAGED: ${{ inputs.staged }}")
}

func TestDispatchCoordinatorCompilationPhases(t *testing.T) {
	dir := testutil.TempDir(t, "dispatch-coordinator-compilation-")
	workflowPath := filepath.Join(dir, "dispatch-coordinator-worker.md")
	workflow := `---
on: workflow_dispatch
name: Dispatch Coordinator Worker Integration
engine: claude
tools:
  work-queue: true
safe-outputs:
  create-issue:
    max: 1
  steps:
    - name: User side effect
      run: echo "must wait for claim reconciliation"
---

Compile each dispatch-coordinator workflow phase.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))

	compiler := NewCompiler(WithVersion("integration"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(dir, "dispatch-coordinator-worker.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)

	activation := extractJobSection(compiled, string(constants.ActivationJobName))
	require.Contains(t, activation, "Snapshot dispatch coordinator state")
	require.Contains(t, activation, constants.DispatchCoordinatorSnapshotPath)

	agent := extractJobSection(compiled, string(constants.AgentJobName))
	require.Contains(t, agent, `"work-queue"`)
	require.Contains(t, agent, constants.DispatchCoordinatorFinishIntentMount)
	require.Contains(t, agent, constants.DispatchCoordinatorFinishIntentPath)
	require.Contains(t, agent, constants.DispatchCoordinatorClaimIntentPath)
	require.Contains(t, agent, `"dispatch_claim_next"`)

	safeOutputs := extractJobSection(compiled, string(constants.SafeOutputsJobName))
	require.Contains(t, safeOutputs, "contents: write")
	require.Contains(t, safeOutputs, "Download activation artifact for dispatch coordinator")
	require.Contains(t, safeOutputs, "Reconcile dispatch work claim")
	gate := "steps.dispatch_claim_reconciliation.outputs.authorized == 'true'"
	require.Contains(t, safeOutputs, gate)
	require.Less(t,
		strings.Index(safeOutputs, "Reconcile dispatch work claim"),
		strings.Index(safeOutputs, "User side effect"),
		"claim reconciliation must precede user safe-output steps",
	)
	require.Contains(t, safeOutputs, "id: process_safe_outputs")
	require.Contains(t, safeOutputs, "GH_AW_DISPATCH_CLAIMS_VERIFIED: ${{ steps.dispatch_claim_reconciliation.outputs.claims_verified }}")
	require.Less(t,
		strings.Index(safeOutputs, "Reconcile dispatch work claim"),
		strings.Index(safeOutputs, "id: process_safe_outputs"),
		"claim reconciliation must precede ordinary safe-output handlers",
	)
	userStart := strings.Index(safeOutputs, "User side effect")
	userStepStart := strings.LastIndex(safeOutputs[:userStart], "      - name:")
	userEnd := strings.Index(safeOutputs[userStart:], "      - name:")
	require.GreaterOrEqual(t, userStepStart, 0)
	require.Greater(t, userEnd, 0)
	require.Contains(t, safeOutputs[userStepStart:userStart+userEnd], gate)

	handlerID := strings.Index(safeOutputs, "id: process_safe_outputs")
	handlerStart := strings.LastIndex(safeOutputs[:handlerID], "      - name:")
	require.GreaterOrEqual(t, handlerStart, 0)
	handlerEnd := strings.Index(safeOutputs[handlerStart+len("      - name:"):], "      - name:")
	if handlerEnd < 0 {
		handlerEnd = len(safeOutputs) - handlerStart
	} else {
		handlerEnd += len("      - name:")
	}
	require.Contains(t, safeOutputs[handlerStart:handlerStart+handlerEnd], gate)
}
