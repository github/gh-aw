//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateMaintenanceWorkflow_CreatesWorkflowDirRecursively(t *testing.T) {
	tmpDir := t.TempDir()
	workflowDir := filepath.Join(tmpDir, "nested", "workflows")

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{
				Name: "test-workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreateDiscussions: &CreateDiscussionsConfig{
						Expires: 24,
					},
				},
			},
		},
		WorkflowDir: workflowDir,
		Version:     "dev",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(workflowDir, "agentics-maintenance.yml"))
	require.NoError(t, statErr)
}

func TestGenerateMaintenanceWorkflow_DeletesExistingFile(t *testing.T) {
	tests := []struct {
		name             string
		workflowDataList []*WorkflowData
		createFileBefore bool
		expectFileExists bool
	}{
		{
			name: "no expires field - should delete existing file",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{},
					},
				},
			},
			createFileBefore: true,
			expectFileExists: false,
		},
		{
			name: "with expires - should create file",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{
							Expires: 168,
						},
					},
				},
			},
			createFileBefore: false,
			expectFileExists: true,
		},
		{
			name: "no expires without existing file - should not error",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{},
					},
				},
			},
			createFileBefore: false,
			expectFileExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			maintenanceFile := filepath.Join(tmpDir, "agentics-maintenance.yml")

			// Create the maintenance file if requested
			if tt.createFileBefore {
				err := os.WriteFile(maintenanceFile, []byte("# Existing maintenance workflow\n"), 0644)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			// Call GenerateMaintenanceWorkflow
			err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
				WorkflowDataList: tt.workflowDataList,
				WorkflowDir:      tmpDir,
				Version:          "v1.0.0",
				ActionMode:       ActionModeDev,
				ActionTag:        "",
				RepoConfig:       nil,
				RepoSlug:         "",
			})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check if file exists
			_, statErr := os.Stat(maintenanceFile)
			fileExists := statErr == nil

			if tt.expectFileExists && !fileExists {
				t.Errorf("Expected maintenance workflow file to exist but it does not")
			}
			if !tt.expectFileExists && fileExists {
				t.Errorf("Expected maintenance workflow file NOT to exist but it does")
			}
		})
	}
}

func TestGenerateMaintenanceWorkflow_OperationJobConditions(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					Expires: 48,
				},
			},
		},
	}

	tmpDir := t.TempDir()
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)
	require.Contains(t, yaml, "This file defines the generated agentic maintenance workflow for this repository.")
	require.Contains(t, yaml, "This workflow is generated automatically when workflows use expiring safe outputs")
	require.Contains(t, yaml, `{"maintenance": false}`)
	require.Contains(t, yaml, "https://github.github.com/gh-aw/reference/ephemerals/#manual-maintenance-operations")

	operationSkipCondition := `github.event_name != 'workflow_dispatch' && github.event_name != 'workflow_call' || inputs.operation == '' || inputs.operation == 'none'`
	operationRunCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation != '' && inputs.operation != 'none' && inputs.operation != 'safe_outputs' && inputs.operation != 'create_labels' && inputs.operation != 'activity_report' && inputs.operation != 'close_agentic_workflows_issues' && inputs.operation != 'clean_cache_memories' && inputs.operation != 'update_pull_request_branches' && inputs.operation != 'validate' && inputs.operation != 'forecast'`
	applySafeOutputsCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'safe_outputs'`
	createLabelsCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'create_labels'`
	updatePullRequestBranchesCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'update_pull_request_branches'`
	activityReportCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'activity_report'`
	forecastCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'forecast'`
	closeAgenticWorkflowIssuesCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'close_agentic_workflows_issues'`
	cleanCacheMemoriesCondition := `github.event_name != 'workflow_dispatch' && github.event_name != 'workflow_call' || inputs.operation == '' || inputs.operation == 'none' || inputs.operation == 'clean_cache_memories'`

	const jobSectionSearchRange = 350
	const runOpSectionSearchRange = 550

	// Jobs that should be disabled when any non-dedicated operation is set (cleanup-cache-memory has its own dedicated operation)
	disabledJobs := []string{
		"close-expired-discussions:",
		"close-expired-issues:",
		"close-expired-pull-requests:",
		"compile-workflows:",
		"secret-validation:",
	}
	for _, job := range disabledJobs {
		// Find the if: condition for each job
		jobIdx := strings.Index(yaml, "\n  "+job)
		if jobIdx == -1 {
			t.Errorf("Job %q not found in generated workflow", job)
			continue
		}
		// Check that the operation skip condition appears after the job name (within a reasonable range)
		jobSection := yaml[jobIdx : jobIdx+jobSectionSearchRange]
		if !strings.Contains(jobSection, operationSkipCondition) {
			t.Errorf("Job %q is missing the operation skip condition %q in:\n%s", job, operationSkipCondition, jobSection)
		}
	}

	type permissionAssertion struct {
		requiredWrite string
		forbidden     []string
	}
	closeExpiredPermissions := map[string]permissionAssertion{
		"close-expired-discussions:": {
			requiredWrite: "discussions: write",
			forbidden:     []string{"issues: write", "pull-requests: write"},
		},
		"close-expired-issues:": {
			requiredWrite: "issues: write",
			forbidden:     []string{"discussions: write", "pull-requests: write"},
		},
		"close-expired-pull-requests:": {
			requiredWrite: "pull-requests: write",
			forbidden:     []string{"discussions: write", "issues: write"},
		},
	}
	for job, perms := range closeExpiredPermissions {
		jobIdx := strings.Index(yaml, "\n  "+job)
		if jobIdx == -1 {
			t.Errorf("Job %q not found in generated workflow", job)
			continue
		}
		jobSection := yaml[jobIdx : jobIdx+jobSectionSearchRange]
		if !strings.Contains(jobSection, perms.requiredWrite) {
			t.Errorf("Job %q should include %q permission in:\n%s", job, perms.requiredWrite, jobSection)
		}
		for _, forbiddenPermission := range perms.forbidden {
			if strings.Contains(jobSection, forbiddenPermission) {
				t.Errorf("Job %q should not include %q permission in:\n%s", job, forbiddenPermission, jobSection)
			}
		}
	}

	// cleanup-cache-memory job should run on schedule, empty operation, or clean_cache_memories operation
	cleanupCacheIdx := strings.Index(yaml, "\n  cleanup-cache-memory:")
	if cleanupCacheIdx == -1 {
		t.Errorf("Job cleanup-cache-memory not found in generated workflow")
	} else {
		cleanupCacheSection := yaml[cleanupCacheIdx : cleanupCacheIdx+jobSectionSearchRange]
		if !strings.Contains(cleanupCacheSection, cleanCacheMemoriesCondition) {
			t.Errorf("Job cleanup-cache-memory should have the clean_cache_memories condition %q in:\n%s", cleanCacheMemoriesCondition, cleanupCacheSection)
		}
	}

	// run_operation job should NOT have the skip condition but should have its own activation condition
	// and should exclude safe_outputs
	runOpIdx := strings.Index(yaml, "\n  run_operation:")
	if runOpIdx == -1 {
		t.Errorf("Job run_operation not found in generated workflow")
	} else {
		runOpSection := yaml[runOpIdx : runOpIdx+runOpSectionSearchRange]
		if strings.Contains(runOpSection, operationSkipCondition) {
			t.Errorf("Job run_operation should NOT have the operation skip condition")
		}
		if !strings.Contains(runOpSection, operationRunCondition) {
			t.Errorf("Job run_operation should have the activation condition %q", operationRunCondition)
		}
	}

	// apply_safe_outputs job should be triggered when operation == 'safe_outputs'
	applyIdx := strings.Index(yaml, "\n  apply_safe_outputs:")
	if applyIdx == -1 {
		t.Errorf("Job apply_safe_outputs not found in generated workflow")
	} else {
		applySection := yaml[applyIdx : applyIdx+runOpSectionSearchRange]
		if !strings.Contains(applySection, applySafeOutputsCondition) {
			t.Errorf("Job apply_safe_outputs should have the activation condition %q in:\n%s", applySafeOutputsCondition, applySection)
		}
	}

	// create_labels job should be triggered when operation == 'create_labels'
	createLabelsIdx := strings.Index(yaml, "\n  create_labels:")
	if createLabelsIdx == -1 {
		t.Errorf("Job create_labels not found in generated workflow")
	} else {
		createLabelsSection := yaml[createLabelsIdx : createLabelsIdx+runOpSectionSearchRange]
		if !strings.Contains(createLabelsSection, createLabelsCondition) {
			t.Errorf("Job create_labels should have the activation condition %q in:\n%s", createLabelsCondition, createLabelsSection)
		}
	}

	// update_pull_request_branches job should be triggered when operation == 'update_pull_request_branches'
	updatePullRequestBranchesIdx := strings.Index(yaml, "\n  update_pull_request_branches:")
	if updatePullRequestBranchesIdx == -1 {
		t.Errorf("Job update_pull_request_branches not found in generated workflow")
	} else {
		updatePullRequestBranchesSection := yaml[updatePullRequestBranchesIdx : updatePullRequestBranchesIdx+runOpSectionSearchRange]
		if !strings.Contains(updatePullRequestBranchesSection, updatePullRequestBranchesCondition) {
			t.Errorf("Job update_pull_request_branches should have the activation condition %q in:\n%s", updatePullRequestBranchesCondition, updatePullRequestBranchesSection)
		}
		if !strings.Contains(updatePullRequestBranchesSection, "pull-requests: write") {
			t.Errorf("Job update_pull_request_branches should include pull-requests: write permission in:\n%s", updatePullRequestBranchesSection)
		}
		if !strings.Contains(updatePullRequestBranchesSection, "contents: write") {
			t.Errorf("Job update_pull_request_branches should include contents: write permission in:\n%s", updatePullRequestBranchesSection)
		}
	}

	// validate_workflows job should be triggered when operation == 'validate'
	validateCondition := `(github.event_name == 'workflow_dispatch' || github.event_name == 'workflow_call') && inputs.operation == 'validate'`
	validateIdx := strings.Index(yaml, "\n  validate_workflows:")
	if validateIdx == -1 {
		t.Errorf("Job validate_workflows not found in generated workflow")
	} else {
		validateSection := yaml[validateIdx : validateIdx+runOpSectionSearchRange]
		if !strings.Contains(validateSection, validateCondition) {
			t.Errorf("Job validate_workflows should have the activation condition %q in:\n%s", validateCondition, validateSection)
		}
	}

	// activity_report job should be triggered when operation == 'activity_report'
	activityReportIdx := strings.Index(yaml, "\n  activity_report:")
	if activityReportIdx == -1 {
		t.Errorf("Job activity_report not found in generated workflow")
	} else {
		activityReportSection := yaml[activityReportIdx : activityReportIdx+runOpSectionSearchRange]
		if !strings.Contains(activityReportSection, activityReportCondition) {
			t.Errorf("Job activity_report should have the activation condition %q in:\n%s", activityReportCondition, activityReportSection)
		}
		if !strings.Contains(activityReportSection, "contents: read") {
			t.Errorf("Job activity_report should include contents: read permission in:\n%s", activityReportSection)
		}
		if !strings.Contains(activityReportSection, "timeout-minutes: 120") {
			t.Errorf("Job activity_report should set timeout-minutes: 120 in:\n%s", activityReportSection)
		}
	}
	if !strings.Contains(yaml, "Restore activity report logs cache") {
		t.Errorf("Job activity_report should include a cache restore step in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Save activity report logs cache") {
		t.Errorf("Job activity_report should include a cache save step in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "if: ${{ always() }}") {
		t.Errorf("Job activity_report should save cache even when earlier steps fail in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "steps.activity_report_logs_cache.outputs.cache-primary-key") {
		t.Errorf("Job activity_report cache save step should use cache primary key output in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "${{ github.run_id }}") {
		t.Errorf("Job activity_report cache key should include run_id for latest-cache resolution in:\n%s", yaml)
	}

	if !strings.Contains(yaml, "Download activity report logs") {
		t.Errorf("Job activity_report should include direct logs download step in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "timeout-minutes: 20") {
		t.Errorf("Job activity_report logs download step should set timeout-minutes: 20 in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "${GH_AW_CMD_PREFIX} logs") {
		t.Errorf("Job activity_report should run gh aw logs directly in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "--start-date -1w") {
		t.Errorf("Job activity_report gh aw logs command should include --start-date -1w in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "--count 500") {
		t.Errorf("Job activity_report gh aw logs command should include --count 500 in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "--format markdown") {
		t.Errorf("Job activity_report gh aw logs command should include --format markdown in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "./.cache/gh-aw/activity-report-logs/report.md") {
		t.Errorf("Job activity_report gh aw logs command should write report markdown output to report.md in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Generate activity report issue") {
		t.Errorf("Job activity_report should include issue generation step after cache save in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "title: '[aw] agentic status report'") {
		t.Errorf("Job activity_report issue generation step should create the activity report issue title in:\n%s", yaml)
	}

	forecastIdx := strings.Index(yaml, "\n  forecast_report:")
	if forecastIdx == -1 {
		t.Errorf("Job forecast_report not found in generated workflow")
	} else {
		forecastSection := yaml[forecastIdx : forecastIdx+runOpSectionSearchRange]
		if !strings.Contains(forecastSection, forecastCondition) {
			t.Errorf("Job forecast_report should have the activation condition %q in:\n%s", forecastCondition, forecastSection)
		}
		if !strings.Contains(forecastSection, "actions: read") {
			t.Errorf("Job forecast_report should include actions: read permission in:\n%s", forecastSection)
		}
		if !strings.Contains(forecastSection, "issues: write") {
			t.Errorf("Job forecast_report should include issues: write permission in:\n%s", forecastSection)
		}
		if !strings.Contains(forecastSection, "timeout-minutes: 60") {
			t.Errorf("Job forecast_report should set timeout-minutes: 60 in:\n%s", forecastSection)
		}
	}
	if !strings.Contains(yaml, "Generate forecast report") {
		t.Errorf("Job forecast_report should include forecast generation step in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "${GH_AW_CMD_PREFIX} forecast") {
		t.Errorf("Job forecast_report should run gh aw forecast directly in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Restore forecast report logs cache") {
		t.Errorf("Job forecast_report should restore logs cache before running forecast in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Save forecast report logs cache") {
		t.Errorf("Job forecast_report should save logs cache after running forecast in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "Debug forecast logs folder") {
		t.Errorf("Job forecast_report should include a debug step that lists forecast logs folder files in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "find ./.github/aw/logs -type f | sort") {
		t.Errorf("Job forecast_report debug step should list files from ./.github/aw/logs in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "./.github/aw/logs") {
		t.Errorf("Job forecast_report cache should target ./.github/aw/logs in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "${{ runner.os }}-forecast-report-logs-${{ github.repository }}-${{ github.ref_name }}-${{ github.run_id }}") {
		t.Errorf("Job forecast_report cache save step should use the explicit forecast logs cache key in:\n%s", yaml)
	}
	if strings.Contains(yaml, "${GH_AW_CMD_PREFIX} logs --repo \"${{ github.repository }}\" --start-date -30d --count 1500 --artifacts agent") {
		t.Errorf("Job forecast_report should not pre-download full logs before running forecast in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "--repo \"$GITHUB_REPOSITORY\" --timeout 30 --verbose --json") {
		t.Errorf("Job forecast_report gh aw forecast command should include --repo, --timeout, --verbose, and --json in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "shell: bash") {
		t.Errorf("Job forecast_report should explicitly use bash shell for stderr filtering in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "timeout-minutes: 30") {
		t.Errorf("Job forecast_report should set a 30-minute timeout on the forecast generation step in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "DEBUG: \"*\"") {
		t.Errorf("Job forecast_report should enable DEBUG=* in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "${GH_AW_CMD_PREFIX} forecast --repo \"$GITHUB_REPOSITORY\" --timeout 30 --verbose --json > ./.cache/gh-aw/forecast/report.json") {
		t.Errorf("Job forecast_report gh aw forecast command should run in verbose mode and write report output in:\n%s", yaml)
	}
	if strings.Contains(yaml, "2> >(grep -Fv \"forecast is an experimental command and may change without notice\" >&2)") {
		t.Errorf("Job forecast_report should not filter forecast stderr in:\n%s", yaml)
	}
	if strings.Contains(yaml, "timeout 10m ${GH_AW_CMD_PREFIX} forecast") {
		t.Errorf("Job forecast_report should not use shell timeout wrapper for forecast command anymore in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "echo '{\"outcome\":\"timeout\",\"message\":\"Forecast computation timed out after 30 minutes.\"}' > ./.cache/gh-aw/forecast/error.json") {
		t.Errorf("Job forecast_report should record timeout errors for issue creation in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "if: ${{ always() }}") {
		t.Errorf("Job forecast_report should always run follow-up issue/cache steps even when forecast fails in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "FORECAST_STEP_OUTCOME: ${{ steps.generate_forecast_report.outcome }}") {
		t.Errorf("Job forecast_report issue generation step should pass forecast step outcome to create_forecast_issue.cjs in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "const { main } = require(path.join(actionsDir, 'create_forecast_issue.cjs'));") {
		t.Errorf("Job forecast_report issue generation step should call create_forecast_issue.cjs in:\n%s", yaml)
	}
	if !strings.Contains(yaml, "setupGlobals(core, github, context, exec, io, getOctokit);") {
		t.Errorf("Job forecast_report issue generation step should initialize setup globals before calling create_forecast_issue.cjs in:\n%s", yaml)
	}

	// close_agentic_workflows_issues job should be triggered when operation == 'close_agentic_workflows_issues'
	closeAgenticWorkflowIssuesIdx := strings.Index(yaml, "\n  close_agentic_workflows_issues:")
	if closeAgenticWorkflowIssuesIdx == -1 {
		t.Errorf("Job close_agentic_workflows_issues not found in generated workflow")
	} else {
		closeAgenticWorkflowIssuesSection := yaml[closeAgenticWorkflowIssuesIdx : closeAgenticWorkflowIssuesIdx+runOpSectionSearchRange]
		if !strings.Contains(closeAgenticWorkflowIssuesSection, closeAgenticWorkflowIssuesCondition) {
			t.Errorf("Job close_agentic_workflows_issues should have the activation condition %q in:\n%s", closeAgenticWorkflowIssuesCondition, closeAgenticWorkflowIssuesSection)
		}
	}

	// Verify create_labels is an option in the operation choices
	if !strings.Contains(yaml, "- 'create_labels'") {
		t.Error("workflow_dispatch operation choices should include 'create_labels'")
	}

	// Verify safe_outputs is an option in the operation choices
	if !strings.Contains(yaml, "- 'safe_outputs'") {
		t.Error("workflow_dispatch operation choices should include 'safe_outputs'")
	}

	// Verify clean_cache_memories is an option in the operation choices
	if !strings.Contains(yaml, "- 'clean_cache_memories'") {
		t.Error("workflow_dispatch operation choices should include 'clean_cache_memories'")
	}

	// Verify update_pull_request_branches is an option in the operation choices
	if !strings.Contains(yaml, "- 'update_pull_request_branches'") {
		t.Error("workflow_dispatch operation choices should include 'update_pull_request_branches'")
	}

	// Verify validate is an option in the operation choices
	if !strings.Contains(yaml, "- 'validate'") {
		t.Error("workflow_dispatch operation choices should include 'validate'")
	}

	// Verify activity_report is an option in the operation choices
	if !strings.Contains(yaml, "- 'activity_report'") {
		t.Error("workflow_dispatch operation choices should include 'activity_report'")
	}

	// Verify forecast is an option in the operation choices
	if !strings.Contains(yaml, "- 'forecast'") {
		t.Error("workflow_dispatch operation choices should include 'forecast'")
	}

	// Verify close_agentic_workflows_issues is an option in the operation choices
	if !strings.Contains(yaml, "- 'close_agentic_workflows_issues'") {
		t.Error("workflow_dispatch operation choices should include 'close_agentic_workflows_issues'")
	}

	// Verify run_url input exists in workflow_dispatch
	if !strings.Contains(yaml, "run_url:") {
		t.Error("workflow_dispatch should include run_url input")
	}

	// Verify workflow_call trigger is present with same inputs
	workflowCallIdx := strings.Index(yaml, "workflow_call:")
	if workflowCallIdx == -1 {
		t.Error("workflow should include workflow_call trigger")
	} else {
		workflowCallSection := yaml[workflowCallIdx:]
		if !strings.Contains(workflowCallSection, "inputs:\n      operation:") {
			t.Error("workflow_call trigger should include operation input")
		}
	}

	// Verify workflow_call outputs are declared
	if !strings.Contains(yaml, "operation_completed:") {
		t.Error("workflow_call outputs should include operation_completed")
	}
	if !strings.Contains(yaml, "applied_run_url:") {
		t.Error("workflow_call outputs should include applied_run_url")
	}

	// Verify run_operation job exposes outputs
	runOpIdx2 := strings.Index(yaml, "\n  run_operation:")
	if runOpIdx2 != -1 {
		runOpEnd := min(runOpIdx2+1200, len(yaml))
		runOpSection2 := yaml[runOpIdx2:runOpEnd]
		if !strings.Contains(runOpSection2, "outputs:\n      operation: ${{ steps.record.outputs.operation }}") {
			t.Errorf("run_operation job should declare operation output, got:\n%s", runOpSection2[:min(300, len(runOpSection2))])
		}
	}

	// Verify apply_safe_outputs job exposes run_url output
	applyIdx2 := strings.Index(yaml, "\n  apply_safe_outputs:")
	if applyIdx2 != -1 {
		applySection2 := yaml[applyIdx2 : applyIdx2+600]
		if !strings.Contains(applySection2, "outputs:\n      run_url: ${{ steps.record.outputs.run_url }}") {
			t.Errorf("apply_safe_outputs job should declare run_url output, got:\n%s", applySection2[:300])
		}
	}
}

func TestGenerateMaintenanceWorkflow_DisableAgenticWorkflowJob(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					Expires: 48,
				},
			},
		},
	}

	tmpDir := t.TempDir()
	trueVal := true
	cfg := &RepoConfig{
		Maintenance: &MaintenanceConfig{LabelTriggers: &trueVal},
	}
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       cfg,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)

	const jobSectionSearchRange = 2000

	// Verify only the issues label trigger is present (pull request is no longer supported)
	if !strings.Contains(yaml, "  issues:\n    types: [labeled]") {
		t.Error("Maintenance workflow should include issues: types: [labeled] trigger")
	}
	if strings.Contains(yaml, "  pull_request:\n    types: [labeled]") {
		t.Error("Maintenance workflow must NOT include pull_request: types: [labeled] trigger (issues-only)")
	}

	// Verify the label_disable_agentic_workflow job exists
	disableJobIdx := strings.Index(yaml, "\n  label_disable_agentic_workflow:")
	if disableJobIdx == -1 {
		t.Fatal("Job label_disable_agentic_workflow not found in generated workflow")
	}
	// Bound the section to just the label_disable_agentic_workflow job by finding the next job start
	nextJobIdx := strings.Index(yaml[disableJobIdx+1:], "\n  label_apply_safe_outputs:")
	if nextJobIdx == -1 {
		nextJobIdx = jobSectionSearchRange
	}
	disableJobSection := yaml[disableJobIdx : disableJobIdx+1+nextJobIdx]

	// Verify the condition triggers only on issues label events (not pull_request)
	if !strings.Contains(disableJobSection, "github.event_name == 'issues'") {
		t.Errorf("label_disable_agentic_workflow job should trigger on issues events in:\n%s", disableJobSection)
	}
	if strings.Contains(disableJobSection, "github.event_name == 'pull_request'") {
		t.Errorf("label_disable_agentic_workflow job must NOT trigger on pull_request events (issues-only) in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "github.event.label.name == 'agentic-workflows:disable'") {
		t.Errorf("label_disable_agentic_workflow job should check for agentic-workflows:disable label in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "github.event.repository.fork") {
		t.Errorf("label_disable_agentic_workflow job should exclude forks in:\n%s", disableJobSection)
	}

	// Verify required permissions (no pull-requests: write since issues-only)
	if !strings.Contains(disableJobSection, "actions: write") {
		t.Errorf("label_disable_agentic_workflow job should have actions: write permission in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "contents: read") {
		t.Errorf("label_disable_agentic_workflow job should have contents: read permission in:\n%s", disableJobSection)
	}
	if strings.Contains(disableJobSection, "contents: write") {
		t.Errorf("label_disable_agentic_workflow job must NOT have contents: write (only read is needed) in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "issues: write") {
		t.Errorf("label_disable_agentic_workflow job should have issues: write permission in:\n%s", disableJobSection)
	}
	if strings.Contains(disableJobSection, "pull-requests: write") {
		t.Errorf("label_disable_agentic_workflow job must NOT have pull-requests: write (issues-only) in:\n%s", disableJobSection)
	}

	// Verify the job uses disable_agentic_workflow.cjs
	if !strings.Contains(disableJobSection, "disable_agentic_workflow.cjs") {
		t.Errorf("label_disable_agentic_workflow job should use disable_agentic_workflow.cjs script in:\n%s", disableJobSection)
	}

	// Verify the job includes the permission check step with an id and that the operation step
	// has an explicit if condition referencing that id (so unauthorized users cannot bypass the check)
	if !strings.Contains(disableJobSection, "check_team_member.cjs") {
		t.Errorf("label_disable_agentic_workflow job should check permissions using check_team_member.cjs in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "id: check_permissions") {
		t.Errorf("label_disable_agentic_workflow permission check step should have id: check_permissions in:\n%s", disableJobSection)
	}
	if !strings.Contains(disableJobSection, "steps.check_permissions.outcome == 'success'") {
		t.Errorf("label_disable_agentic_workflow operation step should have if: steps.check_permissions.outcome == 'success' in:\n%s", disableJobSection)
	}
}

func TestBuildLabeledDisableCondition(t *testing.T) {
	condition := buildLabeledDisableCondition()
	rendered := RenderCondition(condition)

	// Should only include issues event (not pull_request — issues-only by design)
	if !strings.Contains(rendered, "github.event_name == 'issues'") {
		t.Errorf("Condition should include issues event, got: %s", rendered)
	}
	if strings.Contains(rendered, "github.event_name == 'pull_request'") {
		t.Errorf("Condition must not include pull_request event (issues-only), got: %s", rendered)
	}

	// Should check the label name
	if !strings.Contains(rendered, "github.event.label.name == 'agentic-workflows:disable'") {
		t.Errorf("Condition should check for agentic-workflows:disable label, got: %s", rendered)
	}

	// Should exclude forks
	if !strings.Contains(rendered, "github.event.repository.fork") {
		t.Errorf("Condition should exclude forks, got: %s", rendered)
	}

	// Should not include workflow_dispatch or schedule-related conditions
	if strings.Contains(rendered, "workflow_dispatch") || strings.Contains(rendered, "workflow_call") {
		t.Errorf("Condition should not reference workflow_dispatch or workflow_call, got: %s", rendered)
	}
}

func TestBuildLabeledApplySafeOutputsCondition(t *testing.T) {
	condition := buildLabeledApplySafeOutputsCondition()
	rendered := RenderCondition(condition)

	// Should only include issues event
	if !strings.Contains(rendered, "github.event_name == 'issues'") {
		t.Errorf("Condition should include issues event, got: %s", rendered)
	}
	if strings.Contains(rendered, "github.event_name == 'pull_request'") {
		t.Errorf("Condition must not include pull_request event (issues-only), got: %s", rendered)
	}

	// Should check the apply-safe-outputs label name
	if !strings.Contains(rendered, "github.event.label.name == 'agentic-workflows:apply-safe-outputs'") {
		t.Errorf("Condition should check for agentic-workflows:apply-safe-outputs label, got: %s", rendered)
	}

	// Should exclude forks
	if !strings.Contains(rendered, "github.event.repository.fork") {
		t.Errorf("Condition should exclude forks, got: %s", rendered)
	}
}
