package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var updateLog = logger.New("cli:update_command")

// NewUpdateCommand creates the update command
func NewUpdateCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [workflow]...",
		Short: "Update agentic workflows from their source repositories and check for gh aw updates",
		Long: `Update one or more workflows from their source repositories and check for gh aw updates.

The command:
1. Checks if a newer version of gh aw is available
2. Updates GitHub Actions versions in .github/aw/actions-lock.json (unless --no-actions is set)
3. Updates workflows using the 'source' field in the workflow frontmatter
4. Compiles each workflow immediately after update
5. Applies automatic fixes (codemods) to ensure workflows follow latest best practices

By default, the update command replaces local workflow files with the latest version from the source
repository, overriding any local changes. Use the --merge flag to preserve local changes by performing
a 3-way merge between the base version, your local changes, and the latest upstream version.

For workflow updates, it fetches the latest version based on the current ref:
- If the ref is a tag, it updates to the latest release (use --major for major version updates)
- If the ref is a branch, it fetches the latest commit from that branch
- Otherwise, it fetches the latest commit from the default branch

For action updates, it checks each action in .github/aw/actions-lock.json for newer releases
and updates the SHA to pin to the latest version. Use --no-actions to skip action updates.

DEPENDENCY HEALTH AUDIT:
Use --audit to check dependency health without performing updates. This includes:
- Outdated Go dependencies with available updates
- Security advisories from GitHub Security Advisory API
- Dependency maturity analysis (v0.x vs stable versions)
- Comprehensive dependency health report

The --audit flag implies --dry-run (no updates performed).

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` update                    # Check gh aw updates, update actions, and update all workflows
  ` + string(constants.CLIExtensionPrefix) + ` update ci-doctor         # Check gh aw updates, update actions, and update specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` update --no-actions      # Skip action updates, only update workflows
  ` + string(constants.CLIExtensionPrefix) + ` update ci-doctor.md      # Check gh aw updates, update actions, and update specific workflow (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` update ci-doctor --major # Allow major version updates
  ` + string(constants.CLIExtensionPrefix) + ` update --merge           # Update with 3-way merge to preserve local changes
  ` + string(constants.CLIExtensionPrefix) + ` update --pr              # Create PR with changes
  ` + string(constants.CLIExtensionPrefix) + ` update --force           # Force update even if no changes
  ` + string(constants.CLIExtensionPrefix) + ` update --dir custom/workflows  # Update workflows in custom directory
  ` + string(constants.CLIExtensionPrefix) + ` update --audit           # Check dependency health without updating
  ` + string(constants.CLIExtensionPrefix) + ` update --dry-run         # Show what would be updated without making changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			majorFlag, _ := cmd.Flags().GetBool("major")
			forceFlag, _ := cmd.Flags().GetBool("force")
			engineOverride, _ := cmd.Flags().GetString("engine")
			verbose, _ := cmd.Flags().GetBool("verbose")
			prFlag, _ := cmd.Flags().GetBool("pr")
			workflowDir, _ := cmd.Flags().GetString("dir")
			noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
			stopAfter, _ := cmd.Flags().GetString("stop-after")
			mergeFlag, _ := cmd.Flags().GetBool("merge")
			noActions, _ := cmd.Flags().GetBool("no-actions")
			auditFlag, _ := cmd.Flags().GetBool("audit")
			dryRunFlag, _ := cmd.Flags().GetBool("dry-run")
			jsonOutput, _ := cmd.Flags().GetBool("json")

			if err := validateEngine(engineOverride); err != nil {
				return err
			}

			// Handle audit mode
			if auditFlag {
				return runDependencyAudit(verbose, jsonOutput)
			}

			// Handle dry-run mode
			if dryRunFlag {
				// TODO: Implement dry-run mode for workflow updates
				return fmt.Errorf("--dry-run mode not yet implemented for workflow updates")
			}

			return UpdateWorkflowsWithExtensionCheck(args, majorFlag, forceFlag, verbose, engineOverride, prFlag, workflowDir, noStopAfter, stopAfter, mergeFlag, noActions)
		},
	}

	cmd.Flags().Bool("major", false, "Allow major version updates when updating tagged releases")
	cmd.Flags().BoolP("force", "f", false, "Force update even if no changes are detected")
	addEngineFlag(cmd)
	cmd.Flags().Bool("pr", false, "Create a pull request with the workflow changes")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: .github/workflows)")
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")
	cmd.Flags().Bool("merge", false, "Merge local changes with upstream updates instead of overriding")
	cmd.Flags().Bool("no-actions", false, "Skip updating GitHub Actions versions")
	cmd.Flags().Bool("audit", false, "Check dependency health without performing updates (implies --dry-run)")
	cmd.Flags().Bool("dry-run", false, "Show what would be updated without making changes")
	cmd.Flags().BoolP("json", "j", false, "Output audit results in JSON format (only with --audit)")

	// Register completions for update command
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// runDependencyAudit performs a dependency health audit
func runDependencyAudit(verbose bool, jsonOutput bool) error {
	updateLog.Print("Running dependency health audit")

	// Generate comprehensive report
	report, err := GenerateDependencyReport(verbose)
	if err != nil {
		return fmt.Errorf("failed to generate dependency report: %w", err)
	}

	// Display the report
	if jsonOutput {
		return DisplayDependencyReportJSON(report)
	}
	DisplayDependencyReport(report)

	return nil
}

// UpdateWorkflowsWithExtensionCheck performs the complete update process:
// 1. Check for gh-aw extension updates
// 2. Update GitHub Actions versions (unless --no-actions flag is set)
// 3. Update workflows from source repositories (compiles each workflow after update)
// 4. Apply automatic fixes to updated workflows
// 5. Optionally create a PR
func UpdateWorkflowsWithExtensionCheck(workflowNames []string, allowMajor, force, verbose bool, engineOverride string, createPR bool, workflowsDir string, noStopAfter bool, stopAfter string, merge bool, noActions bool) error {
	updateLog.Printf("Starting update process: workflows=%v, allowMajor=%v, force=%v, createPR=%v, merge=%v, noActions=%v", workflowNames, allowMajor, force, createPR, merge, noActions)

	// Step 1: Check for gh-aw extension updates
	if err := checkExtensionUpdate(verbose); err != nil {
		return fmt.Errorf("extension update check failed: %w", err)
	}

	// Step 2: Update GitHub Actions versions (unless disabled)
	if !noActions {
		if err := UpdateActions(allowMajor, verbose); err != nil {
			return fmt.Errorf("action update failed: %w", err)
		}
	}

	// Step 3: Update workflows from source repositories
	// Note: Each workflow is compiled immediately after update
	if err := UpdateWorkflows(workflowNames, allowMajor, force, verbose, engineOverride, workflowsDir, noStopAfter, stopAfter, merge); err != nil {
		return fmt.Errorf("workflow update failed: %w", err)
	}

	// Step 4: Apply automatic fixes to updated workflows
	fixConfig := FixConfig{
		WorkflowIDs: workflowNames,
		Write:       true,
		Verbose:     verbose,
	}
	if err := RunFix(fixConfig); err != nil {
		updateLog.Printf("Fix command failed (non-fatal): %v", err)
		// Don't fail the update if fix fails - this is non-critical
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: automatic fixes failed: %v", err)))
		}
	}

	// Step 5: Optionally create PR if flag is set
	if createPR {
		if err := createUpdatePR(verbose); err != nil {
			return fmt.Errorf("failed to create PR: %w", err)
		}
	}

	return nil
}
