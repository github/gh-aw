//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConclusionWorkQueueSummary(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			data := &WorkflowData{
				Name:        "Work queue worker",
				Tools:       map[string]any{"work-queue": enabled},
				SafeOutputs: &SafeOutputsConfig{},
			}
			compiler := NewCompiler()
			job, err := compiler.buildConclusionJob(data, "agent", []string{"safe_outputs"})
			require.NoError(t, err)
			require.NotNil(t, job)
			steps := strings.Join(job.Steps, "")
			if !enabled {
				require.NotContains(t, steps, "work_queue_summary.cjs")
				require.NotContains(t, steps, "Download activation artifact for work queue summary")
				return
			}
			require.Contains(t, job.Permissions, "contents: read")
			require.Contains(t, job.Needs, "safe_outputs")
			require.Contains(t, steps, "Download activation artifact for work queue summary\n        if: always()")
			require.Contains(t, steps, "Summarize work queue activity\n        if: always()")
			require.Contains(t, steps, "Summarize work queue activity\n        if: always()\n        continue-on-error: true")
			require.Contains(t, steps, "work_queue_summary.cjs")
			require.Contains(t, steps, "path: ${{ runner.temp }}/gh-aw-activation\n")
			require.Contains(t, steps, "snapshotPath: '${{ runner.temp }}/gh-aw-activation/work-queue.snapshot.json'")
			require.Less(t, strings.Index(steps, "Download activation artifact for work queue summary"), strings.Index(steps, "Summarize work queue activity"))
		})
	}
}

func TestConclusionWorkQueueTerminationUsesOnlyQueuePermissions(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{},
		},
	}
	level, ok := computeConclusionJobPermissions(data).Get(PermissionContents)
	require.True(t, ok)
	require.Equal(t, PermissionWrite, level)
	_, hasPullRequests := computeConclusionJobPermissions(data).Get(PermissionPullRequests)
	require.False(t, hasPullRequests)
	actions, ok := computeConclusionJobPermissions(data).Get(PermissionActions)
	require.True(t, ok)
	require.Equal(t, PermissionRead, actions)
	data.RawFrontmatter = map[string]any{"observability": map[string]any{"otlp": map[string]any{"github-app": map[string]any{"audience": "https://otel.example.com"}}}}
	oidc, ok := computeConclusionJobPermissions(data).Get(PermissionIdToken)
	require.True(t, ok)
	require.Equal(t, PermissionWrite, oidc)
	delete(data.Tools, "work-queue")
	level, ok = computeConclusionJobPermissions(data).Get(PermissionContents)
	require.True(t, ok)
	require.Equal(t, PermissionWrite, level)
}

func TestConclusionWorkerTerminationIsInlineAndAlwaysRuns(t *testing.T) {
	for _, required := range []bool{true, false} {
		data := &WorkflowData{
			Tools:       map[string]any{"work-queue": map[string]any{"worker": required}},
			SafeOutputs: &SafeOutputsConfig{},
		}
		compiler := NewCompiler()
		job, err := compiler.buildConclusionJob(data, "agent", []string{"safe_outputs"})
		require.NoError(t, err)
		steps := strings.Join(job.Steps, "")
		if !required {
			require.NotContains(t, steps, "reconcile_work_queue_run.cjs")
			continue
		}
		require.Equal(t, "always()", job.If)
		require.Contains(t, job.Needs, "safe_outputs")
		require.Contains(t, steps, "id: worker_termination\n        if: always()")
		require.NotContains(t, steps, "worker_termination\n        if: always()\n        continue-on-error")
		require.Contains(t, steps, "reconcile_work_queue_run.cjs")
		require.Less(t, strings.Index(steps, "reconcile_work_queue_run.cjs"), strings.Index(steps, "work_queue_summary.cjs"))
	}
}

func TestConclusionWorkQueueReferencesOnlyGeneratedSteps(t *testing.T) {
	for _, tc := range []struct {
		name        string
		queue       any
		dispatch    bool
		participant bool
	}{
		{name: "ordinary", queue: false},
		{name: "observer", queue: true},
		{name: "worker", queue: map[string]any{"worker": true}, participant: true},
		{name: "dispatcher", queue: true, dispatch: true, participant: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				AdmissionEnabled: true,
				Tools:            map[string]any{"work-queue": tc.queue},
				SafeOutputs: &SafeOutputsConfig{
					NoOp:             &NoOpConfig{},
					MissingTool:      &MissingToolConfig{},
					ReportIncomplete: &ReportIncompleteConfig{},
				},
			}
			if tc.dispatch {
				data.SafeOutputs.DispatchWorkflow = &DispatchWorkflowConfig{Workflows: []string{"worker"}}
			}
			job, err := NewCompiler().buildConclusionJob(data, "agent", nil)
			require.NoError(t, err)
			require.NotNil(t, job)
			steps := strings.Join(job.Steps, "")
			for _, id := range []string{"noop", "missing_tool", "report_incomplete", "report_failed_jobs"} {
				require.Equal(t, !tc.participant, strings.Contains(steps, "        id: "+id+"\n"), id)
			}
			for _, key := range []string{"noop_message", "noop_comment_body", "tools_reported", "total_count", "incomplete_count"} {
				_, exists := job.Outputs[key]
				require.Equal(t, !tc.participant, exists, key)
			}
			require.Equal(t, !tc.participant, strings.Contains(steps, "steps.report_failed_jobs.outputs.job_durations"))
			require.Contains(t, job.Outputs, "admission_compact_session")
			require.Contains(t, job.Outputs, "usage_artifact_id")
			require.Contains(t, job.Outputs, "usage_artifact_retry_id")
		})
	}
}

func TestConclusionWorkQueueObserverPreservesOrdinaryPermissions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		contents    PermissionLevel
	}{
		{"summary only", &SafeOutputsConfig{}, PermissionRead},
		{"ordinary pull request", &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}, PermissionWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				Tools:       map[string]any{"work-queue": true},
				SafeOutputs: tc.safeOutputs,
			}
			require.False(t, isWorkQueueParticipant(data))
			level, ok := computeConclusionJobPermissions(data).Get(PermissionContents)
			require.True(t, ok)
			require.Equal(t, tc.contents, level)
		})
	}
}

func TestConclusionWorkQueueSummaryArtifactPrefix(t *testing.T) {
	for _, tc := range []struct {
		trigger      string
		artifactName string
	}{
		{trigger: "workflow_dispatch", artifactName: "activation"},
		{trigger: "workflow_call", artifactName: "${{ needs.activation.outputs.artifact_prefix }}activation"},
	} {
		t.Run(tc.trigger, func(t *testing.T) {
			data := &WorkflowData{
				On:          tc.trigger,
				Tools:       map[string]any{"work-queue": true},
				SafeOutputs: &SafeOutputsConfig{},
			}
			job, err := NewCompiler().buildConclusionJob(data, "agent", []string{"safe_outputs"})
			require.NoError(t, err)
			require.NotNil(t, job)
			require.Contains(t, job.Needs, "activation")
			steps := strings.Join(job.Steps, "")
			require.Contains(t, steps, "          name: "+tc.artifactName+"\n")
			require.NotContains(t, steps, "steps.artifact-prefix.outputs.prefix")
		})
	}
}
