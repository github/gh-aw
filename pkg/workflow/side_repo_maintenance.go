package workflow

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"

	"github.com/github/gh-aw/pkg/stringutil"
)

//go:embed assets/side_repo_maintenance_header.md
var sideRepoMaintenanceHeaderTemplate string

// SideRepoTarget represents a target repository inferred from a checkout block
// with current: true in a compiled workflow. It is used to generate a
// side-repo-specific agentics-maintenance workflow.
type SideRepoTarget struct {
	// Repository is the static owner/repo slug of the target (e.g. "my-org/main-repo").
	// Expression-based repositories (containing "${{") are excluded.
	Repository string

	// GitHubToken is the token expression used to authenticate against the target
	// repository, e.g. "${{ secrets.GH_AW_MAIN_REPO_TOKEN }}". Empty when the
	// checkout config does not specify a custom token.
	// Mutually exclusive with GitHubApp.
	GitHubToken string

	// GitHubApp carries the GitHub App authentication config discovered from the
	// source checkout. When set, each cross-repo maintenance job gets a
	// create-github-app-token mint step and the minted token is used for all
	// github-token: inputs and GH_TOKEN: env vars.
	// Mutually exclusive with GitHubToken.
	GitHubApp *GitHubAppConfig
}

// sideRepoAppTokenStepID is the step ID used for the GitHub App token mint step
// emitted in each cross-repo maintenance job.
const sideRepoAppTokenStepID = "side-repo-app-token"

// sideRepoAppTokenRef is the GitHub Actions expression that references the minted
// token output from the sideRepoAppTokenStepID step.
const sideRepoAppTokenRef = "${{ steps." + sideRepoAppTokenStepID + ".outputs.token }}"

// sideRepoAuth accumulates authentication configuration for a single side-repo target.
// GitHubToken and GitHubApp are mutually exclusive (matching CheckoutConfig).
type sideRepoAuth struct {
	token     string
	githubApp *GitHubAppConfig
}

// collectSideRepoTargets scans all compiled workflow data and returns the unique
// SideRepoTarget entries inferred from checkout blocks with current: true.
// Only checkouts with a static (non-expression) repository string are included.
// When the same repository appears multiple times, a non-empty GitHubToken or a
// non-nil GitHubApp is preferred over an empty auth so that the generated workflow
// uses the custom token rather than falling back to GH_AW_GITHUB_TOKEN.
// The first-seen auth for a given repo is preserved; later occurrences only
// upgrade from "no auth" → "has auth" and never replace an existing auth choice.
func collectSideRepoTargets(workflowDataList []*WorkflowData) []SideRepoTarget {
	maintenanceLog.Printf("Scanning %d workflows for side-repo targets", len(workflowDataList))
	// Use a map to accumulate the best auth seen for each slug.
	// Order slice preserves first-seen repository discovery order for stable output;
	// auth may be upgraded from empty to a non-empty value from later occurrences.
	authByRepo := make(map[string]sideRepoAuth)
	var order []string
	for _, wd := range workflowDataList {
		if wd == nil {
			continue
		}
		for _, checkout := range wd.CheckoutConfigs {
			if !checkout.Current {
				continue
			}
			repo := checkout.Repository
			if repo == "" || strings.Contains(repo, "${{") {
				// Skip empty repositories and expression-based (dynamic) ones.
				continue
			}
			existing, seen := authByRepo[repo]
			if !seen {
				order = append(order, repo)
				authByRepo[repo] = sideRepoAuth{
					token:     checkout.GitHubToken,
					githubApp: checkout.GitHubApp,
				}
			} else if existing.token == "" && existing.githubApp == nil {
				// Upgrade from no-auth to any-auth from a later occurrence.
				if checkout.GitHubToken != "" || checkout.GitHubApp != nil {
					authByRepo[repo] = sideRepoAuth{
						token:     checkout.GitHubToken,
						githubApp: checkout.GitHubApp,
					}
				}
			} else if checkout.GitHubToken != "" || checkout.GitHubApp != nil {
				// A later occurrence provides auth, but an earlier one already set auth.
				// First-seen auth wins; log a notice so users can diagnose unexpected choices.
				maintenanceLog.Printf("Ignoring later auth for %s: first-seen auth (token=%t, app=%t) already recorded",
					repo, existing.token != "", existing.githubApp != nil)
			}
		}
	}
	targets := make([]SideRepoTarget, 0, len(order))
	for _, repo := range order {
		auth := authByRepo[repo]
		targets = append(targets, SideRepoTarget{
			Repository:  repo,
			GitHubToken: auth.token,
			GitHubApp:   auth.githubApp,
		})
	}
	maintenanceLog.Printf("Detected %d side-repo target(s) from checkout configs", len(targets))
	return targets
}

// effectiveSideRepoToken returns the GitHub token expression to use for the
// side-repo maintenance workflow. It prefers the token from the checkout config;
// when a GitHub App is configured it returns the minted token reference; when
// neither is set it falls back to a conventional secret name.
func effectiveSideRepoToken(checkout SideRepoTarget) string {
	if checkout.GitHubToken != "" && checkout.GitHubApp != nil {
		maintenanceLog.Printf("SideRepoTarget %s has both GitHubToken and GitHubApp configured; using explicit GitHubToken", checkout.Repository)
	}
	if checkout.GitHubToken != "" {
		return checkout.GitHubToken
	}
	if checkout.GitHubApp != nil {
		return sideRepoAppTokenRef
	}
	return "${{ secrets.GH_AW_GITHUB_TOKEN }}"
}

// sideRepoAppTokenMintStepYAML generates the YAML snippet for a
// create-github-app-token step to be inserted at the top of each cross-repo
// maintenance job. The step ID is sideRepoAppTokenStepID so the minted token is
// referenced via sideRepoAppTokenRef by subsequent steps in the same job.
func sideRepoAppTokenMintStepYAML(app *GitHubAppConfig, targetRepo string) string {
	var c Compiler
	lines := c.buildGitHubAppTokenMintStepWithMeta(
		app,
		nil, // no additional permission scoping; the app's installation grants determine access
		targetRepo,
		targetRepo,
		"Generate GitHub App token",
		sideRepoAppTokenStepID,
	)
	return strings.Join(lines, "")
}

// generateAllSideRepoMaintenanceWorkflowsOptions configures side-repo maintenance workflow generation.
type generateAllSideRepoMaintenanceWorkflowsOptions struct {
	workflowDataList []*WorkflowData
	workflowDir      string
	version          string
	actionMode       ActionMode
	actionTag        string
	runsOnValue      string
	resolver         SHAResolver
	hasExpires       bool
	minExpiresDays   int
}

// generateAllSideRepoMaintenanceWorkflows detects SideRepoOps targets and
// generates a per-target maintenance workflow for each unique static repository.
func generateAllSideRepoMaintenanceWorkflows(
	ctx context.Context,
	opts generateAllSideRepoMaintenanceWorkflowsOptions,
) error {
	targets := collectSideRepoTargets(opts.workflowDataList)
	maintenanceLog.Printf("Generating maintenance workflows for %d side-repo target(s): hasExpires=%t, minExpiresDays=%d", len(targets), opts.hasExpires, opts.minExpiresDays)
	generatedFiles, err := generateSideRepoMaintenanceFiles(ctx, targets, opts)
	if err != nil {
		return err
	}
	return removeStaleSideRepoMaintenanceFiles(opts.workflowDir, generatedFiles)
}

func generateSideRepoMaintenanceFiles(ctx context.Context, targets []SideRepoTarget, opts generateAllSideRepoMaintenanceWorkflowsOptions) (map[string]struct{}, error) {
	generatedFiles := make(map[string]struct{})
	for _, target := range targets {
		slug := stringutil.SanitizeForFilename(target.Repository)
		filename := "agentics-maintenance-" + slug + ".yml"
		generatedFiles[filename] = struct{}{}
		outPath := filepath.Join(opts.workflowDir, filename)
		maintenanceLog.Printf("Generating side-repo maintenance workflow: %s → %s", target.Repository, filename)
		err := generateSideRepoMaintenanceWorkflow(ctx, generateSideRepoMaintenanceWorkflowOptions{
			target:         target,
			outPath:        outPath,
			version:        opts.version,
			actionMode:     opts.actionMode,
			actionTag:      opts.actionTag,
			runsOnValue:    opts.runsOnValue,
			resolver:       opts.resolver,
			hasExpires:     opts.hasExpires,
			minExpiresDays: opts.minExpiresDays,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to generate side-repo maintenance workflow for %s: %w", target.Repository, err)
		}
		fmt.Fprintf(os.Stderr, "  Generated side-repo maintenance workflow: %s\n", filename)
	}
	return generatedFiles, nil
}

func removeStaleSideRepoMaintenanceFiles(workflowDir string, generatedFiles map[string]struct{}) error {
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return fmt.Errorf("failed to read workflow directory %s for stale side-repo maintenance workflow cleanup: %w", workflowDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !isSideRepoMaintenanceWorkflowFile(entry.Name()) || setutil.Contains(generatedFiles, entry.Name()) {
			continue
		}
		stalePath := filepath.Join(workflowDir, entry.Name())
		maintenanceLog.Printf("Removing stale side-repo maintenance workflow: %s", entry.Name())
		if err := os.Remove(stalePath); err != nil {
			return fmt.Errorf("failed to remove stale side-repo maintenance workflow %s: %w", stalePath, err)
		}
		fmt.Fprintf(os.Stderr, "  Removed stale side-repo maintenance workflow: %s\n", entry.Name())
	}
	return nil
}

func isSideRepoMaintenanceWorkflowFile(name string) bool {
	return strings.HasPrefix(name, "agentics-maintenance-") && strings.HasSuffix(name, ".yml")
}

// generateSideRepoMaintenanceWorkflowOptions configures generation of a single side-repo
// maintenance workflow.
type generateSideRepoMaintenanceWorkflowOptions struct {
	target         SideRepoTarget
	outPath        string
	version        string
	actionMode     ActionMode
	actionTag      string
	runsOnValue    string
	resolver       SHAResolver
	hasExpires     bool
	minExpiresDays int
}

// generateSideRepoMaintenanceWorkflow generates a workflow_call-based maintenance
// workflow that targets an external repository detected via the SideRepoOps pattern.
// The generated workflow mirrors agentics-maintenance.yml but authenticates against
// the target repository using the token from the checkout config and sets
// GH_AW_TARGET_REPO_SLUG for all cross-repo operations.
func generateSideRepoMaintenanceWorkflow(
	ctx context.Context,
	opts generateSideRepoMaintenanceWorkflowOptions,
) error {
	renderCtx := newSideRepoMaintenanceRenderContext(ctx, opts)
	content, err := buildSideRepoMaintenanceWorkflowYAML(renderCtx)
	if err != nil {
		return fmt.Errorf("failed to finalize side-repo maintenance workflow YAML: %w", err)
	}
	maintenanceLog.Printf("Writing side-repo maintenance workflow to %s", renderCtx.outPath)
	if err := os.WriteFile(renderCtx.outPath, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write side-repo maintenance workflow: %w", err)
	}
	return nil
}

type sideRepoMaintenanceRenderContext struct {
	ctx             context.Context
	repoSlug        string
	token           string
	outPath         string
	version         string
	actionMode      ActionMode
	actionTag       string
	runsOnValue     string
	resolver        SHAResolver
	hasExpires      bool
	minExpiresDays  int
	mintStepYAML    string
	setupActionRef  string
	cronSchedule    string
	scheduleDesc    string
	formattedRunsOn string
}

func newSideRepoMaintenanceRenderContext(ctx context.Context, opts generateSideRepoMaintenanceWorkflowOptions) sideRepoMaintenanceRenderContext {
	renderCtx := sideRepoMaintenanceRenderContext{
		ctx:             ctx,
		repoSlug:        opts.target.Repository,
		token:           effectiveSideRepoToken(opts.target),
		outPath:         opts.outPath,
		version:         opts.version,
		actionMode:      opts.actionMode,
		actionTag:       opts.actionTag,
		runsOnValue:     opts.runsOnValue,
		resolver:        opts.resolver,
		hasExpires:      opts.hasExpires,
		minExpiresDays:  opts.minExpiresDays,
		setupActionRef:  ResolveSetupActionReference(ctx, opts.actionMode, opts.version, opts.actionTag, opts.resolver),
		formattedRunsOn: FormatRunsOn(nil, "ubuntu-latest"),
	}
	maintenanceLog.Printf("Building side-repo workflow content: repo=%s, actionMode=%s, hasExpires=%t", renderCtx.repoSlug, renderCtx.actionMode, renderCtx.hasExpires)
	if opts.target.GitHubApp != nil && opts.target.GitHubToken == "" {
		renderCtx.mintStepYAML = sideRepoAppTokenMintStepYAML(opts.target.GitHubApp, opts.target.Repository)
		maintenanceLog.Printf("GitHub App auth configured for %s; will emit mint step in cross-repo jobs", renderCtx.repoSlug)
	} else if opts.target.GitHubApp != nil {
		maintenanceLog.Printf("SideRepoTarget %s has both GitHubToken and GitHubApp configured; skipping app token mint step", renderCtx.repoSlug)
	}
	if renderCtx.hasExpires {
		effectiveDays := renderCtx.minExpiresDays
		if effectiveDays == 0 {
			effectiveDays = 5
		}
		renderCtx.cronSchedule, renderCtx.scheduleDesc = generateSideRepoMaintenanceCron(renderCtx.repoSlug, effectiveDays)
	}
	return renderCtx
}

func buildSideRepoMaintenanceWorkflowYAML(renderCtx sideRepoMaintenanceRenderContext) (string, error) {
	var yaml strings.Builder
	yaml.WriteString(buildSideRepoMaintenanceHeader(renderCtx.repoSlug))
	yaml.WriteString(buildSideRepoMaintenanceOnSection(renderCtx))
	if renderCtx.hasExpires {
		maintenanceLog.Printf("Including close-expired-entities job for %s (cron=%s)", renderCtx.repoSlug, renderCtx.cronSchedule)
		yaml.WriteString(buildCloseExpiredEntitiesJob(renderCtx))
	}
	yaml.WriteString(buildApplySafeOutputsJob(renderCtx))
	yaml.WriteString(buildCreateLabelsJob(renderCtx))
	yaml.WriteString(buildActivityReportJob(renderCtx))
	yaml.WriteString(buildValidateWorkflowsJob(renderCtx))
	finalYAML, err := finalizeRunnerTempSafety(yaml.String())
	if err != nil {
		return "", fmt.Errorf("runner temp safety: %w", err)
	}
	return finalYAML, nil
}

func buildSideRepoMaintenanceHeader(repoSlug string) string {
	customInstructions := strings.ReplaceAll(sideRepoMaintenanceHeaderTemplate, "{REPO_SLUG}", repoSlug)
	return GenerateWorkflowHeader("", "pkg/workflow/side_repo_maintenance.go", customInstructions)
}

func buildSideRepoMaintenanceOnSection(renderCtx sideRepoMaintenanceRenderContext) string {
	onSection := `name: Agentic Maintenance (` + renderCtx.repoSlug + `)

on:
  workflow_dispatch:
    inputs:
      operation:
        description: 'Optional maintenance operation to run'
        required: false
        type: choice
        default: '` + maintenanceNoOperationValue + `'
        options:
          - '` + maintenanceNoOperationValue + `'
          - 'safe_outputs'
          - 'create_labels'
          - 'activity_report'
          - 'validate'
      run_url:
        description: 'Run URL or run ID to replay safe outputs from (e.g. https://github.com/owner/repo/actions/runs/12345 or 12345). Required when operation is safe_outputs.'
        required: false
        type: string
        default: ''
  workflow_call:
    inputs:
      operation:
        description: 'Optional maintenance operation to run (safe_outputs, create_labels, activity_report, validate)'
        required: false
        type: string
        default: ''
      run_url:
        description: 'Run URL or run ID to replay safe outputs from (e.g. https://github.com/owner/repo/actions/runs/12345 or 12345). Required when operation is safe_outputs.'
        required: false
        type: string
        default: ''
    outputs:
      applied_run_url:
        description: 'The run URL that safe outputs were applied from'
        value: ${{ jobs.apply_safe_outputs.outputs.run_url }}
`
	if renderCtx.hasExpires {
		onSection += `  schedule:
    - cron: "` + renderCtx.cronSchedule + `"  # ` + renderCtx.scheduleDesc + ` (based on minimum expires: ` + strconv.Itoa(renderCtx.minExpiresDays) + ` days)
`
	}
	onSection += `
permissions: {}

jobs:
`
	return onSection
}

func buildCloseExpiredEntitiesJob(renderCtx sideRepoMaintenanceRenderContext) string {
	closeExpiredCondition := buildNotForkAndScheduled()
	var b strings.Builder
	b.WriteString(`  close-expired-entities:
    if: ${{ ` + RenderCondition(closeExpiredCondition) + ` }}
    runs-on: ` + renderCtx.runsOnValue + `
    permissions:
      discussions: write
      issues: write
      pull-requests: write
    # Runs on schedule: ` + renderCtx.cronSchedule + ` (` + renderCtx.scheduleDesc + `)
    steps:
`)
	b.WriteString(renderCtx.mintStepYAML)
	b.WriteString(buildSideRepoCheckoutActionsStep(renderCtx.actionMode))
	b.WriteString(buildSideRepoSetupScriptsStep(renderCtx.setupActionRef))
	b.WriteString(buildSideRepoTargetScriptStep("Close expired discussions", "close_expired_discussions.cjs", renderCtx))
	b.WriteString(buildSideRepoTargetScriptStep("Close expired issues", "close_expired_issues.cjs", renderCtx))
	b.WriteString(buildSideRepoTargetScriptStep("Close expired pull requests", "close_expired_pull_requests.cjs", renderCtx))
	return b.String()
}

func buildApplySafeOutputsJob(renderCtx sideRepoMaintenanceRenderContext) string {
	var b strings.Builder
	b.WriteString(`
  apply_safe_outputs:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("safe_outputs")) + ` }}
    runs-on: ` + renderCtx.runsOnValue + `
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
	b.WriteString(renderCtx.mintStepYAML)
	b.WriteString(buildSideRepoCheckoutActionsStep(renderCtx.actionMode))
	b.WriteString(buildSideRepoSetupScriptsStep(renderCtx.setupActionRef))
	b.WriteString(buildSideRepoCheckAdminPermissionsStep(renderCtx.resolver))
	b.WriteString(buildApplySafeOutputsStep(renderCtx))
	b.WriteString(buildApplySafeOutputsRecordStep())
	return b.String()
}

func buildCreateLabelsJob(renderCtx sideRepoMaintenanceRenderContext) string {
	var b strings.Builder
	b.WriteString(`
  create_labels:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("create_labels")) + ` }}
    runs-on: ` + renderCtx.runsOnValue + `
    permissions:
      contents: read
      issues: write
    steps:
`)
	b.WriteString(renderCtx.mintStepYAML)
	b.WriteString(buildSideRepoRepositoryCheckoutStep())
	b.WriteString(buildSideRepoSetupScriptsStep(renderCtx.setupActionRef))
	b.WriteString(buildSideRepoCheckAdminPermissionsStep(renderCtx.resolver))
	b.WriteString(generateInstallCLISteps(renderCtx.ctx, renderCtx.actionMode, renderCtx.version, renderCtx.actionTag, renderCtx.resolver))
	b.WriteString(buildCreateLabelsStep(renderCtx))
	return b.String()
}

func buildActivityReportJob(renderCtx sideRepoMaintenanceRenderContext) string {
	var b strings.Builder
	b.WriteString(`
  activity_report:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("activity_report")) + ` }}
    runs-on: ` + renderCtx.runsOnValue + `
    timeout-minutes: 120
    permissions:
      actions: read
      contents: read
      issues: write
    steps:
`)
	b.WriteString(renderCtx.mintStepYAML)
	b.WriteString(buildSideRepoRepositoryCheckoutStep())
	b.WriteString(buildSideRepoSetupScriptsStep(renderCtx.setupActionRef))
	b.WriteString(buildSideRepoCheckAdminPermissionsStep(renderCtx.resolver))
	b.WriteString(generateInstallCLISteps(renderCtx.ctx, renderCtx.actionMode, renderCtx.version, renderCtx.actionTag, renderCtx.resolver))
	b.WriteString(buildActivityReportCacheSteps(renderCtx))
	b.WriteString(buildActivityReportIssueStep(renderCtx))
	return b.String()
}

func buildValidateWorkflowsJob(renderCtx sideRepoMaintenanceRenderContext) string {
	var b strings.Builder
	b.WriteString(`
  validate_workflows:
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("validate")) + ` }}
    runs-on: ` + renderCtx.formattedRunsOn + `
    permissions:
      contents: read
      issues: write
    steps:
`)
	b.WriteString(buildSideRepoRepositoryCheckoutStep())
	b.WriteString(buildSideRepoSetupScriptsStep(renderCtx.setupActionRef))
	b.WriteString(buildSideRepoCheckAdminPermissionsStep(renderCtx.resolver))
	b.WriteString(generateInstallCLISteps(renderCtx.ctx, renderCtx.actionMode, renderCtx.version, renderCtx.actionTag, renderCtx.resolver))
	b.WriteString(buildValidateWorkflowsStep(renderCtx))
	return b.String()
}

func buildSideRepoCheckoutActionsStep(actionMode ActionMode) string {
	if actionMode != ActionModeDev && actionMode != ActionModeScript {
		return ""
	}
	return `      - name: Checkout actions folder
        uses: ` + getActionPin("actions/checkout") + `
        with:
          sparse-checkout: |
            actions
          clean: false
          persist-credentials: false

`
}

func buildSideRepoRepositoryCheckoutStep() string {
	return `      - name: Checkout repository
        uses: ` + getActionPin("actions/checkout") + `
        with:
          persist-credentials: false

`
}

func buildSideRepoSetupScriptsStep(setupActionRef string) string {
	return `      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: ${{ runner.temp }}/gh-aw/actions

`
}

func buildSideRepoCheckAdminPermissionsStep(resolver SHAResolver) string {
	return `      - name: Check admin/maintainer permissions
        uses: ` + getCachedActionPinFromResolver("actions/github-script", resolver) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/check_team_member.cjs');
            await main();

`
}

func buildSideRepoTargetScriptStep(name, scriptFile string, renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: ` + name + `
        uses: ` + getCachedActionPinFromResolver("actions/github-script", renderCtx.resolver) + `
        env:
          GH_AW_TARGET_REPO_SLUG: "` + renderCtx.repoSlug + `"
        with:
          github-token: ` + renderCtx.token + `
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/` + scriptFile + `');
            await main();

`
}

func buildApplySafeOutputsStep(renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: Apply Safe Outputs
        uses: ` + getCachedActionPinFromResolver("actions/github-script", renderCtx.resolver) + `
        env:
          GH_TOKEN: ` + renderCtx.token + `
          GH_AW_RUN_URL: ${{ inputs.run_url }}
          GH_AW_TARGET_REPO_SLUG: "` + renderCtx.repoSlug + `"
        with:
          github-token: ` + renderCtx.token + `
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/apply_safe_outputs_replay.cjs');
            await main();

`
}

func buildApplySafeOutputsRecordStep() string {
	return `      - name: Record outputs
        id: record
        env:
          GH_AW_RUN_URL: ${{ inputs.run_url }}
        run: echo "run_url=$GH_AW_RUN_URL" >> "$GITHUB_OUTPUT"
`
}

func buildCreateLabelsStep(renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: Create missing labels in target repository
        uses: ` + getCachedActionPinFromResolver("actions/github-script", renderCtx.resolver) + `
        env:
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(renderCtx.actionMode) + `
          GH_AW_TARGET_REPO_SLUG: "` + renderCtx.repoSlug + `"
        with:
          github-token: ` + renderCtx.token + `
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/create_labels.cjs');
            await main();
`
}

func buildActivityReportCacheSteps(renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: Restore activity report logs cache
        id: activity_report_logs_cache
        uses: ` + getActionPin("actions/cache/restore") + `
        with:
          path: ./.cache/gh-aw/activity-report-logs
          key: ${{ runner.os }}-activity-report-logs-` + renderCtx.repoSlug + `-${{ github.ref_name }}-${{ github.run_id }}
          restore-keys: |
            ${{ runner.os }}-activity-report-logs-` + renderCtx.repoSlug + `-
            ${{ runner.os }}-activity-report-logs-
      - name: Download activity report logs in target repository
        timeout-minutes: 20
        shell: bash
        env:
          GH_TOKEN: ` + renderCtx.token + `
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(renderCtx.actionMode) + `
          GH_AW_TARGET_REPO_SLUG: "` + renderCtx.repoSlug + `"
        run: |
          ${GH_AW_CMD_PREFIX} logs \
            --repo "` + renderCtx.repoSlug + `" \
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

func buildActivityReportIssueStep(renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: Generate activity report issue in target repository
        uses: ` + getCachedActionPinFromResolver("actions/github-script", renderCtx.resolver) + `
        with:
          github-token: ` + renderCtx.token + `
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
            const repoSlug = process.env.GH_AW_TARGET_REPO_SLUG || '';
            const [owner, repo] = repoSlug.split('/');
            if (!owner || !repo) {
              core.setFailed('Invalid GH_AW_TARGET_REPO_SLUG: ' + repoSlug);
              return;
            }
            const body = [
              '### Agentic workflow activity report',
              '',
              'Repository: ' + repoSlug,
              'Generated at: ' + new Date().toISOString(),
              '',
              reportBody,
            ].join('\n');
            const createdIssue = await github.rest.issues.create({
              owner,
              repo,
              title: '[aw] agentic status report',
              body,
              labels: ['agentic-workflows'],
            });
            core.info('Created issue #' + createdIssue.data.number + ': ' + createdIssue.data.html_url);
`
}

func buildValidateWorkflowsStep(renderCtx sideRepoMaintenanceRenderContext) string {
	return `      - name: Validate workflows and file issue on findings
        uses: ` + getCachedActionPinFromResolver("actions/github-script", renderCtx.resolver) + `
        env:
          GH_AW_CMD_PREFIX: ` + getCLICmdPrefix(renderCtx.actionMode) + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/run_validate_workflows.cjs');
            await main();
`
}
