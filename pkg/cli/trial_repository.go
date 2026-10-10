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
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var trialRepoLog = logger.New("cli:trial_repository")

// trialRepoInitDelay gives GitHub time to finish initializing a newly created repository.
const trialRepoInitDelay = 2 * time.Second

// checkoutActionPattern matches actions/checkout step lines with leading indentation,
// supporting both the block-mapping form ("  uses: actions/checkout@…") and the
// inline list-item form ("  - uses: actions/checkout@…").  Group 1 is the physical
// leading whitespace; group 2 is the optional "- " list marker.
var checkoutActionPattern = regexp.MustCompile(`^(\s*)(- )?uses:\s*["']?actions/checkout@[^\s"']+["']?\s*(?:#.*)?$`)

func leadingIndentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func isCheckoutStepBoundary(line string, keyIndent int) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	indent := leadingIndentWidth(line)
	if indent < keyIndent {
		return true
	}

	return indent == keyIndent && strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ")
}

func isIndentedKeyLine(line string, key string, indent int) bool {
	if leadingIndentWidth(line) != indent {
		return false
	}

	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, key+":")
}

func trialRepositoryURL(repoSlug string) string {
	return fmt.Sprintf("%s/%s", getGitHubHost(), repoSlug)
}

func trialRepositoryGitURL(repoSlug string) string {
	return trialRepositoryURL(repoSlug) + ".git"
}

func trialRepositoryActionsSettingsURL(repoSlug string) string {
	return trialRepositoryURL(repoSlug) + "/settings/actions"
}

// ensureTrialRepository creates a host repository if it doesn't exist, or reuses existing one
// For clone-repo mode, reusing an existing host repository is not allowed
// If forceDeleteHostRepo is true, deletes the repository if it exists before creating it
// If dryRun is true, only shows what would be done without making changes
func ensureTrialRepository(repoSlug string, cloneRepoSlug string, forceDeleteHostRepo bool, dryRun bool, verbose bool) error {
	trialRepoLog.Printf("Ensuring trial repository: %s (cloneRepo=%s, forceDelete=%v, dryRun=%v)", repoSlug, cloneRepoSlug, forceDeleteHostRepo, dryRun)

	if _, _, err := repoutil.SplitRepoSlug(repoSlug); err != nil {
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
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Force deleting existing host repository: "+repoSlug))
			}

			if dryRun {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Would delete repository: "+repoSlug))
			} else {
				if deleteOutput, deleteErr := workflow.RunGHCombined("Deleting repository...", "repo", "delete", repoSlug, "--yes"); deleteErr != nil {
					return fmt.Errorf("failed to force delete existing host repository %s: %w (output: %s)", repoSlug, deleteErr, string(deleteOutput))
				}

				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Force deleted existing host repository: "+repoSlug))
			}

			// Continue to create the repository below
		} else {
			// Both clone-repo and logical-repo modes: reusing is allowed
			// In clone-repo mode, the cloneRepoContentsIntoHost function will force push the new contents
			if verbose {
				if cloneRepoSlug != "" {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Reusing existing host repository: %s (contents will be force-pushed)", repoSlug)))
				} else {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Reusing existing host repository: "+repoSlug))
				}
			}
			prefix := ""
			if dryRun {
				prefix = "[DRY RUN] "
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("%sUsing existing host repository: %s", prefix, trialRepositoryURL(repoSlug))))
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Would enable GitHub Actions permissions at: "+trialRepositoryActionsSettingsURL(repoSlug)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Would enable discussions"))
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("[DRY RUN] Would create host repository: "+trialRepositoryURL(repoSlug)))
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
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Repository already exists (detected via create error): "+repoSlug))
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Using existing host repository: "+trialRepositoryURL(repoSlug)))
			return nil
		}
		return fmt.Errorf("failed to create host repository: %w (output: %s)", err, string(output))
	}

	// Show host repository creation message with URL
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created host repository: "+trialRepositoryURL(repoSlug)))

	// Prompt user to enable GitHub Actions permissions
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(""))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("IMPORTANT: You must enable GitHub Actions permissions for the repository."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("1. Go to: "+trialRepositoryActionsSettingsURL(repoSlug)))
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Enabling discussions in repository: "+repoSlug))
	}

	if discussionsOutput, discussionsErr := workflow.RunGHCombined("Enabling discussions...", "repo", "edit", repoSlug, "--enable-discussions"); discussionsErr != nil {
		// Non-fatal error, just warn
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to enable discussions: %v (output: %s)", discussionsErr, string(discussionsOutput))))
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Enabled discussions in host repository"))
	}

	// Give GitHub a moment to fully initialize the repository
	time.Sleep(trialRepoInitDelay)

	return nil
}

func cleanupTrialRepository(repoSlug string, verbose bool) error {
	trialRepoLog.Printf("Cleaning up trial repository: %s", repoSlug)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cleaning up host repository: "+repoSlug))
	}

	// Use gh CLI to delete the repository with proper username/repo format
	output, err := workflow.RunGHCombined("Deleting repository...", "repo", "delete", repoSlug, "--yes")

	if err != nil {
		trialRepoLog.Printf("Failed to delete trial repository %s: %v", repoSlug, err)
		return fmt.Errorf("failed to delete host repository: %w (output: %s)", err, string(output))
	}

	trialRepoLog.Printf("Successfully deleted trial repository: %s", repoSlug)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Deleted host repository: "+repoSlug))
	}

	return nil
}

func cloneTrialHostRepository(repoSlug string, verbose bool) (string, error) {
	trialRepoLog.Printf("Cloning trial host repository: %s", repoSlug)
	// Create temporary directory
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("gh-aw-trial-%x", time.Now().UnixNano()))

	// Validate the temporary directory path
	tempDir, err := fileutil.ValidateAbsolutePath(tempDir)
	if err != nil {
		return "", fmt.Errorf("invalid temporary directory path: %w", err)
	}

	// Clone the repository using the full slug
	repoURL := trialRepositoryGitURL(repoSlug)
	trialRepoLog.Printf("Cloning repository from URL to tempDir: %s", tempDir)

	output, err := workflow.RunGitCombined(fmt.Sprintf("Cloning %s...", repoSlug), "clone", repoURL, tempDir)
	if err != nil {
		trialRepoLog.Printf("Failed to clone host repository %s: %v", repoSlug, err)
		return "", fmt.Errorf("failed to clone host repository %s: %w (output: %s)", repoURL, err, string(output))
	}

	trialRepoLog.Printf("Successfully cloned trial repository to: %s", tempDir)
	return tempDir, nil
}

// installWorkflowInTrialMode installs a workflow in trial mode using a parsed spec
func installWorkflowInTrialMode(ctx context.Context, tempDir string, parsedSpec *WorkflowSpec, logicalRepoSlug, cloneRepoSlug, hostRepoSlug string, directTrialMode bool, opts *TrialOptions) error {
	trialRepoLog.Printf("Installing workflow in trial mode: workflow=%s, hostRepo=%s, directMode=%v", parsedSpec.WorkflowName, hostRepoSlug, directTrialMode)

	// Change to temp directory
	originalDir, err := gitutil.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		return fmt.Errorf("failed to change to temp directory: %w", err)
	}

	// Fetch workflow content - handle local workflows specially since they need
	// to be resolved from the original directory, not the tempDir
	specToFetch := parsedSpec
	var fetched *FetchedWorkflow

	if isLocalWorkflowPath(parsedSpec.WorkflowPath) {
		// For local workflows, temporarily change to original dir for fetch
		// Use a closure to ensure directory is restored even on error
		fetched, err = func() (*FetchedWorkflow, error) {
			if chErr := os.Chdir(originalDir); chErr != nil {
				return nil, fmt.Errorf("failed to change to original directory for local fetch: %w", chErr)
			}
			// Always restore to tempDir when this closure exits
			defer os.Chdir(tempDir)

			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing local workflow '%s' from '%s' in trial mode", parsedSpec.WorkflowName, parsedSpec.WorkflowPath)))
			}
			return FetchWorkflowFromSourceWithContext(ctx, specToFetch, opts.Verbose)
		}()
	} else {
		// Remote workflows can be fetched from any directory
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing workflow '%s' from '%s' in trial mode", parsedSpec.WorkflowName, parsedSpec.RepoSlug)))
		}
		fetched, err = FetchWorkflowFromSourceWithContext(ctx, specToFetch, opts.Verbose)
	}

	if err != nil {
		return fmt.Errorf("failed to fetch workflow: %w", err)
	}

	content := fetched.Content

	// When the fetch used a fallback path (e.g. .github/workflows/my-workflow.md
	// instead of the short-form my-workflow.md), SourcePath holds the actual
	// repo-root-relative path. Normalize parsedSpec so all downstream dependency
	// resolution (source field, includes, imports, dispatch workflows, resources)
	// uses the same effective workflow path.
	if !fetched.IsLocal && fetched.SourcePath != "" && fetched.SourcePath != parsedSpec.WorkflowPath {
		specCopy := *parsedSpec
		specCopy.WorkflowPath = fetched.SourcePath
		parsedSpec = &specCopy
	}

	// Add source field to frontmatter for remote workflows
	if !fetched.IsLocal && fetched.CommitSHA != "" {
		sourceString := buildSourceStringWithCommitSHA(parsedSpec, fetched.CommitSHA)
		if sourceString != "" {
			updatedContent, err := addSourceToWorkflow(string(content), sourceString)
			if err != nil {
				if opts.Verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to add source field: %v", err)))
				}
			} else {
				content = []byte(updatedContent)
			}
		}
	}

	// Use common helper for security scan, directory creation, and writing
	result, err := writeWorkflowToTrialDir(tempDir, parsedSpec.WorkflowName, content, opts)
	if err != nil {
		return err
	}

	if opts.Verbose {
		if fetched.IsLocal {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Copied local workflow to "+result.DestPath))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched remote workflow to "+result.DestPath))
		}
	}

	// Fetch and save all remote dependencies (includes, imports, runtime imports,
	// dispatch workflows, resources) using the exact commit resolved for this install.
	if !fetched.IsLocal {
		depSpec := *parsedSpec
		if fetched.CommitSHA != "" {
			depSpec.Version = fetched.CommitSHA
		}
		if err := fetchAllRemoteDependencies(ctx, string(content), &depSpec, result.WorkflowsDir, opts.Verbose, true, nil); err != nil {
			return fmt.Errorf("failed to fetch remote dependencies: %w", err)
		}
	}

	// Modify the workflow for trial mode (skip in direct trial mode)
	if !directTrialMode {
		if err := modifyWorkflowForTrialMode(tempDir, parsedSpec.WorkflowName, logicalRepoSlug, opts.Verbose); err != nil {
			return fmt.Errorf("failed to modify workflow for trial mode: %w", err)
		}
	} else if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Direct trial mode: Skipping trial mode modifications"))
	}

	// Compile the workflow with trial modifications
	config := CompileConfig{
		MarkdownFiles:        []string{constants.WorkflowsDirSlash + parsedSpec.WorkflowName + ".md"},
		Verbose:              opts.Verbose,
		EngineOverride:       opts.EngineOverride,
		Validate:             true,
		Watch:                false,
		WorkflowDir:          "",
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
	// Note: workflowData is used for validation; secrets are ensured before installWorkflowInTrialMode is called
	_ = workflowDataList[0]

	// Commit and push the changes
	if err := commitAndPushWorkflow(tempDir, parsedSpec.WorkflowName, opts.Verbose); err != nil {
		return fmt.Errorf("failed to commit and push workflow: %w", err)
	}

	return nil
}

// trialWorkflowWriteResult contains the result of writing a workflow to the trial directory
type trialWorkflowWriteResult struct {
	DestPath     string
	WorkflowsDir string
}

// writeWorkflowToTrialDir handles the common workflow writing logic for trial mode:
// - Security scanning
// - Creating workflows directory
// - Appending optional text
// - Writing to destination
// Returns the destination path and workflows directory for further processing.
func writeWorkflowToTrialDir(tempDir string, workflowName string, content []byte, opts *TrialOptions) (*trialWorkflowWriteResult, error) {
	trialRepoLog.Printf("Writing workflow to trial dir: workflow=%s, content_size=%d bytes, securityScanDisabled=%v", workflowName, len(content), opts.DisableSecurityScanner)
	// Security scan: reject workflows containing malicious or dangerous content
	if !opts.DisableSecurityScanner {
		if findings := workflow.ScanMarkdownSecurity(string(content)); len(findings) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Security scan failed for workflow"))
			fmt.Fprintln(os.Stderr, workflow.FormatSecurityFindings(findings, workflowName))
			return nil, fmt.Errorf("workflow '%s' failed security scan: %d issue(s) detected", workflowName, len(findings))
		}
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Security scan passed"))
		}
	} else if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Security scanning disabled"))
	}

	// Create the workflows directory in the temp directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	workflowsDir, err := fileutil.ValidateAbsolutePath(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("invalid workflows directory path: %w", err)
	}
	if err := os.MkdirAll(workflowsDir, constants.DirPermPublic); err != nil {
		return nil, fmt.Errorf("failed to create workflows directory: %w", err)
	}

	// Construct the destination path
	destPath := filepath.Join(workflowsDir, workflowName+".md")
	destPath, err = fileutil.ValidateAbsolutePath(destPath)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	// Append text if provided
	if opts.AppendText != "" {
		contentStr := string(content)
		if !strings.HasSuffix(contentStr, "\n") {
			contentStr += "\n"
		}
		contentStr += "\n" + opts.AppendText
		content = []byte(contentStr)
	}

	// Write the content to the destination
	if err := os.WriteFile(destPath, content, constants.FilePermPublic); err != nil {
		return nil, fmt.Errorf("failed to write workflow to destination: %w", err)
	}

	return &trialWorkflowWriteResult{
		DestPath:     destPath,
		WorkflowsDir: workflowsDir,
	}, nil
}

// modifyWorkflowForTrialMode modifies the workflow to work in trial mode
func modifyWorkflowForTrialMode(tempDir, workflowName, logicalRepoSlug string, verbose bool) error {
	trialRepoLog.Printf("Modifying workflow for trial mode: workflow=%s, logicalRepo=%s", workflowName, logicalRepoSlug)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Modifying workflow for trial mode"))
	}

	// Find the workflow markdown file
	workflowPath := filepath.Join(tempDir, constants.GetWorkflowDir(), workflowName+".md")

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

		// Also replace checkout actions to use the simulated host repo
		// Split content into lines to preserve indentation
		lines := strings.Split(modifiedContent, "\n")

		for i := 0; i < len(lines); i++ {
			m := checkoutActionPattern.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}

			keyIndent := leadingIndentWidth(lines[i])
			// For the inline list-item form ("- uses: actions/checkout@…") the
			// effective indentation for child keys (with:, id:, …) is two columns
			// deeper than the physical dash position.
			if len(m) > 2 && m[2] == "- " {
				keyIndent += 2
			}

			withIndex := -1
			for j := i + 1; j < len(lines); j++ {
				if isCheckoutStepBoundary(lines[j], keyIndent) {
					break
				}
				if isIndentedKeyLine(lines[j], "with", keyIndent) {
					withIndex = j
					break
				}
			}

			if withIndex == -1 {
				indent := strings.Repeat(" ", keyIndent)
				lines = append(lines[:i+1], append([]string{
					indent + "with:",
					fmt.Sprintf("%s  repository: %s", indent, logicalRepoSlug),
				}, lines[i+1:]...)...)
				i += 2
				continue
			}

			withChildIndent := keyIndent + 2
			withBlockEnd := withIndex + 1
			for withBlockEnd < len(lines) {
				if isCheckoutStepBoundary(lines[withBlockEnd], keyIndent) {
					break
				}

				indent := leadingIndentWidth(lines[withBlockEnd])
				if strings.TrimSpace(lines[withBlockEnd]) != "" && indent <= keyIndent {
					break
				}

				if strings.TrimSpace(lines[withBlockEnd]) != "" && indent > keyIndent {
					withChildIndent = indent
					break
				}

				withBlockEnd++
			}

			for withBlockEnd = withIndex + 1; withBlockEnd < len(lines); withBlockEnd++ {
				if isCheckoutStepBoundary(lines[withBlockEnd], keyIndent) {
					break
				}
				if strings.TrimSpace(lines[withBlockEnd]) != "" && leadingIndentWidth(lines[withBlockEnd]) <= keyIndent {
					break
				}
			}

			repositoryIndex := -1
			for j := withIndex + 1; j < withBlockEnd; j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "repository:") {
					repositoryIndex = j
					break
				}
			}

			indent := strings.Repeat(" ", withChildIndent)
			repositoryLine := fmt.Sprintf("%srepository: %s", indent, logicalRepoSlug)

			if repositoryIndex >= 0 {
				lines[repositoryIndex] = repositoryLine
				continue
			}

			lines = append(lines[:withIndex+1], append([]string{repositoryLine}, lines[withIndex+1:]...)...)
			i++
		}

		modifiedContent = strings.Join(lines, "\n")
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping repository simulation modifications (using clone-repo mode)"))
	}

	// Write the modified content back
	if err := os.WriteFile(workflowPath, []byte(modifiedContent), constants.FilePermPublic); err != nil {
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
	if strings.TrimSpace(string(statusOutput)) == "" {
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

	branch, err := getCurrentBranchIn(tempDir)
	if err != nil {
		trialRepoLog.Printf("Failed to detect branch in %s: %v, falling back to main", tempDir, err)
		branch = "main"
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Pulling latest changes from "+branch+" branch"))
	}
	cmd = exec.Command("git", "pull", "--rebase", "origin", branch)
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull latest changes: %w (output: %s)", err, string(output))
	}

	// Push to current branch
	cmd = exec.Command("git", "push", "origin", branch)
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
	originalDir, err := gitutil.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(originalDir)

	// Create temporary directory to clone the source repo
	tempCloneDir := filepath.Join(os.TempDir(), fmt.Sprintf("gh-aw-clone-%x", time.Now().UnixNano()))
	defer os.RemoveAll(tempCloneDir)

	// Clone the source repository
	cloneURL := trialRepositoryGitURL(cloneRepoSlug)

	output, err := workflow.RunGitCombined(fmt.Sprintf("Cloning %s...", cloneRepoSlug), "clone", cloneURL, tempCloneDir)
	if err != nil {
		return fmt.Errorf("failed to clone source repository %s: %w (output: %s)", cloneURL, err, string(output))
	}

	// Change to the cloned repository directory
	if err := os.Chdir(tempCloneDir); err != nil {
		return fmt.Errorf("failed to change to clone directory: %w", err)
	}

	// If a version/tag/SHA is specified, checkout that ref
	if cloneRepoVersion != "" {
		checkoutCmd := exec.Command("git", "checkout", cloneRepoVersion)
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to checkout ref '%s': %w (output: %s)", cloneRepoVersion, err, string(output))
		}
	}

	// Add the host repository as a new remote
	hostURL := trialRepositoryGitURL(hostRepoSlug)
	remoteCmd := exec.Command("git", "remote", "add", "host", hostURL)
	if output, err := remoteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add host remote: %w (output: %s)", err, string(output))
	}

	// Force push the current branch to the host repository's main branch
	pushCmd := exec.Command("git", "push", "--force", "host", "HEAD:main")
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to force push to host repository: %w (output: %s)", err, string(output))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully pushed contents from %s to %s", cloneRepoSlug, hostRepoSlug)))
	}

	return nil
}
