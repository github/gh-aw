package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var addInteractiveLog = logger.New("cli:add_interactive")

// AddInteractiveConfig holds configuration for interactive add mode
type AddInteractiveConfig struct {
	WorkflowSpecs   []string
	Verbose         bool
	EngineOverride  string
	NoGitattributes bool
	WorkflowDir     string
	NoStopAfter     bool
	StopAfter       string
	SkipWorkflowRun bool
	RepoOverride    string // owner/repo format, if user provides it

	// isPublicRepo tracks whether the target repository is public
	// This is populated by checkGitRepository() when determining the repo
	isPublicRepo bool

	// existingSecrets tracks which secrets already exist in the repository
	// This is populated by checkExistingSecrets() before engine selection
	existingSecrets map[string]bool

	// addResult holds the result from AddWorkflows, including HasWorkflowDispatch
	addResult *AddWorkflowsResult

	// resolvedWorkflows holds the pre-resolved workflow data including descriptions
	// This is populated early in the flow by resolveWorkflows()
	resolvedWorkflows *ResolvedWorkflows
}

// RunAddInteractive runs the interactive add workflow
// This walks the user through adding an agentic workflow to their repository
func RunAddInteractive(ctx context.Context, workflowSpecs []string, verbose bool, engineOverride string, noGitattributes bool, workflowDir string, noStopAfter bool, stopAfter string) error {
	addInteractiveLog.Print("Starting interactive add workflow")

	// Assert this function is not running in automated unit tests or CI
	if os.Getenv("GO_TEST_MODE") == "true" || os.Getenv("CI") != "" {
		return fmt.Errorf("interactive add cannot be used in automated tests or CI environments")
	}

	config := &AddInteractiveConfig{
		WorkflowSpecs:   workflowSpecs,
		Verbose:         verbose,
		EngineOverride:  engineOverride,
		NoGitattributes: noGitattributes,
		WorkflowDir:     workflowDir,
		NoStopAfter:     noStopAfter,
		StopAfter:       stopAfter,
	}

	// Clear the screen for a fresh interactive experience
	console.ClearScreen()

	// Step 1: Welcome message
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "🚀 Welcome to GitHub Agentic Workflows!")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "This tool will walk you through adding an automated workflow to your repository.")
	fmt.Fprintln(os.Stderr, "")

	// Step 1b: Resolve workflows early to get descriptions and validate specs
	if err := config.resolveWorkflows(); err != nil {
		return err
	}

	// Step 1c: Show workflow descriptions if available
	config.showWorkflowDescriptions()

	// Step 2: Check gh auth status
	if err := config.checkGHAuthStatus(); err != nil {
		return err
	}

	// Step 3: Check git repository and get org/repo
	if err := config.checkGitRepository(); err != nil {
		return err
	}

	// Step 4: Check GitHub Actions is enabled
	if err := config.checkActionsEnabled(); err != nil {
		return err
	}

	// Step 5: Check user permissions
	if err := config.checkUserPermissions(); err != nil {
		return err
	}

	// Step 6: Select coding agent and collect API key
	if err := config.selectAIEngineAndKey(); err != nil {
		return err
	}

	// Step 7: Determine files to add
	filesToAdd, initFiles, err := config.determineFilesToAdd()
	if err != nil {
		return err
	}

	// Step 8: Confirm with user
	secretName, secretValue, err := config.getSecretInfo()
	if err != nil {
		return err
	}

	if err := config.confirmChanges(filesToAdd, initFiles, secretName, secretValue); err != nil {
		return err
	}

	// Step 9: Apply changes (create PR, merge, add secret)
	if err := config.applyChanges(ctx, filesToAdd, initFiles, secretName, secretValue); err != nil {
		return err
	}

	// Step 10: Check status and offer to run
	if err := config.checkStatusAndOfferRun(ctx); err != nil {
		return err
	}

	return nil
}

// resolveWorkflows resolves workflow specifications by installing repositories,
// expanding wildcards, and fetching workflow content (including descriptions).
// This is called early to show workflow information before the user commits to adding them.
func (c *AddInteractiveConfig) resolveWorkflows() error {
	addInteractiveLog.Print("Resolving workflows early for description display")

	resolved, err := ResolveWorkflows(c.WorkflowSpecs, c.Verbose)
	if err != nil {
		return fmt.Errorf("failed to resolve workflows: %w", err)
	}

	c.resolvedWorkflows = resolved
	return nil
}

// showWorkflowDescriptions displays the descriptions of resolved workflows
func (c *AddInteractiveConfig) showWorkflowDescriptions() {
	if c.resolvedWorkflows == nil || len(c.resolvedWorkflows.Workflows) == 0 {
		return
	}

	// Show descriptions for all workflows that have one
	for _, rw := range c.resolvedWorkflows.Workflows {
		if rw.Description != "" {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(rw.Description))
			fmt.Fprintln(os.Stderr, "")
		}
	}
}

// determineFilesToAdd determines which files will be added
func (c *AddInteractiveConfig) determineFilesToAdd() (workflowFiles []string, initFiles []string, err error) {
	addInteractiveLog.Print("Determining files to add")

	// Parse the workflow specs to get the files that will be added
	// This reuses logic from addWorkflowsNormal to determine what files get created
	for _, spec := range c.WorkflowSpecs {
		parsed, parseErr := parseWorkflowSpec(spec)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("invalid workflow specification '%s': %w", spec, parseErr)
		}
		workflowFiles = append(workflowFiles, parsed.WorkflowName+".md")
		workflowFiles = append(workflowFiles, parsed.WorkflowName+".lock.yml")
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The following workflow files will be added:")
	for _, f := range workflowFiles {
		fmt.Fprintf(os.Stderr, "  • .github/workflows/%s\n", f)
	}

	return workflowFiles, initFiles, nil
}

// confirmChanges asks the user to confirm the changes
// secretValue is empty if the secret already exists in the repository
func (c *AddInteractiveConfig) confirmChanges(workflowFiles, initFiles []string, secretName string, secretValue string) error {
	addInteractiveLog.Print("Confirming changes with user")

	fmt.Fprintln(os.Stderr, "")

	confirmed := true // Default to yes
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you want to proceed with these changes?").
				Description("A pull request will be created and merged automatically").
				Affirmative("Yes, create and merge").
				Negative("No, cancel").
				Value(&confirmed),
		),
	).WithAccessible(console.IsAccessibleMode())

	if err := form.Run(); err != nil {
		return fmt.Errorf("confirmation failed: %w", err)
	}

	if !confirmed {
		fmt.Fprintln(os.Stderr, "Operation cancelled.")
		return fmt.Errorf("user cancelled the operation")
	}

	return nil
}

// showFinalInstructions shows final instructions to the user
func (c *AddInteractiveConfig) showFinalInstructions() {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("🎉 Addition complete!"))
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, "")

	// Show summary with workflow name(s)
	if c.resolvedWorkflows != nil && len(c.resolvedWorkflows.Workflows) > 0 {
		wf := c.resolvedWorkflows.Workflows[0]
		fmt.Fprintf(os.Stderr, "The workflow '%s' has been added to the repository and will now run automatically.\n", wf.Spec.WorkflowName)
		c.showWorkflowDescriptions()
	}

	fmt.Fprintln(os.Stderr, "Useful commands:")
	fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("  %s status          # Check workflow status", string(constants.CLIExtensionPrefix))))
	fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("  %s run <workflow>  # Trigger a workflow", string(constants.CLIExtensionPrefix))))
	fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("  %s logs            # View workflow logs", string(constants.CLIExtensionPrefix))))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Learn more at: https://github.github.com/gh-aw/")
	fmt.Fprintln(os.Stderr, "")
}
