//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentPushJobs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetDryRun(enabled)
		for _, name := range []string{"push_repo_memory", "push_ledger_changes", "push_experiments_state", "push_evals_state", "push_custom", "update_cache_memory", "update_drive_memory", "agent", "conclusion"} {
			job := &Job{Name: name, If: "${{ always() }}", Needs: []string{"agent"}}
			require.NoError(t, compiler.jobManager.AddJob(job))
		}
		compiler.disableDryRunPushJobs()
		for name, job := range compiler.jobManager.GetAllJobs() {
			if enabled && (strings.HasPrefix(name, "push_") || name == "update_cache_memory" || name == "update_drive_memory") {
				assert.Equal(t, "false", job.If, name)
			} else {
				assert.Equal(t, "${{ always() }}", job.If, name)
			}
			assert.Equal(t, []string{"agent"}, job.Needs)
		}
	}
}

func TestDryRunWorkflowMutationControls(t *testing.T) {
	enabled := true
	data := &WorkflowData{
		AIReaction: "eyes", StatusComment: &enabled, LockForAgent: true, LabelCommandRemoveLabel: true,
		Tools:             map[string]any{"work-queue": true, "github": true},
		ParsedTools:       &Tools{GitHub: &GitHubToolConfig{}},
		CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}},
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "default"}}},
		SafeOutputs:       &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}},
		Cache:             "cache:\n  key: example\n  path: example\n",
	}
	compiler := NewCompiler()
	compiler.SetDryRun(true)
	result := compiler.dryRunWorkflowData(data)
	assert.True(t, result.DryRun)
	assert.Equal(t, ReactionType("none"), result.AIReaction)
	assert.False(t, *result.StatusComment)
	assert.False(t, result.LockForAgent)
	assert.False(t, result.LabelCommandRemoveLabel)
	assert.True(t, result.ReportBlockedVersionDisabled)
	assert.False(t, isWorkQueueEnabled(result))
	assert.True(t, result.ParsedTools.GitHub.ReadOnly)
	assert.True(t, templatableBoolIsTrue(result.SafeOutputs.Staged))
	assert.True(t, result.CacheMemoryConfig.Caches[0].RestoreOnly)
	assert.True(t, result.DriveMemoryConfig.Drives[0].RestoreOnly)
	var steps strings.Builder
	generateCacheMemorySteps(&steps, result)
	generateCacheSteps(&steps, result, false)
	assert.Contains(t, steps.String(), "actions/cache/restore@")
	assert.NotContains(t, steps.String(), "actions/cache@")
	steps.Reset()
	generateCacheMemoryGitCommitSteps(&steps, result)
	generateDriveMemoryGitCommitSteps(&steps, result)
	generateDriveMemoryPersistence(&steps, result, compiler.getActionPin)
	assert.Empty(t, steps.String())
	assert.Equal(t, ReactionType("eyes"), data.AIReaction)
	assert.True(t, *data.StatusComment)
	assert.True(t, data.LockForAgent)
	assert.True(t, data.LabelCommandRemoveLabel)
	assert.True(t, isWorkQueueEnabled(data))
	assert.False(t, data.CacheMemoryConfig.Caches[0].RestoreOnly)
	assert.False(t, data.DriveMemoryConfig.Drives[0].RestoreOnly)
	assert.Nil(t, data.SafeOutputs.Staged)
	compiler.SetDryRun(false)
	assert.Same(t, data, compiler.dryRunWorkflowData(data))
}

func TestDevelopmentConclusion(t *testing.T) {
	for _, policy := range []string{"true", "${{ vars.REPORT_FAILURE }}"} {
		t.Run(policy, func(t *testing.T) {
			compiler := NewCompiler()
			enabled := true
			report := TemplatableBool(policy)
			data := &WorkflowData{
				Name: "Development test", WorkflowID: "development-test", StatusComment: &enabled,
				SafeOutputs: &SafeOutputsConfig{
					Steer: true, ReportFailureAsIssue: &report, ReportFailedJobs: &report,
					NoOp:             &NoOpConfig{ReportAsIssue: &policy},
					MissingTool:      &IssueReportingConfig{CreateIssue: &policy},
					ReportIncomplete: &IssueReportingConfig{CreateIssue: &policy},
					ThreatDetection:  &ThreatDetectionConfig{ReportAsIssue: &enabled},
					AddComments:      &AddCommentsConfig{},
				},
			}
			normal, err := compiler.buildConclusionJob(data, "agent", nil)
			require.NoError(t, err)
			require.Contains(t, strings.Join(normal.Steps, ""), "Update reaction comment with completion status")
			require.Contains(t, strings.Join(normal.Steps, ""), "Log detection run")
			require.Contains(t, strings.Join(normal.Steps, ""), "Report failed jobs")
			compiler.SetDryRun(true)
			job, err := compiler.buildConclusionJob(data, "agent", nil)
			require.NoError(t, err)
			steps := strings.Join(job.Steps, "")
			for _, flag := range []string{"GH_AW_FAILURE_REPORT_AS_ISSUE", "GH_AW_NOOP_REPORT_AS_ISSUE", "GH_AW_MISSING_TOOL_CREATE_ISSUE", "GH_AW_REPORT_INCOMPLETE_CREATE_ISSUE"} {
				assert.Contains(t, steps, flag+`: "false"`)
			}
			for _, retained := range []string{"Handle agent failure", "Record missing tool", "Record incomplete", "Process no-op messages", "usage"} {
				assert.Contains(t, steps, retained)
			}
			for _, removed := range []string{"Log detection run", "Report failed jobs", "Update reaction comment with completion status", "complete_steering_issue.cjs"} {
				assert.NotContains(t, steps, removed)
			}
			assert.NotContains(t, job.Permissions, "issues: write")
			assert.NotContains(t, job.Permissions, "pull-requests: write")
			assert.Contains(t, job.If, "always()")
			assert.True(t, *data.StatusComment)
			assert.Equal(t, policy, data.SafeOutputs.ReportFailureAsIssue.String())
			assert.Equal(t, policy, *data.SafeOutputs.NoOp.ReportAsIssue)
			assert.Equal(t, policy, *data.SafeOutputs.MissingTool.CreateIssue)
			assert.Equal(t, policy, *data.SafeOutputs.ReportIncomplete.CreateIssue)
			assert.True(t, *data.SafeOutputs.ThreatDetection.ReportAsIssue)
			assert.Nil(t, data.SafeOutputs.Staged)
			compiler.SetDryRun(false)
			restored, err := compiler.buildConclusionJob(data, "agent", nil)
			require.NoError(t, err)
			assert.Equal(t, normal, restored)
		})
	}
}

func TestDryRunExperimentCacheKeepsArtifacts(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		WorkflowID: "dry-run-experiment", ExperimentsStorage: ExperimentsStorageCache,
		Experiments: map[string][]string{"variant": {"a", "b"}},
	}
	normal := strings.Join(compiler.generateExperimentSteps(data), "")
	assert.Contains(t, normal, "actions/cache/save@")
	data.DryRun = true
	steps := strings.Join(compiler.generateExperimentSteps(data), "")
	assert.Contains(t, steps, "actions/cache/restore@")
	assert.Contains(t, steps, "pick_experiment.cjs")
	assert.Contains(t, steps, "actions/upload-artifact@")
	assert.NotContains(t, steps, "actions/cache/save@")
}

func TestDryRunMemoryStepsWithAndWithoutDetection(t *testing.T) {
	for _, detection := range []bool{false, true} {
		data := &WorkflowData{
			CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}},
			DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "default", DriveName: "memory"}}},
			SafeOutputs:       &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EngineDisabled: !detection}},
		}
		var normal strings.Builder
		generateCacheMemorySteps(&normal, data)
		if detection {
			assert.Contains(t, normal.String(), "actions/cache/restore@")
		} else {
			assert.Contains(t, normal.String(), "actions/cache@")
		}
		data.DryRun = true
		var steps strings.Builder
		generateCacheMemorySteps(&steps, data)
		generateDriveMemorySteps(&steps, data, getActionPin)
		assert.Contains(t, steps.String(), "actions/cache/restore@")
		assert.NotContains(t, steps.String(), "actions/cache@")
		assert.Contains(t, steps.String(), "write: false")
		assert.NotContains(t, steps.String(), "write: true")
		steps.Reset()
		generateCacheMemoryGitCommitSteps(&steps, data)
		generateDriveMemoryGitCommitSteps(&steps, data)
		generateDriveMemoryPersistence(&steps, data, getActionPin)
		assert.Empty(t, steps.String())
	}
}

func TestDryRunWorkQueueAndCallWorkflow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	createWorker(t, dir, "worker", "")
	path := filepath.Join(dir, "gateway.md")
	queueSource := `---
on: workflow_dispatch
engine: copilot
tools:
  work-queue:
    require-assignment: true
    worker: true
---
Debug generated queue worker jobs.
`
	delegatedSource := `---
on: workflow_dispatch
engine: copilot
safe-outputs:
  call-workflow: [worker]
---
Debug generated delegated workflow jobs.
`
	for _, source := range []string{queueSource, delegatedSource} {
		require.NoError(t, os.WriteFile(path, []byte(source), 0600))
		compiler := NewCompiler()
		compiler.SetSkipValidation(true)
		compiler.SetApprove(true)
		for _, enabled := range []bool{true, false} {
			compiler.SetDryRun(enabled)
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "gateway.lock.yml"))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			if source == delegatedSource {
				require.Contains(t, compiled.Jobs, "call-worker")
				if enabled {
					assert.Equal(t, false, compiled.Jobs["call-worker"]["if"])
				} else {
					assert.NotEqual(t, false, compiled.Jobs["call-worker"]["if"])
				}
			} else if !enabled {
				assert.Contains(t, string(content), "Snapshot work queue state")
				assert.Contains(t, string(content), "Reconcile work queue claim")
			}
			if enabled {
				assert.NotContains(t, string(content), "Snapshot work queue state")
				assert.NotContains(t, string(content), "Reconcile work queue claim")
				assert.NotContains(t, compiled.Jobs, "work_queue_claim")
			}
		}
	}

	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(queueSource, "---\nDebug", "safe-outputs:\n  call-workflow: [worker]\n---\nDebug", 1)), 0600))
	for _, enabled := range []bool{true, false} {
		compiler := NewCompiler()
		compiler.SetSkipValidation(true)
		compiler.SetApprove(true)
		compiler.SetDryRun(enabled)
		require.ErrorContains(t, compiler.CompileWorkflow(path), "delegated workflow/repository dispatch requires a trusted per-Claim delivery adapter")
	}
}

func TestDevelopmentCompiledJobsAndCompilerReuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "development.md")
	const source = `---
on:
  workflow_dispatch:
  issues:
    types: [labeled]
    lock-for-agent: true
  label_command: debug
  reaction: eyes
  status-comment: true
strict: false
engine: copilot
permissions:
  issues: read
tools:
  repo-memory: true
safe-outputs:
  add-comment:
  report-failure-as-issue: true
  report-failed-jobs: true
  steer: true
  noop:
    report-as-issue: true
  missing-tool:
    create-issue: true
  report-incomplete:
    create-issue: true
jobs:
  push_custom:
    runs-on: ubuntu-latest
    if: always()
    steps:
      - run: echo custom
  custom:
    runs-on: ubuntu-latest
    if: always()
    steps:
      - run: echo custom
---
Test development job controls.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	for _, enabled := range []bool{true, false} {
		compiler.SetDryRun(enabled)
		require.NoError(t, compiler.CompileWorkflow(path))
		content, err := os.ReadFile(filepath.Join(dir, "development.lock.yml"))
		require.NoError(t, err)
		var compiled struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		require.NoError(t, yaml.Unmarshal(content, &compiled))
		for _, name := range []string{"push_repo_memory", "push_custom"} {
			require.Contains(t, compiled.Jobs, name)
			if enabled {
				assert.Equal(t, false, compiled.Jobs[name]["if"], name)
			} else {
				assert.NotEqual(t, false, compiled.Jobs[name]["if"], name)
			}
		}
		activation := extractJobSection(string(content), "activation")
		for _, script := range []string{"add_reaction.cjs", "add_workflow_run_comment.cjs", "lock-issue.cjs", "remove_trigger_label.cjs"} {
			if enabled {
				assert.NotContains(t, activation, script)
			} else {
				assert.Contains(t, activation, script)
			}
		}
		if enabled {
			assert.Contains(t, string(content), `GH_AW_INFO_DRY_RUN: "true"`)
			assert.NotContains(t, compiled.Jobs, "unlock")
			assert.Contains(t, extractJobSection(string(content), "safe_outputs"), `GH_AW_SAFE_OUTPUTS_STAGED: "true"`)
		} else {
			assert.NotContains(t, string(content), "GH_AW_INFO_DRY_RUN:")
			assert.Contains(t, compiled.Jobs, "unlock")
		}
		assert.NotEqual(t, false, compiled.Jobs["custom"]["if"])
		conclusion := extractJobSection(string(content), "conclusion")
		if enabled {
			assert.Contains(t, conclusion, `GH_AW_FAILURE_REPORT_AS_ISSUE: "false"`)
			assert.NotContains(t, conclusion, "Report failed jobs")
			assert.NotContains(t, conclusion, "Log detection run")
			assert.NotContains(t, conclusion, "Update reaction comment with completion status")
			assert.NotContains(t, conclusion, "complete_steering_issue.cjs")
			assert.NotContains(t, conclusion, "issues: write")
			for _, flag := range []string{"GH_AW_NOOP_REPORT_AS_ISSUE", "GH_AW_MISSING_TOOL_CREATE_ISSUE", "GH_AW_REPORT_INCOMPLETE_CREATE_ISSUE"} {
				assert.Contains(t, conclusion, flag+`: "false"`)
			}
		} else {
			assert.Contains(t, conclusion, `GH_AW_FAILURE_REPORT_AS_ISSUE: "true"`)
			assert.Contains(t, conclusion, "Report failed jobs")
			assert.Contains(t, conclusion, "Log detection run")
			assert.Contains(t, conclusion, "Update reaction comment with completion status")
			assert.Contains(t, conclusion, "complete_steering_issue.cjs")
		}
	}
}
