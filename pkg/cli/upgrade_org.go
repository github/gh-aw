package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var upgradeOrgLog = logger.New("cli:upgrade_org")

var runUpgradeForTargetRepoFn = runUpgradeForTargetRepo
var searchOrgLockWorkflowReposFn = searchOrgLockWorkflowRepos
var scanUpgradeRepoFn = scanUpgradeRepo
var createIssueForUpgradeOrgRepoFn = createIssueForUpgradeOrgRepo

const orgUpgradeSkillsDir = constants.GithubDir + "skills"

// runUpgradeForOrg runs the upgrade command across all repositories in an
// organization that have agentic workflow files. Without --create-pull-request
// or --create-issue it prints a dry-run preview; with --create-pull-request it
// checks out each repository, runs the upgrade, and opens a pull request; with
// --create-issue it opens a GitHub issue in each repository.
//
// The function delegates to runCommandForOrg, which provides shared logic for
// organization discovery, rate-limit handling, graceful cancellation, result
// sorting, and per-repo error recovery.
func runUpgradeForOrg(ctx context.Context, org string, repoGlobs []string, opts upgradeOptions, createPR bool, createIssue bool, verbose bool) error {
	upgradeOrgLog.Printf("Running org upgrade: org=%s, globs=%d, createPR=%v, createIssue=%v", org, len(repoGlobs), createPR, createIssue)
	return runCommandForOrg(ctx, org, repoGlobs, orgRunCallbacks{
		AutoYes:  opts.yes,
		SearchFn: searchOrgLockWorkflowReposFn,
		ScanFn: func(ctx context.Context, repo string, v bool) (orgRepoPreview, bool, error) {
			preview, include, err := scanUpgradeRepoFn(ctx, repo, v)
			if err == nil && include && upgradeRepoMatchesVersion(preview, GetVersion()) {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(repo+": "+orgUpgradeVersionMatchStatus))
				include = false
			}
			return preview, include, err
		},
		ReportFn: renderOrgUpgradeReport,
		ApplyFn: func(ctx context.Context, preview orgRepoPreview, v bool) error {
			return runUpgradeForTargetRepoFn(ctx, preview.Repo, opts, true, v)
		},
		IssueFn: func(ctx context.Context, preview orgRepoPreview, v bool) error {
			return createIssueForUpgradeOrgRepoFn(ctx, preview.Repo, v)
		},
		DiscoveringMsg:   "Discovering repositories in " + org + " with agentic workflows...",
		NoReposMsg:       "No repositories found with agentic workflows",
		ScanLabel:        "Inspecting",
		ApplyLabel:       "Upgrading",
		IssueLabel:       "Creating issue in",
		NoResultsMsg:     "No repositories require an agentic workflow upgrade",
		NoResultsStopMsg: "No repositories found before processing stopped",
		AllFailApplyMsg:  "failed to upgrade any repository",
		AllFailIssueMsg:  "failed to create issues in any repository",
	}, createPR, createIssue, verbose)
}

// scanUpgradeRepo shallow-clones repo, counts agentic workflow files, and
// extracts the distinct compiler versions from all lock file headers.
// It returns (preview, true, nil) when the repo has workflows, or
// (orgRepoPreview{}, false, nil) when none are found.
func scanUpgradeRepo(ctx context.Context, repo string, verbose bool) (orgRepoPreview, bool, error) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return orgRepoPreview{}, false, fmt.Errorf("--org requires running inside a git repository: %w", err)
	}

	updatesDir, err := ensureUpdateTargetRepoGitignore(gitRoot)
	if err != nil {
		return orgRepoPreview{}, false, err
	}

	checkoutDir := filepath.Join(updatesDir, sanitizeRepoPath(repo))
	if err := shallowCloneTargetRepo(ctx, repo, checkoutDir); err != nil {
		return orgRepoPreview{}, false, err
	}

	return previewUpgradeRepoCheckout(repo, checkoutDir, verbose)
}

func previewUpgradeRepoCheckout(repo, checkoutDir string, verbose bool) (orgRepoPreview, bool, error) {
	workflowsDir := filepath.Join(checkoutDir, constants.GetWorkflowDir())
	// Count .md workflow files (excluding lock files).
	mdCount, missing, err := countUpgradeWorkflowFiles(workflowsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return orgRepoPreview{}, false, fmt.Errorf("failed to scan workflows in %s: %w", repo, err)
	}

	if mdCount == 0 {
		upgradeOrgLog.Printf("Skipping %s: no agentic workflow files found", repo)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping "+repo+": no agentic workflow files found"))
		}
		return orgRepoPreview{}, false, nil
	}

	versions, unknown, err := extractCompilerVersionsFromWorkflowsDir(workflowsDir)
	if err != nil {
		return orgRepoPreview{}, false, fmt.Errorf("failed to scan compiler versions in %s: %w", repo, err)
	}
	preview := orgRepoPreview{
		Repo:                 repo,
		TotalWorkflows:       mdCount,
		CurrentVersions:      versions,
		UnknownVersionLocks:  unknown,
		MissingWorkflowLocks: missing,
	}
	currentVersions := formatUpgradeCompilerVersions(preview)
	upgradeOrgLog.Printf("Scanned %s: workflows=%d, compilerVersions=%s", repo, mdCount, currentVersions)

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		fmt.Sprintf("%s: %d workflow(s), compilers: %s", repo, mdCount, currentVersions),
	))

	return preview, true, nil
}

// renderOrgUpgradeReport prints the discovered repositories with their workflow
// counts and current compiler versions.
func renderOrgUpgradeReport(results []orgRepoPreview, applying bool) {
	targetVersion := normalizeDisplayVersion(GetVersion())
	if applying {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Repositories with agentic workflows (%d):", len(results))))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry-run preview of upgrade pull requests:"))
	}
	var allVersions []string
	unknown, missing := 0, 0
	for _, r := range results {
		currentVersions := formatUpgradeCompilerVersions(r)
		target := ""
		if targetVersion != "" && currentVersions != targetVersion {
			target = " -> " + targetVersion
		}
		status := ""
		if upgradeRepoMatchesVersion(r, targetVersion) {
			status = "; " + orgUpgradeVersionMatchStatus
		}
		fmt.Fprintf(os.Stderr, "- %s (%s%s; %d workflow(s)%s)\n", r.Repo, currentVersions, target, r.TotalWorkflows, status)
		allVersions = append(allVersions, r.CurrentVersions...)
		unknown += r.UnknownVersionLocks
		missing += r.MissingWorkflowLocks
	}
	if len(results) > 0 {
		fmt.Fprintln(os.Stderr, "Detected compiler versions: "+formatUpgradeCompilerVersions(orgRepoPreview{
			CurrentVersions: allVersions, UnknownVersionLocks: unknown, MissingWorkflowLocks: missing,
		}))
	}
}

// countUpgradeWorkflowFiles counts Markdown workflows and those without lock files.
func countUpgradeWorkflowFiles(workflowsDir string) (count, missing int, err error) {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return 0, 0, err
	}
	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			files[entry.Name()] = struct{}{}
		}
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".md") && !strings.EqualFold(name, "README.md") {
			count++
			if _, exists := files[strings.TrimSuffix(name, ".md")+".lock.yml"]; !exists {
				missing++
			}
		}
	}
	return count, missing, nil
}

// extractCompilerVersionFromLockContent parses the gh-aw-metadata JSON comment
// from a lock file and returns the compiler_version field value.
// Returns empty string if not found or on parse error.
func extractCompilerVersionFromLockContent(content string) string {
	meta, _, err := workflow.ExtractMetadataFromLockFile(content)
	if err != nil || meta == nil {
		return ""
	}
	return meta.CompilerVersion
}

func normalizeDisplayVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(version, "v")
}

// runUpgradeForTargetRepo checks out repo to a temporary directory, runs the
// upgrade command inside it, and opens a pull request with the resulting changes.
func runUpgradeForTargetRepo(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error { //nolint:largefunc // Checkout lifetime, upgrade, and PR creation share ordered cleanup.
	if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker && rollout.dryRun {
		preview, include, err := scanUpgradeRepoFn(ctx, repo, verbose)
		if err != nil {
			return err
		}
		if !include {
			return emitOrgRolloutResult(orgRolloutResult{Status: orgUpgradeNoWorkflowsStatus})
		}
		if upgradeRepoMatchesVersion(preview, GetVersion()) {
			return emitOrgRolloutResult(orgRolloutResult{Status: orgUpgradeVersionMatchStatus, Preview: &preview})
		}
		return emitOrgRolloutPreview(preview, include)
	}
	existing, skip, err := prepareOrgRolloutWorker(ctx, repo, ghawUpgradeMarkerPrefix)
	if err != nil || skip {
		return err
	}
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("--repo/--org requires running inside a git repository: %w", err)
	}

	updatesDir, err := ensureUpdateTargetRepoGitignore(gitRoot)
	if err != nil {
		return err
	}

	checkoutDir := filepath.Join(updatesDir, sanitizeRepoPath(repo))
	if err := shallowCloneTargetRepo(ctx, repo, checkoutDir); err != nil {
		return err
	}

	var preview *orgRepoPreview
	if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker {
		scanned, include, err := previewUpgradeRepoCheckout(repo, checkoutDir, verbose)
		if err != nil {
			return err
		}
		if !include {
			return emitOrgRolloutResult(orgRolloutResult{Status: orgUpgradeNoWorkflowsStatus})
		}
		preview = &scanned
		if upgradeRepoMatchesVersion(scanned, GetVersion()) {
			return emitOrgRolloutResult(orgRolloutResult{Status: orgUpgradeVersionMatchStatus, Preview: preview})
		}
	}

	// Extend sparse checkout to include .github/skills; upgrade also updates
	// the dispatcher skill (ensureAgenticWorkflowsDispatcher) and needs that path present.
	sparseAddCmd := exec.CommandContext(ctx, "git", "-C", checkoutDir, "sparse-checkout", "add", orgUpgradeSkillsDir)
	if output, err := sparseAddCmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("failed to extend sparse checkout for %s: %w", repo, err)
		}
		return fmt.Errorf("failed to extend sparse checkout for %s: %w: %s", repo, err, trimmed)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checked out "+repo+" at "+checkoutDir))
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to read current directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(checkoutDir); err != nil {
		return fmt.Errorf("failed to change directory to checkout %s: %w", checkoutDir, err)
	}

	if createPR {
		if err := PreflightCheckForCreatePR(verbose); err != nil {
			return err
		}
	}

	// Override fields that must be adjusted for a remote-repo upgrade.
	// workflowDir is intentionally reset: --dir is a local-machine concept and
	// must not be forwarded to remote repos where that path may not exist.
	opts.ctx = ctx
	if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker {
		opts.ctx = withUpdateGitHubClient(ctx, verbose)
	}
	opts.skipExtensionUpgrade = true
	opts.verbose = verbose
	opts.workflowDir = ""
	if createPR {
		opts.summary, err = newUpdatePRSummary(ctx, opts.workflowDir)
		if err != nil {
			return err
		}
	}

	if err := runUpgradeCommand(opts); err != nil {
		return err
	}

	if !createPR {
		return nil
	}

	// Skip PR creation when the upgrade produced no changes (e.g. repo is already up to date).
	changed, err := hasPendingChanges()
	if err != nil {
		return err
	}
	if !changed {
		if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker {
			return emitOrgRolloutResult(orgRolloutResult{Status: "unchanged", Preview: preview})
		}
		upgradeOrgLog.Printf("Skipping PR for %s: no pending changes after upgrade", repo)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping PR for "+repo+": already up to date"))
		}
		return nil
	}

	xmlMarker := buildOrgXMLMarker(ghawUpgradeMarkerPrefix, normalizeDisplayVersion(GetVersion()))

	prBody, err := opts.summary.body(ctx, "upgrade")
	if err != nil {
		return err
	}
	prBody = appendPRMessage(prBody, opts.prMessage) + "\n" + xmlMarker
	if err := validatePRBody(prBody); err != nil {
		return err
	}

	// Close stale PRs only after the replacement description is validated.
	if orgRolloutFromContext(ctx) == nil {
		closeExistingOrgPRsByMarker(ctx, repo, ghawUpgradeMarkerPrefix, verbose)
	}

	prURL, err := createPRWithChanges(ctx, "upgrade-agentic-workflows", "chore: upgrade agentic workflows",
		"Upgrade agentic workflows", prBody, verbose, opts.autoMerge)
	if prURL != "" {
		addLabelToOrgPR(ctx, prURL, agenticWorkflowsLabel, verbose)
	}
	if err != nil {
		if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker && prURL != "" {
			return errors.Join(err, emitOrgRolloutResult(orgRolloutResult{Status: "created", URL: prURL, Preview: preview}))
		}
		return err
	}
	if prURL != "" {
		return finishOrgRolloutWorker(ctx, repo, existing, prURL, preview)
	}
	return nil
}

// searchOrgLockWorkflowRepos searches an organization's repositories for
// compiled agentic workflow lock files (.lock.yml) in .github/workflows.
// This is the same discovery strategy used by the update command so both
// commands operate on the same set of repositories.
func searchOrgLockWorkflowRepos(ctx context.Context, org string, verbose bool) ([]string, error) {
	if !isValidOrgSlug(org) {
		return nil, invalidOrgSlugError(org)
	}
	query := fmt.Sprintf(`org:%s path:.github/workflows filename:.lock.yml`, org)
	return searchOrgReposByQuery(ctx, query, verbose)
}

// createIssueForUpgradeOrgRepo opens a GitHub issue in the target repository
// to notify maintainers that agentic workflow upgrades are available. Any
// previously-open issues carrying the gh-aw-upgrade XML marker are closed first
// so that only the most recent notification remains.
func createIssueForUpgradeOrgRepo(ctx context.Context, repo string, verbose bool) error {
	title := "[aw] Upgrade available"

	releaseTag, releaseURL := getGhawReleaseInfo(ctx)
	xmlMarker := buildOrgXMLMarker(ghawUpgradeMarkerPrefix, releaseTag)

	// Close stale upgrade issues before creating the new one.
	closeExistingOrgIssuesByMarker(ctx, repo, ghawUpgradeMarkerPrefix, verbose)

	var releaseSection string
	if releaseURL != "" {
		releaseSection = fmt.Sprintf("\n[View gh-aw release %s](%s)\n", releaseTag, releaseURL)
	}

	body := "Agentic workflow files detected in this repository may have upgrades available.\n\n" +
		"Run `gh aw upgrade` to apply the latest codemods, update GitHub Actions versions, and recompile all workflows.\n\n" +
		"Review the upgrade output and any generated changes before committing to ensure there are no unexpected modifications.\n" +
		releaseSection + "\n" +
		"### How to execute\n\n" +
		"- **Assign to agent**: Assign this issue to Copilot to automatically apply the upgrade\n" +
		"- **Via @copilot comment**: Add a comment `@copilot upgrade agentic workflows` on this issue\n" +
		"- **Via CLI**: Run `gh aw upgrade` in your local checkout\n\n" +
		xmlMarker + "\n"

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Creating upgrade issue in "+repo+"..."))
	}

	if err := createOrgIssue(ctx, repo, title, body, agenticWorkflowsLabel); err != nil {
		return fmt.Errorf("failed to create issue in %s: %w", repo, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created issue in "+repo))
	return nil
}
