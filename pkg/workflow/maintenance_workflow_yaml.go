package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var maintenanceWorkflowYAMLLog = logger.New("workflow:maintenance_workflow_yaml")

// buildMaintenanceWorkflowYAMLOptions configures the maintenance workflow YAML builder.
type buildMaintenanceWorkflowYAMLOptions struct {
	cronSchedule        string
	scheduleDesc        string
	minExpiresDays      int
	runsOnValue         string
	actionMode          ActionMode
	version             string
	actionTag           string
	resolver            SHAResolver
	configuredRunsOn    RunsOnValue
	defaultBranch       string
	disableLabelTrigger bool
	maintenanceConfig   *MaintenanceConfig
	compileGitHubToken  string
	createCompilePR     bool
	copilotOrgBilling   bool // all Copilot workflows use copilot-requests: write (GITHUB_TOKEN); COPILOT_GITHUB_TOKEN is not required
}

// buildMaintenanceWorkflowYAML generates the complete YAML content for the
// agentics-maintenance.yml workflow. It is called by GenerateMaintenanceWorkflow
// after the cron schedule and setup parameters have been resolved.
func buildMaintenanceWorkflowYAML(
	ctx context.Context,
	opts buildMaintenanceWorkflowYAMLOptions,
) (string, error) {
	maintenanceWorkflowYAMLLog.Printf("Building maintenance workflow YAML: actionMode=%s minExpiresDays=%d cronSchedule=%q defaultBranch=%q disableLabelTrigger=%v createCompilePR=%v copilotOrgBilling=%v", opts.actionMode, opts.minExpiresDays, opts.cronSchedule, opts.defaultBranch, opts.disableLabelTrigger, opts.createCompilePR, opts.copilotOrgBilling)
	labelDisableJobEnabled := !opts.disableLabelTrigger && !opts.maintenanceConfig.IsJobDisabled("label_disable_agentic_workflow")
	labelApplySafeOutputsJobEnabled := !opts.disableLabelTrigger && !opts.maintenanceConfig.IsJobDisabled("label_apply_safe_outputs")
	appliedRunURLValue, appliedRunURLDescription := buildMaintenanceAppliedRunURLOutput(opts)
	setupActionRef := ResolveSetupActionReference(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver)

	var yaml strings.Builder
	yaml.WriteString(buildMaintenanceWorkflowHeaderYAML(opts))
	yaml.WriteString(buildMaintenanceWorkflowTriggerYAML(opts, labelDisableJobEnabled, labelApplySafeOutputsJobEnabled, appliedRunURLDescription, appliedRunURLValue))
	yaml.WriteString(buildMaintenanceCloseExpiredJobs(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceCleanupCacheJob(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceRunOperationJob(ctx, opts, setupActionRef))
	yaml.WriteString(buildMaintenanceUpdatePRBranchesJob(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceApplySafeOutputsJob(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceCreateLabelsJob(ctx, opts, setupActionRef))
	yaml.WriteString(buildMaintenanceActivityReportJob(ctx, opts, setupActionRef))
	yaml.WriteString(buildMaintenanceForecastReportJob(ctx, opts, setupActionRef))
	yaml.WriteString(buildMaintenanceCloseIssuesJob(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceValidateWorkflowsJob(ctx, opts, setupActionRef))
	yaml.WriteString(buildMaintenanceLabelTriggeredJobs(opts, setupActionRef))
	yaml.WriteString(buildMaintenanceDevOnlyJobs(ctx, opts, setupActionRef))
	finalYAML, err := finalizeRunnerTempSafety(yaml.String())
	if err != nil {
		return "", fmt.Errorf("runner temp safety: %w", err)
	}
	return finalYAML, nil
}

func buildMaintenanceAppliedRunURLOutput(opts buildMaintenanceWorkflowYAMLOptions) (string, string) {
	appliedRunURLValue := "${{ jobs.apply_safe_outputs.outputs.run_url }}"
	appliedRunURLDescription := "The run URL that safe outputs were applied from"
	if opts.maintenanceConfig.IsJobDisabled("apply_safe_outputs") {
		appliedRunURLValue = "${{ inputs.run_url }}"
		appliedRunURLDescription = "The run URL that safe outputs were applied from (workflow_call falls back to inputs.run_url when apply_safe_outputs is disabled; other triggers leave this empty)"
	}
	return appliedRunURLValue, appliedRunURLDescription
}

func buildMaintenanceWorkflowHeaderYAML(opts buildMaintenanceWorkflowYAMLOptions) string {
	customInstructions := `This file defines the generated agentic maintenance workflow for this repository.
It runs scheduled cleanup for expiring safe outputs and supports manual maintenance operations.

This workflow is generated automatically when workflows use expiring safe outputs
or when repository maintenance features are enabled in .github/workflows/aw.json.

To disable maintenance workflow generation, set in .github/workflows/aw.json:
  {"maintenance": false}

Agentic maintenance docs:
  https://github.github.com/gh-aw/reference/ephemerals/#manual-maintenance-operations`

	return GenerateWorkflowHeader("", "pkg/workflow/maintenance_workflow.go", customInstructions) + `name: Agentic Maintenance

on:
  schedule:
    - cron: "` + opts.cronSchedule + `"  # ` + opts.scheduleDesc + ` (based on minimum expires: ` + strconv.Itoa(opts.minExpiresDays) + ` days)
`
}

func buildMaintenanceWorkflowTriggerYAML(
	opts buildMaintenanceWorkflowYAMLOptions,
	labelDisableJobEnabled bool,
	labelApplySafeOutputsJobEnabled bool,
	appliedRunURLDescription string,
	appliedRunURLValue string,
) string {
	var yaml strings.Builder
	if opts.actionMode == ActionModeDev {
		maintenanceWorkflowYAMLLog.Printf("Adding dev-mode push trigger for branch %q", opts.defaultBranch)
		yaml.WriteString("  push:\n    branches:\n      - " + opts.defaultBranch + "\n    paths:\n      - '.github/workflows/*.md'\n")
	}
	if labelDisableJobEnabled || labelApplySafeOutputsJobEnabled {
		maintenanceWorkflowYAMLLog.Print("Adding issues:labeled trigger for label-triggered maintenance jobs")
		yaml.WriteString("  issues:\n    types: [labeled]\n")
	}
	yaml.WriteString(buildMaintenanceDispatchInputsYAML())
	yaml.WriteString(buildMaintenanceWorkflowCallYAML(appliedRunURLDescription, appliedRunURLValue))
	yaml.WriteString("\npermissions: {}\n\njobs:\n")
	return yaml.String()
}

// buildMaintenanceDispatchInputsYAML returns the workflow_dispatch trigger block.
func buildMaintenanceDispatchInputsYAML() string {
	return fmt.Sprintf(`  workflow_dispatch:
    inputs:
      operation:
        description: 'Optional maintenance operation to run'
        required: false
        type: choice
        default: '%[1]s'
        options:
          - '%[1]s'
          - 'disable'
          - 'enable'
          - 'update'
          - 'upgrade'
          - 'safe_outputs'
          - 'create_labels'
          - 'activity_report'
          - 'close_agentic_workflows_issues'
          - 'clean_cache_memories'
          - 'update_pull_request_branches'
          - 'validate'
          - 'forecast'
      run_url:
        description: 'Run URL or run ID to replay safe outputs from (e.g. https://github.com/owner/repo/actions/runs/12345 or 12345). Required when operation is safe_outputs.'
        required: false
        type: string
        default: ''
`, maintenanceNoOperationValue)
}

// buildMaintenanceWorkflowCallYAML returns the workflow_call trigger block.
func buildMaintenanceWorkflowCallYAML(appliedRunURLDescription, appliedRunURLValue string) string {
	return `  workflow_call:
    inputs:
      operation:
        description: 'Optional maintenance operation to run (disable, enable, update, upgrade, safe_outputs, create_labels, activity_report, close_agentic_workflows_issues, clean_cache_memories, update_pull_request_branches, validate, forecast)'
        required: false
        type: string
        default: ''
      run_url:
        description: 'Run URL or run ID to replay safe outputs from (e.g. https://github.com/owner/repo/actions/runs/12345 or 12345). Required when operation is safe_outputs.'
        required: false
        type: string
        default: ''
    outputs:
      operation_completed:
        description: 'The maintenance operation that was completed (empty when none ran or a scheduled job ran)'
        value: ${{ jobs.run_operation.outputs.operation || inputs.operation }}
      applied_run_url:
        description: '` + appliedRunURLDescription + `'
        value: ` + appliedRunURLValue + `
`
}
