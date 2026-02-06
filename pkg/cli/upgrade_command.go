package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var upgradeLog = logger.New("cli:upgrade_command")

// UpgradeConfig contains configuration for the upgrade command
type UpgradeConfig struct {
	Verbose     bool
	WorkflowDir string
	NoFix       bool
	Push        bool
	NoActions   bool
}

// RunUpgrade runs the upgrade command with the given configuration
func RunUpgrade(config UpgradeConfig) error {
	return runUpgradeCommand(config.Verbose, config.WorkflowDir, config.NoFix, false, config.Push, config.NoActions)
}

// NewUpgradeCommand creates the upgrade command
func NewUpgradeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade repository with latest agent files and apply codemods to all workflows",
		Long: `Upgrade the repository for the latest version of agentic workflows.

This command:
  1. Updates all agent and prompt files to the latest templates (like 'init' command)
  2. Applies automatic codemods to fix deprecated fields in all workflows (like 'fix --write')
  3. Updates GitHub Actions versions in .github/aw/actions-lock.json (unless --no-actions is set)
  4. Compiles all workflows to generate lock files (like 'compile' command)

The upgrade process ensures:
- Dispatcher agent is current (.github/agents/agentic-workflows.agent.md)
- All workflow prompts exist in .github/aw/ (create, update, debug, upgrade)
- All workflows use the latest syntax and configuration options
- Deprecated fields are automatically migrated across all workflows
- GitHub Actions are pinned to the latest versions
- All workflows are compiled and lock files are up-to-date

This command always upgrades all Markdown files in .github/workflows.

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` upgrade                    # Upgrade all workflows
  ` + string(constants.CLIExtensionPrefix) + ` upgrade --no-fix          # Update agent files only (skip codemods, actions, and compilation)
  ` + string(constants.CLIExtensionPrefix) + ` upgrade --no-actions      # Skip updating GitHub Actions versions
  ` + string(constants.CLIExtensionPrefix) + ` upgrade --push            # Upgrade and automatically commit/push changes
  ` + string(constants.CLIExtensionPrefix) + ` upgrade --dir custom/workflows  # Upgrade workflows in custom directory`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			dir, _ := cmd.Flags().GetString("dir")
			noFix, _ := cmd.Flags().GetBool("no-fix")
			push, _ := cmd.Flags().GetBool("push")
			noActions, _ := cmd.Flags().GetBool("no-actions")

			return runUpgradeCommand(verbose, dir, noFix, false, push, noActions)
		},
	}

	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: .github/workflows)")
	cmd.Flags().Bool("no-fix", false, "Skip applying codemods, action updates, and compiling workflows (only update agent files)")
	cmd.Flags().Bool("no-actions", false, "Skip updating GitHub Actions versions")
	cmd.Flags().Bool("push", false, "Automatically commit and push changes after successful upgrade")

	// Register completions
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// runUpgradeCommand executes the upgrade process
func runUpgradeCommand(verbose bool, workflowDir string, noFix bool, noCompile bool, push bool, noActions bool) error {
	upgradeLog.Printf("Running upgrade command: verbose=%v, workflowDir=%s, noFix=%v, noCompile=%v, push=%v, noActions=%v",
		verbose, workflowDir, noFix, noCompile, push, noActions)

	// Step 0a: If --push is enabled, ensure git status is clean before starting
	if push {
		upgradeLog.Print("Checking for clean working directory (--push enabled)")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking git status..."))
		if err := checkCleanWorkingDirectory(verbose); err != nil {
			upgradeLog.Printf("Git status check failed: %v", err)
			return fmt.Errorf("--push requires a clean working directory: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Working directory is clean"))
		}
	}

	// Step 0b: Ensure gh-aw extension is on the latest version
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking gh-aw extension version..."))
	if err := ensureLatestExtensionVersion(verbose); err != nil {
		upgradeLog.Printf("Extension version check failed: %v", err)
		return err
	}

	// Step 1: Update all agent and prompt files (like init command)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updating agent and prompt files..."))
	upgradeLog.Print("Updating agent and prompt files")

	if err := updateAgentFiles(verbose); err != nil {
		upgradeLog.Printf("Failed to update agent files: %v", err)
		return fmt.Errorf("failed to update agent files: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Updated agent and prompt files"))
	}

	// Step 2: Apply codemods to all workflows (unless --no-fix is specified)
	if !noFix {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying codemods to all workflows..."))
		upgradeLog.Print("Applying codemods to all workflows")

		fixConfig := FixConfig{
			WorkflowIDs: nil, // nil means all workflows
			Write:       true,
			Verbose:     verbose,
			WorkflowDir: workflowDir,
		}

		if err := RunFix(fixConfig); err != nil {
			upgradeLog.Printf("Failed to apply codemods: %v", err)
			// Don't fail the upgrade if fix fails - this is non-critical
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to apply codemods: %v", err)))
		}
	} else {
		upgradeLog.Print("Skipping codemods (--no-fix specified)")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping codemods (--no-fix specified)"))
		}
	}

	// Step 3: Update GitHub Actions versions (unless --no-fix or --no-actions is specified)
	if !noFix && !noActions {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updating GitHub Actions versions..."))
		upgradeLog.Print("Updating GitHub Actions versions")

		if err := UpdateActions(false, verbose); err != nil {
			upgradeLog.Printf("Failed to update actions: %v", err)
			// Don't fail the upgrade if action updates fail - this is non-critical
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to update actions: %v", err)))
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Updated GitHub Actions versions"))
		}
	} else {
		if noFix {
			upgradeLog.Print("Skipping action updates (--no-fix specified)")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping action updates (--no-fix specified)"))
			}
		} else if noActions {
			upgradeLog.Print("Skipping action updates (--no-actions specified)")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping action updates (--no-actions specified)"))
			}
		}
	}

	// Step 4: Compile all workflows (unless --no-fix is specified)
	if !noFix {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Compiling all workflows..."))
		upgradeLog.Print("Compiling all workflows")

		// Create and configure compiler
		compiler := createAndConfigureCompiler(CompileConfig{
			Verbose:     verbose,
			WorkflowDir: workflowDir,
		})

		// Determine workflow directory
		workflowsDir := workflowDir
		if workflowsDir == "" {
			workflowsDir = ".github/workflows"
		}

		// Compile all workflow files
		stats, compileErr := compileAllWorkflowFiles(compiler, workflowsDir, verbose)
		if compileErr != nil {
			upgradeLog.Printf("Failed to compile workflows: %v", compileErr)
			// Don't fail the upgrade if compilation fails - this is non-critical
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to compile workflows: %v", compileErr)))
		} else if stats != nil {
			// Print compilation summary
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Compiled %d workflow(s)", stats.Total-stats.Errors)))
			}
			if stats.Errors > 0 {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: %d workflow(s) failed to compile", stats.Errors)))
			}
		}
	} else {
		upgradeLog.Print("Skipping compilation (--no-fix specified)")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping compilation (--no-fix specified)"))
		}
	}

	// Print success message
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Upgrade complete"))

	// Step 5: If --push is enabled, commit and push changes
	if push {
		upgradeLog.Print("Push enabled - preparing to commit and push changes")
		fmt.Fprintln(os.Stderr, "")

		// Check if we're on the default branch
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking current branch..."))
		if err := checkOnDefaultBranch(verbose); err != nil {
			upgradeLog.Printf("Default branch check failed: %v", err)
			return fmt.Errorf("cannot push: %w", err)
		}

		// Confirm with user (skip in CI)
		if err := confirmPushOperation(verbose); err != nil {
			upgradeLog.Printf("Push operation not confirmed: %v", err)
			return fmt.Errorf("push operation cancelled: %w", err)
		}

		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Preparing to commit and push changes..."))

		// Use the helper function to orchestrate the full workflow
		commitMessage := "chore: upgrade agentic workflows"
		if err := commitAndPushChanges(commitMessage, verbose); err != nil {
			// Check if it's the "no changes" case
			hasChanges, checkErr := hasChangesToCommit()
			if checkErr == nil && !hasChanges {
				upgradeLog.Print("No changes to commit")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No changes to commit"))
				return nil
			}
			return err
		}

		// Print success messages based on whether remote exists
		fmt.Fprintln(os.Stderr, "")
		if hasRemote() {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes pushed to remote"))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes committed locally (no remote configured)"))
		}
	}

	return nil
}

// updateAgentFiles updates all agent and prompt files to the latest templates
func updateAgentFiles(verbose bool) error {
	// Update dispatcher agent
	if err := ensureAgenticWorkflowsDispatcher(verbose, false); err != nil {
		upgradeLog.Printf("Failed to update dispatcher agent: %v", err)
		return fmt.Errorf("failed to update dispatcher agent: %w", err)
	}

	// Upgrade copilot-setup-steps.yml version
	actionMode := workflow.DetectActionMode(GetVersion())
	if err := upgradeCopilotSetupSteps(verbose, actionMode, GetVersion()); err != nil {
		upgradeLog.Printf("Failed to upgrade copilot-setup-steps.yml: %v", err)
		// Don't fail the upgrade if copilot-setup-steps upgrade fails - this is non-critical
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to upgrade copilot-setup-steps.yml: %v", err)))
	}

	return nil
}
