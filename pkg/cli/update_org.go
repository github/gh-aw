package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

// orgUpdateLog logs org-mode update progress and rate-limit telemetry for debugging.
var orgUpdateLog = logger.New("cli:update_org")

// orgUpdateCoreBuffer preserves a 500-request safety margin on the core API budget
// before org-mode applies a delay to avoid exhausting the hourly quota mid-run.
const orgUpdateCoreBuffer = 500

// orgUpdateSearchBuffer preserves at least one search request because GitHub's search
// quota is much smaller and org discovery only needs a narrow cushion between pages.
const orgUpdateSearchBuffer = 1

// orgUpdateCriticalConsumed is the number of core API requests that, once consumed,
// marks the budget as critical (i.e. remaining <= limit-1000). At that point org-mode
// stops processing additional repositories instead of risking quota exhaustion or a
// long wait for the hourly reset.
const orgUpdateCriticalConsumed = 1000

// errOrgRateLimitCritical signals that the GitHub API budget reached a critical level
// and remaining work should be short-circuited rather than waiting for the reset.
var errOrgRateLimitCritical = errors.New("github api rate limit reached critical level")

var previewOrgRepoUpdatesFn = previewOrgRepoUpdates
var runUpdateForTargetRepoFn = runUpdateForTargetRepo
var waitForOrgRateLimitFn = waitForOrgRateLimit
var createIssueForOrgRepoFn = createIssueForOrgRepo

type orgRateLimitResponse struct {
	Resources struct {
		Core   rateLimitResource `json:"core"`
		Search rateLimitResource `json:"search"`
	} `json:"resources"`
}

type orgWorkflowPreview struct {
	Name       string
	Path       string
	CurrentRef string
	LatestRef  string
	Redirected bool
	EditedAt   time.Time
}

type orgRepoPreview struct {
	Repo           string
	TotalWorkflows int
	Workflows      []orgWorkflowPreview
	OldestEdit     time.Time
	// CurrentVersion holds the compiler version extracted from the repo's lock
	// files and is populated by the upgrade scan phase. It is empty for repos
	// discovered by the update command.
	CurrentVersion string
}

func runUpdateForOrg(ctx context.Context, org string, repoGlobs []string, opts UpdateWorkflowsOptions, createPR bool, createIssue bool, verbose bool) error {
	clearUpdateResolutionCaches()
	searchFn := func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return searchOrgWorkflowReposFn(ctx, org, opts.WorkflowNames, verbose)
	}

	// scanFn previews a single repo and decides whether to include it.
	// It also prints a per-repo workflow summary to stderr.
	scanFn := func(ctx context.Context, repo string, v bool) (orgRepoPreview, bool, error) {
		preview, err := previewOrgRepoUpdatesFn(ctx, repo, opts, v)
		if err != nil {
			return orgRepoPreview{}, false, err
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			fmt.Sprintf("%s: %d workflow(s), %d with updates", repo, preview.TotalWorkflows, len(preview.Workflows)),
		))
		if len(preview.Workflows) == 0 {
			if v {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping "+repo+": already up to date"))
			}
			return orgRepoPreview{}, false, nil
		}
		return preview, true, nil
	}

	return runCommandForOrg(ctx, org, repoGlobs, orgRunCallbacks{
		AutoYes:  opts.Yes,
		SearchFn: searchFn,
		ScanFn:   scanFn,
		ReportFn: renderOrgPreviewReport,
		ApplyFn: func(ctx context.Context, preview orgRepoPreview, v bool) error {
			return runUpdateForTargetRepoFn(ctx, preview.Repo, opts, true, v)
		},
		IssueFn: func(ctx context.Context, preview orgRepoPreview, v bool) error {
			return createIssueForOrgRepoFn(ctx, preview, v)
		},
		DiscoveringMsg:   "Discovering repositories in " + org + " with source-managed workflows...",
		NoReposMsg:       formatUpdateOrgNoReposMessage(opts.WorkflowNames),
		ScanLabel:        "Inspecting",
		ApplyLabel:       "Updating",
		IssueLabel:       "Creating issue in",
		NoResultsMsg:     "All matching repositories are already up to date",
		NoResultsStopMsg: "No updates found before processing stopped",
		AllFailApplyMsg:  "failed to update any repository",
		AllFailIssueMsg:  "failed to create issues in any repository",
	}, createPR, createIssue, verbose)
}

func formatUpdateOrgNoReposMessage(workflowNames []string) string {
	if len(workflowNames) == 0 {
		return "No repositories found with source-managed workflows"
	}

	filters := make([]string, 0, len(workflowNames))
	seen := make(map[string]struct{}, len(workflowNames))
	for _, workflowName := range workflowNames {
		normalized := normalizeWorkflowID(workflowName)
		if normalized == "" || normalized == "." {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		filters = append(filters, normalized)
	}
	if len(filters) == 0 {
		return "No repositories found with source-managed workflows matching the requested workflow filters"
	}

	slices.Sort(filters)
	return "No repositories found with source-managed workflows matching: " + strings.Join(filters, ", ")
}

// renderOrgPreviewReport prints the discovered updates for each repository. It is
// intentionally cheap (no API calls) so it can be shown even when a run is stopped
// early by a cancellation signal or a critical rate-limit condition.
func renderOrgPreviewReport(previewByRepo []orgRepoPreview, applying bool) {
	if applying {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Repositories with updates available (%d):", len(previewByRepo))))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry-run preview of update pull requests:"))
	}
	for _, repo := range previewByRepo {
		fmt.Fprintf(os.Stderr, "- %s (%d workflow(s))\n", repo.Repo, repo.TotalWorkflows)
		for _, wf := range repo.Workflows {
			// CurrentRef and LatestRef are already resolved to version labels
			// (tags or short SHAs) by previewOrgRepoUpdates.
			fmt.Fprintf(os.Stderr, "  - %s: %s -> %s\n", wf.Name, wf.CurrentRef, wf.LatestRef)
		}
	}
}

func previewOrgRepoUpdates(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return orgRepoPreview{}, fmt.Errorf("--org requires running inside a git repository: %w", err)
	}

	updatesDir, err := ensureUpdateTargetRepoGitignore(gitRoot)
	if err != nil {
		return orgRepoPreview{}, err
	}

	checkoutDir := filepath.Join(updatesDir, sanitizeRepoPath(repo))
	if err := shallowCloneTargetRepo(ctx, repo, checkoutDir); err != nil {
		return orgRepoPreview{}, err
	}

	workflowsDir := filepath.Join(checkoutDir, constants.GetWorkflowDir())
	workflows, err := findWorkflowsWithSource(workflowsDir, opts.WorkflowNames, verbose)
	if err != nil {
		return orgRepoPreview{}, fmt.Errorf("failed to scan workflows in shallow checkout: %w", err)
	}

	preview := orgRepoPreview{
		Repo:           repo,
		TotalWorkflows: len(workflows),
		Workflows:      make([]orgWorkflowPreview, 0, len(workflows)),
	}
	for _, wf := range workflows {
		sourceSpec, err := parseSourceSpec(wf.SourceSpec)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s/%s: failed to parse source: %v", repo, wf.Name, err)))
			orgUpdateLog.Printf("Failed to parse source for %s/%s: %v", repo, wf.Name, err)
			continue
		}
		name := normalizeWorkflowID(wf.Name)
		resolved, err := resolveRedirectedUpdateLocation(ctx, name, sourceSpec, opts.AllowMajor, verbose, opts.NoRedirect, opts.CoolDown)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s/%s: %v", repo, wf.Name, err)))
			orgUpdateLog.Printf("Failed to resolve update location for %s/%s: %v", repo, wf.Name, err)
			continue
		}
		if resolved.currentRef == resolved.latestRef && len(resolved.redirectHistory) == 0 && !opts.Force {
			continue
		}

		editedAt, err := getLatestWorkflowEditTimeFromCheckout(ctx, checkoutDir, wf.Path)
		if err == nil {
			if preview.OldestEdit.IsZero() || editedAt.Before(preview.OldestEdit) {
				preview.OldestEdit = editedAt
			}
		}
		// Resolve SHAs to version-tag labels so the preview shows human-readable
		// identifiers (e.g. v1.2.3) instead of bare short SHAs when possible.
		currentLabel := resolveVersionLabel(ctx, resolved.sourceSpec.Repo, resolved.currentRef)
		latestLabel := resolveVersionLabel(ctx, resolved.sourceSpec.Repo, resolved.latestRef)
		preview.Workflows = append(preview.Workflows, orgWorkflowPreview{
			Name:       name,
			Path:       wf.Path,
			CurrentRef: currentLabel,
			LatestRef:  latestLabel,
			Redirected: len(resolved.redirectHistory) > 0,
			EditedAt:   editedAt,
		})
	}

	return preview, nil
}

func getLatestWorkflowEditTimeFromCheckout(ctx context.Context, checkoutDir, workflowPath string) (time.Time, error) {
	relativePath := workflowPath
	if filepath.IsAbs(workflowPath) {
		rel, err := filepath.Rel(checkoutDir, workflowPath)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to derive workflow path relative to checkout %s for %s: %w", checkoutDir, workflowPath, err)
		}
		relativePath = rel
	}

	cmd := exec.CommandContext(ctx, "git", "-C", checkoutDir, "log", "--max-count=1", "--format=%cI", "--", relativePath)
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read latest commit time for %s in checkout %s: %w", relativePath, checkoutDir, err)
	}
	date := strings.TrimSpace(string(output))
	if date == "" {
		return time.Time{}, fmt.Errorf("no commit date available for %s", workflowPath)
	}
	return time.Parse(time.RFC3339, date)
}

func waitForOrgRateLimit(ctx context.Context, resource string, verbose bool) error {
	output, err := workflow.RunGHContext(ctx, "Checking rate limit...", "api", "rate_limit")
	if err != nil {
		return err
	}

	var response orgRateLimitResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return err
	}

	limit := response.Resources.Core
	buffer := orgUpdateCoreBuffer
	if resource == "search" {
		limit = response.Resources.Search
		buffer = orgUpdateSearchBuffer
	}
	if limit.Limit == 0 {
		return nil
	}

	orgUpdateLog.Printf("GitHub %s rate limit: %d/%d remaining (reset at %s)", resource, limit.Remaining, limit.Limit, time.Unix(limit.Reset, 0).Format(time.RFC3339))

	// Critical level: once consumption reaches limit-1000 API units, stop processing
	// additional work rather than risking quota exhaustion or a long reset wait. The
	// search resource has a tiny quota, so the critical threshold only applies to core.
	if resource != "search" {
		criticalRemaining := limit.Limit - orgUpdateCriticalConsumed
		if criticalRemaining > buffer && limit.Remaining <= criticalRemaining {
			orgUpdateLog.Printf("GitHub %s rate limit critical: %d/%d remaining (<= %d)", resource, limit.Remaining, limit.Limit, criticalRemaining)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("GitHub %s API budget critical: %d/%d remaining (consumed %d API units)", resource, limit.Remaining, limit.Limit, limit.Limit-limit.Remaining),
			))
			return errOrgRateLimitCritical
		}
	}

	if limit.Remaining <= buffer {
		resetAt := time.Unix(limit.Reset, 0)
		waitFor := time.Until(resetAt) + rateLimitResetBuffer
		if waitFor > 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Applying a %s delay to avoid reaching the GitHub %s rate limit (%d/%d remaining)", waitFor.Round(time.Second), resource, limit.Remaining, limit.Limit),
			))
			timer := time.NewTimer(waitFor)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
			return nil
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
			fmt.Sprintf("GitHub %s rate limit OK: %d/%d remaining", resource, limit.Remaining, limit.Limit),
		))
	}
	return nil
}

// createIssueForOrgRepo opens a GitHub issue in the target repository listing
// the source-managed workflows that have updates available. Any previously-open
// issues carrying the gh-aw-update XML marker are closed first so that only the
// most recent notification remains.
func createIssueForOrgRepo(ctx context.Context, preview orgRepoPreview, verbose bool) error {
	releaseTag, releaseURL := getGhawReleaseInfo(ctx)
	xmlMarker := buildOrgXMLMarker(ghawUpdateMarkerPrefix, releaseTag)

	// Close stale update issues before creating the new one.
	closeExistingOrgIssuesByMarker(ctx, preview.Repo, ghawUpdateMarkerPrefix, verbose)

	title, body := buildOrgUpdateIssue(preview, releaseTag, releaseURL, xmlMarker)

	if err := createOrgIssue(ctx, preview.Repo, title, body, agenticWorkflowsLabel); err != nil {
		return fmt.Errorf("failed to create issue in %s: %w", preview.Repo, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created issue in "+preview.Repo))
	return nil
}

func buildOrgUpdateIssue(preview orgRepoPreview, releaseTag, releaseURL, xmlMarker string) (string, string) {
	title := "[aw] Updates available"

	var body strings.Builder
	body.WriteString("## Agentic Workflows Update Available\n\n")
	body.WriteString("The `gh aw update` command found source-managed workflow updates for this repository.\n\n")
	body.WriteString("**Workflows with updates:**\n\n")
	for _, wf := range preview.Workflows {
		// CurrentRef and LatestRef are already resolved to version labels.
		fmt.Fprintf(&body, "- `%s`: `%s` -> `%s`\n", wf.Name, wf.CurrentRef, wf.LatestRef)
	}
	if releaseURL != "" {
		fmt.Fprintf(&body, "\n[View gh-aw release %s](%s)\n", releaseTag, releaseURL)
	}
	body.WriteString("\n### How to execute\n\n")
	body.WriteString("- **Assign to agent**: Assign this issue to Copilot to automatically apply the update\n")
	body.WriteString("- **Via @copilot comment**: Add a comment `@copilot update agentic workflows` on this issue\n")
	body.WriteString("- **Via CLI**: Run `gh aw update` in your local checkout\n\n")
	body.WriteString(xmlMarker + "\n")

	return title, body.String()
}
