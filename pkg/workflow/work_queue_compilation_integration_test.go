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

func TestWorkQueueCompilationPhases(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-compilation-")
	workflowPath := filepath.Join(dir, "work-queue-worker.md")
	workflow := `---
on: workflow_dispatch
name: Work Queue Worker Integration
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

Compile each work-queue workflow phase.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))

	compiler := NewCompiler(WithVersion("integration"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(dir, "work-queue-worker.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)

	activation := extractJobSection(compiled, string(constants.ActivationJobName))
	require.Contains(t, activation, "Snapshot work queue state")
	require.Contains(t, activation, constants.WorkQueueSnapshotPath)

	agent := extractJobSection(compiled, string(constants.AgentJobName))
	require.Contains(t, agent, `"work-queue"`)
	require.Contains(t, agent, constants.WorkQueueFinishIntentMount)
	require.Contains(t, agent, constants.WorkQueueFinishIntentPath)

	safeOutputs := extractJobSection(compiled, string(constants.SafeOutputsJobName))
	require.Contains(t, safeOutputs, "contents: write")
	require.Contains(t, safeOutputs, "Download activation artifact for work queue")
	require.Contains(t, safeOutputs, "Reconcile work queue claim")
	gate := "steps.work_queue_claim_reconciliation.outputs.authorized == 'true'"
	require.Contains(t, safeOutputs, gate)
	require.Less(t,
		strings.Index(safeOutputs, "Reconcile work queue claim"),
		strings.Index(safeOutputs, "User side effect"),
		"claim reconciliation must precede user safe-output steps",
	)
	require.Contains(t, safeOutputs, "id: process_safe_outputs")
	require.Less(t,
		strings.Index(safeOutputs, "Reconcile work queue claim"),
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

	conclusion := extractJobSection(compiled, "conclusion")
	require.Contains(t, conclusion, "contents: read")
	require.Contains(t, conclusion, "Download activation artifact for work queue summary")
	require.Contains(t, conclusion, "Summarize work queue activity\n        if: always()")
	require.Contains(t, conclusion, "work_queue_summary.cjs")
	require.Contains(t, conclusion, "await main({ core, githubClient: github, context });")
}
