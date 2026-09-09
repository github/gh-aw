package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var autoUpdateWorkflowLog = logger.New("workflow:auto_update_workflow")

// AutoUpdateWorkflowFileName is the filename for the generated auto-upgrade workflow.
const AutoUpdateWorkflowFileName = "agentic-auto-upgrade.yml"

// autoUpdateWorkflowIdentifier is the stable identifier used to scatter the
// FUZZY:WEEKLY cron schedule. It is combined with the repo slug to ensure
// that different repositories scatter to different time slots.
const autoUpdateWorkflowIdentifier = "agentic-auto-upgrade"

// GenerateAutoUpdateWorkflowOptions configures an auto-update workflow generation run.
type GenerateAutoUpdateWorkflowOptions struct {
	// Context is used for action reference resolution in non-dev modes.
	// When nil, context.Background() is used.
	Context context.Context
	// WorkflowDir is the directory where the workflow file will be written.
	WorkflowDir string
	// Enabled indicates whether auto-updates are enabled in the repo config.
	Enabled bool
	// RepoSlug is the owner/repo slug used to deterministically scatter the
	// weekly cron schedule across different repositories. Pass an empty string
	// when the slug is not available; scattering will still succeed using only
	// the workflow identifier as seed.
	RepoSlug string
	// SetupActionRef is the resolved reference for the gh-aw actions/setup action.
	// For example: "./actions/setup" (dev mode) or "github/gh-aw/actions/setup@<sha>" (release mode).
	// When empty, "./actions/setup" is used as a fallback.
	SetupActionRef string
	// GitHubScriptPin is the pinned reference for actions/github-script.
	// When empty, getActionPin("actions/github-script") is used as a fallback.
	GitHubScriptPin string
	// ActionMode controls how CLI install steps and command prefixes are generated.
	// Defaults to ActionModeDev when empty.
	ActionMode ActionMode
	// Version is the gh-aw version used by generateInstallCLISteps in non-dev modes.
	Version string
	// ActionTag optionally overrides the setup-cli version tag in non-dev modes.
	ActionTag string
	// Resolver optionally resolves setup-cli action tags to SHA-pinned refs.
	Resolver SHAResolver
	// CustomCron is an optional cron expression that overrides the default
	// fuzzy weekly schedule. When non-empty, it is used as-is in the generated
	// workflow without scattering. An empty string falls back to FUZZY:WEEKLY.
	CustomCron string
	// UpgradeOptions contains supported command-line options passed to gh aw upgrade.
	UpgradeOptions []string
}

// GenerateAutoUpdateWorkflow generates or removes the agentic-auto-upgrade.yml workflow
// based on whether auto-updates are enabled in the repository's aw.json.
//
// When enabled, it generates a workflow that runs on a fuzzy weekly schedule
// and inlines the upgrade operation JavaScript to check for and report available
// workflow upgrades via a GitHub issue.
//
// When disabled (or when maintenance is disabled), any existing agentic-auto-upgrade.yml
// is deleted.
func GenerateAutoUpdateWorkflow(opts GenerateAutoUpdateWorkflowOptions) error { //nolint:largefunc // Generation keeps schedule resolution and emitted workflow inputs together.
	outputFile := filepath.Join(opts.WorkflowDir, AutoUpdateWorkflowFileName)

	if !opts.Enabled {
		autoUpdateWorkflowLog.Print("Auto-updates not enabled, removing agentic-auto-upgrade.yml if present")
		if _, err := os.Stat(outputFile); err == nil {
			autoUpdateWorkflowLog.Printf("Deleting existing auto-upgrade workflow: %s", outputFile)
			if err := os.Remove(outputFile); err != nil {
				return fmt.Errorf("failed to delete auto-upgrade workflow: %w", err)
			}
			autoUpdateWorkflowLog.Print("Auto-upgrade workflow deleted successfully")
		}
		return nil
	}

	actionMode := opts.ActionMode
	if actionMode == "" {
		actionMode = DetectActionMode(opts.Version)
	}

	var cronSchedule string
	if opts.CustomCron != "" {
		cronSchedule = opts.CustomCron
		autoUpdateWorkflowLog.Printf("Using custom cron schedule: %q", cronSchedule)
	} else {
		seed := buildAutoUpdateSeed(opts.RepoSlug, actionMode)
		var err error
		cronSchedule, err = parser.ScatterSchedule("FUZZY:WEEKLY", seed)
		if err != nil {
			return fmt.Errorf("failed to scatter FUZZY:WEEKLY schedule for auto-update workflow: %w", err)
		}
		autoUpdateWorkflowLog.Printf("Scattered FUZZY:WEEKLY to %q for seed %q", cronSchedule, seed)
	}

	setupActionRef := opts.SetupActionRef
	if setupActionRef == "" {
		setupActionRef = "./actions/setup"
	}
	githubScriptPin := opts.GitHubScriptPin
	if githubScriptPin == "" {
		githubScriptPin = getActionPin("actions/github-script")
	}

	ctx := ctxutil.OrBackground(opts.Context)
	content, err := buildAutoUpdateWorkflowYAML(
		cronSchedule,
		setupActionRef,
		githubScriptPin,
		generateInstallCLISteps(ctx, actionMode, opts.Version, opts.ActionTag, opts.Resolver),
		getCLICmdPrefix(actionMode),
		opts.CustomCron != "",
		opts.UpgradeOptions,
	)
	if err != nil {
		return fmt.Errorf("failed to finalize auto-update workflow YAML: %w", err)
	}

	autoUpdateWorkflowLog.Printf("Writing auto-update workflow to %s", outputFile)
	if err := fileutil.EnsureParentDir(outputFile, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create auto-update workflow directory: %w", err)
	}
	if err := os.WriteFile(outputFile, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write auto-update workflow: %w", err)
	}

	autoUpdateWorkflowLog.Print("Auto-update workflow generated successfully")
	return nil
}

// buildAutoUpdateSeed returns the deterministic seed string used to scatter the
// FUZZY:WEEKLY cron schedule.
//
// In dev mode a stable "dev/" prefix is used instead of the slug, which may not
// be available (e.g. in sandbox environments where the git remote URL is a
// localhost proxy). This mirrors the behaviour of normalizeScheduleString in
// schedule_preprocessing.go and prevents the generated schedule from changing
// between dev builds when --schedule-seed is sometimes provided and sometimes not.
//
// In all other modes (release, action, script) the repo slug is incorporated so
// that different repositories scatter to distinct time slots. Note: released
// binaries auto-detect ActionModeAction, not ActionModeRelease, so checking only
// IsRelease() would cause ActionModeAction builds to silently use the dev seed.
func buildAutoUpdateSeed(repoSlug string, actionMode ActionMode) string {
	if actionMode.IsDev() {
		// Dev mode: use a fixed prefix that does not depend on git remote detection.
		return "dev/" + autoUpdateWorkflowIdentifier
	}
	// Release/action/script mode: incorporate repo slug for per-repo scattering.
	if repoSlug != "" {
		return repoSlug + "/" + autoUpdateWorkflowIdentifier //nolint:manualpathconcat // Repository slugs are not filesystem paths.
	}
	return autoUpdateWorkflowIdentifier
}

// buildAutoUpdateWorkflowYAML generates the YAML content for agentic-auto-upgrade.yml.
func buildAutoUpdateWorkflowYAML( //nolint:largefunc // The renderer preserves the generated workflow field order.
	cronSchedule, setupActionRef, githubScriptPin, installCLISteps, cliCmdPrefix string,
	isCustomCron bool,
	upgradeOptions []string,
) (string, error) {
	var customInstructions string
	if isCustomCron {
		customInstructions = `Alternative regeneration methods:
  make recompile

Or use the gh-aw CLI directly:
  ./gh-aw compile --validate --verbose

The workflow is generated when auto_upgrade.cron is set in aw.json.
The schedule is pinned to the custom cron expression configured in aw.json.`
	} else {
		customInstructions = `Alternative regeneration methods:
  make recompile

Or use the gh-aw CLI directly:
  ./gh-aw compile --validate --verbose

The workflow is generated when auto_upgrade is enabled in aw.json (true or object form).
When auto_upgrade is an object without a cron, the fuzzy weekly schedule is used.
The weekly schedule is deterministically scattered based on the repository slug.`
	}

	scheduleComment := "Custom schedule (auto-upgrade)"
	if !isCustomCron {
		scheduleComment = "Weekly (auto-upgrade)"
	}

	header := GenerateWorkflowHeader("", "pkg/workflow/auto_update_workflow.go", customInstructions)
	upgradeOptionsEnv := ""
	if len(upgradeOptions) > 0 {
		encodedOptions, err := json.Marshal(upgradeOptions)
		if err != nil {
			return "", fmt.Errorf("failed to encode auto-upgrade options: %w", err)
		}
		upgradeOptionsEnv = "\n          GH_AW_UPGRADE_OPTIONS: '" + escapeYAMLSingleQuoted(string(encodedOptions)) + "'"
	}

	yaml := header + `name: Agentic Auto-Upgrade

on:
  schedule:
    - cron: "` + cronSchedule + `"  # ` + scheduleComment + `
  workflow_dispatch:

permissions:
  contents: read
  issues: write

jobs:
  auto-upgrade:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: ` + getActionPin("actions/checkout") + `

` + installCLISteps + `      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: ${{ runner.temp }}/gh-aw/actions

      - name: Run upgrade notification
        uses: ` + githubScriptPin + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_OPERATION: upgrade
          GH_AW_CMD_PREFIX: ` + cliCmdPrefix + upgradeOptionsEnv + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { mainNotifyIssue } = require('${{ runner.temp }}/gh-aw/actions/run_operation_update_upgrade.cjs');
            await mainNotifyIssue();
`
	finalYAML, err := finalizeRunnerTempSafety(yaml)
	if err != nil {
		return "", fmt.Errorf("runner temp safety: %w", err)
	}
	return finalYAML, nil
}
