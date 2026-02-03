package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var repositoryLog = logger.New("cli:add_workflow_repository")

// handleRepoOnlySpec handles the case when user provides only owner/repo without workflow name.
// It installs the package and lists available workflows with interactive selection.
func handleRepoOnlySpec(repoSpec string, verbose bool) error {
	repositoryLog.Printf("Handling repo-only specification: %s", repoSpec)

	// Parse the repository specification to extract repo slug and version
	spec, err := parseRepoSpec(repoSpec)
	if err != nil {
		return fmt.Errorf("invalid repository specification '%s': %w", repoSpec, err)
	}

	// Install the repository
	repoWithVersion := spec.RepoSlug
	if spec.Version != "" {
		repoWithVersion = fmt.Sprintf("%s@%s", spec.RepoSlug, spec.Version)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing repository %s...", repoWithVersion)))
	}

	if err := InstallPackage(repoWithVersion, verbose); err != nil {
		return fmt.Errorf("failed to install repository %s: %w", repoWithVersion, err)
	}

	// List workflows in the installed package with metadata
	workflows, err := listWorkflowsWithMetadata(spec.RepoSlug, verbose)
	if err != nil {
		return fmt.Errorf("failed to list workflows in %s: %w", spec.RepoSlug, err)
	}

	// Display the list of available workflows
	if len(workflows) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No workflows found in repository %s", spec.RepoSlug)))
		return nil
	}

	// Try interactive selection first
	selected, err := showInteractiveWorkflowSelection(spec.RepoSlug, workflows, spec.Version, verbose)
	if err == nil && selected != "" {
		// User selected a workflow, proceed to add it
		repositoryLog.Printf("User selected workflow: %s", selected)
		return nil // Successfully displayed and allowed selection
	}

	// If interactive selection failed or was cancelled, fall back to table display
	repositoryLog.Printf("Interactive selection failed or cancelled, showing table: %v", err)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Available workflows in %s:", spec.RepoSlug)))
	fmt.Fprintln(os.Stderr, "")

	// Render workflows as a table using console helpers
	fmt.Fprint(os.Stderr, console.RenderStruct(workflows))

	fmt.Fprintln(os.Stderr, "Example:")
	fmt.Fprintln(os.Stderr, "")

	// Show example with first workflow
	exampleSpec := fmt.Sprintf("%s/%s", spec.RepoSlug, workflows[0].ID)
	if spec.Version != "" {
		exampleSpec += "@" + spec.Version
	}

	fmt.Fprintf(os.Stderr, "  %s add %s\n", string(constants.CLIExtensionPrefix), exampleSpec)
	fmt.Fprintln(os.Stderr, "")

	return nil
}

// showInteractiveWorkflowSelection displays an interactive list of workflows
// and allows the user to select one.
func showInteractiveWorkflowSelection(repoSlug string, workflows []WorkflowInfo, version string, verbose bool) (string, error) {
	repositoryLog.Printf("Showing interactive workflow selection: repo=%s, workflows=%d", repoSlug, len(workflows))

	// Convert WorkflowInfo to ListItems
	items := make([]console.ListItem, len(workflows))
	for i, wf := range workflows {
		items[i] = console.NewListItem(wf.Name, wf.Description, wf.ID)
	}

	// Show interactive list
	title := fmt.Sprintf("Select a workflow from %s:", repoSlug)
	selectedID, err := console.ShowInteractiveList(title, items)
	if err != nil {
		return "", err
	}

	// Build the workflow spec
	workflowSpec := fmt.Sprintf("%s/%s", repoSlug, selectedID)
	if version != "" {
		workflowSpec += "@" + version
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To add this workflow, run:"))
	fmt.Fprintf(os.Stderr, "  %s add %s\n", string(constants.CLIExtensionPrefix), workflowSpec)
	fmt.Fprintln(os.Stderr, "")

	return selectedID, nil
}

// displayAvailableWorkflows lists available workflows from an installed package
// with interactive selection when in TTY mode.
func displayAvailableWorkflows(repoSlug, version string, verbose bool) error {
	repositoryLog.Printf("Displaying available workflows for repository: %s", repoSlug)

	// List workflows in the installed package with metadata
	workflows, err := listWorkflowsWithMetadata(repoSlug, verbose)
	if err != nil {
		return fmt.Errorf("failed to list workflows in %s: %w", repoSlug, err)
	}

	// Display the list of available workflows
	if len(workflows) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No workflows found in repository %s", repoSlug)))
		return nil
	}

	// Try interactive selection first
	_, err = showInteractiveWorkflowSelection(repoSlug, workflows, version, verbose)
	if err == nil {
		// Successfully displayed and allowed selection
		return nil
	}

	// If interactive selection failed or was cancelled, fall back to table display
	repositoryLog.Printf("Interactive selection failed or cancelled, showing table: %v", err)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Available workflows in %s:", repoSlug)))
	fmt.Fprintln(os.Stderr, "")

	// Render workflows as a table using console helpers
	fmt.Fprint(os.Stderr, console.RenderStruct(workflows))

	fmt.Fprintln(os.Stderr, "Example:")
	fmt.Fprintln(os.Stderr, "")

	// Show example with first workflow
	exampleSpec := fmt.Sprintf("%s/%s", repoSlug, workflows[0].ID)
	if version != "" {
		exampleSpec += "@" + version
	}

	fmt.Fprintf(os.Stderr, "  %s add %s\n", string(constants.CLIExtensionPrefix), exampleSpec)
	fmt.Fprintln(os.Stderr, "")

	return nil
}
