package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var trialRepoLog = logger.New("cli:trial_repository")

// ensureTrialRepository creates a host repository if it doesn't exist, or reuses existing one
// For clone-repo mode, reusing an existing host repository is not allowed
// If forceDeleteHostRepo is true, deletes the repository if it exists before creating it
// If dryRun is true, only shows what would be done without making changes
func ensureTrialRepository(repoSlug string, cloneRepoSlug string, forceDeleteHostRepo bool, dryRun bool, verbose bool) error {
	trialRepoLog.Printf("Ensuring trial repository: %s (cloneRepo=%s, forceDelete=%v, dryRun=%v)", repoSlug, cloneRepoSlug, forceDeleteHostRepo, dryRun)

	parts := strings.Split(repoSlug, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repository slug format: %s. Expected format: owner/repo. Example: github/gh-aw", repoSlug)
	}

	// Check if repository already exists
	cmd := workflow.ExecGH("repo", "view", repoSlug)
	output, err := cmd.CombinedOutput()
	repoExists := err == nil

	if dryRun && verbose {
		if repoExists {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("[DRY RUN] Repository %s exists", repoSlug)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("[DRY RUN] Repository %s does not exist (output: %s)", repoSlug, string(output))))
		}
	}

	if repoExists {
		trialRepoLog.Printf("Repository %s already exists", repoSlug)
		// Repository exists - determine what to do
		if forceDeleteHostRepo {
			// Force delete mode: delete the existing repository first
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Force deleting existing host repository: %s", repoSlug)))
			}

			if dryRun {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("[DRY RUN] Would delete repository: %s", repoSlug)))
			} else {
				if deleteOutput, deleteErr := workflow.RunGHCombined("Deleting repository...", "repo", "delete", repoSlug, "--yes"); deleteErr != nil {
					return fmt.Errorf("failed to force delete existing host repository %s: %w (output: %s)", repoSlug, deleteErr, string(deleteOutput))
				}

				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Force deleted existing host repository: %s", repoSlug)))
			}

			// Continue to create the repository below
		} else {
			// Both clone-repo and logical-repo modes: reusing is allowed
			// In clone-repo mode, the cloneRepoContentsIntoHost function will force push the new contents
			if verbose {
				if cloneRepoSlug != "" {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Reusing existing host repository: %s (contents will be force-pushed)", repoSlug)))
				} else {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Reusing existing host repository: %s", repoSlug)))
				}
			}
			prefix := ""
			if dryRun {
				prefix = "[DRY RUN] "
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("%sUsing existing host repository: https://github.com/%s", prefix, repoSlug)))
			return nil
		}
	}

	// Repository doesn't exist, create it
	if verbose || dryRun {
		prefix := ""
		if dryRun {
			prefix = "[DRY RUN] "
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%sCreating private host repository: %s", prefix, repoSlug)))
	}

	if dryRun {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Would create repository with description: 'GitHub Agentic Workflows host repository'"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("[DRY RUN] Would enable GitHub Actions permissions at: https://github.com/%s/settings/actions", repoSlug)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Would enable discussions"))
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("[DRY RUN] Would create host repository: https://github.com/%s", repoSlug)))
		return nil
	}

	// Use gh CLI to create private repo with initial README using full OWNER/REPO format
	output, err = workflow.RunGHCombined("Creating repository...", "repo", "create", repoSlug, "--private", "--add-readme", "--description", "GitHub Agentic Workflows host repository")

	if err != nil {
		// Check if the error is because the repository already exists
		outputStr := string(output)
		if strings.Contains(outputStr, "name already exists") {
			// Repository exists but gh repo view failed earlier - this is okay, reuse it
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Repository already exists (detected via create error): %s", repoSlug)))
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Using existing host repository: https://github.com/%s", repoSlug)))
			return nil
		}
		return fmt.Errorf("failed to create host repository: %w (output: %s)", err, string(output))
	}

	// Show host repository creation message with URL
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Created host repository: https://github.com/%s", repoSlug)))

	// Prompt user to enable GitHub Actions permissions
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(""))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("IMPORTANT: You must enable GitHub Actions permissions for the repository."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("1. Go to: https://github.com/%s/settings/actions", repoSlug)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("2. Under 'Workflow permissions', select 'Allow GitHub Actions to create and approve pull requests'"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("3. Click 'Save'"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(""))

	// Wait for user confirmation
	fmt.Fprint(os.Stderr, console.FormatPromptMessage("Press Enter after you have enabled these permissions..."))
	var userInput string
	_, _ = fmt.Scanln(&userInput) // Ignore error (user pressed Enter without typing anything)
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Continuing with trial setup"))

	// Enable discussions in the repository as most workflows use them
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Enabling discussions in repository: %s", repoSlug)))
	}

	if discussionsOutput, discussionsErr := workflow.RunGHCombined("Enabling discussions...", "repo", "edit", repoSlug, "--enable-discussions"); discussionsErr != nil {
		// Non-fatal error, just warn
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to enable discussions: %v (output: %s)", discussionsErr, string(discussionsOutput))))
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Enabled discussions in host repository"))
	}

	// Give GitHub a moment to fully initialize the repository
	time.Sleep(2 * time.Second)

	return nil
}

func cleanupTrialRepository(repoSlug string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cleaning up host repository: %s", repoSlug)))
	}

	// Use gh CLI to delete the repository with proper username/repo format
	output, err := workflow.RunGHCombined("Deleting repository...", "repo", "delete", repoSlug, "--yes")

	if err != nil {
		return fmt.Errorf("failed to delete host repository: %w (output: %s)", err, string(output))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Deleted host repository: %s", repoSlug)))
	}

	return nil
}

func cloneTrialHostRepository(repoSlug string, verbose bool) (string, error) {
	// Create temporary directory
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("gh-aw-trial-%x", time.Now().UnixNano()))

	// Validate the temporary directory path
	tempDir, err := fileutil.ValidateAbsolutePath(tempDir)
	if err != nil {
		return "", fmt.Errorf("invalid temporary directory path: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning host repository to: %s", tempDir)))
	}

	// Clone the repository using the full slug
	repoURL := fmt.Sprintf("https://github.com/%s.git", repoSlug)
	cmd := exec.Command("git", "clone", repoURL, tempDir)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("failed to clone host repository %s: %w (output: %s)", repoURL, err, string(output))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Cloned host repository to: %s", tempDir)))
	}

	return tempDir, nil
}

// installWorkflowInTrialMode installs a workflow in trial mode using a parsed spec
func installWorkflowInTrialMode(ctx context.Context, tempDir string, parsedSpec *WorkflowSpec, logicalRepoSlug, cloneRepoSlug, hostRepoSlug string, secretTracker *TrialSecretTracker, engineOverride string, appendText string, pushSecrets bool, directTrialMode bool, verbose bool) error {
	trialRepoLog.Printf("Installing workflow in trial mode: workflow=%s, hostRepo=%s, directMode=%v", parsedSpec.WorkflowName, hostRepoSlug, directTrialMode)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		return fmt.Errorf("failed to change to temp directory: %w", err)
	}

	// Check if this is a local workflow
	if strings.HasPrefix(parsedSpec.WorkflowPath, "./") {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing local workflow '%s' from '%s' in trial mode", parsedSpec.WorkflowName, parsedSpec.WorkflowPath)))
		}

		// For local workflows, copy the file directly from the filesystem
		if err := installLocalWorkflowInTrialMode(originalDir, tempDir, parsedSpec, appendText, verbose); err != nil {
			return fmt.Errorf("failed to install local workflow: %w", err)
		}
	} else {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing workflow '%s' from '%s' in trial mode", parsedSpec.WorkflowName, parsedSpec.RepoSlug)))
		}

		// Install the source repository as a package
		if err := InstallPackage(parsedSpec.RepoSlug, verbose); err != nil {
			return fmt.Errorf("failed to install source repository: %w", err)
		}

		// Add the workflow from the installed package
		if _, err := AddWorkflows([]string{parsedSpec.String()}, 1, verbose, "", "", true, appendText, false, false, false, "", false, ""); err != nil {
			return fmt.Errorf("failed to add workflow: %w", err)
		}
	}

	// Modify the workflow for trial mode (skip in direct trial mode)
	if !directTrialMode {
		if err := modifyWorkflowForTrialMode(tempDir, parsedSpec.WorkflowName, logicalRepoSlug, verbose); err != nil {
			return fmt.Errorf("failed to modify workflow for trial mode: %w", err)
		}
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Direct trial mode: Skipping trial mode modifications"))
	}

	// Compile the workflow with trial modifications
	config := CompileConfig{
		MarkdownFiles:        []string{".github/workflows/" + parsedSpec.WorkflowName + ".md"},
		Verbose:              verbose,
		EngineOverride:       engineOverride,
		Validate:             true,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            !directTrialMode && (cloneRepoSlug == ""), // Enable trial mode in compiler unless in direct mode or clone-repo mode
		TrialLogicalRepoSlug: logicalRepoSlug,
	}
	workflowDataList, err := CompileWorkflows(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to compile workflow: %w", err)
	}
	if len(workflowDataList) != 1 {
		return fmt.Errorf("expected one compiled workflow, got %d", len(workflowDataList))
	}
	workflowData := workflowDataList[0]

	// Determine required engine secret from workflow data
	if pushSecrets {
		if err := determineAndAddEngineSecret(workflowData.EngineConfig, hostRepoSlug, secretTracker, engineOverride, verbose); err != nil {
			return fmt.Errorf("failed to determine engine secret: %w", err)
		}
	}

	// Commit and push the changes
	if err := commitAndPushWorkflow(tempDir, parsedSpec.WorkflowName, verbose); err != nil {
		return fmt.Errorf("failed to commit and push workflow: %w", err)
	}

	return nil
}

// installLocalWorkflowInTrialMode installs a local workflow file for trial mode
func installLocalWorkflowInTrialMode(originalDir, tempDir string, parsedSpec *WorkflowSpec, appendText string, verbose bool) error {
	// Construct the source path (relative to original directory)
	sourcePath := filepath.Join(originalDir, parsedSpec.WorkflowPath)

	// Validate source path
	sourcePath, err := fileutil.ValidateAbsolutePath(sourcePath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}

	// Check if the source file exists
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return fmt.Errorf("local workflow file does not exist: %s", sourcePath)
	}

	// Create the workflows directory in the temp directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())

	// Validate workflows directory path
	workflowsDir, err = fileutil.ValidateAbsolutePath(workflowsDir)
	if err != nil {
		return fmt.Errorf("invalid workflows directory path: %w", err)
	}

	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflows directory: %w", err)
	}

	// Construct the destination path
	destPath := filepath.Join(workflowsDir, parsedSpec.WorkflowName+".md")

	// Validate destination path
	destPath, err = fileutil.ValidateAbsolutePath(destPath)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Read the source file
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read local workflow file: %w", err)
	}

	// Append text if provided
	if appendText != "" {
		contentStr := string(content)
		// Ensure we have a newline before appending
		if !strings.HasSuffix(contentStr, "\n") {
			contentStr += "\n"
		}
		contentStr += "\n" + appendText
		content = []byte(contentStr)
	}

	// Write the content to the destination
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write workflow to destination: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Copied local workflow from %s to %s", sourcePath, destPath)))
	}

	return nil
}

// modifyWorkflowForTrialMode modifies the workflow to work in trial mode
func modifyWorkflowForTrialMode(tempDir, workflowName, logicalRepoSlug string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Modifying workflow for trial mode"))
	}

	// Find the workflow markdown file
	workflowPath := filepath.Join(tempDir, constants.GetWorkflowDir(), fmt.Sprintf("%s.md", workflowName))

	// Validate workflow path
	workflowPath, err := fileutil.ValidateAbsolutePath(workflowPath)
	if err != nil {
		return fmt.Errorf("invalid workflow path: %w", err)
	}

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow file: %w", err)
	}

	// Replace repository references in the content only if logicalRepoSlug is not empty
	modifiedContent := string(content)

	if logicalRepoSlug != "" {
		// Replace github.repository references to point to simulated host repo
		modifiedContent = strings.ReplaceAll(modifiedContent, "${{ github.repository }}", logicalRepoSlug)

		// Also replace any hardcoded checkout actions to use the simulated host repo
		// Split content into lines to preserve indentation
		lines := strings.Split(modifiedContent, "\n")
		checkoutPattern := regexp.MustCompile(`^(\s*)(uses: actions/checkout@[^\s]*)(.*)$`)

		var newLines []string
		for _, line := range lines {
			if matches := checkoutPattern.FindStringSubmatch(line); len(matches) >= 3 {
				indentation := matches[1]
				usesLine := matches[2]
				remainder := matches[3]

				// Add the original uses line
				newLines = append(newLines, fmt.Sprintf("%s%s%s", indentation, usesLine, remainder))
				// Add the with clause at the same indentation level as uses
				newLines = append(newLines, fmt.Sprintf("%swith:", indentation))
				newLines = append(newLines, fmt.Sprintf("%s  repository: %s", indentation, logicalRepoSlug))
			} else {
				newLines = append(newLines, line)
			}
		}

		modifiedContent = strings.Join(newLines, "\n")
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping repository simulation modifications (using clone-repo mode)"))
	}

	// Write the modified content back
	if err := os.WriteFile(workflowPath, []byte(modifiedContent), 0644); err != nil {
		return fmt.Errorf("failed to write modified workflow: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow modified for trial mode"))
	}

	return nil
}

// commitAndPushWorkflow commits and pushes the workflow changes
func commitAndPushWorkflow(tempDir, workflowName string, verbose bool) error {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Committing workflow and lock files to host repository"))

	// Add all changes
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add changes: %w (output: %s)", err, string(output))
	}

	// Check if there are any changes to commit
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = tempDir
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	// If no changes, skip commit and push
	if len(strings.TrimSpace(string(statusOutput))) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No changes detected, skipping commit"))
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow and lock files are up to date in host repository"))
		return nil
	}

	// Commit changes
	commitMsg := fmt.Sprintf("Add trial workflow: %s and compiled lock files", workflowName)
	cmd = exec.Command("git", "commit", "-m", commitMsg)
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to commit changes: %w (output: %s)", err, string(output))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Pulling latest changes from main branch"))
	}
	cmd = exec.Command("git", "pull", "origin", "main")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull latest changes: %w (output: %s)", err, string(output))
	}

	// Push to main
	cmd = exec.Command("git", "push", "origin", "main")
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push changes: %w (output: %s)", err, string(output))
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow and lock files committed and pushed to host repository"))

	return nil
}

// cloneRepoContentsIntoHost clones the contents of the source repo into the host repo
// Uses a simplified approach with force push since host repo is freshly created
func cloneRepoContentsIntoHost(cloneRepoSlug string, cloneRepoVersion string, hostRepoSlug string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning contents from %s into host repository %s", cloneRepoSlug, hostRepoSlug)))
	}

	// Save the original working directory to restore it later
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	// Create temporary directory to clone the source repo
	tempCloneDir := filepath.Join(os.TempDir(), fmt.Sprintf("gh-aw-clone-%x", time.Now().UnixNano()))
	defer os.RemoveAll(tempCloneDir)

	// Clone the source repository
	cloneURL := fmt.Sprintf("https://github.com/%s.git", cloneRepoSlug)
	cmd := exec.Command("git", "clone", cloneURL, tempCloneDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone source repository %s: %w (output: %s)", cloneURL, err, string(output))
	}

	// Change to the cloned repository directory
	if err := os.Chdir(tempCloneDir); err != nil {
		return fmt.Errorf("failed to change to clone directory: %w", err)
	}

	// If a version/tag/SHA is specified, checkout that ref
	if cloneRepoVersion != "" {
		cmd = exec.Command("git", "checkout", cloneRepoVersion)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to checkout ref '%s': %w (output: %s)", cloneRepoVersion, err, string(output))
		}
	}

	// Add the host repository as a new remote
	hostURL := fmt.Sprintf("https://github.com/%s.git", hostRepoSlug)
	cmd = exec.Command("git", "remote", "add", "host", hostURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add host remote: %w (output: %s)", err, string(output))
	}

	// Force push the current branch to the host repository's main branch
	cmd = exec.Command("git", "push", "--force", "host", "HEAD:main")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to force push to host repository: %w (output: %s)", err, string(output))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully pushed contents from %s to %s", cloneRepoSlug, hostRepoSlug)))
	}

	return nil
}
