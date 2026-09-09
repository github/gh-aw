package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var maintenanceLog = logger.New("workflow:maintenance_workflow")
var actionFailureIssueExpiryLineRegex = regexp.MustCompile(`(?m)^(\s*GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS:\s*")[1-9]\d*(")\s*$`)

// generateInstallCLISteps generates YAML steps to install or build the gh-aw CLI.
// In dev mode: builds from source using Setup Go + Build gh-aw (./gh-aw binary available)
// In release mode: installs the released CLI via the setup-cli action (gh aw available)
// In action mode: installs the released CLI via the gh-aw-actions/setup-cli action (gh aw available)
// When resolver is non-nil, attempts to resolve the setup-cli action to a SHA-pinned reference.
func generateInstallCLISteps(ctx context.Context, actionMode ActionMode, version string, actionTag string, resolver SHAResolver) string {
	if actionMode == ActionModeDev {
		return `      - name: Setup Go
        uses: ` + getActionPin("actions/setup-go") + `
        with:
          go-version-file: go.mod
          cache: true

      - name: Build gh-aw
        run: make build

`
	}

	cliTag := actionTag
	if cliTag == "" {
		cliTag = version
	}

	// Action mode: use setup-cli action from external gh-aw-actions repository
	if actionMode == ActionModeAction {
		actionRepo := GitHubActionsOrgRepo + "/setup-cli"
		ref := resolveActionRef(ctx, actionRepo, cliTag, resolver)
		return `      - name: Install gh-aw
        uses: ` + ref + `
        with:
          version: ` + cliTag + `

`
	}

	// Release mode: use setup-cli action from external gh-aw-actions repository
	actionRepo := GitHubActionsOrgRepo + "/setup-cli"
	ref := resolveActionRef(ctx, actionRepo, cliTag, resolver)
	return `      - name: Install gh-aw
        uses: ` + ref + `
        with:
          version: ` + cliTag + `

`
}

// resolveActionRef attempts to resolve an action repo@tag to a SHA-pinned reference
// using the provided resolver. If the resolver is nil or resolution fails, it returns
// the tag-based reference (repo@tag).
func resolveActionRef(ctx context.Context, actionRepo, tag string, resolver SHAResolver) string {
	if resolver != nil && tag != "" && tag != "dev" {
		sha, err := resolver.ResolveSHA(ctx, actionRepo, tag)
		if err != nil {
			maintenanceLog.Printf("Failed to resolve SHA for %s@%s: %v, falling back to tag reference", actionRepo, tag, err)
		} else if sha != "" {
			return formatActionReference(actionRepo, sha, tag)
		}
	}
	return actionRepo + "@" + tag
}

// getCLICmdPrefix returns the CLI command prefix based on action mode.
// In dev mode: "./gh-aw" (local binary built from source)
// In release mode: "gh aw" (installed via gh extension)
func getCLICmdPrefix(actionMode ActionMode) string {
	if actionMode == ActionModeDev {
		return "./gh-aw"
	}
	return "gh aw"
}

// FetchDefaultBranch queries the GitHub API to determine the default branch of the
// given repository slug (owner/repo). Returns "main" as a fallback when the slug is
// empty, not in owner/repo format, or when the API call fails.
func FetchDefaultBranch(slug string) string {
	const fallback = "main"
	if slug == "" || strings.Count(slug, "/") != 1 {
		maintenanceLog.Printf("No valid repository slug, using default branch fallback: %s", fallback)
		return fallback
	}
	maintenanceLog.Printf("Fetching default branch for repository: %s", slug)
	output, err := RunGH("Fetching default branch...", "api", "/repos/"+slug, "--jq", ".default_branch")
	if err != nil {
		maintenanceLog.Printf("Failed to fetch default branch for %s: %v, falling back to %s", slug, err, fallback)
		return fallback
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		maintenanceLog.Printf("Empty default branch response for %s, falling back to %s", slug, fallback)
		return fallback
	}
	maintenanceLog.Printf("Default branch for %s: %s", slug, branch)
	return branch
}

// GenerateMaintenanceWorkflowOptions configures a maintenance workflow generation run.
type GenerateMaintenanceWorkflowOptions struct {
	WorkflowDataList []*WorkflowData
	WorkflowDir      string
	Version          string
	ActionMode       ActionMode
	ActionTag        string
	RepoConfig       *RepoConfig
	RepoSlug         string
}

const defaultNoOpIssueExpirationHours = 24 * 30

func isNoOpReportAsIssueEnabled(reportAsIssue *string) bool {
	return reportAsIssue == nil || !strings.EqualFold(strings.TrimSpace(*reportAsIssue), "false")
}

// GenerateMaintenanceWorkflow generates the agentics-maintenance.yml workflow
// if any workflows use expiring safe outputs or noop issue reporting.
// When opts.RepoConfig is non-nil and opts.RepoConfig.MaintenanceDisabled is true the
// maintenance workflow is deleted and the function returns immediately.
// opts.RepoSlug is the owner/repo slug used to determine the default branch for the push
// trigger; pass an empty string to fall back to "main".
func GenerateMaintenanceWorkflow(ctx context.Context, opts GenerateMaintenanceWorkflowOptions) error { //nolint:largefunc // Existing workflow orchestration remains centralized.
	workflowDataList := opts.WorkflowDataList
	workflowDir := opts.WorkflowDir
	version := opts.Version
	actionMode := opts.ActionMode
	actionTag := opts.ActionTag
	repoConfig := opts.RepoConfig
	repoSlug := opts.RepoSlug
	maintenanceLog.Print("Checking if maintenance workflow is needed")

	// Compute the resolver and setup action reference early — needed in all code
	// paths including the maintenance-disabled early-exit path.
	var resolver SHAResolver
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.ActionResolver != nil {
			resolver = workflowData.ActionResolver
			break
		}
	}
	setupActionRef := ResolveSetupActionReference(ctx, actionMode, version, actionTag, resolver)
	githubScriptPin := getCachedActionPinFromResolver("actions/github-script", resolver)

	// Respect explicit opt-out from aw.json: maintenance: false
	if repoConfig != nil && repoConfig.MaintenanceDisabled {
		if err := handleMaintenanceDisabled(workflowDataList, workflowDir); err != nil {
			return err
		}
		return GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
			Context:         ctx,
			WorkflowDir:     workflowDir,
			Enabled:         repoConfig.IsAutoUpgradeEnabled(),
			RepoSlug:        repoSlug,
			SetupActionRef:  setupActionRef,
			GitHubScriptPin: githubScriptPin,
			ActionMode:      actionMode,
			Version:         version,
			ActionTag:       actionTag,
			Resolver:        resolver,
			CustomCron:      autoUpgradeCronFrom(repoConfig),
			UpgradeOptions:  autoUpgradeOptionsFrom(repoConfig),
		})
	}

	// Determine the runs-on value to use for all maintenance jobs.
	const defaultRunsOn = "ubuntu-slim"
	var configuredRunsOn RunsOnValue
	disableLabelTrigger := true // default: disable label-triggered jobs (opt-in)
	var maintenanceConfig *MaintenanceConfig
	var compileGitHubTokenSecret string
	enableCompileCreatePullRequest := false
	if repoConfig != nil && repoConfig.Maintenance != nil {
		maintenanceConfig = repoConfig.Maintenance
		configuredRunsOn = maintenanceConfig.RunsOn
		disableLabelTrigger = !maintenanceConfig.IsLabelTriggerEnabled()
		if maintenanceConfig.Compile != nil {
			compileGitHubTokenSecret = maintenanceConfig.Compile.CreatePullRequestGitHubToken
			enableCompileCreatePullRequest = strings.TrimSpace(compileGitHubTokenSecret) != ""
		}
	}
	runsOnValue := FormatRunsOn(configuredRunsOn, defaultRunsOn)

	// Scan workflows for expires fields and track the minimum expires value
	hasExpires, minExpires, triggerReason := scanWorkflowsForExpires(workflowDataList, repoConfig)

	if !hasExpires {
		maintenanceLog.Print("No workflows use expires field, skipping maintenance workflow generation")

		// No maintenance workflow means no scheduled close-expired-issues consumer.
		// Since the implicit 168-hour action-failure default was not an opt-in
		// (see scanWorkflowsForExpires), disable the runtime expiration marker in
		// the already-compiled lock files so failure issues do not claim an
		// expiration that nothing will enforce.
		disableDefaultActionFailureExpiryMarkers(workflowDataList, workflowDir)

		// Delete existing maintenance workflow file if it exists (no expires means no need for maintenance)
		maintenanceFile := filepath.Join(workflowDir, "agentics-maintenance.yml")
		if _, err := os.Stat(maintenanceFile); err == nil {
			maintenanceLog.Printf("Deleting existing maintenance workflow: %s", maintenanceFile)
			if err := os.Remove(maintenanceFile); err != nil {
				return fmt.Errorf("failed to delete maintenance workflow: %w", err)
			}
			maintenanceLog.Print("Maintenance workflow deleted successfully")
		}

		// Even without expires, side-repo targets still need maintenance workflows
		// for safe_outputs, create_labels, and validate operations.
		if err := generateAllSideRepoMaintenanceWorkflows(ctx, generateAllSideRepoMaintenanceWorkflowsOptions{
			workflowDataList: workflowDataList,
			workflowDir:      workflowDir,
			version:          version,
			actionMode:       actionMode,
			actionTag:        actionTag,
			runsOnValue:      runsOnValue,
			resolver:         resolver,
			hasExpires:       false,
			minExpiresDays:   0,
		}); err != nil {
			return err
		}

		return GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
			Context:         ctx,
			WorkflowDir:     workflowDir,
			Enabled:         repoConfig != nil && repoConfig.IsAutoUpgradeEnabled(),
			RepoSlug:        repoSlug,
			SetupActionRef:  setupActionRef,
			GitHubScriptPin: githubScriptPin,
			ActionMode:      actionMode,
			Version:         version,
			ActionTag:       actionTag,
			Resolver:        resolver,
			CustomCron:      autoUpgradeCronFrom(repoConfig),
			UpgradeOptions:  autoUpgradeOptionsFrom(repoConfig),
		})
	}

	maintenanceLog.Printf("Maintenance workflow generation triggered: %s", triggerReason)
	maintenanceLog.Printf("Generating maintenance workflow for expired discussions, issues, and pull requests (minimum expires: %d hours)", minExpires)

	// Convert hours to days for cron schedule generation
	minExpiresDays := minExpires / 24
	if minExpires%24 > 0 {
		minExpiresDays++ // Round up partial days
	}

	// Generate cron schedule based on minimum expires value
	cronSchedule, scheduleDesc := generateMaintenanceCron(minExpiresDays)
	maintenanceLog.Printf("Maintenance schedule: %s (%s)", cronSchedule, scheduleDesc)

	// Fetch the default branch for the push trigger (dev mode only)
	// Resolved here to avoid passing it through multiple layers; empty slug falls back to "main"
	defaultBranch := FetchDefaultBranch(repoSlug)

	// Generate the YAML content for the maintenance workflow
	maintenanceLog.Printf(
		"Maintenance compile configuration: createPullRequest=%v tokenSecretConfigured=%v",
		enableCompileCreatePullRequest,
		strings.TrimSpace(compileGitHubTokenSecret) != "",
	)
	copilotOrgBilling := allCopilotWorkflowsUseOrgBilling(workflowDataList)
	content, err := buildMaintenanceWorkflowYAML(ctx, buildMaintenanceWorkflowYAMLOptions{
		cronSchedule:        cronSchedule,
		scheduleDesc:        scheduleDesc,
		minExpiresDays:      minExpiresDays,
		runsOnValue:         runsOnValue,
		actionMode:          actionMode,
		version:             version,
		actionTag:           actionTag,
		resolver:            resolver,
		configuredRunsOn:    configuredRunsOn,
		defaultBranch:       defaultBranch,
		disableLabelTrigger: disableLabelTrigger,
		maintenanceConfig:   maintenanceConfig,
		compileGitHubToken:  getEffectiveMaintenanceGitHubToken(compileGitHubTokenSecret),
		createCompilePR:     enableCompileCreatePullRequest,
		copilotOrgBilling:   copilotOrgBilling,
	})
	if err != nil {
		return fmt.Errorf("failed to finalize maintenance workflow YAML: %w", err)
	}

	// Write the maintenance workflow file
	maintenanceFile := filepath.Join(workflowDir, "agentics-maintenance.yml")
	maintenanceLog.Printf("Writing maintenance workflow to %s", maintenanceFile)

	if err := fileutil.EnsureParentDir(maintenanceFile, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create maintenance workflow directory: %w", err)
	}
	if err := os.WriteFile(maintenanceFile, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write maintenance workflow: %w", err)
	}

	maintenanceLog.Print("Maintenance workflow generated successfully")

	// Generate side-repo maintenance workflows for any SideRepoOps targets detected.
	if err := generateAllSideRepoMaintenanceWorkflows(ctx, generateAllSideRepoMaintenanceWorkflowsOptions{
		workflowDataList: workflowDataList,
		workflowDir:      workflowDir,
		version:          version,
		actionMode:       actionMode,
		actionTag:        actionTag,
		runsOnValue:      runsOnValue,
		resolver:         resolver,
		hasExpires:       hasExpires,
		minExpiresDays:   minExpiresDays,
	}); err != nil {
		return err
	}

	return GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		Context:         ctx,
		WorkflowDir:     workflowDir,
		Enabled:         repoConfig != nil && repoConfig.IsAutoUpgradeEnabled(),
		RepoSlug:        repoSlug,
		SetupActionRef:  setupActionRef,
		GitHubScriptPin: githubScriptPin,
		ActionMode:      actionMode,
		Version:         version,
		ActionTag:       actionTag,
		Resolver:        resolver,
		CustomCron:      autoUpgradeCronFrom(repoConfig),
		UpgradeOptions:  autoUpgradeOptionsFrom(repoConfig),
	})
}

// autoUpgradeCronFrom returns the custom cron expression from the repo config,
// or an empty string if the config is nil or no custom cron is set.
func autoUpgradeCronFrom(cfg *RepoConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.AutoUpgradeCron
}

func autoUpgradeOptionsFrom(cfg *RepoConfig) []string {
	if cfg == nil {
		return nil
	}
	return cfg.AutoUpgradeOptions
}

// handleMaintenanceDisabled handles the case where maintenance is disabled in repo config.
// It warns about workflows that use expires and deletes any existing maintenance workflow.
func handleMaintenanceDisabled(workflowDataList []*WorkflowData, workflowDir string) error {
	maintenanceLog.Print("Maintenance disabled via repo config, skipping generation")

	// Explicit opt-out means no scheduled close-expired-issues consumer will
	// exist regardless of any action_failure_issue_expires configuration, so
	// disable the runtime expiration marker just like the no-recognized-source case.
	disableDefaultActionFailureExpiryMarkers(workflowDataList, workflowDir)

	// Warn if any workflow uses expires — those features rely on maintenance
	// and will silently become no-ops when it is disabled.
	for _, workflowData := range workflowDataList {
		if workflowData == nil || workflowData.SafeOutputs == nil {
			continue
		}
		usesExpires := (workflowData.SafeOutputs.CreateDiscussions != nil && workflowData.SafeOutputs.CreateDiscussions.Expires > 0) ||
			(workflowData.SafeOutputs.CreateIssues != nil && workflowData.SafeOutputs.CreateIssues.Expires > 0) ||
			(workflowData.SafeOutputs.CreatePullRequests != nil && workflowData.SafeOutputs.CreatePullRequests.Expires > 0)
		if usesExpires {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Workflow '%s' uses the 'expires' field but maintenance is disabled in aw.json. "+
					"Expiration will not run until maintenance is re-enabled.", workflowData.Name)))
		}
	}

	maintenanceFile := filepath.Join(workflowDir, "agentics-maintenance.yml")
	if _, err := os.Stat(maintenanceFile); err == nil {
		maintenanceLog.Printf("Deleting existing maintenance workflow: %s", maintenanceFile)
		if err := os.Remove(maintenanceFile); err != nil {
			return fmt.Errorf("failed to delete maintenance workflow: %w", err)
		}
	}
	return nil
}

// allCopilotWorkflowsUseOrgBilling reports whether all Copilot-engine workflows
// in the list have copilot-requests: write set. This indicates org billing mode,
// where the GITHUB_TOKEN is used for Copilot authentication and the
// COPILOT_GITHUB_TOKEN secret is not required.
// Returns false if no Copilot workflows are found (billing mode is indeterminate)
// or if any Copilot workflow does not have copilot-requests: write set.
func allCopilotWorkflowsUseOrgBilling(workflowDataList []*WorkflowData) bool {
	copilotCount := 0
	for _, data := range workflowDataList {
		if data == nil {
			continue
		}
		engineID := ResolveEngineID(data)
		// Default engine (empty string) is Copilot, as is an explicit "copilot" ID.
		if engineID != "" && engineID != string(constants.CopilotEngine) {
			continue
		}
		copilotCount++
		if !hasCopilotRequestsWritePermission(data) {
			return false
		}
	}
	return copilotCount > 0
}

// scanWorkflowsForExpires checks all workflow data for expires fields and returns
// whether any expires fields are set, the minimum expires value in hours, and the
// first reason that triggered maintenance workflow generation.
//
// repoConfig may be nil. When maintenance.action_failure_issue_expires is
// explicitly configured in aw.json, it is treated as an opt-in trigger for
// maintenance workflow generation (see IsActionFailureIssueExpiresExplicit).
// The implicit 168-hour default is intentionally excluded from this scan:
// since safe-outputs.report-failure-as-issue defaults to true, treating the
// implicit default as an always-on trigger would force agentics-maintenance.yml
// into essentially every repository with a gh-aw workflow.
func scanWorkflowsForExpires(workflowDataList []*WorkflowData, repoConfig *RepoConfig) (bool, int, string) { //nolint:largefunc // Existing expiration scan remains centralized.
	hasExpires := false
	minExpires := 0 // Track minimum expires value in hours
	triggerReason := ""

	setTriggerReason := func(reason string) {
		if triggerReason == "" {
			triggerReason = reason
			maintenanceLog.Printf("Maintenance workflow became required: %s", reason)
		}
	}

	for _, workflowData := range workflowDataList {
		if workflowData == nil || workflowData.SafeOutputs == nil {
			continue
		}
		// Check for expired discussions
		if workflowData.SafeOutputs.CreateDiscussions != nil {
			if workflowData.SafeOutputs.CreateDiscussions.Expires > 0 {
				hasExpires = true
				expires := workflowData.SafeOutputs.CreateDiscussions.Expires
				setTriggerReason(fmt.Sprintf("workflow %q sets safe_outputs.create_discussions.expires=%dh", workflowData.Name, expires))
				maintenanceLog.Printf("Workflow %s has expires field set to %d hours for discussions", workflowData.Name, expires)
				if minExpires == 0 || expires < minExpires {
					minExpires = expires
				}
			}
		}
		// Check for expired issues
		if workflowData.SafeOutputs.CreateIssues != nil {
			if workflowData.SafeOutputs.CreateIssues.Expires > 0 {
				hasExpires = true
				expires := workflowData.SafeOutputs.CreateIssues.Expires
				setTriggerReason(fmt.Sprintf("workflow %q sets safe_outputs.create_issues.expires=%dh", workflowData.Name, expires))
				maintenanceLog.Printf("Workflow %s has expires field set to %d hours for issues", workflowData.Name, expires)
				if minExpires == 0 || expires < minExpires {
					minExpires = expires
				}
			}
		}
		// Check for expired pull requests
		if workflowData.SafeOutputs.CreatePullRequests != nil {
			if workflowData.SafeOutputs.CreatePullRequests.Expires > 0 {
				hasExpires = true
				expires := workflowData.SafeOutputs.CreatePullRequests.Expires
				setTriggerReason(fmt.Sprintf("workflow %q sets safe_outputs.create_pull_requests.expires=%dh", workflowData.Name, expires))
				maintenanceLog.Printf("Workflow %s has expires field set to %d hours for pull requests", workflowData.Name, expires)
				if minExpires == 0 || expires < minExpires {
					minExpires = expires
				}
			}
		}
		// Check for no-op runs issue expiration (runtime defaults to 30 days).
		// The implicit noop fallback now defaults report-as-issue to false (see
		// safe_outputs_config_extraction.go), so it creates no issues and never
		// needs a maintenance workflow. The Implicit check below is kept as a
		// defense-in-depth guard in case that default ever changes.
		if workflowData.SafeOutputs.NoOp != nil && !workflowData.SafeOutputs.NoOp.Implicit {
			if isNoOpReportAsIssueEnabled(workflowData.SafeOutputs.NoOp.ReportAsIssue) {
				hasExpires = true
				expires := defaultNoOpIssueExpirationHours
				setTriggerReason(fmt.Sprintf("workflow %q enables no-op issue reporting (default expiration %dh)", workflowData.Name, expires))
				maintenanceLog.Printf("Workflow %s has no-op report-as-issue enabled, using %d-hour no-op issue expiration", workflowData.Name, expires)
				if minExpires == 0 || expires < minExpires {
					minExpires = expires
				}
			}
		}
	}

	// Check for an explicitly configured action-failure issue expiry. Unlike the
	// implicit 168-hour default, an explicit value is an opt-in and should both
	// trigger maintenance workflow generation and participate in the minimum
	// expires calculation, but only when some workflow could actually create
	// action-failure issues (report-failure-as-issue defaults to true).
	if repoConfig.IsActionFailureIssueExpiresExplicit() && anyWorkflowMayReportFailureAsIssue(workflowDataList) {
		hasExpires = true
		expires := repoConfig.ActionFailureIssueExpiresHours()
		setTriggerReason(fmt.Sprintf("maintenance.action_failure_issue_expires=%dh is explicitly configured in %s", expires, RepoConfigFileName))
		maintenanceLog.Printf("Repo config explicitly sets action_failure_issue_expires to %d hours", expires)
		if minExpires == 0 || expires < minExpires {
			minExpires = expires
		}
	}

	return hasExpires, minExpires, triggerReason
}

// disableDefaultActionFailureExpiryMarkers rewrites the already-compiled lock
// files for workflowDataList so that the implicit 168-hour action-failure
// expiration marker is disabled (set to "0", meaning "no expiration"). This is
// called only when scanWorkflowsForExpires determined that no maintenance
// workflow will be generated, so the implicit default (which is not an
// opt-in) must not be advertised in failure issues since nothing would enforce
// it. Workflows that explicitly configured maintenance.action_failure_issue_expires
// are never affected by this function, because an explicit configuration
// always makes scanWorkflowsForExpires report hasExpires=true.
func disableDefaultActionFailureExpiryMarkers(workflowDataList []*WorkflowData, workflowDir string) {
	var lockFiles []string
	for _, workflowData := range workflowDataList {
		if workflowData == nil || workflowData.WorkflowID == "" {
			continue
		}
		lockFiles = append(lockFiles, filepath.Join(workflowDir, workflowData.WorkflowID+".lock.yml"))
	}
	patchActionFailureExpiryMarkersInFiles(lockFiles)
}

// patchActionFailureExpiryMarkersInFiles disables the implicit action-failure
// expiry marker (see actionFailureIssueExpiryLineRegex) in each of the given
// lock files, in place. Missing files (e.g. from --no-emit compiles) are
// skipped without error.
func patchActionFailureExpiryMarkersInFiles(lockFiles []string) {
	for _, lockFile := range lockFiles {
		if lockFile == "" {
			continue
		}
		content, err := os.ReadFile(lockFile)
		if err != nil {
			// Lock file may not exist (e.g. --no-emit compiles); nothing to patch.
			continue
		}
		updated := actionFailureIssueExpiryLineRegex.ReplaceAllString(string(content), `${1}0${2}`)
		if updated == string(content) {
			continue
		}
		if err := os.WriteFile(lockFile, []byte(updated), constants.FilePermPublic); err != nil {
			maintenanceLog.Printf("Warning: failed to disable action-failure expiry marker in %s: %v", lockFile, err)
			continue
		}
		maintenanceLog.Printf("Disabled implicit action-failure expiry marker in %s (no maintenance workflow will enforce it)", lockFile)
	}
}

// anyWorkflowMayReportFailureAsIssue returns true unless every workflow in the
// list explicitly disables safe-outputs.report-failure-as-issue. The setting
// defaults to enabled (true) when unset, but an empty list cannot report
// failures and therefore returns false.
func anyWorkflowMayReportFailureAsIssue(workflowDataList []*WorkflowData) bool {
	for _, workflowData := range workflowDataList {
		if workflowData == nil {
			continue
		}
		if workflowData.SafeOutputs == nil || workflowData.SafeOutputs.ReportFailureAsIssue == nil {
			return true
		}
		if workflowData.SafeOutputs.ReportFailureAsIssue.String() != "false" {
			return true
		}
	}
	return false
}
