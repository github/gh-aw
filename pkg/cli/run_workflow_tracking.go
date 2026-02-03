package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var runWorkflowTrackingLog = logger.New("cli:run_workflow_tracking")

// WorkflowRunInfo contains information about a workflow run
type WorkflowRunInfo struct {
	URL        string
	DatabaseID int64
	Status     string
	Conclusion string
	CreatedAt  time.Time
}

// getLatestWorkflowRunWithRetry gets information about the most recent run of the specified workflow
// with retry logic to handle timing issues when a workflow has just been triggered
func getLatestWorkflowRunWithRetry(lockFileName string, repo string, verbose bool) (*WorkflowRunInfo, error) {
	runWorkflowTrackingLog.Printf("Getting latest workflow run: workflow=%s, repo=%s, max_retries=6", lockFileName, repo)
	const maxRetries = 6
	const initialDelay = 2 * time.Second
	const maxDelay = 10 * time.Second

	if repo != "" {
		console.LogVerbose(verbose, fmt.Sprintf("Getting latest run for workflow: %s in repo: %s (with retry logic)", lockFileName, repo))
	} else {
		console.LogVerbose(verbose, fmt.Sprintf("Getting latest run for workflow: %s (with retry logic)", lockFileName))
	}

	// Capture the current time before we start polling
	// This helps us identify runs that were created after the workflow was triggered
	startTime := time.Now().UTC()
	runWorkflowTrackingLog.Printf("Start time for polling: %s", startTime.Format(time.RFC3339))

	// Create spinner outside the loop so we can update it
	var spinner *console.SpinnerWrapper
	if !verbose {
		spinner = console.NewSpinner("Waiting for workflow run to appear...")
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate delay with exponential backoff, capped at maxDelay
			delay := time.Duration(attempt) * initialDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			// Calculate elapsed time since start
			elapsed := time.Since(startTime).Round(time.Second)

			console.LogVerbose(verbose, fmt.Sprintf("Waiting %v before retry attempt %d/%d...", delay, attempt+1, maxRetries))

			if !verbose {
				// Show spinner starting from second attempt to avoid flickering
				if attempt == 1 && spinner != nil {
					spinner.Start()
				}
				// Update spinner with progress information
				if spinner != nil {
					spinner.UpdateMessage(fmt.Sprintf("Waiting for workflow run... (attempt %d/%d, %v elapsed)", attempt+1, maxRetries, elapsed))
				}
			}
			time.Sleep(delay)
		}

		// Build command with optional repo parameter
		var cmd *exec.Cmd
		if repo != "" {
			cmd = workflow.ExecGH("run", "list", "--repo", repo, "--workflow", lockFileName, "--limit", "1", "--json", "url,databaseId,status,conclusion,createdAt")
		} else {
			cmd = workflow.ExecGH("run", "list", "--workflow", lockFileName, "--limit", "1", "--json", "url,databaseId,status,conclusion,createdAt")
		}

		output, err := cmd.Output()
		if err != nil {
			lastErr = fmt.Errorf("failed to get workflow runs: %w", err)
			runWorkflowTrackingLog.Printf("Attempt %d/%d failed to get runs: %v", attempt+1, maxRetries, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Attempt %d/%d failed: %v", attempt+1, maxRetries, err)))
			}
			continue
		}

		if len(output) == 0 || string(output) == "[]" {
			lastErr = fmt.Errorf("no runs found for workflow")
			runWorkflowTrackingLog.Printf("Attempt %d/%d: no runs found, output empty or []", attempt+1, maxRetries)
			console.LogVerbose(verbose, fmt.Sprintf("Attempt %d/%d: no runs found yet", attempt+1, maxRetries))
			continue
		}

		// Parse the JSON output
		var runs []struct {
			URL        string `json:"url"`
			DatabaseID int64  `json:"databaseId"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			CreatedAt  string `json:"createdAt"`
		}

		if err := json.Unmarshal(output, &runs); err != nil {
			lastErr = fmt.Errorf("failed to parse workflow run data: %w", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Attempt %d/%d failed to parse JSON: %v", attempt+1, maxRetries, err)))
			}
			continue
		}

		if len(runs) == 0 {
			lastErr = fmt.Errorf("no runs found")
			console.LogVerbose(verbose, fmt.Sprintf("Attempt %d/%d: no runs in parsed JSON", attempt+1, maxRetries))
			continue
		}

		run := runs[0]

		// Parse the creation timestamp
		var createdAt time.Time
		if run.CreatedAt != "" {
			if parsedTime, err := time.Parse(time.RFC3339, run.CreatedAt); err == nil {
				createdAt = parsedTime
			} else if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not parse creation time '%s': %v", run.CreatedAt, err)))
			}
		}

		runInfo := &WorkflowRunInfo{
			URL:        run.URL,
			DatabaseID: run.DatabaseID,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			CreatedAt:  createdAt,
		}

		// If we found a run and it was created after we started (within 30 seconds tolerance),
		// it's likely the run we just triggered
		if !createdAt.IsZero() && createdAt.After(startTime.Add(-30*time.Second)) {
			runWorkflowTrackingLog.Printf("Found matching run: id=%d, created_at=%s, within_tolerance=true", run.DatabaseID, createdAt.Format(time.RFC3339))
			console.LogVerbose(verbose, fmt.Sprintf("Found recent run (ID: %d) created at %v (started polling at %v)",
				run.DatabaseID, createdAt.Format(time.RFC3339), startTime.Format(time.RFC3339)))
			if spinner != nil {
				spinner.StopWithMessage("✓ Found workflow run")
			}
			return runInfo, nil
		}

		if createdAt.IsZero() {
			console.LogVerbose(verbose, fmt.Sprintf("Attempt %d/%d: Found run (ID: %d) but no creation timestamp available", attempt+1, maxRetries, run.DatabaseID))
		} else {
			console.LogVerbose(verbose, fmt.Sprintf("Attempt %d/%d: Found run (ID: %d) but it was created at %v (too old)",
				attempt+1, maxRetries, run.DatabaseID, createdAt.Format(time.RFC3339)))
		}

		// For the first few attempts, if we have a run but it's too old, keep trying
		if attempt < 3 {
			lastErr = fmt.Errorf("workflow run appears to be from a previous execution")
			continue
		}

		// For later attempts, return what we found even if timing is uncertain
		console.LogVerbose(verbose, fmt.Sprintf("Returning workflow run (ID: %d) after %d attempts (timing uncertain)", run.DatabaseID, attempt+1))
		if spinner != nil {
			spinner.StopWithMessage("✓ Found workflow run")
		}
		return runInfo, nil
	}

	// Stop spinner on failure
	if spinner != nil {
		spinner.Stop()
	}

	// If we exhausted all retries, return the last error
	if lastErr != nil {
		return nil, fmt.Errorf("failed to get workflow run after %d attempts: %w", maxRetries, lastErr)
	}

	return nil, fmt.Errorf("no workflow run found after %d attempts", maxRetries)
}
