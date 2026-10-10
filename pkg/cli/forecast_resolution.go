package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var forecastResolutionLog = logger.New("cli:forecast_resolution")

const (
	forecastRateLimitMaxAttempts = 3
	forecastRateLimitBaseBackoff = 2 * time.Second
)

var (
	forecastFetchGitHubWorkflows = func(ctx context.Context, repoOverride string, verbose bool) (map[string]*GitHubWorkflow, error) {
		return fetchGitHubWorkflows(ctx, repoOverride, verbose)
	}
	forecastListWorkflowRunsPaginated = listWorkflowRunsWithPagination
	forecastRateLimitSleep            = func(ctx context.Context, delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
)

// resolveForecastWorkflows returns the ordered list of workflow IDs to forecast.
// When WorkflowIDs is empty, all agentic workflow IDs in the repository are returned.
// When RepoOverride is set, workflows are discovered via the GitHub API instead of local files.
func resolveForecastWorkflows(ctx context.Context, config ForecastConfig) ([]string, error) {
	forecastResolutionLog.Printf("Resolving forecast workflows: repoOverride=%q, explicitIDs=%d", config.RepoOverride, len(config.WorkflowIDs))
	if config.RepoOverride != "" {
		return resolveForecastWorkflowsFromRemote(ctx, config.WorkflowIDs, config.RepoOverride, config.Verbose)
	}

	if len(config.WorkflowIDs) > 0 {
		// Resolve each provided ID to the workflow display name returned by FindWorkflowName.
		resolved := make([]string, 0, len(config.WorkflowIDs))
		for _, id := range config.WorkflowIDs {
			name, err := workflow.FindWorkflowName(id)
			if err != nil {
				return nil, fmt.Errorf("workflow %q not found: %w", id, err)
			}
			resolved = append(resolved, name)
		}
		return resolved, nil
	}

	// No explicit IDs: discover all agentic workflows from .lock.yml files.
	names, err := getAgenticWorkflowNames(config.Verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to discover agentic workflows: %w", err)
	}
	return names, nil
}

// resolveForecastWorkflowsFromRemote resolves workflow names for a remote repository using
// the GitHub API. When ids is empty, all workflows in the remote repository are returned.
// When ids are provided, each is matched (case-insensitively) against remote workflow names
// and file-path basenames.
func resolveForecastWorkflowsFromRemote(ctx context.Context, ids []string, repoOverride string, verbose bool) ([]string, error) {
	githubWorkflows, err := fetchWorkflowsWithBackoff(ctx, ids, repoOverride, verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows in %s: %w", repoOverride, err)
	}

	if len(ids) == 0 {
		// Return display names for all workflows in the remote repo.
		names := make([]string, 0, len(githubWorkflows))
		for _, wf := range githubWorkflows {
			names = append(names, wf.Name)
		}
		sort.Strings(names)
		return names, nil
	}

	// Match each provided ID against the remote workflow list.
	resolved := make([]string, 0, len(ids))
	for _, id := range ids {
		matched := matchRemoteWorkflowName(id, githubWorkflows)
		if matched == "" {
			return nil, fmt.Errorf("workflow %q not found in %s", id, repoOverride)
		}
		resolved = append(resolved, matched)
	}
	return resolved, nil
}

func forecastRateLimitBackoffDuration(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(attempt) * forecastRateLimitBaseBackoff
}

func fetchWorkflowsWithBackoff(ctx context.Context, ids []string, repoOverride string, verbose bool) (map[string]*GitHubWorkflow, error) {
	var lastErr error

	for attempt := 1; attempt <= forecastRateLimitMaxAttempts; attempt++ {
		githubWorkflows, err := forecastFetchGitHubWorkflows(ctx, repoOverride, verbose)
		if err == nil {
			return githubWorkflows, nil
		}
		if !errorutil.IsRateLimitError(err.Error()) {
			return nil, err
		}

		lastErr = err
		if attempt == forecastRateLimitMaxAttempts {
			break
		}

		backoff := forecastRateLimitBackoffDuration(attempt)
		forecastResolutionLog.Printf("Rate limited discovering workflows in %s; backing off %s before retry %d/%d", repoOverride, backoff, attempt+1, forecastRateLimitMaxAttempts)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("GitHub API rate limit hit while discovering workflows in %s; backing off for %s before retry %d/%d",
				repoOverride, backoff, attempt+1, forecastRateLimitMaxAttempts)))
		if err := forecastRateLimitSleep(ctx, backoff); err != nil {
			return nil, err
		}
	}

	if len(ids) > 0 {
		forecastResolutionLog.Printf("Rate limit exhausted in %s; returning %d caller-supplied workflow IDs as partial results", repoOverride, len(ids))
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("GitHub API rate limit exhausted while discovering workflows in %s; continuing with caller-supplied workflow IDs as partial results",
				repoOverride)))

		partialWorkflows := make(map[string]*GitHubWorkflow, len(ids))
		for _, id := range ids {
			partialWorkflows[id] = &GitHubWorkflow{Name: id, Path: id, State: "unknown"}
		}
		return partialWorkflows, nil
	}

	return nil, fmt.Errorf("GitHub API rate limit exhausted after %d attempts: %w", forecastRateLimitMaxAttempts, lastErr)
}

func listRunsWithBackoff(ctx context.Context, opts ListWorkflowRunsOptions, workflowID string) ([]WorkflowRun, int, error) {
	var lastErr error
	opts.Context = ctx

	for attempt := 1; attempt <= forecastRateLimitMaxAttempts; attempt++ {
		runs, total, err := forecastListWorkflowRunsPaginated(opts)
		if err == nil {
			return runs, total, nil
		}
		if !errorutil.IsRateLimitError(err.Error()) {
			return nil, 0, err
		}

		lastErr = err
		if attempt == forecastRateLimitMaxAttempts {
			break
		}

		backoff := forecastRateLimitBackoffDuration(attempt)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("GitHub API rate limit hit while sampling %s; backing off for %s before retry %d/%d",
				workflowID, backoff, attempt+1, forecastRateLimitMaxAttempts)))
		if err := forecastRateLimitSleep(ctx, backoff); err != nil {
			return nil, 0, err
		}
	}

	return nil, 0, lastErr
}

// matchRemoteWorkflowName returns the display name of the workflow in the remote map that
// best matches id. Matching is tried against the file-based key (e.g. "ci-doctor") and the
// display name (e.g. "CI Failure Doctor"), both case-insensitively. Returns "" on no match.
func matchRemoteWorkflowName(id string, workflows map[string]*GitHubWorkflow) string {
	for key, wf := range workflows {
		if strings.EqualFold(key, id) || strings.EqualFold(wf.Name, id) {
			return wf.Name
		}
	}
	return ""
}
