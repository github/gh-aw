//go:build integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
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
  work-queue:
    storage: git
    require-assignment: true
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
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(dir, "work-queue-worker.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)
	require.Contains(t, compiled, "work_queue_claim:")
	require.Contains(t, compiled, "aw_context:")

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
	require.Contains(t, safeOutputs, "requireAssignment: true")
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

func TestIssueWorkQueueCompilationPhases(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-issues-")
	workflowPath := filepath.Join(dir, "issue-worker.md")
	workflow := `---
on: workflow_dispatch
name: Issue Work Queue Worker
engine: claude
tools:
  work-queue:
    storage: issues
---

Read and finish assigned work.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))
	issueCompiler := NewCompiler(WithVersion("integration"))
	issueCompiler.SetApprove(true)
	require.NoError(t, issueCompiler.CompileWorkflow(workflowPath))
	lock, err := os.ReadFile(filepath.Join(dir, "issue-worker.lock.yml"))
	require.NoError(t, err)
	compiled := string(lock)
	activation := extractJobSection(compiled, string(constants.ActivationJobName))
	require.Contains(t, activation, "issues: read")
	require.Contains(t, activation, "GH_AW_WORK_QUEUE_STORAGE: issues")
	require.Contains(t, activation, "WORK_QUEUE_HMAC_SECRET: ${{ secrets.GH_AW_WORK_QUEUE_HMAC_SECRET }}")
	safeOutputs := extractJobSection(compiled, string(constants.SafeOutputsJobName))
	require.Contains(t, safeOutputs, "issues: write")
	require.Contains(t, safeOutputs, "GH_AW_WORK_QUEUE_STORAGE: issues")
	require.Contains(t, safeOutputs, "WORK_QUEUE_HMAC_SECRET: ${{ secrets.GH_AW_WORK_QUEUE_HMAC_SECRET }}")
	require.NotContains(t, safeOutputs, "contents: write")
	conclusion := extractJobSection(compiled, "conclusion")
	require.Contains(t, conclusion, "GH_AW_WORK_QUEUE_STORAGE: issues")
	require.Contains(t, conclusion, "WORK_QUEUE_HMAC_SECRET: ${{ secrets.GH_AW_WORK_QUEUE_HMAC_SECRET }}")
	require.Regexp(t, `issues: (read|write)`, conclusion)
	require.Contains(t, extractJobSection(compiled, string(constants.AgentJobName)), `"work-queue"`)
}

func TestWorkQueueDispatchCompilerConfiguration(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-dispatch-")
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
	workerPath := filepath.Join(workflowsDir, "worker.md")
	require.NoError(t, os.WriteFile(workerPath, []byte(`---
on:
  workflow_dispatch:
tools:
  work-queue:
    storage: issues
---
Process the assigned work.
`), 0o600))
	dispatcherPath := filepath.Join(workflowsDir, "dispatcher.md")
	require.NoError(t, os.WriteFile(dispatcherPath, []byte(`---
on: workflow_dispatch
tools:
  work-queue:
    storage: issues
safe-outputs:
  dispatch-workflow:
    workflows: [worker]
---
Read the queue and dispatch an available Work identity.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(dispatcherPath))
	compiled, err := os.ReadFile(filepath.Join(workflowsDir, "dispatcher.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compiled), `work_queue_enabled`)
	require.Contains(t, string(compiled), `work_queue_workflows`)
	require.Contains(t, string(compiled), `work_queue`)
	require.Equal(t, 4, strings.Count(string(compiled), "WORK_QUEUE_HMAC_SECRET: ${{ secrets.GH_AW_WORK_QUEUE_HMAC_SECRET }}"))
}

func TestWorkQueueSmokeVerification(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-work-queue.md")
	require.NoError(t, err)
	dir := testutil.TempDir(t, "work-queue-smoke-")
	workflowPath := filepath.Join(dir, "smoke-work-queue.md")
	require.NoError(t, os.WriteFile(workflowPath, source, 0o600))
	require.NoError(t, NewCompiler(WithVersion("integration")).CompileWorkflow(workflowPath))

	lock, err := os.ReadFile(filepath.Join(dir, "smoke-work-queue.lock.yml"))
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]struct {
			Needs any
			Steps []struct {
				Name string
				Run  string
			}
		}
	}
	require.NoError(t, yaml.Unmarshal(lock, &compiled))
	job, ok := compiled.Jobs["verify_smoke_result"]
	require.True(t, ok)
	require.Contains(t, job.Needs, "safe_outputs", "failure issues must be processed before failing the smoke run")
	var script string
	for _, step := range job.Steps {
		if step.Name == "Verify smoke result" {
			script = step.Run
		}
	}
	require.NotEmpty(t, script)

	for _, tc := range []struct {
		name    string
		outputs string
		intent  string
		success bool
	}{
		{"completed", "{\"type\":\"noop\"}\n", "{\"outcome\":\"completed\"}\n", true},
		{"reported failure", "{\"type\":\"create_issue\"}\n", "", false},
		{"failure with success artifacts", "{\"type\":\"noop\"}\n{\"type\":\"create_issue\"}\n", "{\"outcome\":\"completed\"}\n", false},
		{"missing finish intent", "{\"type\":\"noop\"}\n", "", false},
		{"cancelled", "{\"type\":\"noop\"}\n", "{\"outcome\":\"cancelled\"}\n", false},
		{"missing noop", "", "{\"outcome\":\"completed\"}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(evidence, "safeoutputs.jsonl"), []byte(tc.outputs), 0o600))
			if tc.intent != "" {
				require.NoError(t, os.WriteFile(filepath.Join(evidence, "work-queue.finish.jsonl"), []byte(tc.intent), 0o600))
			}
			output, err := exec.Command("bash", "-e", "-c", strings.ReplaceAll(script, "/tmp/gh-aw/", evidence+"/")).CombinedOutput()
			if tc.success {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "%s", output)
			}
		})
	}
}
