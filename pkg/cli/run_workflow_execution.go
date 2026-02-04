package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var executionLog = logger.New("cli:run_workflow_execution")

// RunWorkflowOnGitHub runs an agentic workflow on GitHub Actions
func RunWorkflowOnGitHub(ctx context.Context, workflowIdOrName string, enable bool, engineOverride string, repoOverride string, refOverride string, autoMergePRs bool, pushSecrets bool, push bool, waitForCompletion bool, inputs []string, verbose bool, dryRun bool) error {
	executionLog.Printf("Starting workflow run: workflow=%s, enable=%v, engineOverride=%s, repo=%s, ref=%s, push=%v, wait=%v, inputs=%v", workflowIdOrName, enable, engineOverride, repoOverride, refOverride, push, waitForCompletion, inputs)

	// Check context cancellation at the start
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}

	if workflowIdOrName == "" {
		return fmt.Errorf("workflow name or ID is required")
	}

	// Validate input format early before attempting workflow validation
	for _, input := range inputs {
		if !strings.Contains(input, "=") {
			return fmt.Errorf("invalid input format '%s': expected key=value", input)
		}
		// Check that key (before '=') is not empty
		parts := strings.SplitN(input, "=", 2)
		if len(parts[0]) == 0 {
			return fmt.Errorf("invalid input format '%s': key cannot be empty", input)
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Running workflow on GitHub Actions: %s", workflowIdOrName)))
	}

	// Check if gh CLI is available
	if !isGHCLIAvailable() {
		return fmt.Errorf("GitHub CLI (gh) is required but not available")
	}

	// Validate workflow exists and is runnable
	if repoOverride != "" {
		executionLog.Printf("Validating remote workflow: %s in repo %s", workflowIdOrName, repoOverride)
		// For remote repositories, use remote validation
		if err := validateRemoteWorkflow(workflowIdOrName, repoOverride, verbose); err != nil {
			return fmt.Errorf("failed to validate remote workflow: %w", err)
		}
		// Note: We skip local runnable check for remote workflows as we assume they are properly configured
	} else {
		executionLog.Printf("Validating local workflow: %s", workflowIdOrName)
		// For local workflows, use existing local validation
		workflowFile, err := resolveWorkflowFile(workflowIdOrName, verbose)
		if err != nil {
			// Return error directly without wrapping - it already contains formatted message with suggestions
			return err
		}

		// Check if the workflow is runnable (has workflow_dispatch trigger)
		runnable, err := IsRunnable(workflowFile)
		if err != nil {
			return fmt.Errorf("failed to check if workflow %s is runnable: %w", workflowFile, err)
		}

		if !runnable {
			return fmt.Errorf("workflow '%s' cannot be run on GitHub Actions - it must have 'workflow_dispatch' trigger", workflowIdOrName)
		}
		executionLog.Printf("Workflow is runnable: %s", workflowFile)

		// Validate workflow inputs
		if err := validateWorkflowInputs(workflowFile, inputs); err != nil {
			return fmt.Errorf("%w", err)
		}

		// Check if the workflow file has local modifications
		if status, err := checkWorkflowFileStatus(workflowFile); err == nil && status != nil {
			var warnings []string

			if status.IsModified {
				warnings = append(warnings, "The workflow file has unstaged changes")
			}
			if status.IsStaged {
				warnings = append(warnings, "The workflow file has staged changes")
			}
			if status.HasUnpushedCommits {
				warnings = append(warnings, "The workflow file has unpushed commits")
			}

			if len(warnings) > 0 {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(strings.Join(warnings, ", ")))
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("These changes will not be reflected in the GitHub Actions run"))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Consider pushing your changes before running the workflow"))
			}
		}
	}

	// Handle --enable flag logic: check workflow state and enable if needed
	var wasDisabled bool
	var workflowID int64
	if enable {
		// Get current workflow status
		wf, err := getWorkflowStatus(workflowIdOrName, repoOverride, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check workflow status: %v", err)))
			}
		}

		// If we successfully got workflow status, check if it needs enabling
		if err == nil {
			workflowID = wf.ID
			if wf.State == "disabled_manually" {
				wasDisabled = true
				executionLog.Printf("Workflow %s is disabled, temporarily enabling for this run (id=%d)", workflowIdOrName, wf.ID)
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow '%s' is disabled, enabling it temporarily...", workflowIdOrName)))
				}
				// Enable the workflow
				enableArgs := []string{"workflow", "enable", strconv.FormatInt(wf.ID, 10)}
				if repoOverride != "" {
					enableArgs = append(enableArgs, "--repo", repoOverride)
				}
				cmd := workflow.ExecGH(enableArgs...)
				if err := cmd.Run(); err != nil {
					executionLog.Printf("Failed to enable workflow %s: %v", workflowIdOrName, err)
					return fmt.Errorf("failed to enable workflow '%s': %w", workflowIdOrName, err)
				}
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Enabled workflow: %s", workflowIdOrName)))
			} else {
				executionLog.Printf("Workflow %s is already enabled (state=%s)", workflowIdOrName, wf.State)
			}
		}
	}

	// Normalize workflow ID to handle both \"workflow-name\" and \".github/workflows/workflow-name.md\" formats
	normalizedID := normalizeWorkflowID(workflowIdOrName)

	// Construct lock file name from normalized ID (same for both local and remote)
	lockFileName := normalizedID + ".lock.yml"

	// For local workflows, validate the workflow exists and check for lock file
	var lockFilePath string
	if repoOverride == "" {
		// For local workflows, validate the workflow exists locally
		workflowsDir := getWorkflowsDir()

		_, _, err := readWorkflowFile(normalizedID+".md", workflowsDir)
		if err != nil {
			return fmt.Errorf("failed to find workflow in local .github/workflows: %w", err)
		}

		// Check if the lock file exists in .github/workflows
		lockFilePath = filepath.Join(".github/workflows", lockFileName)
		if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
			executionLog.Printf("Lock file not found: %s (workflow must be compiled first)", lockFilePath)
			suggestions := []string{
				fmt.Sprintf("Run '%s compile' to compile all workflows", string(constants.CLIExtensionPrefix)),
				fmt.Sprintf("Run '%s compile %s' to compile this specific workflow", string(constants.CLIExtensionPrefix), normalizedID),
			}
			errMsg := console.FormatErrorWithSuggestions(
				fmt.Sprintf("workflow lock file '%s' not found in .github/workflows", lockFileName),
				suggestions,
			)
			return fmt.Errorf("%s", errMsg)
		}
		executionLog.Printf("Found lock file: %s", lockFilePath)
	}

	// Recompile workflow if engine override is provided (only for local workflows)
	if engineOverride != "" && repoOverride == "" {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Recompiling workflow with engine override: %s", engineOverride)))
		}

		workflowMarkdownPath := stringutil.LockFileToMarkdown(lockFilePath)
		config := CompileConfig{
			MarkdownFiles:        []string{workflowMarkdownPath},
			Verbose:              verbose,
			EngineOverride:       engineOverride,
			Validate:             true,
			Watch:                false,
			WorkflowDir:          "",
			SkipInstructions:     false,
			NoEmit:               false,
			Purge:                false,
			TrialMode:            false,
			TrialLogicalRepoSlug: "",
			Strict:               false,
		}
		if _, err := CompileWorkflows(ctx, config); err != nil {
			return fmt.Errorf("failed to recompile workflow with engine override: %w", err)
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully recompiled workflow with engine: %s", engineOverride)))
		}
	} else if engineOverride != "" && repoOverride != "" {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Note: Engine override ignored for remote repository workflows"))
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using lock file: %s", lockFileName)))
	}

	// Check for missing or outdated lock files (when not using --push)
	if !push && repoOverride == "" {
		workflowMarkdownPath := stringutil.LockFileToMarkdown(lockFilePath)
		if status, err := checkLockFileStatus(workflowMarkdownPath); err == nil {
			if status.Missing {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Lock file is missing"))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run 'gh aw run %s --push' to automatically compile and push the lock file", workflowIdOrName)))
			} else if status.Outdated {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Lock file is outdated (workflow file is newer)"))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run 'gh aw run %s --push' to automatically compile and push the lock file", workflowIdOrName)))
			}
		}
	}

	// Handle --push flag: commit and push workflow files before running
	if push {
		// Only valid for local workflows
		if repoOverride != "" {
			return fmt.Errorf("--push flag is only supported for local workflows, not remote repositories")
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Collecting workflow files for push..."))
		}

		// Collect the workflow .md file, .lock.yml file, and transitive imports
		workflowMarkdownPath := stringutil.LockFileToMarkdown(lockFilePath)
		files, err := collectWorkflowFiles(ctx, workflowMarkdownPath, verbose)
		if err != nil {
			return fmt.Errorf("failed to collect workflow files: %w", err)
		}

		// Commit and push the files (includes branch verification if --ref is specified)
		if err := pushWorkflowFiles(workflowIdOrName, files, refOverride, verbose); err != nil {
			return fmt.Errorf("failed to push workflow files: %w", err)
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully pushed %d file(s) for workflow %s", len(files), workflowIdOrName)))
	}

	// Handle secret pushing if requested
	var secretTracker *TrialSecretTracker
	if pushSecrets {
		// Determine target repository
		var targetRepo string
		if repoOverride != "" {
			targetRepo = repoOverride
		} else {
			// Get current repository slug
			currentRepo, err := GetCurrentRepoSlug()
			if err != nil {
				return fmt.Errorf("failed to determine current repository for secret handling: %w", err)
			}
			targetRepo = currentRepo
		}

		secretTracker = NewTrialSecretTracker(targetRepo)
		executionLog.Printf("Created secret tracker for repository: %s", targetRepo)

		// Set up secret cleanup to always run on exit
		defer func() {
			if err := cleanupTrialSecrets(targetRepo, secretTracker, verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup secrets: %v", err)))
			}
		}()

		// Add GitHub token secret
		if err := addGitHubTokenSecret(targetRepo, secretTracker, verbose); err != nil {
			return fmt.Errorf("failed to add GitHub token secret: %w", err)
		}

		// Determine and add engine secrets
		if repoOverride == "" && lockFilePath != "" {
			// For local workflows, read and parse the workflow to determine engine requirements
			workflowMarkdownPath := stringutil.LockFileToMarkdown(lockFilePath)
			config := CompileConfig{
				MarkdownFiles:        []string{workflowMarkdownPath},
				Verbose:              false, // Don't be verbose during secret determination
				EngineOverride:       engineOverride,
				Validate:             false,
				Watch:                false,
				WorkflowDir:          "",
				SkipInstructions:     true,
				NoEmit:               true, // Don't emit files, just compile for analysis
				Purge:                false,
				TrialMode:            false,
				TrialLogicalRepoSlug: "",
				Strict:               false,
			}
			workflowDataList, err := CompileWorkflows(ctx, config)
			if err == nil && len(workflowDataList) == 1 {
				workflowData := workflowDataList[0]
				if err := determineAndAddEngineSecret(workflowData.EngineConfig, targetRepo, secretTracker, engineOverride, verbose); err != nil {
					// Log warning but don't fail - the workflow might still run without secrets
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to determine engine secret: %v", err)))
					}
				}
			} else if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Failed to compile workflow for secret determination - continuing without engine secrets"))
			}
		} else if repoOverride != "" {
			// For remote workflows, we can't analyze the workflow file, so create a minimal EngineConfig
			// with engine information and reuse the existing determineAndAddEngineSecret function
			var engineType string
			if engineOverride != "" {
				engineType = engineOverride
			} else {
				engineType = "copilot" // Default engine
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using default Copilot engine for remote workflow secret handling"))
				}
			}

			// Create minimal EngineConfig with engine information
			engineConfig := &workflow.EngineConfig{
				ID: engineType,
			}

			if err := determineAndAddEngineSecret(engineConfig, targetRepo, secretTracker, engineOverride, verbose); err != nil {
				// Log warning but don't fail - the workflow might still run without secrets
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to determine engine secret for remote workflow: %v", err)))
				}
			}
		}
	}

	// Build the gh workflow run command with optional repo and ref overrides
	args := []string{"workflow", "run", lockFileName}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}

	// Determine the ref to use (branch/tag)
	// If refOverride is specified, use it; otherwise for local workflows, use current branch
	ref := refOverride
	if ref == "" && repoOverride == "" {
		// For local workflows without explicit ref, use the current branch
		if currentBranch, err := getCurrentBranch(); err == nil {
			ref = currentBranch
			executionLog.Printf("Using current branch for workflow run: %s", ref)
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Note: Could not determine current branch: %v", err)))
		}
	}
	if ref != "" {
		args = append(args, "--ref", ref)
	}

	// Add workflow inputs if provided
	if len(inputs) > 0 {
		for _, input := range inputs {
			// Add as raw field flag to gh workflow run
			args = append(args, "-f", input)
		}
	}

	// Record the start time for auto-merge PR filtering
	workflowStartTime := time.Now()

	// Handle dry-run mode: validate everything but skip actual execution
	if dryRun {
		if verbose {
			var cmdParts []string
			cmdParts = append(cmdParts, "gh workflow run", lockFileName)
			if repoOverride != "" {
				cmdParts = append(cmdParts, "--repo", repoOverride)
			}
			if ref != "" {
				cmdParts = append(cmdParts, "--ref", ref)
			}
			if len(inputs) > 0 {
				for _, input := range inputs {
					cmdParts = append(cmdParts, "-f", input)
				}
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry run mode - command that would be executed:"))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(strings.Join(cmdParts, " ")))
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Validation passed for workflow: %s (dry run - not executed)", lockFileName)))

		// Restore workflow state if it was disabled and we enabled it
		if enable && wasDisabled && workflowID != 0 {
			restoreWorkflowState(workflowIdOrName, workflowID, repoOverride, verbose)
		}

		return nil
	}

	// Execute gh workflow run command and capture output
	cmd := workflow.ExecGH(args...)

	if verbose {
		var cmdParts []string
		cmdParts = append(cmdParts, "gh workflow run", lockFileName)
		if repoOverride != "" {
			cmdParts = append(cmdParts, "--repo", repoOverride)
		}
		if ref != "" {
			cmdParts = append(cmdParts, "--ref", ref)
		}
		if len(inputs) > 0 {
			for _, input := range inputs {
				cmdParts = append(cmdParts, "-f", input)
			}
		}
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage(strings.Join(cmdParts, " ")))
	}

	// Capture both stdout and stderr
	stdout, err := cmd.Output()
	if err != nil {
		// If there's an error, try to get stderr for better error reporting
		var stderrOutput string
		if exitError, ok := err.(*exec.ExitError); ok {
			stderrOutput = string(exitError.Stderr)
			fmt.Fprintf(os.Stderr, "%s", exitError.Stderr)
		}

		// Restore workflow state if it was disabled and we enabled it (even on error)
		if enable && wasDisabled && workflowID != 0 {
			restoreWorkflowState(workflowIdOrName, workflowID, repoOverride, verbose)
		}

		// Check if this is a permission error in a codespace
		errorMsg := err.Error() + " " + stderrOutput
		if isRunningInCodespace() && is403PermissionError(errorMsg) {
			// Show specialized error message for codespace users
			fmt.Fprint(os.Stderr, getCodespacePermissionErrorMessage())
			return fmt.Errorf("failed to run workflow on GitHub Actions: permission denied (403)")
		}

		return fmt.Errorf("failed to run workflow on GitHub Actions: %w", err)
	}

	// Display the output from gh workflow run
	output := strings.TrimSpace(string(stdout))
	if output != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(output))
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully triggered workflow: %s", lockFileName)))
	executionLog.Printf("Workflow triggered successfully: %s", lockFileName)

	// Try to get the latest run for this workflow to show a direct link
	// Add a delay to allow GitHub Actions time to register the new workflow run
	runInfo, runErr := getLatestWorkflowRunWithRetry(lockFileName, repoOverride, verbose)
	if runErr == nil && runInfo.URL != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("🔗 View workflow run: %s", runInfo.URL)))
		executionLog.Printf("Workflow run URL: %s (ID: %d)", runInfo.URL, runInfo.DatabaseID)

		// Suggest audit command for analysis
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("💡 To analyze this run, use: %s audit %d", string(constants.CLIExtensionPrefix), runInfo.DatabaseID)))
	} else if verbose && runErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Note: Could not get workflow run URL: %v", runErr)))
	}

	// Wait for workflow completion if requested (for --repeat or --auto-merge-prs)
	if waitForCompletion || autoMergePRs {
		if runErr != nil {
			if autoMergePRs {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not get workflow run information for auto-merge: %v", runErr)))
			} else if waitForCompletion {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not get workflow run information: %v", runErr)))
			}
		} else {
			// Determine target repository: use repo override if provided, otherwise get current repo
			targetRepo := repoOverride
			if targetRepo == "" {
				if currentRepo, err := GetCurrentRepoSlug(); err != nil {
					if autoMergePRs {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not determine target repository for auto-merge: %v", err)))
					}
					targetRepo = ""
				} else {
					targetRepo = currentRepo
				}
			}

			if targetRepo != "" {
				// Wait for workflow completion
				if autoMergePRs {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Auto-merge PRs enabled - waiting for workflow completion..."))
				} else {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Waiting for workflow completion..."))
				}

				runIDStr := fmt.Sprintf("%d", runInfo.DatabaseID)
				if err := WaitForWorkflowCompletion(targetRepo, runIDStr, 30, verbose); err != nil {
					if autoMergePRs {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow did not complete successfully, skipping auto-merge: %v", err)))
					} else {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow did not complete successfully: %v", err)))
					}
				} else {
					// Auto-merge PRs if requested and workflow completed successfully
					if autoMergePRs {
						if err := AutoMergePullRequestsCreatedAfter(targetRepo, workflowStartTime, verbose); err != nil {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to auto-merge pull requests: %v", err)))
						}
					}
				}
			}
		}
	}

	// Restore workflow state if it was disabled and we enabled it
	if enable && wasDisabled && workflowID != 0 {
		restoreWorkflowState(workflowIdOrName, workflowID, repoOverride, verbose)
	}

	return nil
}

// RunWorkflowsOnGitHub runs multiple agentic workflows on GitHub Actions, optionally repeating a specified number of times
func RunWorkflowsOnGitHub(ctx context.Context, workflowNames []string, repeatCount int, enable bool, engineOverride string, repoOverride string, refOverride string, autoMergePRs bool, pushSecrets bool, push bool, inputs []string, verbose bool, dryRun bool) error {
	if len(workflowNames) == 0 {
		return fmt.Errorf("at least one workflow name or ID is required")
	}

	// Check context cancellation at the start
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}

	// Validate all workflows exist and are runnable before starting
	for _, workflowName := range workflowNames {
		if workflowName == "" {
			return fmt.Errorf("workflow name cannot be empty")
		}

		// Validate workflow exists
		if repoOverride != "" {
			// For remote repositories, use remote validation
			if err := validateRemoteWorkflow(workflowName, repoOverride, verbose); err != nil {
				return fmt.Errorf("failed to validate remote workflow '%s': %w", workflowName, err)
			}
		} else {
			// For local workflows, use existing local validation
			workflowFile, err := resolveWorkflowFile(workflowName, verbose)
			if err != nil {
				// Return error directly without wrapping - it already contains formatted message with suggestions
				return err
			}

			runnable, err := IsRunnable(workflowFile)
			if err != nil {
				return fmt.Errorf("failed to check if workflow '%s' is runnable: %w", workflowName, err)
			}

			if !runnable {
				return fmt.Errorf("workflow '%s' cannot be run on GitHub Actions - it must have 'workflow_dispatch' trigger", workflowName)
			}
		}
	}

	// Function to run all workflows once
	runAllWorkflows := func() error {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Running %d workflow(s)...", len(workflowNames))))

		// Wait for completion when using --repeat to ensure workflows finish before next iteration
		waitForCompletion := repeatCount > 0

		for i, workflowName := range workflowNames {
			// Check for cancellation before each workflow
			select {
			case <-ctx.Done():
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
				return ctx.Err()
			default:
			}

			if len(workflowNames) > 1 {
				fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Running workflow %d/%d: %s", i+1, len(workflowNames), workflowName)))
			}

			if err := RunWorkflowOnGitHub(ctx, workflowName, enable, engineOverride, repoOverride, refOverride, autoMergePRs, pushSecrets, push, waitForCompletion, inputs, verbose, dryRun); err != nil {
				return fmt.Errorf("failed to run workflow '%s': %w", workflowName, err)
			}

			// Add a small delay between workflows to avoid overwhelming GitHub API
			if i < len(workflowNames)-1 {
				time.Sleep(1 * time.Second)
			}
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully triggered %d workflow(s)", len(workflowNames))))
		return nil
	}
	// Execute workflows with optional repeat functionality
	return ExecuteWithRepeat(RepeatOptions{
		RepeatCount:   repeatCount,
		RepeatMessage: "Repeating workflow run",
		ExecuteFunc:   runAllWorkflows,
		UseStderr:     false, // Use stdout for run command
	})
}
