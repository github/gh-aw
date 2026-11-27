package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveWorkQueueWorkerSource(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	markdownPath := filepath.Join(dir, "dispatcher.md")
	for name, content := range map[string]string{
		"worker":   "---\ntools:\n  work-queue:\n    worker: true\n---\n",
		"ordinary": "---\ntools:\n  work-queue: true\n---\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "generated.lock.yml"), nil, 0o600))

	sourcePath, err := resolveWorkQueueWorkerSource("worker", markdownPath)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "worker.md"), sourcePath)

	sourcePath, err = resolveWorkQueueWorkerSource("ordinary", markdownPath)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "ordinary.md"), sourcePath)

	sourcePath, err = resolveWorkQueueWorkerSource("generated", markdownPath)
	require.ErrorIs(t, err, errNotCompilerManagedWorkQueueTarget)
	require.Empty(t, sourcePath)

	sourcePath, err = resolveWorkQueueWorkerSource("../outside", markdownPath)
	require.ErrorContains(t, err, "path traversal not allowed")
	require.Empty(t, sourcePath)
}

func TestWorkQueueDispatchTargetsMustBeCompilerManagedWorkers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	markdownPath := filepath.Join(dir, "dispatcher.md")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ordinary.md"), []byte("---\ntools:\n  work-queue: true\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.md"), []byte("---\ntools:\n  work-queue:\n    worker: true\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "generated.lock.yml"), nil, 0o600))
	dispatcher := func(target string) *WorkflowData {
		return &WorkflowData{
			Tools:           map[string]any{"work-queue": true},
			WorkQueuePolicy: &WorkQueuePolicyConfig{},
			SafeOutputs:     &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{target}}},
		}
	}
	compiler := NewCompiler()
	require.NoError(t, compiler.validateWorkQueueTargets(dispatcher("worker"), markdownPath))
	require.EqualError(t, compiler.validateWorkQueueTargets(dispatcher("ordinary"), markdownPath),
		"work-queue: approved target \"ordinary\" must declare tools.work-queue.worker: true; ordinary delegated workflows have no Claim-scoped delivery adapter")
	require.EqualError(t, compiler.validateWorkQueueTargets(dispatcher("../outside"), markdownPath),
		"work-queue: approved worker \"../outside\": invalid workflow name '../outside' (path traversal not allowed)")
	require.EqualError(t, compiler.validateWorkQueueTargets(dispatcher("generated"), markdownPath),
		"work-queue: approved worker \"generated\" requires compiler-managed workflow source")
}

func TestWorkQueueConfigurationIsMandatoryAndCannotBeDisabled(t *testing.T) {
	ordinary := &WorkflowData{Tools: map[string]any{}, RawFrontmatter: map[string]any{}}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Nil(t, ordinary.WorkQueuePolicy)
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, RawFrontmatter: map[string]any{}}
	require.NoError(t, validateWorkQueueConfiguration(worker))
	require.True(t, workQueueRequiresAssignment(worker))
	require.NotNil(t, worker.WorkQueuePolicy)
	for _, value := range []any{false, map[string]any{"worker": true, "require-assignment": false}} {
		require.Error(t, validateWorkQueueConfiguration(&WorkflowData{Tools: map[string]any{"work-queue": value}}))
	}
	require.ErrorContains(t, validateWorkQueueConfiguration(&WorkflowData{
		Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"on": map[string]any{"push": map[string]any{}}},
	}), "workers require workflow_dispatch")
}

func TestWorkQueueObserverPreservesOrdinaryOutputsAndProtectedRuntimeRoles(t *testing.T) {
	observer := &WorkflowData{
		Tools:          map[string]any{"work-queue": true},
		SafeOutputs:    &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}, Steps: []any{map[string]any{"run": "echo ordinary"}}},
		RawFrontmatter: map[string]any{},
	}
	require.NoError(t, validateWorkQueueConfiguration(observer))
	compiler := NewCompiler()
	require.NoError(t, compiler.validateWorkQueueTargets(observer, "observer.md"))
	require.Equal(t, "observer", workQueueRuntimeRole(observer))
	require.Empty(t, compiler.buildWorkQueueFinishStep(observer))
	controlSteps, err := compiler.buildWorkQueueControlProcessingStep(observer)
	require.NoError(t, err)
	require.Empty(t, controlSteps)
	permissions, _ := safeOutputsJobPermissions(observer)
	require.NotContains(t, permissions.RenderToYAML(), "contents: write")
	require.NotContains(t, permissions.RenderToYAML(), "actions: write")
	steps, err := compiler.buildSafeOutputsUserProvidedSteps(observer)
	require.NoError(t, err)
	require.Contains(t, strings.Join(steps, ""), "echo ordinary")
	require.Contains(t, strings.Join(workQueuePolicyEnvironment(observer), ""), `GH_AW_WORK_QUEUE_ROLE: "observer"`)
	require.NotContains(t, strings.Join(workQueuePolicyEnvironment(observer), ""), "GH_AW_WORK_QUEUE_POLICY")
	for _, env := range []map[string]string{compiler.buildMainJobEnv(observer), compiler.buildJobLevelSafeOutputEnvVars(observer, "observer")} {
		require.Equal(t, `"observer"`, env["GH_AW_WORK_QUEUE_ROLE"])
	}
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	require.Equal(t, `"worker"`, compiler.buildMainJobEnv(worker)["GH_AW_WORK_QUEUE_ROLE"])
	require.Equal(t, `"worker"`, compiler.buildJobLevelSafeOutputEnvVars(worker, "worker")["GH_AW_WORK_QUEUE_ROLE"])
	require.NotContains(t, compiler.buildMainJobOutputs(worker), "work_queue_origin")
	for _, role := range []*WorkflowData{observer, worker} {
		permissions, err := compiler.buildMainJobPermissions(role)
		require.NoError(t, err)
		if role == worker {
			require.Contains(t, permissions, "actions: read")
		} else {
			require.NotContains(t, permissions, "actions: read")
		}
	}
}

func TestWorkQueueWorkerRunNameUsesDispatchCorrelation(t *testing.T) {
	data := &WorkflowData{
		Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"on": map[string]any{"workflow_dispatch": map[string]any{}, "push": map[string]any{}}},
		RunName:        `run-name: "Existing generated default"`,
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Equal(t, workQueueWorkerRunName, data.RunName)
	require.Contains(t, data.RunName, "github.event_name == 'workflow_dispatch' && inputs.work_queue_assignment != ''")
	require.Contains(t, data.RunName, "format('gh-aw work-queue {0}', fromJSON(inputs.work_queue_assignment).dispatch_id)")
	data.RawFrontmatter["run-name"] = "arbitrary correlation"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "worker run-name is reserved")
	ordinary := &WorkflowData{Tools: map[string]any{}, RunName: `run-name: "Ordinary"`}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Equal(t, `run-name: "Ordinary"`, ordinary.RunName)
	dispatcher := &WorkflowData{Tools: map[string]any{"work-queue": true}, RunName: `run-name: "Dispatcher"`}
	require.NoError(t, validateWorkQueueConfiguration(dispatcher))
	require.Equal(t, `run-name: "Dispatcher"`, dispatcher.RunName)
}

func TestWorkQueueControlsAndFinishStepPlacement(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: &SafeOutputsConfig{}}
	require.NoError(t, validateWorkQueueConfiguration(data))
	compiler := NewCompiler()
	controlSteps, err := compiler.buildWorkQueueControlProcessingStep(data)
	require.NoError(t, err)
	step := strings.Join(controlSteps, "")
	require.Contains(t, step, "GH_AW_WORK_QUEUE_CONTROL_CONFIG")
	require.Contains(t, step, "JSON.parse(process.env.GH_AW_WORK_QUEUE_CONTROL_CONFIG)")
	require.Contains(t, step, "work_queue_control_adapter.cjs")
	require.Contains(t, step, "requireAssignment: true")
	require.NotContains(t, step, "work_queue_dispatch_credential")
	require.Equal(t, 1, strings.Count(step, "setupGlobals(core, github, context, exec, io, getOctokit, process.env.GH_AW_GITHUB_TOKEN_SOURCE);"))

	setup, err := compiler.buildSafeOutputsSetupAndDownloadSteps(data, "")
	require.NoError(t, err)
	require.NotContains(t, strings.Join(setup, ""), "work_queue_finish")
	handler, outputs, _, err := compiler.buildSafeOutputsHandlerOutputsAndActionSteps(data, "", "worker.md")
	require.NoError(t, err)
	all := strings.Join(handler, "")
	require.Less(t, strings.Index(all, "id: process_safe_outputs"), strings.Index(all, "- name: Finish work queue claim"))
	require.Contains(t, all, "id: work_queue_finish")
	require.Contains(t, all, "finish_work_queue_claim.cjs")
	require.Contains(t, all, "GH_AW_WORK_QUEUE_FINISH_INTENT: /tmp/gh-aw/work-queue.finish.jsonl")
	require.Equal(t, 2, strings.Count(all, "GH_AW_WORK_QUEUE_EFFECTS: /tmp/gh-aw/work-queue.effects.jsonl"))
	require.Equal(t, "${{ steps.work_queue_finish.outputs.work_queue_outcome }}", outputs["work_queue_outcome"])

	dispatcher := &WorkflowData{Tools: map[string]any{"work-queue": true}, SafeOutputs: &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}}}}
	require.NoError(t, validateWorkQueueConfiguration(dispatcher))
	controlSteps, err = compiler.buildWorkQueueControlProcessingStep(dispatcher)
	require.NoError(t, err)
	require.NotContains(t, strings.Join(controlSteps, ""), "requireAssignment:")
	require.Empty(t, compiler.buildWorkQueueFinishStep(dispatcher))
}

func TestWorkQueueRejectsUnscopedDelegation(t *testing.T) {
	for _, safe := range []*SafeOutputsConfig{
		{Steps: []any{map[string]any{"run": "echo unsafe"}}},
		{Jobs: map[string]*SafeJobConfig{"custom": {}}},
		{Actions: map[string]*SafeOutputActionConfig{"custom": {}}},
		{Scripts: map[string]*SafeScriptConfig{"custom": {}}},
		{CallWorkflow: &CallWorkflowConfig{}},
		{DispatchRepository: &DispatchRepositoryConfig{}},
		{UploadCodeCoverage: &UploadCodeCoverageConfig{}},
		{CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{}},
	} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: safe}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim delivery adapter")
	}
}

func TestWorkQueueRejectsPersistentStandaloneWrites(t *testing.T) {
	for _, data := range []*WorkflowData{
		{LedgerConfig: &LedgerToolConfig{}},
		{RepoMemoryConfig: &RepoMemoryConfig{}},
		{DriveMemoryConfig: &DriveMemoryConfig{}},
	} {
		data.Tools = map[string]any{"work-queue": map[string]any{"worker": true}}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim delivery adapter")
	}
}
