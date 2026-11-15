//go:build !integration

package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func pendingWriteTestData(t *testing.T, compiler *Compiler) *WorkflowData {
	t.Helper()
	data, err := compiler.ParseWorkflowString(`---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
checkout:
  repository: other/project
  github-token: ${{ secrets.EXISTING_READ_TOKEN }}
tools:
  repo-memory:
    branch-name: memory/test
  cache-memory: true
safe-outputs:
  threat-detection: false
  create-issue:
  jobs:
    publish:
      runs-on: ubuntu-latest
      steps:
        - name: Publish
          if: always()
          run: echo publish
---
Test pending writes.
`, filepath.Join(t.TempDir(), "pending.md"))
	require.NoError(t, err)
	return data
}

func TestPendingWriteSpecCoversConfiguredPersistence(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	spec := buildPendingWriteSpec(data)
	publications := map[string]bool{}
	for _, publication := range spec.Publications {
		publications[publication.Job] = true
	}
	require.True(t, publications[pushRepoMemoryJobName])
	require.True(t, publications[updateCacheMemoryJobName])
	require.True(t, publications["publish"])
	require.Contains(t, spec.CustomTypes, "publish")
}

func TestPendingWritePreparationUsesAvailableReadCredentials(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	steps, err := compiler.buildPendingWritesPreparationSteps(data, true)
	require.NoError(t, err)
	output := strings.Join(steps, "")
	require.Contains(t, output, "id: pending_writes")
	require.Contains(t, output, "GITHUB_WORKFLOW_SHA: ${{ github.workflow_sha }}")
	require.Contains(t, output, "GH_AW_PENDING_READ_TOKENS")
	require.Contains(t, output, "EXISTING_READ_TOKEN")
	require.NotContains(t, output, "create-github-app-token")
	require.Contains(t, output, "pending_writes_runtime.cjs")
	ungated, err := compiler.buildPendingWritesPreparationSteps(data, false)
	require.NoError(t, err)
	require.Empty(t, ungated)
}

func TestPendingWritePublicationGuardCannotBeBypassedByAlways(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	for _, name := range []string{"agent", "safe_outputs"} {
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, RunsOn: "runs-on: ubuntu-latest", Steps: []string{"      - run: echo ready\n"}}))
	}
	job := &Job{
		Name:  "publish",
		Needs: []string{string(constants.SafeOutputsJobName)},
		Steps: []string{
			"      - name: Download agent output artifact\n        uses: actions/download-artifact@v4\n        with:\n          name: agent\n          path: /tmp/input/\n",
			"      - name: Publish\n        id: writer\n        if: always()\n        env:\n          GH_AW_AGENT_OUTPUT: /tmp/wrong/input.json\n        run: echo publish\n",
		},
	}
	require.NoError(t, compiler.jobManager.AddJob(job))
	require.NoError(t, compiler.guardPendingWritePublications(data, true, map[string]struct{}{"publish": {}}))
	output := strings.Join(job.Steps, "")
	require.Contains(t, output, "Download authorized pending writes")
	require.Contains(t, output, "pending_writes_setup")
	require.Contains(t, output, "pending_writes_runtime.cjs")
	require.Contains(t, output, "outputs.pending_writes_verified == 'true'")
	require.Contains(t, output, "always()")
	require.NotContains(t, output, "/tmp/wrong/input.json")
	require.Contains(t, job.Env["GH_AW_PENDING_WRITES_SHA256"], "needs.safe_outputs.outputs.pending_writes_sha256")
	require.Contains(t, job.Env["GH_AW_WRITE_GATE_DECISIONS_SHA256"], "needs.safe_outputs.outputs.write_gate_decisions_sha256")
	require.NotEmpty(t, job.Outputs["pending_writes_item_sha256"])
}

func TestPendingWriteGuardRejectsUnrepresentedPublisher(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "safe_outputs", Steps: []string{"      - run: echo ready\n"}}))
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "unexpected", Needs: []string{"safe_outputs"}, Steps: []string{"      - run: echo publish\n"}}))
	require.ErrorContains(t, compiler.guardPendingWritePublications(data, true, map[string]struct{}{"unexpected": {}}), "no handler-derived pending-write entry")
}

func TestPendingWriteCallHasVerifiedImmutableInputs(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	data.SafeOutputs.CallWorkflow = &CallWorkflowConfig{Workflows: []string{"worker"}}
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "safe_outputs", Steps: []string{"      - run: echo ready\n"}}))
	call := &Job{
		Name: "call-worker", Uses: "./.github/workflows/worker.yml", Needs: []string{"safe_outputs"},
		If:   "needs.safe_outputs.outputs.call_workflow_name == 'worker'",
		With: map[string]any{"payload": "${{ needs.safe_outputs.outputs.call_workflow_payload }}"},
	}

	require.NoError(t, compiler.jobManager.AddJob(call))
	require.NoError(t, compiler.guardPendingWritePublications(data, true, map[string]struct{}{"call-worker": {}}))
	verification, exists := compiler.jobManager.GetJob("verify-pending-call-worker")
	require.True(t, exists)
	require.Contains(t, verification.Permissions, "contents: read")
	require.NotContains(t, verification.Permissions, "write")
	require.Contains(t, strings.Join(verification.Steps, ""), "GH_AW_CALL_WORKFLOW_PAYLOAD")
	require.Contains(t, call.Needs, "verify-pending-call-worker")
	require.Contains(t, call.If, "outputs.pending_writes_verified == 'true'")
	require.Nil(t, call.Env)
	require.Equal(t, "${{ needs.safe_outputs.outputs.call_workflow_payload }}", call.With["payload"])
}

func TestPendingWriteInvocationReusesNativeWriterLimits(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	memory := data.RepoMemoryConfig.Memories[0]
	memory.MaxFileCount = 17
	memory.FileGlob = []string{"*.json"}
	memory.AllowedExtensions = []string{".json"}
	data.RepoMemoryConfig.Memories[0] = memory
	native := compiler.buildSinglePushRepoMemoryStep(data, memory, true, true)
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: pushRepoMemoryJobName, Steps: []string{native}}))
	spec := buildPendingWriteSpec(data)
	require.NoError(t, compiler.freezePendingInvocations(&spec))
	var invocation *pendingWriteInvocation
	for _, publication := range spec.Publications {
		if publication.Job == pushRepoMemoryJobName {
			invocation = publication.Invocation
		}
	}
	require.NotNil(t, invocation)
	require.Equal(t, "push_repo_memory.cjs", invocation.Module)
	require.Equal(t, "17", invocation.Env["MAX_FILE_COUNT"])
	require.Equal(t, "*.json", invocation.Env["FILE_GLOB_FILTER"])
	require.Equal(t, memory.ID, invocation.Env["MEMORY_ID"])
	require.Equal(t, "repo_memory:"+memory.ID, invocation.PathEnv["ARTIFACT_DIR"])
	require.NotContains(t, invocation.Env, "GH_TOKEN")
}

func TestPendingWriteArtifactsIsolateReusableInvocations(t *testing.T) {
	compiler := NewCompiler()
	data := pendingWriteTestData(t, compiler)
	data.On = "on:\n  workflow_call:\n"
	uploads := strings.Join(compiler.buildPendingWritesUploadSteps(data), "")
	for _, name := range []string{pendingWritesArtifactName, "pending-write-inputs", "write-decisions"} {
		require.Contains(t, uploads, "name: ${{ needs.agent.outputs.artifact_prefix }}"+name)
	}
	require.Contains(t, strings.Join(compiler.pendingWritesDownloadSteps(data), ""), "name: ${{ needs.activation.outputs.artifact_prefix }}"+pendingWritesArtifactName)
	for _, name := range []string{"agent", "activation", "safe_outputs"} {
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, Steps: []string{"      - run: echo ready\n"}}))
	}
	writer := &Job{Name: "publish", Needs: []string{"safe_outputs"}, Steps: []string{"      - name: Publish\n        env:\n          GH_AW_AGENT_OUTPUT: /tmp/input.json\n        run: echo publish\n"}}
	require.NoError(t, compiler.jobManager.AddJob(writer))
	require.NoError(t, compiler.guardPendingWritePublications(data, true, map[string]struct{}{"publish": {}}))
	require.Contains(t, writer.Needs, "activation")
	require.Contains(t, strings.Join(writer.Steps, ""), "name: ${{ needs.activation.outputs.artifact_prefix }}write-decisions")
}
