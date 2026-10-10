package cli

import (
	"context"
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
			return scanUpgradeRepoFn(ctx, repo, v)
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
		NoResultsMsg:     "No repositories with agentic workflows found",
		NoResultsStopMsg: "No repositories found before processing stopped",
		AllFailApplyMsg:  "failed to upgrade any repository",
		AllFailIssueMsg:  "failed to create issues in any repository",
	}, createPR, createIssue, verbose)
}

// scanUpgradeRepo shallow-clones repo, counts agentic workflow files, and
// extracts the current compiler version from the first lock file it finds.
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

	workflowsDir := filepath.Join(checkoutDir, constants.GetWorkflowDir())

	// Count .md workflow files (excluding lock files).
	mdCount, err := countWorkflowMDFiles(workflowsDir)
	if err != nil && !os.IsNotExist(err) {
		return orgRepoPreview{}, false, fmt.Errorf("failed to scan workflows in %s: %w", repo, err)
	}

	if mdCount == 0 {
		upgradeOrgLog.Printf("Skipping %s: no agentic workflow files found", repo)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping "+repo+": no agentic workflow files found"))
		}
		return orgRepoPreview{}, false, nil
	}

	// Extract compiler version from lock file metadata.
	currentVersion := extractCompilerVersionFromWorkflowsDir(workflowsDir)
	upgradeOrgLog.Printf("Scanned %s: workflows=%d, currentVersion=%s", repo, mdCount, currentVersion)

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		fmt.Sprintf("%s: %d workflow(s)%s", repo, mdCount, formatCurrentVersionSuffix(currentVersion)),
	))

	return orgRepoPreview{
		Repo:           repo,
		TotalWorkflows: mdCount,
		CurrentVersion: currentVersion,
	}, true, nil
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
	for _, r := range results {
		versionPart := ""
		currentVersion := normalizeDisplayVersion(r.CurrentVersion)
		if currentVersion != "" {
			target := ""
			if targetVersion != "" && currentVersion != targetVersion {
				target = " -> " + targetVersion
			}
			versionPart = fmt.Sprintf(" (%s%s)", currentVersion, target)
		} else if r.TotalWorkflows > 0 {
			versionPart = fmt.Sprintf(" (%d workflow(s))", r.TotalWorkflows)
		}
		fmt.Fprintf(os.Stderr, "- %s%s\n", r.Repo, versionPart)
	}
}

// countWorkflowMDFiles returns the number of .md files (excluding .lock.yml)
// in workflowsDir.
func countWorkflowMDFiles(workflowsDir string) (int, error) {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".md") && !strings.EqualFold(name, "README.md") {
			count++
		}
	}
	return count, nil
}

// extractCompilerVersionFromWorkflowsDir reads the first .lock.yml file in
// workflowsDir and extracts the compiler_version from its gh-aw-metadata
// comment. Returns an empty string if no lock file is found or parsing fails.
func extractCompilerVersionFromWorkflowsDir(workflowsDir string) string {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lock.yml") {
			continue
		}
		lockPath := filepath.Join(workflowsDir, e.Name())
		content, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		version := extractCompilerVersionFromLockContent(string(content))
		if version != "" {
			return version
		}
	}
	return ""
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

// formatCurrentVersionSuffix formats a version string for inline display,
// e.g. ", compiler: v1.2.3". Returns an empty string when version is empty.
func formatCurrentVersionSuffix(version string) string {
	normalized := normalizeDisplayVersion(version)
	if normalized == "" {
		return ""
	}
	return ", compiler: " + normalized
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
func runUpgradeForTargetRepo(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
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
	opts.skipExtensionUpgrade = true
	opts.verbose = verbose
	opts.workflowDir = ""

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
		upgradeOrgLog.Printf("Skipping PR for %s: no pending changes after upgrade", repo)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping PR for "+repo+": already up to date"))
		}
		return nil
	}

	releaseTag, releaseURL := getGhawReleaseInfo(ctx)
	xmlMarker := buildOrgXMLMarker(ghawUpgradeMarkerPrefix, releaseTag)

	// Close any stale upgrade PRs in the target repo before creating the new one.
	closeExistingOrgPRsByMarker(ctx, repo, ghawUpgradeMarkerPrefix, verbose)

	var releaseLine string
	if releaseURL != "" {
		releaseLine = fmt.Sprintf("\n[View gh-aw release %s](%s)\n", releaseTag, releaseURL)
	}
	prBody := "This PR upgrades agentic workflows by applying the latest codemods, " +
		"updating GitHub Actions versions, and recompiling all workflows." +
		releaseLine + "\n" + xmlMarker

	prURL, err := CreatePRWithChanges(ctx, "upgrade-agentic-workflows", "chore: upgrade agentic workflows",
		"Upgrade agentic workflows", prBody, verbose)
	if err != nil {
		return err
	}

	if prURL != "" {
		addLabelToOrgPR(ctx, prURL, agenticWorkflowsLabel, verbose)
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
