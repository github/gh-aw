package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var prLog = logger.New("cli:pr_command")

// PullRequest represents a GitHub pull request across transfer and automerge flows.
// Callers should treat this as a superset model:
//   - transfer paths populate repository/branch/author fields.
//   - automerge paths typically populate only number/title/draft/mergeability/timestamps.
//
// Fields that are not returned by a given GH API query are intentionally left as zero values.
type PullRequest struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	HeadSHA     string    `json:"headSHA"`
	BaseBranch  string    `json:"baseBranch"`
	HeadBranch  string    `json:"headBranch"`
	SourceRepo  string    `json:"sourceRepo"`
	TargetRepo  string    `json:"targetRepo"`
	AuthorLogin string    `json:"authorLogin"`
	IsDraft     bool      `json:"isDraft"`
	Mergeable   string    `json:"mergeable"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PRInfo is kept as an alias for backward compatibility.
type PRInfo = PullRequest

// NewPRCommand creates the main pr command with subcommands
func NewPRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request utilities",
		Long: `Pull request management utilities for transferring PRs between repositories.

This command provides a tool for transferring pull requests from one repository
to another, including the code changes, title, and body. This is useful for
migrating work from trial repositories to production repositories.

Available subcommands:
  - transfer - Transfer a pull request to another repository`,
		Example: `  gh aw pr transfer https://github.com/trial/repo/pull/234
  gh aw pr transfer https://github.com/source/repo/pull/123 --repo owner/target
  gh aw pr transfer https://github.com/gh-aw-trial/repo/pull/5 --repo owner/prod-repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
	cmd.AddCommand(newLegacyGHGuardSubcommand())
	cmd.AddCommand(NewPRTransferSubcommand())

	return cmd
}

// NewPRTransferSubcommand creates the pr transfer subcommand
func NewPRTransferSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer <pr-url>",
		Short: "Transfer a pull request to another repository",
		Long: `Transfer a pull request from one repository to another.

This command fetches the pull request details, applies the changes as a single commit,
and creates a new pull request in the target repository with the same title and body.

The target repository defaults to the current repository unless --repo is specified.

The command will:
1. Fetch the PR details (title, body, changes)
2. Apply changes as a single squashed commit
3. Create a new PR in the target repository
4. Copy the original title and body`,
		Example: `  gh aw pr transfer https://github.com/owner/repo/pull/234
  gh aw pr transfer https://github.com/owner/repo/pull/234 --repo owner/target-repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prURL := args[0]
			targetRepo, _ := cmd.Flags().GetString("repo")
			verbose, _ := cmd.Flags().GetBool("verbose")

			if err := transferPR(prURL, targetRepo, verbose); err != nil {
				return err
			}
			return nil
		},
	}

	addRepoFlag(cmd)

	return cmd
}

// checkRepositoryAccess checks if the current user has write access to the target repository
func checkRepositoryAccess(owner, repo string) (bool, error) {
	prLog.Printf("Checking repository access: %s/%s", owner, repo)

	// Get current user
	output, err := workflow.RunGH("Fetching user info...", "api", "/user", "--jq", ".login")
	if err != nil {
		prLog.Printf("Failed to get current user: %s", err)
		return false, fmt.Errorf("could not get current user; ensure required prerequisites are configured, then retry: %w", err)
	}
	username := strings.TrimSpace(string(output))
	prLog.Printf("Current user: %s", username)

	// Check user's permission level for the repository
	output, err = workflow.RunGH("Checking repository permissions...", "api", fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", owner, repo, username))
	if err != nil {
		// If we get an error, it likely means we don't have access or the repo doesn't exist
		prLog.Print("Repository access denied or repository not found")
		return false, nil
	}

	var permissionInfo struct {
		Permission string `json:"permission"`
	}

	if err := json.Unmarshal(output, &permissionInfo); err != nil {
		return false, fmt.Errorf("could not parse permission info; the GitHub API response may be malformed or unexpected: %w", err)
	}

	// Check if user has write, maintain, or admin access
	permission := permissionInfo.Permission
	hasWriteAccess := permission == "write" || permission == "maintain" || permission == "admin"
	prLog.Printf("User permission level: %s, has write access: %v", permission, hasWriteAccess)

	return hasWriteAccess, nil
}

// createForkIfNeeded creates a fork of the target repository and returns the fork repo name
func createForkIfNeeded(targetOwner, targetRepo string, verbose bool) (forkOwner, forkRepo string, err error) {
	// Get current user
	output, err := workflow.RunGH("Fetching user info...", "api", "/user", "--jq", ".login")
	if err != nil {
		return "", "", fmt.Errorf("could not get current user; ensure required prerequisites are configured, then retry: %w", err)
	}
	currentUser := strings.TrimSpace(string(output))

	// Check if fork already exists
	forkRepoSpec := fmt.Sprintf("%s/%s", currentUser, targetRepo)
	checkCmd := workflow.ExecGH("repo", "view", forkRepoSpec, "--json", "name")
	if checkCmd.Run() == nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fork already exists: "+forkRepoSpec))
		}
		return currentUser, targetRepo, nil
	}

	// Create fork
	_, err = workflow.RunGH(fmt.Sprintf("Creating fork of %s/%s...", targetOwner, targetRepo), "repo", "fork", fmt.Sprintf("%s/%s", targetOwner, targetRepo), "--clone=false")
	if err != nil {
		return "", "", fmt.Errorf("could not create fork; ensure required prerequisites are configured, then retry: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully created fork: "+forkRepoSpec))
	}

	return currentUser, targetRepo, nil
}

// fetchPRInfo fetches detailed information about a pull request
func fetchPRInfo(owner, repo string, prNumber int) (*PRInfo, error) {
	prLog.Printf("Fetching PR info: %s/%s#%d", owner, repo, prNumber)

	// Fetch PR details using gh API
	output, err := workflow.RunGH("Fetching pull request info...", "api", fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prNumber),
		"--jq", `{
			number: .number,
			title: .title,
			body: .body,
			state: .state,
			headSHA: .head.sha,
			baseBranch: .base.ref,
			headBranch: .head.ref,
			sourceRepo: .head.repo.full_name,
			targetRepo: .base.repo.full_name,
			authorLogin: .user.login
		}`)
	if err != nil {
		prLog.Printf("Failed to fetch PR info: %s", err)
		return nil, fmt.Errorf("could not fetch PR info; ensure required prerequisites are configured, then retry: %w", err)
	}

	var prInfo PRInfo
	if err := json.Unmarshal(output, &prInfo); err != nil {
		return nil, fmt.Errorf("could not parse PR info; the GitHub API response may be malformed or unexpected: %w", err)
	}

	prLog.Printf("Fetched PR #%d: state=%s, author=%s", prInfo.Number, prInfo.State, prInfo.AuthorLogin)
	return &prInfo, nil
}

// createPatchFromPR creates a git patch from the PR changes using gh pr diff
func createPatchFromPR(sourceOwner, sourceRepo string, prInfo *PRInfo, verbose bool) (string, error) {
	// Create a temporary directory for the patch
	tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-")
	if err != nil {
		return "", fmt.Errorf("could not create temp directory; ensure required prerequisites are configured, then retry: %w", err)
	}

	patchFile := filepath.Join(tempDir, "pr.patch")

	// Use gh pr diff command directly - this is the most reliable method
	diffContent, err := workflow.RunGH("Fetching pull request diff...", "pr", "diff", strconv.Itoa(prInfo.Number), "--repo", fmt.Sprintf("%s/%s", sourceOwner, sourceRepo))
	if err != nil {
		return "", fmt.Errorf("could not get PR diff; ensure required prerequisites are configured, then retry: %w", err)
	}

	if len(diffContent) == 0 {
		return "", errors.New("PR diff is empty")
	}

	// Keep the patch as a raw diff. PR metadata is added to the commit separately
	// so untrusted body text cannot be interpreted as mailbox or patch content.
	if err := os.WriteFile(patchFile, diffContent, constants.FilePermPublic); err != nil {
		return "", fmt.Errorf("could not write patch file; ensure required prerequisites are configured, then retry: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Successfully created patch using gh pr diff"))
	}

	return patchFile, nil
}

func ensureCleanGitWorktree() error {
	output, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("could not inspect git status; ensure this is a valid git repository, then retry: %w", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		return errors.New("target repository has uncommitted changes; commit, stash, or remove them before transferring a PR")
	}
	return nil
}

func resetGitWorktreeToHEAD() error {
	if err := exec.Command("git", "reset", "--hard", "HEAD").Run(); err != nil {
		return fmt.Errorf("could not reset transfer branch to HEAD: %w", err)
	}
	if err := exec.Command("git", "clean", "-fd").Run(); err != nil {
		return fmt.Errorf("could not remove untracked files from failed patch apply: %w", err)
	}
	return nil
}

func checkoutUpdatedDefaultBranch(targetOwner, targetRepo string, verbose bool) error {
	// Get the default branch of the target repository
	defaultBranchOutput, err := workflow.RunGH("Fetching default branch...", "api", fmt.Sprintf("/repos/%s/%s", targetOwner, targetRepo), "--jq", ".default_branch")
	if err != nil {
		return fmt.Errorf("could not get default branch; ensure required prerequisites are configured, then retry: %w", err)
	}
	defaultBranch := strings.TrimSpace(string(defaultBranchOutput))

	// Ensure we're on the latest version of the default branch
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checking out and updating %s branch...", defaultBranch)))
	}

	cmd := exec.Command("git", "checkout", defaultBranch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not checkout default branch %s; ensure the branch exists locally and has no conflicting changes, then retry: %w", defaultBranch, err)
	}

	cmd = exec.Command("git", "pull", "origin", defaultBranch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not pull latest %s from origin; ensure network access and remote permissions are available, then retry: %w", defaultBranch, err)
	}
	return nil
}

func applyPatchToIndexWithFallback(patchFile, currentBranch, branchName string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying patch with git apply..."))
	}

	cmd := exec.Command("git", "apply", "--3way", "--index", patchFile)
	if err := cmd.Run(); err != nil {
		return applyPatchToIndexFallback(patchFile, currentBranch, branchName, verbose)
	}
	return nil
}

func applyPatchToIndexFallback(patchFile, currentBranch, branchName string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("3-way merge failed, trying with whitespace options..."))
	}
	if resetErr := resetGitWorktreeToHEAD(); resetErr != nil {
		_ = exec.Command("git", "checkout", currentBranch).Run()
		_ = exec.Command("git", "branch", "-D", branchName).Run()
		return resetErr
	}

	cmd := exec.Command("git", "apply", "--index", "--ignore-space-change", "--ignore-whitespace", patchFile)
	if err := cmd.Run(); err != nil {
		reportPatchRejectDetails(patchFile, verbose)
		if resetErr := resetGitWorktreeToHEAD(); resetErr != nil {
			return resetErr
		}
		_ = exec.Command("git", "checkout", currentBranch).Run()
		_ = exec.Command("git", "branch", "-D", branchName).Run()
		return fmt.Errorf("could not apply patch; resolve conflicts manually, then rerun transfer-pr. underlying error: %w", err)
	}
	return nil
}

func reportPatchRejectDetails(patchFile string, verbose bool) {
	if !verbose {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Standard apply failed, trying with --reject to see what failed..."))
	if resetErr := resetGitWorktreeToHEAD(); resetErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to reset before generating reject details: %v", resetErr)))
	}
	rejectCmd := exec.Command("git", "apply", "--reject", patchFile)
	rejectOutput, _ := rejectCmd.CombinedOutput()
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Patch rejection details:"))
	fmt.Fprintln(os.Stderr, string(rejectOutput))
}

func logPatchSummary(patchFile string, verbose bool) {
	if !verbose {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying patch..."))
	patchContent, err := os.ReadFile(patchFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(patchContent), "\n")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Patch file has %d lines", len(lines))))
	if len(lines) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("First line: "+lines[0]))
	}
}

// applyPatchToRepo applies a patch to the target repository and returns the branch name
func applyPatchToRepo(patchFile string, prInfo *PRInfo, targetOwner, targetRepo string, verbose bool) (string, error) {
	currentBranch, err := getCurrentBranch()
	if err != nil {
		return "", fmt.Errorf("could not get current branch; ensure required prerequisites are configured, then retry: %w", err)
	}
	if err := ensureCleanGitWorktree(); err != nil {
		return "", err
	}
	if err := checkoutUpdatedDefaultBranch(targetOwner, targetRepo, verbose); err != nil {
		return "", err
	}

	branchName := fmt.Sprintf("transfer-pr-%d-%d", prInfo.Number, time.Now().Unix())
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Creating branch: "+branchName))
	}
	if err := createAndSwitchBranch(branchName, verbose); err != nil {
		return "", fmt.Errorf("could not create new branch; ensure required prerequisites are configured, then retry: %w", err)
	}

	logPatchSummary(patchFile, verbose)
	if err := applyPatchToIndexWithFallback(patchFile, currentBranch, branchName, verbose); err != nil {
		return "", err
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Successfully applied patch with git apply"))
	}

	// Create the commit separately from patch application.
	commitMsg := fmt.Sprintf("Transfer PR #%d from %s\n\n%s", prInfo.Number, prInfo.SourceRepo, prInfo.Title)
	if prInfo.Body != "" {
		commitMsg += "\n\n" + prInfo.Body
	}
	commitMsg += fmt.Sprintf("\n\nOriginal-PR: %s#%d", prInfo.SourceRepo, prInfo.Number)
	commitMsg += "\nOriginal-Author: " + prInfo.AuthorLogin

	cmd := exec.Command("git", "commit", "-m", commitMsg)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("could not commit changes; ensure required prerequisites are configured, then retry: %w", err)
	}

	return branchName, nil
}

// createTransferPR creates a new PR in the target repository
func createTransferPR(targetOwner, targetRepo string, prInfo *PRInfo, branchName string, verbose bool) error { //nolint:largefunc
	// Check if user has write access to target repository
	hasWriteAccess, err := checkRepositoryAccess(targetOwner, targetRepo)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check repository access: %v", err)))
	}

	var forkOwner, forkRepo string
	var needsFork bool

	if !hasWriteAccess {
		needsFork = true
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No write access to target repository, using fork workflow..."))
		}

		forkOwner, forkRepo, err = createForkIfNeeded(targetOwner, targetRepo, verbose)
		if err != nil {
			return fmt.Errorf("could not create fork; ensure required prerequisites are configured, then retry: %w", err)
		}

		// Add fork as remote if not already present
		remoteName := "fork"
		githubHost := getGitHubHost()
		forkRepoURL := fmt.Sprintf("%s/%s/%s.git", githubHost, forkOwner, forkRepo)

		// Check if fork remote exists
		checkRemoteCmd := exec.Command("git", "remote", "get-url", remoteName)
		if checkRemoteCmd.Run() != nil {
			// Remote doesn't exist, add it
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Adding fork remote: "+forkRepoURL))
			}
			addRemoteCmd := exec.Command("git", "remote", "add", remoteName, forkRepoURL)
			if err := addRemoteCmd.Run(); err != nil {
				return fmt.Errorf("could not add fork remote; ensure required prerequisites are configured, then retry: %w", err)
			}
		}

		// Also ensure target repository is set as upstream remote if not already present
		upstreamRemote := "upstream"
		targetRepoURL := fmt.Sprintf("https://github.com/%s/%s.git", targetOwner, targetRepo)

		// Check if upstream remote exists and points to the right repo
		checkUpstreamCmd := exec.Command("git", "remote", "get-url", upstreamRemote)
		upstreamOutput, err := checkUpstreamCmd.Output()
		if err != nil || strings.TrimSpace(string(upstreamOutput)) != targetRepoURL {
			// Upstream doesn't exist or points to wrong repo, add/update it
			if err != nil {
				// Remote doesn't exist, add it
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Adding upstream remote: "+targetRepoURL))
				}
				addUpstreamCmd := exec.Command("git", "remote", "add", upstreamRemote, targetRepoURL)
				if err := addUpstreamCmd.Run(); err != nil {
					return fmt.Errorf("could not add upstream remote; ensure required prerequisites are configured, then retry: %w", err)
				}
			} else {
				// Remote exists but points to wrong repo, update it
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updating upstream remote: "+targetRepoURL))
				}
				setUpstreamCmd := exec.Command("git", "remote", "set-url", upstreamRemote, targetRepoURL)
				if err := setUpstreamCmd.Run(); err != nil {
					return fmt.Errorf("could not update upstream remote; ensure required prerequisites are configured, then retry: %w", err)
				}
			}
		}
	}

	// Push the branch
	if verbose {
		if needsFork {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Pushing branch to fork..."))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Pushing branch to remote..."))
		}
	}

	var pushCmd *exec.Cmd
	if needsFork {
		pushCmd = exec.Command("git", "push", "-u", "fork", branchName)
	} else {
		pushCmd = exec.Command("git", "push", "-u", "origin", branchName)
	}

	if err := pushCmd.Run(); err != nil {
		if needsFork {
			return fmt.Errorf("could not push branch to fork; ensure required prerequisites are configured, then retry: %w", err)
		}
		return fmt.Errorf("could not push branch; ensure required prerequisites are configured, then retry: %w", err)
	}

	// Create PR body with original info
	prBody := prInfo.Body
	if prBody != "" {
		prBody += "\n\n---\n\n"
	}
	prBody += fmt.Sprintf("**Transferred from:** %s#%d\n", prInfo.SourceRepo, prInfo.Number)
	prBody += "**Original Author:** @" + prInfo.AuthorLogin

	// Create the PR
	repoFlag := fmt.Sprintf("%s/%s", targetOwner, targetRepo)
	var headRef string
	if needsFork {
		headRef = fmt.Sprintf("%s:%s", forkOwner, branchName)
	} else {
		headRef = branchName
	}

	output, err := workflow.RunGH("Creating pull request...", "pr", "create",
		"--repo", repoFlag,
		"--title", prInfo.Title,
		"--body", prBody,
		"--head", headRef)
	if err != nil {
		return fmt.Errorf("could not create PR; ensure required prerequisites are configured, then retry: %w", err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("PR created successfully!"))
	if needsFork {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("PR created from fork %s/%s to %s/%s", forkOwner, forkRepo, targetOwner, targetRepo)))
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("URL: "+strings.TrimSpace(string(output))))

	return nil
}

// transferPR is the main function that orchestrates the PR transfer
func transferPR(prURL, targetRepo string, verbose bool) error { //nolint:largefunc
	prLog.Printf("Starting PR transfer: url=%s, targetRepo=%s", prURL, targetRepo)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting PR transfer..."))
	}

	// Parse PR URL
	sourceOwner, sourceRepoName, prNumber, err := parser.ParsePRURL(prURL)
	if err != nil {
		prLog.Printf("Failed to parse PR URL: %s", err)
		return err
	}
	prLog.Printf("Parsed source: %s/%s#%d", sourceOwner, sourceRepoName, prNumber)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Source: %s/%s PR #%d", sourceOwner, sourceRepoName, prNumber)))
	}

	// Determine target repository
	var targetOwner, targetRepoName string
	if targetRepo != "" {
		repoSpec, err := parseRepoSpec(targetRepo)
		if err != nil {
			return fmt.Errorf("target repository must use owner/repo format (example: github/gh-aw): %w", err)
		}
		parts := strings.SplitN(repoSpec.RepoSlug, "/", 2)
		if len(parts) != 2 {
			return errors.New("target repository must use owner/repo format (example: github/gh-aw)")
		}
		targetOwner, targetRepoName = parts[0], parts[1]
	} else {
		// Use current repository as target
		slug, err := GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("could not determine target repository; ensure required prerequisites are configured, then retry: %w", err)
		}
		targetOwner, targetRepoName, err = repoutil.SplitRepoSlug(slug)
		if err != nil {
			return fmt.Errorf("could not parse target repository; ensure required prerequisites are configured, then retry: %w", err)
		}
	}

	prLog.Printf("Determined target repository: %s/%s", targetOwner, targetRepoName)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Target: %s/%s", targetOwner, targetRepoName)))
	}

	// Check if source and target are the same
	if sourceOwner == targetOwner && sourceRepoName == targetRepoName {
		prLog.Print("Source and target repositories are the same - aborting")
		return errors.New("source and target repositories are expected to be different; choose a target repository in owner/repo format that is not the source")
	}

	// Ensure we're in the correct git repository
	var workingDir string
	var needsCleanup bool

	if targetRepo != "" {
		// Check if we're already in the target repository
		if isGitRepo() {
			slug, err := GetCurrentRepoSlug()
			if err == nil {
				currentOwner, currentRepoName, err := repoutil.SplitRepoSlug(slug)
				if err == nil && currentOwner == targetOwner && currentRepoName == targetRepoName {
					// We're already in the target repo
					workingDir = "."
				} else {
					// We need to clone the target repository
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning target repository %s/%s...", targetOwner, targetRepoName)))
					}
					tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-repo-")
					if err != nil {
						return fmt.Errorf("could not create temp directory for repo; ensure required prerequisites are configured, then retry: %w", err)
					}

					cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
					if err := cloneCmd.Run(); err != nil {
						// Clean up temporary directory on error
						if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
						}
						return fmt.Errorf("could not clone target repository; ensure required prerequisites are configured, then retry: %w", err)
					}

					workingDir = tempDir
					needsCleanup = true

					// Change to the cloned repository directory
					if err := os.Chdir(tempDir); err != nil {
						// Clean up temporary directory on error
						if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
						}
						return fmt.Errorf("could not change to cloned repository directory; ensure required prerequisites are configured, then retry: %w", err)
					}
				}
			} else {
				// Error getting current repo, clone anyway
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning target repository %s/%s...", targetOwner, targetRepoName)))
				}
				tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-repo-")
				if err != nil {
					return fmt.Errorf("could not create temp directory for repo; ensure required prerequisites are configured, then retry: %w", err)
				}

				cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
				if err := cloneCmd.Run(); err != nil {
					// Clean up temporary directory on error
					if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
					}
					return fmt.Errorf("could not clone target repository; ensure required prerequisites are configured, then retry: %w", err)
				}

				workingDir = tempDir
				needsCleanup = true

				// Change to the cloned repository directory
				if err := os.Chdir(tempDir); err != nil {
					// Clean up temporary directory on error
					if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
					}
					return fmt.Errorf("could not change to cloned repository directory; ensure required prerequisites are configured, then retry: %w", err)
				}
			}
		} else {
			// We're not in a git repository and need to clone
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning target repository %s/%s...", targetOwner, targetRepoName)))
			}
			tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-repo-")
			if err != nil {
				return fmt.Errorf("could not create temp directory for repo; ensure required prerequisites are configured, then retry: %w", err)
			}

			cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
			if err := cloneCmd.Run(); err != nil {
				// Clean up temporary directory on error
				if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
				}
				return fmt.Errorf("could not clone target repository; ensure required prerequisites are configured, then retry: %w", err)
			}

			workingDir = tempDir
			needsCleanup = true

			// Change to the cloned repository directory
			if err := os.Chdir(tempDir); err != nil {
				// Clean up temporary directory on error
				if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
				}
				return fmt.Errorf("could not change to cloned repository directory; ensure required prerequisites are configured, then retry: %w", err)
			}
		}
	} else {
		// Using current repository as target
		if !isGitRepo() {
			return errors.New("current directory is expected to be a git repository; run this command inside a cloned repository")
		}
		workingDir = "."
	}

	// Cleanup function
	defer func() {
		if needsCleanup && workingDir != "" {
			// Clean up temporary directory when done
			if err := os.RemoveAll(workingDir); err != nil && verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", workingDir, err)))
			}
		}
	}()

	// Fetch PR information
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching PR details..."))
	}

	prInfo, err := fetchPRInfo(sourceOwner, sourceRepoName, prNumber)
	if err != nil {
		return err
	}

	if prInfo.State != "open" && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: PR is in '%s' state", prInfo.State)))
	}

	// Create patch from PR
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Creating patch from PR changes..."))
	}

	patchFile, err := createPatchFromPR(sourceOwner, sourceRepoName, prInfo, verbose)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(patchFile)) // Clean up temp directory

	// Apply patch to target repository
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying changes to target repository..."))
	}

	branchName, err := applyPatchToRepo(patchFile, prInfo, targetOwner, targetRepoName, verbose)
	if err != nil {
		return err
	}

	// Create PR in target repository
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Creating new PR in target repository..."))
	}

	if err := createTransferPR(targetOwner, targetRepoName, prInfo, branchName, verbose); err != nil {
		return err
	}

	return nil
}

var createPRRunGHContextWithHost = workflow.RunGHContextWithHost

// createPR creates a pull request using GitHub CLI and returns the PR number.
func createPR(ctx context.Context, branchName, title, body string, verbose bool) (int, string, error) {
	return createPRForRepo(ctx, branchName, title, body, "", verbose)
}

// createPRForRepo creates a pull request in repoSlug. When repoSlug is empty,
// it resolves the current repository for compatibility with other PR callers.
func createPRForRepo(ctx context.Context, branchName, title, body, repoSlug string, verbose bool) (int, string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage("Creating PR: "+title))
	}

	// Detect the GitHub host from the git remote so that GitHub Enterprise Server
	// repositories are targeted correctly instead of defaulting to github.com.
	remoteHost := getHostFromOriginRemote()

	repoSpec := repoSlug
	if repoSpec == "" {
		// Use GH_HOST env var instead of --hostname (which is only valid for gh api, not gh repo view).
		repoOutput, err := createPRRunGHContextWithHost(ctx, "Fetching repository info...", remoteHost, "repo", "view", "--json", "owner,name")
		if err != nil {
			return 0, "", fmt.Errorf("could not get current repository info; ensure required prerequisites are configured, then retry: %w", err)
		}

		var repoInfo struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		}

		if err := json.Unmarshal(repoOutput, &repoInfo); err != nil {
			return 0, "", fmt.Errorf("could not parse repository info; the GitHub API response may be malformed or unexpected: %w", err)
		}

		repoSpec = fmt.Sprintf("%s/%s", repoInfo.Owner.Login, repoInfo.Name)
	}

	// Build gh pr create args. Explicitly specifying --repo ensures the PR is created in the
	// current repo (not an upstream fork). Use GH_HOST env var instead of --hostname
	// (which is only valid for gh api, not gh pr create).
	prCreateArgs := []string{"pr", "create", "--repo", repoSpec, "--title", title, "--body", body, "--head", branchName}
	output, err := createPRRunGHContextWithHost(ctx, "Creating pull request...", remoteHost, prCreateArgs...)
	if err != nil {
		// Try to get stderr for better error reporting
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return 0, "", fmt.Errorf("could not create pull request; ensure branch permissions and PR inputs are valid, then retry. gh output: %s; gh stderr: %s; underlying error: %w", string(output), string(exitError.Stderr), err)
		}
		return 0, "", fmt.Errorf("could not create PR; ensure required prerequisites are configured, then retry: %w", err)
	}

	prURL := strings.TrimSpace(string(output))

	// Parse PR number from URL (e.g., https://github.com/owner/repo/pull/123)
	prNumber := 0
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		if num, parseErr := strconv.Atoi(parts[len(parts)-1]); parseErr == nil {
			prNumber = num
		}
	}

	return prNumber, prURL, nil
}
