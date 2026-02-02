package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

// checkStatusAndOfferRun checks if the workflow appears in status and offers to run it
func (c *AddInteractiveConfig) checkStatusAndOfferRun(ctx context.Context) error {
	addInteractiveLog.Print("Checking workflow status and offering to run")

	// Wait a moment for GitHub to process the merge
	fmt.Fprintln(os.Stderr, "")

	// Use spinner only in non-verbose mode (spinner can't be restarted after stop)
	var spinner *console.SpinnerWrapper
	if !c.Verbose {
		spinner = console.NewSpinner("Waiting for workflow to be available...")
		spinner.Start()
	}

	// Try a few times to see the workflow in status
	var workflowFound bool
	for i := 0; i < 5; i++ {
		// Wait 2 seconds before each check (including the first)
		select {
		case <-ctx.Done():
			if spinner != nil {
				spinner.Stop()
			}
			return ctx.Err()
		case <-time.After(2 * time.Second):
			// Continue with check
		}

		// Use the workflow name from the first spec
		if len(c.WorkflowSpecs) > 0 {
			parsed, _ := parseWorkflowSpec(c.WorkflowSpecs[0])
			if parsed != nil {
				if c.Verbose {
					fmt.Fprintf(os.Stderr, "Checking workflow status (attempt %d/5) for: %s\n", i+1, parsed.WorkflowName)
				}
				// Check if workflow is in status
				statuses, err := getWorkflowStatuses(parsed.WorkflowName, c.RepoOverride, c.Verbose)
				if err != nil {
					if c.Verbose {
						fmt.Fprintf(os.Stderr, "Status check error: %v\n", err)
					}
				} else if len(statuses) > 0 {
					if c.Verbose {
						fmt.Fprintf(os.Stderr, "Found %d workflow(s) matching pattern\n", len(statuses))
					}
					workflowFound = true
					break
				} else if c.Verbose {
					fmt.Fprintln(os.Stderr, "No workflows found matching pattern yet")
				}
			}
		}
	}

	if spinner != nil {
		spinner.Stop()
	}

	if !workflowFound {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify workflow status."))
		fmt.Fprintf(os.Stderr, "You can check status with: %s status\n", string(constants.CLIExtensionPrefix))
		c.showFinalInstructions()
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow is ready"))

	// Only offer to run if workflow has workflow_dispatch trigger
	if c.addResult == nil || !c.addResult.HasWorkflowDispatch {
		addInteractiveLog.Print("Workflow does not have workflow_dispatch trigger, skipping run offer")
		c.showFinalInstructions()
		return nil
	}

	// In Codespaces, don't offer to trigger - provide link to Actions page instead
	if os.Getenv("CODESPACES") == "true" {
		addInteractiveLog.Print("Running in Codespaces, skipping run offer and showing Actions link")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running in GitHub Codespaces - please trigger the workflow manually from the Actions page"))
		fmt.Fprintf(os.Stderr, "🔗 https://github.com/%s/actions\n", c.RepoOverride)
		c.showFinalInstructions()
		return nil
	}

	// Ask if user wants to run the workflow
	fmt.Fprintln(os.Stderr, "")
	runNow := true // Default to yes
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Would you like to run the workflow once now?").
				Description("This will trigger the workflow immediately").
				Affirmative("Yes, run once now").
				Negative("No, I'll run later").
				Value(&runNow),
		),
	).WithAccessible(console.IsAccessibleMode())

	if err := form.Run(); err != nil {
		return nil // Not critical, just skip
	}

	if !runNow {
		c.showFinalInstructions()
		return nil
	}

	// Run the workflow interactively (collects inputs if the workflow has them)
	if len(c.WorkflowSpecs) > 0 {
		parsed, _ := parseWorkflowSpec(c.WorkflowSpecs[0])
		if parsed != nil {
			fmt.Fprintln(os.Stderr, "")

			if err := RunSpecificWorkflowInteractively(ctx, parsed.WorkflowName, c.Verbose, c.EngineOverride, c.RepoOverride, "", false, false, false, false); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Failed to run workflow: %v", err)))
				c.showFinalInstructions()
				return nil
			}

			// Get the run URL for step 10
			runInfo, err := getLatestWorkflowRunWithRetry(parsed.WorkflowName+".lock.yml", c.RepoOverride, c.Verbose)
			if err == nil && runInfo.URL != "" {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow triggered successfully!"))
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintf(os.Stderr, "🔗 View workflow run: %s\n", runInfo.URL)
			}
		}
	}

	c.showFinalInstructions()
	return nil
}

// getWorkflowStatuses is a helper to get workflow statuses for a pattern
// The pattern is matched against the workflow filename (basename without extension)
func getWorkflowStatuses(pattern, repoOverride string, verbose bool) ([]WorkflowStatus, error) {
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
		return []WorkflowStatus{{Workflow: pattern}}, nil
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Workflow with filename '%s' NOT found in workflow list\n", pattern)
	}
	return nil, nil
}
