package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/spf13/cobra"
)

var addLog = logger.New("cli:add_command")

// AddWorkflowsResult contains the result of adding workflows
type AddWorkflowsResult struct {
	// PRNumber is the PR number if a PR was created, or 0 if no PR was created
	PRNumber int
	// PRURL is the URL of the created PR, or empty if no PR was created
	PRURL string
	// HasWorkflowDispatch is true if any of the added workflows has a workflow_dispatch trigger
	HasWorkflowDispatch bool
}

// NewAddCommand creates the add command
func NewAddCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <workflow>...",
		Aliases: []string{"add-wizard"},
		Short:   "Add agentic workflows from repositories to .github/workflows",
		Long: `Add one or more workflows from repositories to .github/workflows.

By default, this command runs in interactive mode, which guides you through:
  - Selecting an AI engine (Copilot, Claude, or Codex)
  - Configuring API keys and secrets
  - Creating a pull request with the workflow
  - Optionally running the workflow

Use --non-interactive to skip the guided setup and add workflows directly.

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/daily-repo-status        # Interactive setup (recommended)
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics                           # List available workflows
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --non-interactive  # Skip interactive mode
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor@v1.0.0         # Add with version
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/workflows/ci-doctor.md@main
  ` + string(constants.CLIExtensionPrefix) + ` add https://github.com/githubnext/agentics/blob/main/workflows/ci-doctor.md
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --create-pull-request --force
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --push         # Add and push changes
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/*
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/*@v1.0.0
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --dir shared   # Add to .github/workflows/shared/

Workflow specifications:
  - Two parts: "owner/repo[@version]" (lists available workflows in the repository)
  - Three parts: "owner/repo/workflow-name[@version]" (implicitly looks in workflows/ directory)
  - Four+ parts: "owner/repo/workflows/workflow-name.md[@version]" (requires explicit .md extension)
  - GitHub URL: "https://github.com/owner/repo/blob/branch/path/to/workflow.md"
  - Wildcard: "owner/repo/*[@version]" (adds all workflows from the repository)
  - Version can be tag, branch, or SHA

The -n flag allows you to specify a custom name for the workflow file (only applies to the first workflow when adding multiple).
The --dir flag allows you to specify a subdirectory under .github/workflows/ where the workflow will be added.
The --create-pull-request flag (or --pr) automatically creates a pull request with the workflow changes.
The --push flag automatically commits and pushes changes after successful workflow addition.
The --force flag overwrites existing workflow files.
The --non-interactive flag skips the guided setup and uses traditional behavior.

Note: To create a new workflow from scratch, use the 'new' command instead.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflows := args
			numberFlag, _ := cmd.Flags().GetInt("number")
			engineOverride, _ := cmd.Flags().GetString("engine")
			nameFlag, _ := cmd.Flags().GetString("name")
			createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
			prFlagAlias, _ := cmd.Flags().GetBool("pr")
			prFlag := createPRFlag || prFlagAlias // Support both --create-pull-request and --pr
			pushFlag, _ := cmd.Flags().GetBool("push")
			forceFlag, _ := cmd.Flags().GetBool("force")
			appendText, _ := cmd.Flags().GetString("append")
			verbose, _ := cmd.Flags().GetBool("verbose")
			noGitattributes, _ := cmd.Flags().GetBool("no-gitattributes")
			workflowDir, _ := cmd.Flags().GetString("dir")
			noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
			stopAfter, _ := cmd.Flags().GetString("stop-after")
			nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

			if err := validateEngine(engineOverride); err != nil {
				return err
			}

			// Determine if we should use interactive mode
			// Interactive mode is the default for TTY unless:
			// - --non-interactive flag is set
			// - Any of the batch/automation flags are set (--create-pull-request, --force, --name, --number > 1, --append)
			// - Not a TTY (piped input/output)
			// - In CI environment
			// - This is a repo-only spec (listing workflows)
			useInteractive := !nonInteractive &&
				!prFlag &&
				!forceFlag &&
				nameFlag == "" &&
				numberFlag == 1 &&
				appendText == "" &&
				tty.IsStdoutTerminal() &&
				os.Getenv("CI") == "" &&
				os.Getenv("GO_TEST_MODE") != "true" &&
				!isRepoOnlySpec(workflows[0])

			if useInteractive {
				addLog.Print("Using interactive mode")
				return RunAddInteractive(cmd.Context(), workflows, verbose, engineOverride, noGitattributes, workflowDir, noStopAfter, stopAfter)
			}

			// Handle normal (non-interactive) mode
			_, err := AddWorkflows(workflows, numberFlag, verbose, engineOverride, nameFlag, forceFlag, appendText, prFlag, pushFlag, noGitattributes, workflowDir, noStopAfter, stopAfter)
			return err
		},
	}

	// Add number flag to add command
	cmd.Flags().Int("number", 1, "Create multiple numbered copies")

	// Add name flag to add command
	cmd.Flags().StringP("name", "n", "", "Specify name for the added workflow (without .md extension)")

	// Add AI flag to add command
	addEngineFlag(cmd)

	// Add repository flag to add command
	cmd.Flags().StringP("repo", "r", "", "Source repository containing workflows (owner/repo format)")

	// Add PR flag to add command (--create-pull-request with --pr as alias)
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request with the workflow changes")
	cmd.Flags().Bool("pr", false, "Alias for --create-pull-request")
	_ = cmd.Flags().MarkHidden("pr") // Hide the short alias from help output

	// Add push flag to add command
	cmd.Flags().Bool("push", false, "Automatically commit and push changes after successful workflow addition")

	// Add force flag to add command
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing workflow files without confirmation")

	// Add append flag to add command
	cmd.Flags().String("append", "", "Append extra content to the end of agentic workflow on installation")

	// Add no-gitattributes flag to add command
	cmd.Flags().Bool("no-gitattributes", false, "Skip updating .gitattributes file")

	// Add workflow directory flag to add command
	cmd.Flags().StringP("dir", "d", "", "Subdirectory under .github/workflows/ (e.g., 'shared' creates .github/workflows/shared/)")

	// Add no-stop-after flag to add command
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")

	// Add stop-after flag to add command
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")

	// Add non-interactive flag to add command
	cmd.Flags().Bool("non-interactive", false, "Skip interactive setup and use traditional behavior (for CI/automation)")

	// Register completions for add command
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// AddWorkflows adds one or more workflows from components to .github/workflows
// with optional repository installation and PR creation.
// Returns AddWorkflowsResult containing PR number (if created) and other metadata.
func AddWorkflows(workflows []string, number int, verbose bool, engineOverride string, name string, force bool, appendText string, createPR bool, push bool, noGitattributes bool, workflowDir string, noStopAfter bool, stopAfter string) (*AddWorkflowsResult, error) {
	// Check if this is a repo-only specification (owner/repo instead of owner/repo/workflow)
	// If so, list available workflows and exit
	if len(workflows) == 1 && isRepoOnlySpec(workflows[0]) {
		return &AddWorkflowsResult{}, handleRepoOnlySpec(workflows[0], verbose)
	}

	// Resolve workflows first
	resolved, err := ResolveWorkflows(workflows, verbose)
	if err != nil {
		return nil, err
	}

	return AddResolvedWorkflows(workflows, resolved, number, verbose, false, engineOverride, name, force, appendText, createPR, push, noGitattributes, workflowDir, noStopAfter, stopAfter)
}

// AddResolvedWorkflows adds workflows using pre-resolved workflow data.
// This allows callers to resolve workflows early (e.g., to show descriptions) and then add them later.
// The quiet parameter suppresses detailed output (useful for interactive mode where output is already shown).
func AddResolvedWorkflows(workflowStrings []string, resolved *ResolvedWorkflows, number int, verbose bool, quiet bool, engineOverride string, name string, force bool, appendText string, createPR bool, push bool, noGitattributes bool, workflowDir string, noStopAfter bool, stopAfter string) (*AddWorkflowsResult, error) {
	addLog.Printf("Adding workflows: count=%d, engineOverride=%s, createPR=%v, noGitattributes=%v, workflowDir=%s, noStopAfter=%v, stopAfter=%s", len(workflowStrings), engineOverride, createPR, noGitattributes, workflowDir, noStopAfter, stopAfter)

	result := &AddWorkflowsResult{}

	// If creating a PR, check prerequisites
	if createPR {
		// Check if GitHub CLI is available
		if !isGHCLIAvailable() {
			return nil, fmt.Errorf("GitHub CLI (gh) is required for PR creation but not available")
		}

		// Check if we're in a git repository
		if !isGitRepo() {
			return nil, fmt.Errorf("not in a git repository - PR creation requires a git repository")
		}

		// Check no other changes are present
		if err := checkCleanWorkingDirectory(verbose); err != nil {
			return nil, fmt.Errorf("working directory is not clean: %w", err)
		}
	}

	// Extract the workflow specs for processing
	processedWorkflows := make([]*WorkflowSpec, len(resolved.Workflows))
	for i, rw := range resolved.Workflows {
		processedWorkflows[i] = rw.Spec
	}

	// Set workflow_dispatch result
	result.HasWorkflowDispatch = resolved.HasWorkflowDispatch

	// Handle PR creation workflow
	if createPR {
		addLog.Print("Creating workflow with PR")
		prNumber, prURL, err := addWorkflowsWithPR(processedWorkflows, number, verbose, quiet, engineOverride, name, force, appendText, push, noGitattributes, resolved.HasWildcard, workflowDir, noStopAfter, stopAfter)
		if err != nil {
			return nil, err
		}
		result.PRNumber = prNumber
		result.PRURL = prURL
		return result, nil
	}

	// Handle normal workflow addition
	addLog.Print("Adding workflows normally without PR")
	return result, addWorkflowsNormal(processedWorkflows, number, verbose, quiet, engineOverride, name, force, appendText, push, noGitattributes, resolved.HasWildcard, workflowDir, noStopAfter, stopAfter)
}

// addWorkflowsNormal handles normal workflow addition without PR creation
func addWorkflowsNormal(workflows []*WorkflowSpec, number int, verbose bool, quiet bool, engineOverride string, name string, force bool, appendText string, push bool, noGitattributes bool, fromWildcard bool, workflowDir string, noStopAfter bool, stopAfter string) error {
	// Create file tracker for all operations
	tracker, err := NewFileTracker()
	if err != nil {
		// If we can't create a tracker (e.g., not in git repo), fall back to non-tracking behavior
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not create file tracker: %v", err)))
		}
		tracker = nil
	}

	// Ensure .gitattributes is configured unless flag is set
	if !noGitattributes {
		addLog.Print("Configuring .gitattributes")
		if err := ensureGitAttributes(); err != nil {
			addLog.Printf("Failed to configure .gitattributes: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
			// Don't fail the entire operation if gitattributes update fails
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured .gitattributes"))
		}
	}

	if !quiet && len(workflows) > 1 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding %d workflow(s)...", len(workflows))))
	}

	// Add each workflow
	for i, workflow := range workflows {
		if !quiet && len(workflows) > 1 {
			fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Adding workflow %d/%d: %s", i+1, len(workflows), workflow.WorkflowName)))
		}

		// For multiple workflows, only use the name flag for the first one
		currentName := ""
		if i == 0 && name != "" {
			currentName = name
		}

		if err := addWorkflowWithTracking(workflow, number, verbose, quiet, engineOverride, currentName, force, appendText, tracker, fromWildcard, workflowDir, noStopAfter, stopAfter); err != nil {
			return fmt.Errorf("failed to add workflow '%s': %w", workflow.String(), err)
		}
	}

	if !quiet && len(workflows) > 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully added all %d workflows", len(workflows))))
	}

	// If --push is enabled, commit and push changes
	if push {
		addLog.Print("Push enabled - preparing to commit and push changes")
		fmt.Fprintln(os.Stderr, "")

		// Check if we're on the default branch
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking current branch..."))
		if err := checkOnDefaultBranch(verbose); err != nil {
			addLog.Printf("Default branch check failed: %v", err)
			return fmt.Errorf("cannot push: %w", err)
		}

		// Confirm with user (skip in CI)
		if err := confirmPushOperation(verbose); err != nil {
			addLog.Printf("Push operation not confirmed: %v", err)
			return fmt.Errorf("push operation cancelled: %w", err)
		}

		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Preparing to commit and push changes..."))

		// Create commit message
		var commitMessage string
		if len(workflows) == 1 {
			commitMessage = fmt.Sprintf("chore: add workflow %s", workflows[0].WorkflowName)
		} else {
			commitMessage = fmt.Sprintf("chore: add %d workflows", len(workflows))
		}

		// Use the helper function to orchestrate the full workflow
		if err := commitAndPushChanges(commitMessage, verbose); err != nil {
			// Check if it's the "no changes" case
			hasChanges, checkErr := hasChangesToCommit()
			if checkErr == nil && !hasChanges {
				addLog.Print("No changes to commit")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No changes to commit"))
			} else {
				return err
			}
		} else {
			// Print success messages based on whether remote exists
			fmt.Fprintln(os.Stderr, "")
			if hasRemote() {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes pushed to remote"))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes committed locally (no remote configured)"))
			}
		}
	}

	return nil
}

// addWorkflowWithTracking adds a workflow from components to .github/workflows with file tracking
func addWorkflowWithTracking(workflow *WorkflowSpec, number int, verbose bool, quiet bool, engineOverride string, name string, force bool, appendText string, tracker *FileTracker, fromWildcard bool, workflowDir string, noStopAfter bool, stopAfter string) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding workflow: %s", workflow.String())))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Number of copies: %d", number)))
		if force {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Force flag enabled: will overwrite existing files"))
		}
	}

	// Validate number of copies
	if number < 1 {
		return fmt.Errorf("number of copies must be a positive integer")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, "Locating workflow components...")
	}

	workflowPath := workflow.WorkflowPath

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Looking for workflow file: %s", workflowPath)))
	}

	// Try to read the workflow content from multiple sources
	sourceContent, sourceInfo, err := findWorkflowInPackageForRepo(workflow, verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Workflow '%s' not found.", workflowPath)))

		// Try to list available workflows from the installed package
		if err := displayAvailableWorkflows(workflow.RepoSlug, workflow.Version, verbose); err != nil {
			// If we can't list workflows, provide generic help
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To add workflows to your project:"))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Use the 'add' command with repository/workflow specifications:"))
			fmt.Fprintf(os.Stderr, "  %s add owner/repo/workflow-name\n", string(constants.CLIExtensionPrefix))
			fmt.Fprintf(os.Stderr, "  %s add owner/repo/workflow-name@version\n", string(constants.CLIExtensionPrefix))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Example:"))
			fmt.Fprintf(os.Stderr, "  %s add githubnext/agentics/ci-doctor\n", string(constants.CLIExtensionPrefix))
			fmt.Fprintf(os.Stderr, "  %s add githubnext/agentics/daily-plan@main\n", string(constants.CLIExtensionPrefix))
		}

		return fmt.Errorf("workflow not found: %s", workflowPath)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Read workflow content (%d bytes)", len(sourceContent))))
	}

	// Find git root to ensure consistent placement
	gitRoot, err := findGitRoot()
	if err != nil {
		return fmt.Errorf("add workflow requires being in a git repository: %w", err)
	}

	// Determine the target workflow directory
	var githubWorkflowsDir string
	if workflowDir != "" {
		// Validate that the path is relative
		if filepath.IsAbs(workflowDir) {
			return fmt.Errorf("workflow directory must be a relative path, got: %s", workflowDir)
		}
		// Clean the path to avoid issues with ".." or other problematic elements
		workflowDir = filepath.Clean(workflowDir)
		// Ensure the path is under .github/workflows
		if !strings.HasPrefix(workflowDir, ".github/workflows") {
			// If user provided a subdirectory name, prepend .github/workflows/
			githubWorkflowsDir = filepath.Join(gitRoot, ".github/workflows", workflowDir)
		} else {
			githubWorkflowsDir = filepath.Join(gitRoot, workflowDir)
		}
	} else {
		// Use default .github/workflows directory
		githubWorkflowsDir = filepath.Join(gitRoot, ".github/workflows")
	}

	// Ensure the target directory exists
	if err := os.MkdirAll(githubWorkflowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflow directory %s: %w", githubWorkflowsDir, err)
	}

	// Determine the workflowName to use
	var workflowName string
	if name != "" {
		// Use the explicitly provided name
		workflowName = name
	} else {
		// Extract filename from workflow path and remove .md extension for processing
		workflowName = workflow.WorkflowName
	}

	// Check if a workflow with this name already exists
	existingFile := filepath.Join(githubWorkflowsDir, workflowName+".md")
	if _, err := os.Stat(existingFile); err == nil && !force {
		// When adding with wildcard, emit warning and skip instead of error
		if fromWildcard {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow '%s' already exists in .github/workflows/. Skipping.", workflowName)))
			return nil
		}
		return fmt.Errorf("workflow '%s' already exists in .github/workflows/. Use a different name with -n flag, remove the existing workflow first, or use --force to overwrite", workflowName)
	}

	// Collect all @include dependencies from the workflow file
	includeDeps, err := collectPackageIncludeDependencies(string(sourceContent), sourceInfo.PackagePath, verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to collect include dependencies: %v", err)))
	}

	// Copy all @include dependencies to .github/workflows maintaining relative paths
	if err := copyIncludeDependenciesFromPackageWithForce(includeDeps, githubWorkflowsDir, verbose, force, tracker); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to copy include dependencies: %v", err)))
	}

	// Process each copy
	for i := 1; i <= number; i++ {
		// Construct the destination file path with numbering in .github/workflows
		var destFile string
		if number == 1 {
			destFile = filepath.Join(githubWorkflowsDir, workflowName+".md")
		} else {
			destFile = filepath.Join(githubWorkflowsDir, fmt.Sprintf("%s-%d.md", workflowName, i))
		}

		// Check if destination file already exists
		fileExists := false
		if _, err := os.Stat(destFile); err == nil {
			fileExists = true
			if !force {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Destination file '%s' already exists, skipping.", destFile)))
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Overwriting existing file: %s", destFile)))
		}

		// Process content for numbered workflows
		content := string(sourceContent)
		if number > 1 {
			// Update H1 title to include number
			content = updateWorkflowTitle(content, i)
		}

		// Add source field to frontmatter
		sourceString := buildSourceStringWithCommitSHA(workflow, sourceInfo.CommitSHA)
		if sourceString != "" {
			updatedContent, err := addSourceToWorkflow(content, sourceString)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to add source field: %v", err)))
				}
			} else {
				content = updatedContent
			}

			// Process imports field and replace with workflowspec
			processedImportsContent, err := processImportsWithWorkflowSpec(content, workflow, sourceInfo.CommitSHA, verbose)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process imports: %v", err)))
				}
			} else {
				content = processedImportsContent
			}

			// Process @include directives and replace with workflowspec
			processedContent, err := processIncludesWithWorkflowSpec(content, workflow, sourceInfo.CommitSHA, sourceInfo.PackagePath, verbose)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process includes: %v", err)))
				}
			} else {
				content = processedContent
			}
		}

		// Handle stop-after field modifications
		if noStopAfter {
			// Remove stop-after field if requested
			cleanedContent, err := RemoveFieldFromOnTrigger(content, "stop-after")
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove stop-after field: %v", err)))
				}
			} else {
				content = cleanedContent
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed stop-after field from workflow"))
				}
			}
		} else if stopAfter != "" {
			// Set custom stop-after value if provided
			updatedContent, err := SetFieldInOnTrigger(content, "stop-after", stopAfter)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to set stop-after field: %v", err)))
				}
			} else {
				content = updatedContent
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Set stop-after field to: %s", stopAfter)))
				}
			}
		}

		// Append text if provided
		if appendText != "" {
			// Ensure we have a newline before appending
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += "\n" + appendText
		}

		// Track the file based on whether it existed before (if tracker is available)
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(destFile)
			} else {
				tracker.TrackCreated(destFile)
			}
		}

		// Write the file with restrictive permissions (0600) to follow security best practices
		if err := os.WriteFile(destFile, []byte(content), 0600); err != nil {
			return fmt.Errorf("failed to write destination file '%s': %w", destFile, err)
		}

		// Show detailed output only when not in quiet mode
		if !quiet {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Added workflow: %s", destFile)))

			// Extract and display description if present
			if description := ExtractWorkflowDescription(content); description != "" {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(description))
				fmt.Fprintln(os.Stderr, "")
			}
		}

		// Try to compile the workflow and track generated files
		if tracker != nil {
			if err := compileWorkflowWithTracking(destFile, verbose, quiet, engineOverride, tracker); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
			}
		} else {
			// Fall back to basic compilation without tracking
			if err := compileWorkflow(destFile, verbose, quiet, engineOverride); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
			}
		}
	}

	// Stage tracked files to git if in a git repository
	if isGitRepo() && tracker != nil {
		if err := tracker.StageAllFiles(verbose); err != nil {
			return fmt.Errorf("failed to stage workflow files: %w", err)
		}
	}

	return nil
}
