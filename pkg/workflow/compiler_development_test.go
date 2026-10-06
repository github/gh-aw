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
		compiler.SetDevelopmentMode(enabled)
		for _, name := range []string{"push_repo_memory", "push_ledger_changes", "push_experiments_state", "push_evals_state", "push_custom", "update_cache_memory", "update_drive_memory", "agent", "conclusion"} {
			job := &Job{Name: name, If: "${{ always() }}", Needs: []string{"agent"}}
			require.NoError(t, compiler.jobManager.AddJob(job))
		}
		compiler.disableDevelopmentPushJobs()
		for name, job := range compiler.jobManager.GetAllJobs() {
			if enabled && strings.HasPrefix(name, "push_") {
				assert.Equal(t, "false", job.If, name)
			} else {
				assert.Equal(t, "${{ always() }}", job.If, name)
			}
			assert.Equal(t, []string{"agent"}, job.Needs)
		}
	}
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
			compiler.SetDevelopmentMode(true)
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
			compiler.SetDevelopmentMode(false)
			restored, err := compiler.buildConclusionJob(data, "agent", nil)
			require.NoError(t, err)
			assert.Equal(t, normal, restored)
		})
	}
}

func TestDevelopmentCompiledJobsAndCompilerReuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "development.md")
	const source = `---
on:
  workflow_dispatch:
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
		compiler.SetDevelopmentMode(enabled)
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
