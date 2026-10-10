package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var prAutomergeLog = logger.New("cli:pr_automerge")

// AutoMergePullRequestsCreatedAfter checks for open PRs in the repository created after a specific time and auto-merges them
// This function filters PRs to only those created after the specified time to avoid merging unrelated PRs
func AutoMergePullRequestsCreatedAfter(repoSlug string, createdAfter time.Time, verbose bool) error {
	prAutomergeLog.Printf("Checking for PRs in repo=%s created after %s", repoSlug, createdAfter.Format(time.RFC3339))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checking for open pull requests in %s created after %s", repoSlug, createdAfter.Format(time.RFC3339))))
	}

	// List open PRs with creation time information
	output, err := workflow.RunGH("Listing pull requests...", "pr", "list", "--repo", repoSlug, "--json", "number,title,isDraft,mergeable,createdAt,updatedAt")
	if err != nil {
		prAutomergeLog.Printf("Failed to list pull requests: %v", err)
		return fmt.Errorf("could not list pull requests; ensure required prerequisites are configured, then retry: %w", err)
	}

	var prs []PullRequest
	if err := json.Unmarshal(output, &prs); err != nil {
		return fmt.Errorf("could not parse pull request list; the GitHub API response may be malformed or unexpected: %w", err)
	}

	if len(prs) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No open pull requests found"))
		}
		return nil
	}

	// Filter PRs to only those created after the specified time
	var eligiblePRs []PullRequest
	for _, pr := range prs {
		if pr.CreatedAt.After(createdAfter) {
			eligiblePRs = append(eligiblePRs, pr)
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Skipping PR #%d: created at %s (before workflow start time)", pr.Number, pr.CreatedAt.Format(time.RFC3339))))
		}
	}

	if len(eligiblePRs) == 0 {
		prAutomergeLog.Print("No eligible PRs found for auto-merge")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No pull requests found created after "+createdAfter.Format(time.RFC3339)))
		}
		return nil
	}

	prAutomergeLog.Printf("Found %d eligible PRs for auto-merge", len(eligiblePRs))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d pull request(s) created after workflow start time", len(eligiblePRs))))

	for _, pr := range eligiblePRs {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing PR #%d: %s (draft: %t, mergeable: %s, created: %s)",
				pr.Number, pr.Title, pr.IsDraft, pr.Mergeable, pr.CreatedAt.Format(time.RFC3339))))
		}

		// Convert from draft to non-draft if necessary
		if pr.IsDraft {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Converting PR #%d from draft to ready for review", pr.Number)))
			if output, err := workflow.RunGHCombined("Converting draft to ready...", "pr", "ready", strconv.Itoa(pr.Number), "--repo", repoSlug); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to convert PR #%d from draft: %v (output: %s)", pr.Number, err, string(output))))
				continue
			}
		}

		// Check if PR is mergeable
		if pr.Mergeable != "MERGEABLE" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("PR #%d is not mergeable (status: %s), skipping auto-merge", pr.Number, pr.Mergeable)))
			continue
		}

		// Auto-merge the PR
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auto-merging PR #%d", pr.Number)))
		if output, err := workflow.RunGHCombined("Auto-merging pull request...", "pr", "merge", strconv.Itoa(pr.Number), "--repo", repoSlug, "--auto", "--squash"); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to auto-merge PR #%d: %v (output: %s)", pr.Number, err, string(output))))
			continue
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully enabled auto-merge for PR #%d", pr.Number)))
	}

	return nil
}

// AutoMergePullRequestsLegacy is the legacy function that auto-merges all open PRs (used by trial command for backward compatibility)
func AutoMergePullRequestsLegacy(repoSlug string, verbose bool) error {
	// Use a very old time (Unix epoch) to include all PRs
	return AutoMergePullRequestsCreatedAfter(repoSlug, time.Unix(0, 0), verbose)
}

// WaitForWorkflowCompletion waits for a workflow run to complete, with a specified timeout
func WaitForWorkflowCompletion(ctx context.Context, repoSlug, runID string, timeoutMinutes int, verbose bool) error {
	prAutomergeLog.Printf("Waiting for workflow completion: repo=%s, runID=%s, timeout=%d minutes", repoSlug, runID, timeoutMinutes)

	timeout := time.Duration(timeoutMinutes) * time.Minute

	return PollWithSignalHandling(PollOptions{
		Ctx:          ctx,
		PollInterval: 10 * time.Second,
		Timeout:      timeout,
		PollFunc: func(ctx context.Context) (PollResult, error) {
			// Check workflow status with context-aware GH execution.
			// ctx is cancelled on Ctrl-C, which causes RunGHContext to abort the gh subprocess.
			output, err := workflow.RunGHContext(ctx, "Checking workflow status...", "run", "view", runID, "--repo", repoSlug, "--json", "status,conclusion")
			if err != nil {
				return PollFailure, fmt.Errorf("could not check workflow status; ensure required prerequisites are configured, then retry: %w", err)
			}

			status := string(output)

			// Check if completed
			if strings.Contains(status, `"status":"completed"`) {
				if strings.Contains(status, `"conclusion":"success"`) {
					return PollSuccess, nil
				} else if strings.Contains(status, `"conclusion":"failure"`) {
					return PollFailure, errors.New("workflow completed with failure; inspect workflow logs and rerun after fixing reported errors")
				} else if strings.Contains(status, `"conclusion":"cancelled"`) {
					return PollFailure, errors.New("workflow was cancelled; rerun the workflow to continue auto-merge")
				} else {
					return PollFailure, errors.New("workflow completed with an unknown conclusion; inspect run details to confirm success before proceeding")
				}
			}

			// Still running, continue polling
			return PollContinue, nil
		},
		StartMessage:    fmt.Sprintf("Waiting for workflow completion (timeout: %d minutes)", timeoutMinutes),
		ProgressMessage: "Workflow still running...",
		SuccessMessage:  "Workflow completed successfully",
		Verbose:         verbose,
	})
}
