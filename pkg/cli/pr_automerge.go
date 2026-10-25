package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var prAutomergeLog = logger.New("cli:pr_automerge")

var autoMergeRunGH = workflow.RunGHContext

type prAutoMergeRepository struct {
	AutoMergeAllowed         bool   `json:"autoMergeAllowed"`
	ViewerDefaultMergeMethod string `json:"viewerDefaultMergeMethod"`
	MergeCommitAllowed       bool   `json:"mergeCommitAllowed"`
	SquashMergeAllowed       bool   `json:"squashMergeAllowed"`
	RebaseMergeAllowed       bool   `json:"rebaseMergeAllowed"`
	PullRequest              struct {
		ID string `json:"id"`
	} `json:"pullRequest"`
}

func (r prAutoMergeRepository) mergeMethod() (string, error) {
	allowed := map[string]bool{
		"MERGE": r.MergeCommitAllowed, "SQUASH": r.SquashMergeAllowed, "REBASE": r.RebaseMergeAllowed,
	}
	if allowed[r.ViewerDefaultMergeMethod] {
		return r.ViewerDefaultMergeMethod, nil
	}
	for _, method := range []string{"MERGE", "SQUASH", "REBASE"} {
		if allowed[method] {
			return method, nil
		}
	}
	return "", errors.New("repository has no supported merge method enabled")
}

// enablePRAutoMerge schedules native auto-merge without merging directly or bypassing checks.
func enablePRAutoMerge(ctx context.Context, prURL string) error {
	if err := configurePRAutoMerge(ctx, prURL); err != nil {
		return fmt.Errorf("pull request %s was created and remains open, but auto-merge could not be enabled: %w", prURL, err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Enabled auto-merge for pull request: "+prURL))
	return nil
}

func configurePRAutoMerge(ctx context.Context, prURL string) error {
	parsed, err := url.Parse(prURL)
	if err != nil {
		return fmt.Errorf("invalid pull request URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("expected an HTTPS pull request URL with owner/repo/pull/number")
	}
	owner, repoName, number, err := parser.ParsePRURL(prURL)
	if err != nil {
		return err
	}
	if number <= 0 {
		return errors.New("expected a positive pull request number in the URL")
	}
	repo, err := readPRAutoMergeRepository(ctx, parsed.Host, owner, repoName, number)
	if err != nil {
		return err
	}
	return enableRepositoryPRAutoMerge(ctx, parsed.Host, repo)
}

func readPRAutoMergeRepository(ctx context.Context, hostname, owner, repoName string, number int) (*prAutoMergeRepository, error) {
	const query = `query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			autoMergeAllowed viewerDefaultMergeMethod
			mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed
			pullRequest(number: $number) { id }
		}
	}`
	output, err := autoMergeRunGH(ctx, "Checking auto-merge configuration...",
		"api", "graphql", "--hostname", hostname,
		"-f", "query="+query, "-f", "owner="+owner, "-f", "name="+repoName, "-F", fmt.Sprintf("number=%d", number))
	if err != nil {
		return nil, fmt.Errorf("could not read repository auto-merge configuration: %w", err)
	}
	var response struct {
		Data struct {
			Repository *prAutoMergeRepository `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("could not parse repository auto-merge configuration: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("could not read repository auto-merge configuration: %s", response.Errors[0].Message)
	}
	if response.Data.Repository == nil {
		return nil, errors.New("GitHub did not return repository auto-merge configuration")
	}
	return response.Data.Repository, nil
}

func enableRepositoryPRAutoMerge(ctx context.Context, hostname string, repo *prAutoMergeRepository) error {
	if !repo.AutoMergeAllowed {
		return errors.New("auto-merge is disabled or unavailable in this repository; enable it in repository settings")
	}
	if repo.PullRequest.ID == "" {
		return errors.New("GitHub did not return the created pull request ID")
	}
	method, err := repo.mergeMethod()
	if err != nil {
		return err
	}

	const mutation = `mutation($prId: ID!, $mergeMethod: PullRequestMergeMethod!) {
		enablePullRequestAutoMerge(input: {pullRequestId: $prId, mergeMethod: $mergeMethod}) {
			pullRequest { id }
		}
	}`
	output, err := autoMergeRunGH(ctx, "Enabling pull request auto-merge...",
		"api", "graphql", "--hostname", hostname,
		"-f", "query="+mutation, "-f", "prId="+repo.PullRequest.ID, "-f", "mergeMethod="+method)
	if err != nil {
		return fmt.Errorf("GitHub refused auto-merge; check repository settings, branch rules, and token permissions: %w", err)
	}
	var mutationResponse struct {
		Data struct {
			EnablePullRequestAutoMerge *struct {
				PullRequest *struct {
					ID string `json:"id"`
				} `json:"pullRequest"`
			} `json:"enablePullRequestAutoMerge"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output, &mutationResponse); err != nil {
		return fmt.Errorf("could not parse auto-merge response: %w", err)
	}
	if len(mutationResponse.Errors) > 0 {
		return fmt.Errorf("GitHub refused auto-merge; check repository settings, branch rules, and token permissions: %s", mutationResponse.Errors[0].Message)
	}
	if mutationResponse.Data.EnablePullRequestAutoMerge == nil ||
		mutationResponse.Data.EnablePullRequestAutoMerge.PullRequest == nil ||
		mutationResponse.Data.EnablePullRequestAutoMerge.PullRequest.ID == "" {
		return errors.New("GitHub did not confirm that auto-merge was enabled")
	}
	if mutationResponse.Data.EnablePullRequestAutoMerge.PullRequest.ID != repo.PullRequest.ID {
		return errors.New("GitHub returned an unexpected pull request after enabling auto-merge")
	}
	return nil
}

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

	return autoMergeEligiblePullRequests(repoSlug, prs, createdAfter, verbose)
}

func autoMergeEligiblePullRequests(repoSlug string, prs []PullRequest, createdAfter time.Time, verbose bool) error {
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
		autoMergeEligiblePullRequest(repoSlug, pr, verbose)
	}
	return nil
}

func autoMergeEligiblePullRequest(repoSlug string, pr PullRequest, verbose bool) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing PR #%d: %s (draft: %t, mergeable: %s, created: %s)",
			pr.Number, pr.Title, pr.IsDraft, pr.Mergeable, pr.CreatedAt.Format(time.RFC3339))))
	}

	// Convert from draft to non-draft if necessary
	if pr.IsDraft {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Converting PR #%d from draft to ready for review", pr.Number)))
		if output, err := workflow.RunGHCombined("Converting draft to ready...", "pr", "ready", strconv.Itoa(pr.Number), "--repo", repoSlug); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to convert PR #%d from draft: %v (output: %s)", pr.Number, err, string(output))))
			return
		}
	}

	// Check if PR is mergeable
	if pr.Mergeable != "MERGEABLE" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("PR #%d is not mergeable (status: %s), skipping auto-merge", pr.Number, pr.Mergeable)))
		return
	}

	// Auto-merge the PR
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auto-merging PR #%d", pr.Number)))
	if output, err := workflow.RunGHCombined("Auto-merging pull request...", "pr", "merge", strconv.Itoa(pr.Number), "--repo", repoSlug, "--auto", "--squash"); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to auto-merge PR #%d: %v (output: %s)", pr.Number, err, string(output))))
		return
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully enabled auto-merge for PR #%d", pr.Number)))
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
