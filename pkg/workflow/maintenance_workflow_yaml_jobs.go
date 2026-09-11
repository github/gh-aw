package workflow

import (
	"context"
	"strconv"
	"strings"
)

func writeMaintenanceConditionalActionsCheckoutStep(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions) {
	if opts.actionMode != ActionModeDev && opts.actionMode != ActionModeScript {
		return
	}
	b.WriteString(`      - name: Checkout actions folder
        uses: ` + getActionPin("actions/checkout") + `
        with:
          sparse-checkout: |
            actions
          clean: false
          persist-credentials: false

`)
}

func writeMaintenanceActionsFolderCheckoutStep(b *strings.Builder) {
	b.WriteString(`      - name: Checkout actions folder
        uses: ` + getActionPin("actions/checkout") + `
        with:
          sparse-checkout: |
            actions
          clean: false
          persist-credentials: false

`)
}

func writeMaintenanceRepositoryCheckoutStep(b *strings.Builder) {
	b.WriteString(`      - name: Checkout repository
        uses: ` + getActionPin("actions/checkout") + `
        with:
          persist-credentials: false

`)
}

func writeMaintenanceSetupScriptsStep(b *strings.Builder, setupActionRef string) {
	b.WriteString(`      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: ${{ runner.temp }}/gh-aw/actions

`)
}

func writeMaintenanceAdminPermissionsStep(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, id string) {
	b.WriteString("      - name: Check admin/maintainer permissions\n")
	if id != "" {
		b.WriteString("        id: " + id + "\n")
	}
	b.WriteString(`        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/check_team_member.cjs');
            await main();

`)
}

func writeMaintenanceCloseExpiredJobYAML(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef, jobName, permissionLine, stepName, scriptName string) {
	b.WriteString(`  ` + jobName + `:
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnly()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      ` + permissionLine + `
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(b, opts)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	b.WriteString(`      - name: ` + stepName + `
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/` + scriptName + `.cjs');
            await main();
`)
}

func buildMaintenanceCloseExpiredJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	if opts.maintenanceConfig.IsJobDisabled("close-expired-entities") {
		return ""
	}
	var b strings.Builder
	writeMaintenanceCloseExpiredJobYAML(&b, opts, setupActionRef, "close-expired-discussions", "discussions: write", "Close expired discussions", "close_expired_discussions")
	writeMaintenanceCloseExpiredJobYAML(&b, opts, setupActionRef, "close-expired-issues", "issues: write", "Close expired issues", "close_expired_issues")
	writeMaintenanceCloseExpiredJobYAML(&b, opts, setupActionRef, "close-expired-pull-requests", "pull-requests: write", "Close expired pull requests", "close_expired_pull_requests")
	return b.String()
}

func buildMaintenanceCleanupCacheJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  cleanup-cache-memory:
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnlyOrOperation("clean_cache_memories")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	b.WriteString(`      - name: Cleanup outdated cache-memory entries
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/cleanup_cache_memory.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceRunOperationJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  run_operation:
    if: ${{ ` + RenderCondition(buildRunOperationCondition("safe_outputs", "create_labels", "activity_report", "close_agentic_workflows_issues", "clean_cache_memories", "update_pull_request_branches", "validate", "forecast")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: write
      contents: write
      pull-requests: write
    outputs:
      operation: ${{ steps.record.outputs.operation }}
    steps:
`)
	writeMaintenanceRepositoryCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(`      - name: Run operation
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_OPERATION: ${{ inputs.operation }}
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(opts.actionMode) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/run_operation_update_upgrade.cjs');
            await main();

      - name: Record outputs
        id: record
        env:
          GH_AW_OPERATION: ${{ inputs.operation }}
        run: echo "operation=$GH_AW_OPERATION" >> "$GITHUB_OUTPUT"
`)
	return b.String()
}

func buildMaintenanceUpdatePRBranchesJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  update_pull_request_branches:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("update_pull_request_branches")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: write
      pull-requests: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(`      - name: Update pull request branches
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/update_pull_request_branches.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceApplySafeOutputsJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	if opts.maintenanceConfig.IsJobDisabled("apply_safe_outputs") {
		return ""
	}
	var b strings.Builder
	b.WriteString(`
  apply_safe_outputs:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("safe_outputs")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: write
      discussions: write
      issues: write
      pull-requests: write
    outputs:
      run_url: ${{ steps.record.outputs.run_url }}
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(`      - name: Apply Safe Outputs
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_RUN_URL: ${{ inputs.run_url }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/apply_safe_outputs_replay.cjs');
            await main();

      - name: Record outputs
        id: record
        env:
          GH_AW_RUN_URL: ${{ inputs.run_url }}
        run: echo "run_url=$GH_AW_RUN_URL" >> "$GITHUB_OUTPUT"
`)
	return b.String()
}

func buildMaintenanceCreateLabelsJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  create_labels:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("create_labels")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: read
      issues: write
    steps:
`)
	writeMaintenanceRepositoryCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(`      - name: Create missing labels
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(opts.actionMode) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/create_labels.cjs');
            await main();
`)
	return b.String()
}

func writeMaintenanceIssueReportJobPrefix(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef, jobName, operation string, timeoutMinutes int) {
	b.WriteString(`
  ` + jobName + `:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition(operation)) + ` }}
    runs-on: ` + opts.runsOnValue + `
    timeout-minutes: ` + strconv.Itoa(timeoutMinutes) + `
    permissions:
      actions: read
      contents: read
      issues: write
    steps:
`)
	writeMaintenanceRepositoryCheckoutStep(b)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(b, opts, "")
}

func buildMaintenanceActivityReportJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	writeMaintenanceIssueReportJobPrefix(&b, opts, setupActionRef, "activity_report", "activity_report", 120)
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(buildMaintenanceActivityReportCacheAndDownloadSteps(opts))
	b.WriteString(buildMaintenanceActivityReportIssueStep(opts.resolver))
	return b.String()
}

// buildMaintenanceActivityReportCacheAndDownloadSteps returns the cache-restore, download, and cache-save steps.
func buildMaintenanceActivityReportCacheAndDownloadSteps(opts buildMaintenanceWorkflowYAMLOptions) string {
	return `      - name: Restore activity report logs cache
        id: activity_report_logs_cache
        uses: ` + getActionPin("actions/cache/restore") + `
        with:
          path: ./.cache/gh-aw/activity-report-logs
          key: ${{ runner.os }}-activity-report-logs-${{ github.repository }}-${{ github.ref_name }}-${{ github.run_id }}
          restore-keys: |
            ${{ runner.os }}-activity-report-logs-${{ github.repository }}-
            ${{ runner.os }}-activity-report-logs-
      - name: Download activity report logs
        timeout-minutes: 20
        shell: bash
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(opts.actionMode) + `
        run: |
          ${GH_AW_CMD_PREFIX} logs \
            --repo "$GITHUB_REPOSITORY" \
            --start-date -1w \
            --count 500 \
            --output ./.cache/gh-aw/activity-report-logs \
            --format markdown \
            --report-file ./.cache/gh-aw/activity-report-logs/report.md

      - name: Save activity report logs cache
        if: ${{ always() }}
        uses: ` + getActionPin("actions/cache/save") + `
        with:
          path: ./.cache/gh-aw/activity-report-logs
          key: ${{ steps.activity_report_logs_cache.outputs.cache-primary-key }}

`
}

// buildMaintenanceActivityReportIssueStep returns the "Generate activity report issue" step.
func buildMaintenanceActivityReportIssueStep(resolver SHAResolver) string {
	return `      - name: Generate activity report issue
        uses: ` + getCachedActionPinFromResolver("actions/github-script", resolver) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const fs = require('node:fs');
            const reportPath = './.cache/gh-aw/activity-report-logs/report.md';
            if (!fs.existsSync(reportPath)) {
              core.warning('Activity report markdown not found at ' + reportPath + '; skipping issue creation.');
              return;
            }
            let reportBody = '';
            try {
              reportBody = fs.readFileSync(reportPath, 'utf8').trim();
            } catch (error) {
              core.warning('Failed to read activity report markdown at ' + reportPath + ': ' + error.message);
              return;
            }
            if (!reportBody) {
              core.warning('Activity report markdown is empty at ' + reportPath + '; skipping issue creation.');
              return;
            }
            const repoSlug = context.repo.owner + '/' + context.repo.repo;
            const body = [
              '### Agentic workflow activity report',
              '',
              'Repository: ' + repoSlug,
              'Generated at: ' + new Date().toISOString(),
              '',
              reportBody,
            ].join('\n');
            const createdIssue = await github.rest.issues.create({
              owner: context.repo.owner,
              repo: context.repo.repo,
              title: '[aw] agentic status report',
              body,
              labels: ['agentic-workflows'],
            });
            core.info('Created issue #' + createdIssue.data.number + ': ' + createdIssue.data.html_url);
`
}

func buildMaintenanceForecastReportJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	writeMaintenanceIssueReportJobPrefix(&b, opts, setupActionRef, "forecast_report", "forecast", 60)
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(buildMaintenanceForecastRunSteps(opts))
	b.WriteString(buildMaintenanceForecastIssueStep(opts.resolver))
	return b.String()
}

// buildMaintenanceForecastRunSteps returns the cache-restore, run, debug, and cache-save steps.
func buildMaintenanceForecastRunSteps(opts buildMaintenanceWorkflowYAMLOptions) string {
	return `      - name: Restore forecast report logs cache
        id: forecast_report_logs_cache
        uses: ` + getActionPin("actions/cache/restore") + `
        with:
          path: ./.github/aw/logs
          key: ${{ runner.os }}-forecast-report-logs-${{ github.repository }}-${{ github.ref_name }}-${{ github.run_id }}
          restore-keys: |
            ${{ runner.os }}-forecast-report-logs-${{ github.repository }}-
            ${{ runner.os }}-forecast-report-logs-

      - name: Generate forecast report
        id: generate_forecast_report
        timeout-minutes: 30
        shell: bash
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          DEBUG: "*"
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(opts.actionMode) + `
        run: |
          mkdir -p ./.cache/gh-aw/forecast
          set +e
          ${GH_AW_CMD_PREFIX} forecast --repo "$GITHUB_REPOSITORY" --timeout 30 --verbose --json > ./.cache/gh-aw/forecast/report.json
          forecast_exit_code=$?
          set -e
          if [ "${forecast_exit_code}" -eq 124 ]; then
            echo '{"outcome":"timeout","message":"Forecast computation timed out after 30 minutes."}' > ./.cache/gh-aw/forecast/error.json
            echo "::error::Forecast computation timed out after 30 minutes."
            exit 1
          fi
          if [ "${forecast_exit_code}" -ne 0 ]; then
            echo '{"outcome":"error","message":"Forecast computation failed before producing a report."}' > ./.cache/gh-aw/forecast/error.json
            echo "::error::Forecast computation failed with exit code ${forecast_exit_code}."
            exit 1
          fi

      - name: Debug forecast logs folder
        if: ${{ always() }}
        shell: bash
        run: |
          if [ ! -d ./.github/aw/logs ]; then
            echo "Logs directory not found: ./.github/aw/logs"
            exit 0
          fi
          echo "Files under ./.github/aw/logs:"
          find ./.github/aw/logs -type f | sort

      - name: Save forecast report logs cache
        if: ${{ always() }}
        uses: ` + getActionPin("actions/cache/save") + `
        with:
          path: ./.github/aw/logs
          key: ${{ runner.os }}-forecast-report-logs-${{ github.repository }}-${{ github.ref_name }}-${{ github.run_id }}

`
}

// buildMaintenanceForecastIssueStep returns the "Generate forecast issue" step.
func buildMaintenanceForecastIssueStep(resolver SHAResolver) string {
	return `      - name: Generate forecast issue
        if: ${{ always() }}
        uses: ` + getCachedActionPinFromResolver("actions/github-script", resolver) + `
        env:
          FORECAST_STEP_OUTCOME: ${{ steps.generate_forecast_report.outcome }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/create_forecast_issue.cjs');
            await main();
`
}

func buildMaintenanceCloseIssuesJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  close_agentic_workflows_issues:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("close_agentic_workflows_issues")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      issues: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(`      - name: Close no-repro agentic-workflows issues
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/close_agentic_workflows_issues.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceValidateWorkflowsJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  validate_workflows:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("validate")) + ` }}
    runs-on: ` + FormatRunsOn(opts.configuredRunsOn, "ubuntu-latest") + `
    permissions:
      contents: read
      issues: write
    steps:
`)
	writeMaintenanceRepositoryCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "")
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(`      - name: Validate workflows and file issue on findings
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(opts.actionMode) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/run_validate_workflows.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceLabelDisableJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  label_disable_agentic_workflow:
    if: ${{ ` + RenderCondition(buildLabeledDisableCondition()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: write
      contents: read
      issues: write
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "check_permissions")
	b.WriteString(`      - name: Disable agentic workflow
        if: ${{ steps.check_permissions.outcome == 'success' }}
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/disable_agentic_workflow.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceLabelApplySafeOutputsJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  label_apply_safe_outputs:
    if: ${{ ` + RenderCondition(buildLabeledApplySafeOutputsCondition()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: write
      discussions: write
      issues: write
      pull-requests: write
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&b)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&b, opts, "check_permissions")
	b.WriteString(`      - name: Apply safe outputs from referenced run
        if: ${{ steps.check_permissions.outcome == 'success' }}
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/label_apply_safe_outputs.cjs');
            await main();
`)
	return b.String()
}

func buildMaintenanceLabelTriggeredJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	labelDisableJobEnabled := !opts.disableLabelTrigger && !opts.maintenanceConfig.IsJobDisabled("label_disable_agentic_workflow")
	labelApplySafeOutputsJobEnabled := !opts.disableLabelTrigger && !opts.maintenanceConfig.IsJobDisabled("label_apply_safe_outputs")
	if !labelDisableJobEnabled && !labelApplySafeOutputsJobEnabled {
		return ""
	}
	var b strings.Builder
	if labelDisableJobEnabled {
		b.WriteString(buildMaintenanceLabelDisableJob(opts, setupActionRef))
	}
	if labelApplySafeOutputsJobEnabled {
		b.WriteString(buildMaintenanceLabelApplySafeOutputsJob(opts, setupActionRef))
	}
	return b.String()
}

func writeMaintenanceCompileWorkflowsTokenStep(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions) {
	b.WriteString(`      - name: Check for out-of-sync workflows and create issue or pull request if needed
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
`)
	if opts.compileGitHubToken != "" {
		b.WriteString(`        env:
          GH_AW_MAINTENANCE_GITHUB_TOKEN: ` + opts.compileGitHubToken + `
`)
	}
	b.WriteString(`        with:
`)
	if opts.compileGitHubToken != "" {
		b.WriteString(`          github-token: ${{ env.GH_AW_MAINTENANCE_GITHUB_TOKEN }}
`)
	}
	b.WriteString(`          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/check_workflow_recompile_needed.cjs');
            await main();
`)
}

func buildMaintenanceCompileWorkflowsJob(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  compile-workflows:
    if: ${{ ` + RenderCondition(buildNotForkAndScheduled()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    concurrency:
      group: ${{ github.workflow }}-compile-workflows-${{ github.repository }}
      cancel-in-progress: true
    permissions:
      contents: read
      issues: write
    steps:
      - name: Checkout repository
        uses: ` + getActionPin("actions/checkout") + `
        with:
          persist-credentials: false

`)
	b.WriteString(generateInstallCLISteps(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver))
	b.WriteString(`      - name: Pre-compile validation
        run: |
          ` + getCLICmdPrefix(opts.actionMode) + ` compile --validate --no-emit --verbose
          echo "✓ Pre-compile validation passed"

      - name: Compile workflows
        run: |
          ` + getCLICmdPrefix(opts.actionMode) + ` compile --validate --verbose
          echo "✓ All workflows compiled successfully"

`)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	writeMaintenanceCompileWorkflowsTokenStep(&b, opts)
	return b.String()
}

func buildMaintenanceSecretValidationJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	copilotOrgBillingLine := ""
	if opts.copilotOrgBilling {
		maintenanceWorkflowYAMLLog.Print("Copilot org billing mode detected: adding GH_AW_COPILOT_ORG_BILLING=true to secret-validation step")
		copilotOrgBillingLine = `          GH_AW_COPILOT_ORG_BILLING: "true"
`
	}
	var b strings.Builder
	b.WriteString(`
  secret-validation:
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnly()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: read
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&b)
	b.WriteString(`      - name: Setup Node.js
        uses: actions/setup-node@39370e3970a6d050c480ffad4ff0ed4d3fdee5af # v4.1.0
        with:
          node-version: '22'

`)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	b.WriteString(`      - name: Validate Secrets
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          # GitHub tokens
          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}
          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}
          GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
          GH_AW_COPILOT_TOKEN: ${{ secrets.GH_AW_COPILOT_TOKEN }}
` + copilotOrgBillingLine + `          # AI Engine API keys
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          BRAVE_API_KEY: ${{ secrets.BRAVE_API_KEY }}
          # Integration tokens
          NOTION_API_TOKEN: ${{ secrets.NOTION_API_TOKEN }}
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/validate_secrets.cjs');
            await main();

      - name: Upload secret validation report
        if: always()
        uses: ` + getActionPin("actions/upload-artifact") + `
        with:
          name: secret-validation-report
          path: secret-validation-report.md
          retention-days: 30
          if-no-files-found: warn
`)
	return b.String()
}

func buildMaintenanceDevOnlyJobs(ctx context.Context, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	if opts.actionMode != ActionModeDev {
		return ""
	}
	maintenanceWorkflowYAMLLog.Printf("Adding dev-only jobs: compile-workflows and secret-validation")
	return buildMaintenanceCompileWorkflowsJob(ctx, opts, setupActionRef) + buildMaintenanceSecretValidationJob(opts, setupActionRef)
}
