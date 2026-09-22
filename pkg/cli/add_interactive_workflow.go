package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

// checkStatusAndOfferRun checks if the workflow appears in status and offers to run it
func (c *AddInteractiveConfig) checkStatusAndOfferRun(ctx context.Context) error {
	addInteractiveLog.Print("Checking workflow status and offering to run")

	// Wait a moment for GitHub to process the merge
	workflowFound, err := c.waitForWorkflowStatus(ctx)
	if err != nil {
		return err
	}

	if !workflowFound {
		c.showWorkflowStatusUnavailableInstructions()
		c.showFinalInstructions()
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow is ready"))

	// Only offer to run if workflow has workflow_dispatch trigger
	if !c.shouldOfferAddedWorkflowRun() {
		addInteractiveLog.Print("Workflow does not have workflow_dispatch trigger, skipping run offer")
		c.showFinalInstructions()
		return nil
	}

	// In Codespaces, don't offer to trigger - provide link to Actions page instead
	if isRunningInCodespace() {
		c.showCodespaceRunInstructions()
		c.showFinalInstructions()
		return nil
	}

	runNow, err := confirmRunAddedWorkflow(ctx)
	if err != nil {
		if console.IsCancelled(err) {
			c.showFinalInstructions()
			return nil
		}
		return err
	}

	if !runNow {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected workflow run: later"))
		c.showFinalInstructions()
		return nil
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected workflow run: now"))

	if err := c.runAddedWorkflowOnce(ctx); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Failed to run workflow: %v", err)))
		c.showFinalInstructions()
		return nil
	}

	c.showFinalInstructions()
	return nil
}

func (c *AddInteractiveConfig) waitForWorkflowStatus(ctx context.Context) (bool, error) {
	addInteractiveLog.Printf("Waiting for workflow %q to appear in status (up to 5 attempts)", c.primaryWorkflowName())

	// Use spinner only in non-verbose mode (spinner can't be restarted after stop)
	var spinner *console.SpinnerWrapper
	if !c.Verbose {
		spinner = console.NewSpinner("Waiting for workflow to be available...")
		spinner.Start()
	}

	// Try a few times to see the workflow in status
	var workflowFound bool
	for i := range 5 {
		// Wait 2 seconds before each check (including the first)
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if spinner != nil {
				spinner.Stop()
			}
			return false, ctx.Err()
		case <-timer.C:
			// Continue with check
		}

		found := c.checkWorkflowStatusAttempt(i)
		if found {
			workflowFound = true
			break
		}
	}

	if spinner != nil {
		spinner.Stop()
	}

	addInteractiveLog.Printf("Workflow status wait complete: found=%t", workflowFound)
	return workflowFound, nil
}

func (c *AddInteractiveConfig) checkWorkflowStatusAttempt(attempt int) bool {
	workflowName := c.primaryWorkflowName()
	if workflowName == "" {
		return false
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "Checking workflow status (attempt %d/5) for: %s\n", attempt+1, workflowName)
	}

	// Check if workflow is in status
	statuses, err := findWorkflowsByFilenamePattern(workflowName, c.RepoOverride, c.Verbose)
	if err != nil {
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "Status check error: %v\n", err)
		}
		return false
	}

	if len(statuses) > 0 {
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "Found %d workflow(s) matching pattern\n", len(statuses))
		}
		return true
	}

	if c.Verbose {
		fmt.Fprintln(os.Stderr, "No workflows found matching pattern yet")
	}
	return false
}

func (c *AddInteractiveConfig) showWorkflowStatusUnavailableInstructions() {
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify workflow status."))
	fmt.Fprintf(os.Stderr, "You can check status with: %s status\n", string(constants.CLIExtensionPrefix))
}

func (c *AddInteractiveConfig) shouldOfferAddedWorkflowRun() bool {
	return c.addResult != nil && c.addResult.HasWorkflowDispatch
}

func (c *AddInteractiveConfig) showCodespaceRunInstructions() {
	addInteractiveLog.Print("Running in Codespaces, skipping run offer and showing Actions link")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running in GitHub Codespaces - please trigger the workflow manually from the Actions page"))
	fmt.Fprintf(os.Stderr, "🔗 https://github.com/%s/actions\n", c.RepoOverride)
}

func confirmRunAddedWorkflow(ctx context.Context) (bool, error) {
	// Ask if user wants to run the workflow
	runNow := true // Default to yes
	form := console.NewConfirmForm(
		huh.NewConfirm().
			Title("Would you like to run the workflow once now?").
			Description("This will trigger the workflow immediately").
			Affirmative("Yes, run once now").
			Negative("No, I'll run later").
			Value(&runNow),
	)

	if err := form.RunWithContext(ctx); err != nil {
		return false, err
	}

	return runNow, nil
}

func (c *AddInteractiveConfig) runAddedWorkflowOnce(ctx context.Context) error {
	// Run the workflow interactively (collects inputs if the workflow has them)
	workflowName := c.primaryWorkflowName()
	if workflowName == "" {
		return nil
	}

	c.updateLocalBranchBeforeWorkflowRun()

	if err := RunSpecificWorkflowInteractively(ctx, RunWorkflowOptions{
		WorkflowName:       workflowName,
		Verbose:            c.Verbose,
		EngineOverride:     c.EngineOverride,
		RepoOverride:       c.RepoOverride,
		requiredInputsOnly: true,
	}); err != nil {
		return err
	}

	return nil
}

func (c *AddInteractiveConfig) updateLocalBranchBeforeWorkflowRun() {
	// Pull the merged workflow files now that we know GitHub has processed the
	// merge (workflowFound is true). Doing this here—rather than immediately
	// after the PR merge—avoids a race where git fetch runs before GitHub's git
	// objects have been updated, which caused "workflow file not found" errors.
	var spinner *console.SpinnerWrapper
	if !c.Verbose {
		spinner = console.NewSpinner("Updating local branch...")
		spinner.Start()
	}
	if err := c.updateLocalBranch(); err != nil {
		if spinner != nil {
			spinner.Stop()
		}
		addInteractiveLog.Printf("Failed to update local branch: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not update local branch: %v", err)))
		fmt.Fprintln(os.Stderr, "You may need to switch to your repository's default branch (for example 'main') and run 'git pull' manually before running the workflow.")
		return
	}
	if spinner != nil {
		spinner.StopWithMessage(console.FormatSuccessMessage("Updated local branch"))
	}
}

// findWorkflowsByFilenamePattern is a helper to find workflows registered in GitHub by filename pattern.
// The pattern is matched against the workflow filename (basename without extension)
func findWorkflowsByFilenamePattern(pattern, repoOverride string, verbose bool) ([]WorkflowStatus, error) {
	// This would normally call StatusWorkflows but we need just a simple check
	// For now, we'll use the gh CLI directly
	// Request 'path' field so we can match by filename, not by workflow name
	args := []string{"workflow", "list", "--json", "name,state,path"}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Running: gh %s\n", strings.Join(args, " "))
	}

	output, err := workflow.RunGH("Checking workflow status...", args...)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "gh workflow list failed: %v\n", err)
		}
		return nil, err
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "gh workflow list output: %s\n", string(output))
		fmt.Fprintf(os.Stderr, "Looking for workflow with filename containing: %s\n", pattern)
	}

	// Check if any workflow path contains the pattern
	// The pattern is the workflow name (e.g., "daily-repo-status")
	// The path is like ".github/workflows/daily-repo-status.lock.yml"
	// We check if the path contains the pattern
	if strings.Contains(string(output), pattern+".lock.yml") || strings.Contains(string(output), pattern+".md") {
		if verbose {
			fmt.Fprintf(os.Stderr, "Workflow with filename '%s' found in workflow list\n", pattern)
		}
		addInteractiveLog.Printf("Workflow lookup by filename pattern=%q: found", pattern)
		return []WorkflowStatus{{WorkflowListItem: WorkflowListItem{Workflow: pattern}}}, nil
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Workflow with filename '%s' NOT found in workflow list\n", pattern)
	}
	addInteractiveLog.Printf("Workflow lookup by filename pattern=%q: not found", pattern)
	return nil, nil
}
