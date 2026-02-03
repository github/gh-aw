package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var prLog = logger.New("cli:pr_command")

// PRInfo represents the details of a pull request
type PRInfo struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	HeadSHA     string `json:"headSHA"`
	BaseBranch  string `json:"baseBranch"`
	HeadBranch  string `json:"headBranch"`
	SourceRepo  string `json:"sourceRepo"`
	TargetRepo  string `json:"targetRepo"`
	AuthorLogin string `json:"authorLogin"`
}

// NewPRCommand creates the main pr command with subcommands
func NewPRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request utilities",
		Long: `Pull request management utilities for transferring PRs between repositories.

This command provides tools for transferring pull requests from one repository
to another, including the code changes, title, and description. Useful for
migrating work from trial repositories to production repositories.

Available subcommands:
  • transfer - Transfer a PR from one repository to another

Examples:
  gh aw pr transfer https://github.com/trial/repo/pull/234
  gh aw pr transfer https://github.com/source/repo/pull/123 --repo owner/target
  gh aw pr transfer https://github.com/gh-aw-trial/repo/pull/5 --repo owner/prod-repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
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

Examples:
  gh aw pr transfer https://github.com/trial/repo/pull/234
  gh aw pr transfer https://github.com/PR-OWNER/PR-REPO/pull/234 --repo owner/target-repo

The command will:
1. Fetch the PR details (title, body, changes)
2. Apply changes as a single squashed commit
3. Create a new PR in the target repository
4. Copy the original title and description`,
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
	cmd.Flags().BoolP("verbose", "v", false, "Verbose output")

	return cmd
}

// parsePRURL extracts owner, repo, and PR number from a GitHub PR URL
func parsePRURL(prURL string) (owner, repo string, prNumber int, err error) {
	return parser.ParsePRURL(prURL)
}

// checkRepositoryAccess checks if the current user has write access to the target repository
func checkRepositoryAccess(owner, repo string) (bool, error) {
	prLog.Printf("Checking repository access: %s/%s", owner, repo)

	// Get current user
	output, err := workflow.RunGH("Fetching user info...", "api", "/user", "--jq", ".login")
	if err != nil {
		prLog.Printf("Failed to get current user: %s", err)
		return false, fmt.Errorf("failed to get current user: %w", err)
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
		return false, fmt.Errorf("failed to parse permission info: %w", err)
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
		return "", "", fmt.Errorf("failed to get current user: %w", err)
	}
	currentUser := strings.TrimSpace(string(output))

	// Check if fork already exists
	forkRepoSpec := fmt.Sprintf("%s/%s", currentUser, targetRepo)
	checkCmd := workflow.ExecGH("repo", "view", forkRepoSpec, "--json", "name")
	if checkCmd.Run() == nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fork already exists: %s", forkRepoSpec)))
		}
		return currentUser, targetRepo, nil
	}

	// Create fork
	_, err = workflow.RunGH(fmt.Sprintf("Creating fork of %s/%s...", targetOwner, targetRepo), "repo", "fork", fmt.Sprintf("%s/%s", targetOwner, targetRepo), "--clone=false")
	if err != nil {
		return "", "", fmt.Errorf("failed to create fork: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully created fork: %s", forkRepoSpec)))
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
		return nil, fmt.Errorf("failed to fetch PR info: %w", err)
	}

	var prInfo PRInfo
	if err := json.Unmarshal(output, &prInfo); err != nil {
		return nil, fmt.Errorf("failed to parse PR info: %w", err)
	}

	prLog.Printf("Fetched PR #%d: state=%s, author=%s", prInfo.Number, prInfo.State, prInfo.AuthorLogin)
	return &prInfo, nil
}

// createPatchFromPR creates a git patch from the PR changes using gh pr diff
func createPatchFromPR(sourceOwner, sourceRepo string, prInfo *PRInfo, verbose bool) (string, error) {
	// Create a temporary directory for the patch
	tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	patchFile := filepath.Join(tempDir, "pr.patch")

	// Use gh pr diff command directly - this is the most reliable method
	diffContent, err := workflow.RunGH("Fetching pull request diff...", "pr", "diff", fmt.Sprintf("%d", prInfo.Number), "--repo", fmt.Sprintf("%s/%s", sourceOwner, sourceRepo))
	if err != nil {
		return "", fmt.Errorf("failed to get PR diff: %w", err)
	}

	if len(diffContent) == 0 {
		return "", fmt.Errorf("PR diff is empty")
	}

	// Create proper mailbox format patch that git am expects
	var patchBuilder strings.Builder

	// Required mailbox format headers for git am
	fmt.Fprintf(&patchBuilder, "From %s Mon Sep 17 00:00:00 2001\n", prInfo.HeadSHA)
	fmt.Fprintf(&patchBuilder, "From: %s <%s@users.noreply.github.com>\n", prInfo.AuthorLogin, prInfo.AuthorLogin)
	fmt.Fprintf(&patchBuilder, "Date: %s\n", time.Now().Format(time.RFC1123))
	fmt.Fprintf(&patchBuilder, "Subject: [PATCH] %s\n", prInfo.Title)
	patchBuilder.WriteString("\n")

	if prInfo.Body != "" {
		fmt.Fprintf(&patchBuilder, "%s\n", prInfo.Body)
		patchBuilder.WriteString("\n")
	}

	fmt.Fprintf(&patchBuilder, "Original-PR: %s#%d\n", prInfo.SourceRepo, prInfo.Number)
	fmt.Fprintf(&patchBuilder, "Original-Author: %s\n", prInfo.AuthorLogin)
	patchBuilder.WriteString("---\n")

	// Add the actual diff content
	patchBuilder.Write(diffContent)

	if err := os.WriteFile(patchFile, []byte(patchBuilder.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write patch file: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Successfully created patch using gh pr diff"))
	}

	return patchFile, nil
} // applyPatchToRepo applies a patch to the target repository and returns the branch name
func applyPatchToRepo(patchFile string, prInfo *PRInfo, targetOwner, targetRepo string, verbose bool) (string, error) {
	// Get current branch to restore later
	cmd := exec.Command("git", "branch", "--show-current")
	currentBranchOutput, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	currentBranch := strings.TrimSpace(string(currentBranchOutput))

	// Get the default branch of the target repository
	defaultBranchOutput, err := workflow.RunGH("Fetching default branch...", "api", fmt.Sprintf("/repos/%s/%s", targetOwner, targetRepo), "--jq", ".default_branch")
	if err != nil {
		return "", fmt.Errorf("failed to get default branch: %w", err)
	}
	defaultBranch := strings.TrimSpace(string(defaultBranchOutput))

	// Ensure we're on the latest version of the default branch
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checking out and updating %s branch...", defaultBranch)))
	}

	cmd = exec.Command("git", "checkout", defaultBranch)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to checkout default branch %s: %w", defaultBranch, err)
	}

	cmd = exec.Command("git", "pull", "origin", defaultBranch)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to pull latest %s: %w", defaultBranch, err)
	}

	// Create a new branch for the transfer based on the updated default branch
	branchName := fmt.Sprintf("transfer-pr-%d-%d", prInfo.Number, time.Now().Unix())
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Creating branch: %s", branchName)))
	}

	cmd = exec.Command("git", "checkout", "-b", branchName)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create new branch: %w", err)
	}

	// Apply the patch
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying patch..."))

		// Show some info about the patch file
		patchContent, err := os.ReadFile(patchFile)
		if err == nil {
			lines := strings.Split(string(patchContent), "\n")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Patch file has %d lines", len(lines))))
			if len(lines) > 0 {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("First line: %s", lines[0])))
			}
		}
	}

	// Check if patch looks like a mailbox format (starts with "From ")
	patchContent, err := os.ReadFile(patchFile)
	if err != nil {
		return "", fmt.Errorf("failed to read patch file: %w", err)
	}

	var appliedWithAm bool
	isMailboxFormat := strings.HasPrefix(string(patchContent), "From ")

	if isMailboxFormat {
		// Try git am for mailbox format patches
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying mailbox format patch with git am..."))
		}

		cmd = exec.Command("git", "am", patchFile)
		if err := cmd.Run(); err == nil {
			appliedWithAm = true
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Successfully applied patch with git am"))
			}
		} else {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("git am failed, trying git apply..."))
			}
			// Reset any partial am state
			_ = exec.Command("git", "am", "--abort").Run()
		}
	}

	if !appliedWithAm {
		// Try git apply for standard diff format or as fallback
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying patch with git apply..."))
		}

		cmd = exec.Command("git", "apply", "--3way", patchFile)
		if err := cmd.Run(); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("3-way merge failed, trying with whitespace options..."))
			}

			// Try with --ignore-space-change and --ignore-whitespace
			cmd = exec.Command("git", "apply", "--ignore-space-change", "--ignore-whitespace", patchFile)
			if err := cmd.Run(); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Standard apply failed, trying with --reject to see what failed..."))

					// Try with --reject to see which parts fail
					rejectCmd := exec.Command("git", "apply", "--reject", patchFile)
					rejectOutput, _ := rejectCmd.CombinedOutput()
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Patch rejection details:"))
					fmt.Fprintln(os.Stderr, string(rejectOutput))
				}

				// Try to reset back to original branch and clean up
				_ = exec.Command("git", "checkout", currentBranch).Run()
				_ = exec.Command("git", "branch", "-D", branchName).Run()
				return "", fmt.Errorf("failed to apply patch: %w. You may need to resolve conflicts manually", err)
			}
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Successfully applied patch with git apply"))
		}
	} // If we didn't use git am, we need to stage and commit manually
	if !appliedWithAm {
		// Stage all changes
		cmd = exec.Command("git", "add", ".")
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to stage changes: %w", err)
		}

		// Create commit with meaningful message
		commitMsg := fmt.Sprintf("Transfer PR #%d from %s\n\n%s", prInfo.Number, prInfo.SourceRepo, prInfo.Title)
		if prInfo.Body != "" {
			commitMsg += "\n\n" + prInfo.Body
		}
		commitMsg += fmt.Sprintf("\n\nOriginal-PR: %s#%d", prInfo.SourceRepo, prInfo.Number)
		commitMsg += fmt.Sprintf("\nOriginal-Author: %s", prInfo.AuthorLogin)

		cmd = exec.Command("git", "commit", "-m", commitMsg)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to commit changes: %w", err)
		}
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applied patch using git am (includes commit)"))
	}

	return branchName, nil
}

// createTransferPR creates a new PR in the target repository
func createTransferPR(targetOwner, targetRepo string, prInfo *PRInfo, branchName string, verbose bool) error {
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
			return fmt.Errorf("failed to create fork: %w", err)
		}

		// Add fork as remote if not already present
		remoteName := "fork"
		forkRepoURL := fmt.Sprintf("https://github.com/%s/%s.git", forkOwner, forkRepo)

		// Check if fork remote exists
		checkRemoteCmd := exec.Command("git", "remote", "get-url", remoteName)
		if checkRemoteCmd.Run() != nil {
			// Remote doesn't exist, add it
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding fork remote: %s", forkRepoURL)))
			}
			addRemoteCmd := exec.Command("git", "remote", "add", remoteName, forkRepoURL)
			if err := addRemoteCmd.Run(); err != nil {
				return fmt.Errorf("failed to add fork remote: %w", err)
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
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding upstream remote: %s", targetRepoURL)))
				}
				addUpstreamCmd := exec.Command("git", "remote", "add", upstreamRemote, targetRepoURL)
				if err := addUpstreamCmd.Run(); err != nil {
					return fmt.Errorf("failed to add upstream remote: %w", err)
				}
			} else {
				// Remote exists but points to wrong repo, update it
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Updating upstream remote: %s", targetRepoURL)))
				}
				setUpstreamCmd := exec.Command("git", "remote", "set-url", upstreamRemote, targetRepoURL)
				if err := setUpstreamCmd.Run(); err != nil {
					return fmt.Errorf("failed to update upstream remote: %w", err)
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
			return fmt.Errorf("failed to push branch to fork: %w", err)
		}
		return fmt.Errorf("failed to push branch: %w", err)
	}

	// Create PR body with original info
	prBody := prInfo.Body
	if prBody != "" {
		prBody += "\n\n---\n\n"
	}
	prBody += fmt.Sprintf("**Transferred from:** %s#%d\n", prInfo.SourceRepo, prInfo.Number)
	prBody += fmt.Sprintf("**Original Author:** @%s", prInfo.AuthorLogin)

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
		return fmt.Errorf("failed to create PR: %w", err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("PR created successfully!"))
	if needsFork {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("PR created from fork %s/%s to %s/%s", forkOwner, forkRepo, targetOwner, targetRepo)))
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("URL: %s", strings.TrimSpace(string(output)))))

	return nil
}

// transferPR is the main function that orchestrates the PR transfer
func transferPR(prURL, targetRepo string, verbose bool) error {
	prLog.Printf("Starting PR transfer: url=%s, targetRepo=%s", prURL, targetRepo)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting PR transfer..."))
	}

	// Parse PR URL
	sourceOwner, sourceRepoName, prNumber, err := parsePRURL(prURL)
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
			return fmt.Errorf("invalid target repository format: %w", err)
		}
		parts := strings.SplitN(repoSpec.RepoSlug, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid target repository format, expected: owner/repo")
		}
		targetOwner, targetRepoName = parts[0], parts[1]
	} else {
		// Use current repository as target
		slug, err := GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("failed to determine target repository: %w", err)
		}
		targetOwner, targetRepoName, err = SplitRepoSlug(slug)
		if err != nil {
			return fmt.Errorf("failed to parse target repository: %w", err)
		}
	}

	prLog.Printf("Determined target repository: %s/%s", targetOwner, targetRepoName)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Target: %s/%s", targetOwner, targetRepoName)))
	}

	// Check if source and target are the same
	if sourceOwner == targetOwner && sourceRepoName == targetRepoName {
		prLog.Print("Source and target repositories are the same - aborting")
		return fmt.Errorf("source and target repositories cannot be the same")
	}

	// Ensure we're in the correct git repository
	var workingDir string
	var needsCleanup bool

	if targetRepo != "" {
		// Check if we're already in the target repository
		if isGitRepo() {
			slug, err := GetCurrentRepoSlug()
			if err == nil {
				currentOwner, currentRepoName, err := SplitRepoSlug(slug)
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
						return fmt.Errorf("failed to create temp directory for repo: %w", err)
					}

					cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
					if err := cloneCmd.Run(); err != nil {
						// Clean up temporary directory on error
						if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
						}
						return fmt.Errorf("failed to clone target repository: %w", err)
					}

					workingDir = tempDir
					needsCleanup = true

					// Change to the cloned repository directory
					if err := os.Chdir(tempDir); err != nil {
						// Clean up temporary directory on error
						if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
						}
						return fmt.Errorf("failed to change to cloned repository directory: %w", err)
					}
				}
			} else {
				// Error getting current repo, clone anyway
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning target repository %s/%s...", targetOwner, targetRepoName)))
				}
				tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-repo-")
				if err != nil {
					return fmt.Errorf("failed to create temp directory for repo: %w", err)
				}

				cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
				if err := cloneCmd.Run(); err != nil {
					// Clean up temporary directory on error
					if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
					}
					return fmt.Errorf("failed to clone target repository: %w", err)
				}

				workingDir = tempDir
				needsCleanup = true

				// Change to the cloned repository directory
				if err := os.Chdir(tempDir); err != nil {
					// Clean up temporary directory on error
					if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
					}
					return fmt.Errorf("failed to change to cloned repository directory: %w", err)
				}
			}
		} else {
			// We're not in a git repository and need to clone
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cloning target repository %s/%s...", targetOwner, targetRepoName)))
			}
			tempDir, err := os.MkdirTemp("", "gh-aw-pr-transfer-repo-")
			if err != nil {
				return fmt.Errorf("failed to create temp directory for repo: %w", err)
			}

			cloneCmd := workflow.ExecGH("repo", "clone", fmt.Sprintf("%s/%s", targetOwner, targetRepoName), tempDir)
			if err := cloneCmd.Run(); err != nil {
				// Clean up temporary directory on error
				if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
				}
				return fmt.Errorf("failed to clone target repository: %w", err)
			}

			workingDir = tempDir
			needsCleanup = true

			// Change to the cloned repository directory
			if err := os.Chdir(tempDir); err != nil {
				// Clean up temporary directory on error
				if rmErr := os.RemoveAll(tempDir); rmErr != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("failed to clean up temporary directory %s: %v", tempDir, rmErr)))
				}
				return fmt.Errorf("failed to change to cloned repository directory: %w", err)
			}
		}
	} else {
		// Using current repository as target
		if !isGitRepo() {
			return fmt.Errorf("not in a git repository")
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

// createPR creates a pull request using GitHub CLI and returns the PR number
func createPR(branchName, title, body string, verbose bool) (int, string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Creating PR: %s", title)))
	}

	// Get the current repository info to ensure PR is created in the correct repo
	repoOutput, err := workflow.RunGH("Fetching repository info...", "repo", "view", "--json", "owner,name")
	if err != nil {
		return 0, "", fmt.Errorf("failed to get current repository info: %w", err)
	}

	var repoInfo struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}

	if err := json.Unmarshal(repoOutput, &repoInfo); err != nil {
		return 0, "", fmt.Errorf("failed to parse repository info: %w", err)
	}

	repoSpec := fmt.Sprintf("%s/%s", repoInfo.Owner.Login, repoInfo.Name)

	// Explicitly specify the repository to ensure PR is created in the current repo (not upstream)
	output, err := workflow.RunGH("Creating pull request...", "pr", "create", "--repo", repoSpec, "--title", title, "--body", body, "--head", branchName)
	if err != nil {
		// Try to get stderr for better error reporting
		if exitError, ok := err.(*exec.ExitError); ok {
			return 0, "", fmt.Errorf("failed to create PR: %w\nOutput: %s\nError: %s", err, string(output), string(exitError.Stderr))
		}
		return 0, "", fmt.Errorf("failed to create PR: %w", err)
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
