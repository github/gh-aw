package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var orgRunnerLog = logger.New("cli:org_runner")
var orgConfirmActionFn = console.ConfirmAction
var isRunningInCIFn = IsRunningInCI

// orgWorkflowCountSuffix returns a parenthetical workflow-count string for use
// in progress messages, e.g. " (5 workflow(s))". Returns an empty string when
// the count is zero (e.g. upgrade repos before scanning).
func orgWorkflowCountSuffix(preview orgRepoPreview) string {
	if preview.TotalWorkflows <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%d workflow(s))", preview.TotalWorkflows)
}

// orgRunCallbacks holds the pluggable functions for runCommandForOrg.
//
// runCommandForOrg implements the shared algorithm used by both the update and
// upgrade commands when operating across an organization: it discovers
// repositories, filters them, optionally scans each one for pending work,
// sorts the results, renders a report, and—when the caller requests it—applies
// the command or opens issues, all with rate-limit awareness and graceful
// cancellation support.
type orgRunCallbacks struct {
	// AutoYes auto-accepts per-repository confirmations in apply/issue phases.
	AutoYes bool

	// SearchFn returns candidate repos in the org. Required.
	SearchFn func(ctx context.Context, org string, verbose bool) ([]string, error)

	// ScanFn inspects a single repo and returns (preview, include, error).
	// Return include=false to silently skip the repo (e.g. already up to date).
	// A non-nil error causes the repo to be skipped with a warning.
	// If nil, all discovered repos are included without per-repo scanning
	// or rate-limiting during the discovery phase.
	ScanFn func(ctx context.Context, repo string, verbose bool) (orgRepoPreview, bool, error)

	// ReportFn renders the summary before applying changes. Required. The applying
	// parameter is true when createPR or createIssue is set.
	ReportFn func(results []orgRepoPreview, applying bool)

	// ApplyFn applies the command to a single repo (--create-pull-request). Required when createPR is true.
	ApplyFn func(ctx context.Context, preview orgRepoPreview, verbose bool) error

	// IssueFn creates an issue in a single repo (--create-issue). Required when createIssue is true.
	IssueFn func(ctx context.Context, preview orgRepoPreview, verbose bool) error

	// Optional message overrides; an empty string falls back to a generic default.

	// DiscoveringMsg is printed while the SearchFn is running.
	DiscoveringMsg string
	// NoReposMsg is printed when SearchFn returns no repos.
	NoReposMsg string
	// ScanLabel is the per-repo progress label used in the scan phase: e.g. "Inspecting".
	ScanLabel string
	// ApplyLabel is the per-repo progress label used in the apply phase: e.g. "Updating".
	ApplyLabel string
	// IssueLabel is the per-repo progress label used in the issue phase: e.g. "Creating issue in".
	IssueLabel string
	// NoResultsMsg is printed when the scan phase finishes with no results.
	NoResultsMsg string
	// NoResultsStopMsg is printed when the scan phase was stopped early with no results.
	NoResultsStopMsg string
	// AllFailApplyMsg is returned as an error when every apply attempt fails.
	AllFailApplyMsg string
	// AllFailIssueMsg is returned as an error when every issue-creation attempt fails.
	AllFailIssueMsg string
}

func renderOrgActionSummary(preview orgRepoPreview, action string) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Ready to "+action))
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr("Repository: "+preview.Repo))
	if preview.TotalWorkflows > 0 {
		fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Workflows: %d", preview.TotalWorkflows)))
	}
	if len(preview.Workflows) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatListItemStderr("Pending workflow updates:"))
		for _, wf := range preview.Workflows {
			fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%s: %s -> %s", wf.Name, wf.CurrentRef, wf.LatestRef)))
		}
	}
	currentVersion := normalizeDisplayVersion(preview.CurrentVersion)
	targetVersion := normalizeDisplayVersion(GetVersion())
	if currentVersion != "" {
		if targetVersion != "" && targetVersion != currentVersion {
			fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Compiler version: %s -> %s", currentVersion, targetVersion)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatListItemStderr("Compiler version: "+currentVersion))
		}
	}
}

func confirmOrgAction(preview orgRepoPreview, action string, autoYes bool) (bool, error) {
	renderOrgActionSummary(preview, action)
	if autoYes {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Auto-accepted because --yes was provided"))
		return true, nil
	}

	confirmed, err := orgConfirmActionFn(
		"Proceed with this repository?",
		"Accept",
		"Skip",
	)
	if err != nil {
		return false, fmt.Errorf("failed to confirm %s for %s: %w", action, preview.Repo, err)
	}
	return confirmed, nil
}

// runCommandForOrg is the shared org-wide runner used by both the update and
// upgrade commands. It:
//  1. Validates org and repoGlobs inputs.
//  2. Installs a signal handler so Ctrl-C / SIGTERM render a partial report
//     instead of exiting abruptly.
//  3. Calls cbs.SearchFn to discover candidate repos and filters by repoGlobs.
//  4. If cbs.ScanFn is non-nil, runs a per-repo scan loop with rate-limit
//     awareness; otherwise all discovered repos are included directly.
//  5. Sorts results by oldest-edit time (ascending; ties broken alphabetically).
//  6. Calls cbs.ReportFn to display the summary.
//  7. When createPR or createIssue is set, iterates through results calling
//     cbs.ApplyFn or cbs.IssueFn, continuing past per-repo errors.
func runCommandForOrg(ctx context.Context, org string, repoGlobs []string, cbs orgRunCallbacks, createPR bool, createIssue bool, verbose bool) error {
	if strings.TrimSpace(org) == "" {
		return errors.New("--org cannot be empty")
	}
	if err := validateRepoGlobs(repoGlobs); err != nil {
		return err
	}
	if createPR && createIssue {
		return errors.New("createPR and createIssue are mutually exclusive")
	}
	if cbs.SearchFn == nil {
		return errors.New("orgRunCallbacks.SearchFn is required")
	}
	if cbs.ReportFn == nil {
		return errors.New("orgRunCallbacks.ReportFn is required")
	}
	if createPR && cbs.ApplyFn == nil {
		return errors.New("orgRunCallbacks.ApplyFn is required when createPR is true")
	}
	if createIssue && cbs.IssueFn == nil {
		return errors.New("orgRunCallbacks.IssueFn is required when createIssue is true")
	}
	if (createPR || createIssue) && !cbs.AutoYes && isRunningInCIFn() {
		return errors.New("confirmation is required for --org create operations in CI; re-run with --yes to auto-accept")
	}

	// Handle Ctrl-C / SIGTERM so an interrupted run still renders the report
	// gathered so far instead of exiting abruptly.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	discMsg := cbs.DiscoveringMsg
	if discMsg == "" {
		discMsg = "Discovering repositories in " + org + "..."
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(discMsg))

	repoPaths, err := cbs.SearchFn(ctx, org, verbose)
	if err != nil {
		return err
	}
	if len(repoPaths) == 0 {
		noReposMsg := cbs.NoReposMsg
		if noReposMsg == "" {
			noReposMsg = "No repositories found"
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(noReposMsg))
		return nil
	}

	repos := filterOrgRepos(repoPaths, repoGlobs)
	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No repositories matched the requested --repos filters"))
		return nil
	}

	// Build the result set.
	var results []orgRepoPreview

	if cbs.ScanFn == nil {
		// No per-repo scanning: include every discovered repo directly.
		results = make([]orgRepoPreview, 0, len(repos))
		for _, repo := range repos {
			results = append(results, orgRepoPreview{Repo: repo})
		}
	} else {
		total := len(repos)
		scanLabel := cbs.ScanLabel
		if scanLabel == "" {
			scanLabel = "Inspecting"
		}
		results = make([]orgRepoPreview, 0, len(repos))
		stopped := false

		for i, repo := range repos {
			// Honor a cancellation signal between repos so we can still show
			// the report for the work completed so far.
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cancellation requested; stopping after %d/%d repositories", i, total)))
				orgRunnerLog.Printf("Context canceled during scan at repo %d/%d: %v", i, total, ctx.Err())
				stopped = true
				break
			}

			fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("[%d/%d] %s %s", i+1, total, scanLabel, repo)))

			if err := waitForOrgRateLimitFn(ctx, "core", verbose); err != nil {
				if errors.Is(err, errOrgRateLimitCritical) {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("GitHub API budget critical; stopping after %d/%d repositories and reporting what was found", i, total)))
					orgRunnerLog.Printf("Rate limit critical during scan at repo %d/%d", i, total)
					stopped = true
					break
				}
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Continuing after rate limit check failure for %s: %v", repo, err)))
				}
			}

			preview, include, scanErr := cbs.ScanFn(ctx, repo, verbose)
			if scanErr != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", repo, scanErr)))
				orgRunnerLog.Printf("Failed to scan %s: %v", repo, scanErr)
				continue
			}
			if !include {
				continue
			}
			results = append(results, preview)
		}

		if len(results) == 0 {
			if stopped {
				msg := cbs.NoResultsStopMsg
				if msg == "" {
					msg = "No results found before processing stopped"
				}
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(msg))
				return nil
			}
			msg := cbs.NoResultsMsg
			if msg == "" {
				msg = "All matching repositories are already up to date"
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(msg))
			return nil
		}
	}

	// Sort by oldest-edit time (oldest first); ties broken alphabetically.
	slices.SortStableFunc(results, func(a, b orgRepoPreview) int {
		if a.OldestEdit.IsZero() && b.OldestEdit.IsZero() {
			return strings.Compare(a.Repo, b.Repo)
		}
		if a.OldestEdit.IsZero() {
			return 1
		}
		if b.OldestEdit.IsZero() {
			return -1
		}
		if a.OldestEdit.Equal(b.OldestEdit) {
			return strings.Compare(a.Repo, b.Repo)
		}
		if a.OldestEdit.Before(b.OldestEdit) {
			return -1
		}
		return 1
	})

	// Always render the report before applying anything: it is cheap and lets
	// the user see results even if the run is stopped early.
	cbs.ReportFn(results, createPR || createIssue)

	if !createPR && !createIssue {
		return nil
	}

	if createIssue {
		issueLabel := cbs.IssueLabel
		if issueLabel == "" {
			issueLabel = "Creating issue in"
		}
		processed := 0
		attempted := 0
		for i, result := range results {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cancellation requested; created issues for %d/%d repositories", processed, len(results))))
				orgRunnerLog.Printf("Context canceled during issue creation at %d/%d: %v", i, len(results), ctx.Err())
				return nil
			}
			if err := waitForOrgRateLimitFn(ctx, "core", verbose); err != nil {
				if errors.Is(err, errOrgRateLimitCritical) {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("GitHub API budget critical; created issues for %d/%d repositories", processed, len(results))))
					orgRunnerLog.Printf("Rate limit critical during issue creation at %d/%d", i, len(results))
					return nil
				}
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Continuing after rate limit check failure for %s: %v", result.Repo, err)))
				}
			}
			fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("[%d/%d] %s %s%s", i+1, len(results), issueLabel, result.Repo, orgWorkflowCountSuffix(result))))
			confirmed, err := confirmOrgAction(result, "create an issue", cbs.AutoYes)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipped "+result.Repo))
				continue
			}
			attempted++
			if err := cbs.IssueFn(ctx, result, verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", result.Repo, err)))
				orgRunnerLog.Printf("Failed to create issue in %s: %v", result.Repo, err)
				continue
			}
			processed++
		}
		if attempted == 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No repositories were accepted for issue creation"))
			return nil
		}
		if processed == 0 {
			msg := cbs.AllFailIssueMsg
			if msg == "" {
				msg = "failed to create issues in any repository"
			}
			return errors.New(msg)
		}
		return nil
	}

	// createPR
	applyLabel := cbs.ApplyLabel
	if applyLabel == "" {
		applyLabel = "Processing"
	}
	processed := 0
	attempted := 0
	for i, result := range results {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cancellation requested; processed %d/%d repositories", processed, len(results))))
			orgRunnerLog.Printf("Context canceled during apply at %d/%d: %v", i, len(results), ctx.Err())
			return nil
		}
		if err := waitForOrgRateLimitFn(ctx, "core", verbose); err != nil {
			if errors.Is(err, errOrgRateLimitCritical) {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("GitHub API budget critical; processed %d/%d repositories", processed, len(results))))
				orgRunnerLog.Printf("Rate limit critical during apply at %d/%d", i, len(results))
				return nil
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Continuing after rate limit check failure for %s: %v", result.Repo, err)))
			}
		}
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("[%d/%d] %s %s%s", i+1, len(results), applyLabel, result.Repo, orgWorkflowCountSuffix(result))))
		confirmed, err := confirmOrgAction(result, "create a pull request", cbs.AutoYes)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipped "+result.Repo))
			continue
		}
		attempted++
		if err := cbs.ApplyFn(ctx, result, verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", result.Repo, err)))
			orgRunnerLog.Printf("Failed to apply to %s: %v", result.Repo, err)
			continue
		}
		processed++
	}
	if attempted == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No repositories were accepted for pull request creation"))
		return nil
	}
	if processed == 0 {
		msg := cbs.AllFailApplyMsg
		if msg == "" {
			msg = "failed to process any repository"
		}
		return errors.New(msg)
	}

	return nil
}
