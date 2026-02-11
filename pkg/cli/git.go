package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var gitLog = logger.New("cli:git")

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// findGitRoot finds the root directory of the git repository
func findGitRoot() (string, error) {
	gitLog.Print("Finding git root directory")
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to find git root: %v", err)
		return "", fmt.Errorf("not in a git repository or git command failed: %w", err)
	}
	gitRoot := strings.TrimSpace(string(output))
	gitLog.Printf("Found git root: %s", gitRoot)
	return gitRoot, nil
}

// findGitRootForPath finds the root directory of the git repository containing the specified path
func findGitRootForPath(path string) (string, error) {
	gitLog.Printf("Finding git root for path: %s", path)

	// Get absolute path first
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Validate the absolute path
	absPath, err = fileutil.ValidateAbsolutePath(absPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Use the directory containing the file
	dir := filepath.Dir(absPath)

	// Run git command in the file's directory
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get repository root for path %s: %w", path, err)
	}
	gitRoot := strings.TrimSpace(string(output))
	gitLog.Printf("Found git root for path: %s", gitRoot)
	return gitRoot, nil
}

// parseGitHubRepoSlugFromURL extracts owner/repo from a GitHub URL
// Supports both HTTPS (https://github.com/owner/repo) and SSH (git@github.com:owner/repo) formats
func parseGitHubRepoSlugFromURL(url string) string {
	gitLog.Printf("Parsing GitHub repo slug from URL: %s", url)

	// Remove .git suffix if present
	url = strings.TrimSuffix(url, ".git")

	// Handle HTTPS URLs: https://github.com/owner/repo
	if strings.HasPrefix(url, "https://github.com/") {
		slug := strings.TrimPrefix(url, "https://github.com/")
		gitLog.Printf("Extracted slug from HTTPS URL: %s", slug)
		return slug
	}

	// Handle SSH URLs: git@github.com:owner/repo
	if strings.HasPrefix(url, "git@github.com:") {
		slug := strings.TrimPrefix(url, "git@github.com:")
		gitLog.Printf("Extracted slug from SSH URL: %s", slug)
		return slug
	}

	gitLog.Print("Could not extract slug from URL")
	return ""
}

// getRepositorySlugFromRemote extracts the repository slug (owner/repo) from git remote URL
func getRepositorySlugFromRemote() string {
	gitLog.Print("Getting repository slug from git remote")

	// Try to get from git remote URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to get remote URL: %v", err)
		return ""
	}

	url := strings.TrimSpace(string(output))
	slug := parseGitHubRepoSlugFromURL(url)

	if slug != "" {
		gitLog.Printf("Repository slug: %s", slug)
	}

	return slug
}

// getRepositorySlugFromRemoteForPath extracts the repository slug (owner/repo) from the git remote URL
// of the repository containing the specified file path
func getRepositorySlugFromRemoteForPath(path string) string {
	gitLog.Printf("Getting repository slug for path: %s", path)

	// Get absolute path first
	absPath, err := filepath.Abs(path)
	if err != nil {
		gitLog.Printf("Failed to get absolute path: %v", err)
		return ""
	}

	// Validate the absolute path
	absPath, err = fileutil.ValidateAbsolutePath(absPath)
	if err != nil {
		gitLog.Printf("Invalid path: %v", err)
		return ""
	}

	// Use the directory containing the file
	dir := filepath.Dir(absPath)

	// Try to get from git remote URL in the file's repository
	cmd := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to get remote URL for path: %v", err)
		return ""
	}

	url := strings.TrimSpace(string(output))
	slug := parseGitHubRepoSlugFromURL(url)

	if slug != "" {
		gitLog.Printf("Repository slug for path: %s", slug)
	}

	return slug
}

func stageWorkflowChanges() {
	// Find git root and add .github/workflows relative to it
	if gitRoot, err := findGitRoot(); err == nil {
		workflowsPath := filepath.Join(gitRoot, ".github/workflows/")
		_ = exec.Command("git", "-C", gitRoot, "add", workflowsPath).Run()

		// Also stage .gitattributes if it was modified
		_ = stageGitAttributesIfChanged()
	} else {
		// Fallback to relative path if git root can't be found
		_ = exec.Command("git", "add", ".github/workflows/").Run()
		_ = exec.Command("git", "add", ".gitattributes").Run()
	}
}

// ensureGitAttributes ensures that .gitattributes contains the entry to mark .lock.yml files as generated
func ensureGitAttributes() error {
	gitLog.Print("Ensuring .gitattributes is updated")
	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	gitAttributesPath := filepath.Join(gitRoot, ".gitattributes")
	lockYmlEntry := ".github/workflows/*.lock.yml linguist-generated=true merge=ours"
	requiredEntries := []string{lockYmlEntry}

	// Read existing .gitattributes file if it exists
	var lines []string
	if content, err := os.ReadFile(gitAttributesPath); err == nil {
		lines = strings.Split(string(content), "\n")
		gitLog.Printf("Read existing .gitattributes with %d lines", len(lines))
	} else {
		gitLog.Print("No existing .gitattributes file found")
	}

	modified := false
	for _, required := range requiredEntries {
		found := false
		for i, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == required {
				found = true
				break
			}
			// Check for old format entries that need updating
			if strings.HasPrefix(trimmedLine, ".github/workflows/*.lock.yml") && required == lockYmlEntry {
				gitLog.Print("Updating old .gitattributes entry format")
				lines[i] = lockYmlEntry
				found = true
				modified = true
				break
			}
		}

		if !found {
			gitLog.Printf("Adding new .gitattributes entry: %s", required)
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, required)
			modified = true
		}
	}

	if !modified {
		gitLog.Print(".gitattributes already contains required entries")
		return nil
	}

	// Write back to file with owner-only read/write permissions (0600) for security best practices
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(gitAttributesPath, []byte(content), 0600); err != nil {
		gitLog.Printf("Failed to write .gitattributes: %v", err)
		return fmt.Errorf("failed to write .gitattributes: %w", err)
	}

	gitLog.Print("Successfully updated .gitattributes")
	return nil
}

// stageGitAttributesIfChanged stages .gitattributes if it was modified
func stageGitAttributesIfChanged() error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return err
	}
	gitAttributesPath := filepath.Join(gitRoot, ".gitattributes")
	return exec.Command("git", "-C", gitRoot, "add", gitAttributesPath).Run()
}

// ensureLogsGitignore ensures that .github/aw/logs/.gitignore exists to ignore log files
func ensureLogsGitignore() error {
	gitLog.Print("Ensuring .github/aw/logs/.gitignore exists")
	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	logsDir := filepath.Join(gitRoot, ".github", "aw", "logs")
	gitignorePath := filepath.Join(logsDir, ".gitignore")

	// Check if .gitignore already exists
	if _, err := os.Stat(gitignorePath); err == nil {
		gitLog.Print(".github/aw/logs/.gitignore already exists")
		return nil
	}

	gitLog.Print("Creating .github/aw/logs directory and .gitignore")
	// Create the logs directory if it doesn't exist
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		gitLog.Printf("Failed to create logs directory: %v", err)
		return fmt.Errorf("failed to create .github/aw/logs directory: %w", err)
	}

	// Write the .gitignore file with owner-only read/write permissions (0600) for security best practices
	gitignoreContent := `# Ignore all downloaded workflow logs
*

# But keep the .gitignore file itself
!.gitignore
`
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0600); err != nil {
		gitLog.Printf("Failed to write .gitignore: %v", err)
		return fmt.Errorf("failed to write .github/aw/logs/.gitignore: %w", err)
	}

	gitLog.Print("Successfully created .github/aw/logs/.gitignore")
	return nil
}

// getCurrentBranch gets the current git branch name
func getCurrentBranch() (string, error) {
	gitLog.Print("Getting current git branch")
	cmd := exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to get current branch: %v", err)
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	branch := strings.TrimSpace(string(output))
	if branch == "" {
		gitLog.Print("Could not determine current branch")
		return "", fmt.Errorf("could not determine current branch")
	}

	gitLog.Printf("Current branch: %s", branch)
	return branch, nil
}

// createAndSwitchBranch creates a new branch and switches to it
func createAndSwitchBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, fmt.Sprintf("Creating and switching to branch: %s", branchName))

	cmd := exec.Command("git", "checkout", "-b", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create and switch to branch %s: %w", branchName, err)
	}

	return nil
}

// switchBranch switches to the specified branch
func switchBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, fmt.Sprintf("Switching to branch: %s", branchName))

	cmd := exec.Command("git", "checkout", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to branch %s: %w", branchName, err)
	}

	return nil
}

// commitChanges commits all staged changes with the given message
func commitChanges(message string, verbose bool) error {
	console.LogVerbose(verbose, fmt.Sprintf("Committing changes with message: %s", message))

	cmd := exec.Command("git", "commit", "-m", message)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	return nil
}

// pushBranch pushes the specified branch to origin
func pushBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, fmt.Sprintf("Pushing branch: %s", branchName))

	cmd := exec.Command("git", "push", "-u", "origin", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}

// checkCleanWorkingDirectory checks if there are uncommitted changes
func checkCleanWorkingDirectory(verbose bool) error {
	console.LogVerbose(verbose, "Checking for uncommitted changes...")

	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if len(strings.TrimSpace(string(output))) > 0 {
		return fmt.Errorf("working directory has uncommitted changes, please commit or stash them first")
	}

	console.LogVerbose(verbose, "Working directory is clean")
	return nil
}

// WorkflowFileStatus represents the status of a workflow file in git
type WorkflowFileStatus struct {
	IsModified         bool // File has unstaged changes
	IsStaged           bool // File has staged changes
	HasUnpushedCommits bool // File has unpushed commits affecting it
}

// ToGitRootRelativePath converts a path to be relative to the git root directory
// This provides stable paths regardless of the current working directory
func ToGitRootRelativePath(path string) string {
	gitLog.Printf("Normalizing path to git root: %s", path)

	// If it's not an absolute path, try to make it absolute first
	absPath := path
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			gitLog.Printf("Failed to get working directory: %v", err)
			return path
		}
		absPath = filepath.Join(wd, path)
		gitLog.Printf("Converted to absolute path: %s", absPath)
	}

	// Validate and clean the absolute path
	absPath, err := fileutil.ValidateAbsolutePath(absPath)
	if err != nil {
		gitLog.Printf("Invalid path: %v", err)
		return path
	}

	// Find the git root by looking for .github directory
	// Walk up the directory tree to find it
	currentDir := filepath.Dir(absPath)
	for {
		githubDir := filepath.Join(currentDir, ".github")
		if info, err := os.Stat(githubDir); err == nil && info.IsDir() {
			gitLog.Printf("Found .github directory at: %s", currentDir)
			// Found the git root, now make path relative to it
			relPath, err := filepath.Rel(currentDir, absPath)
			if err != nil {
				gitLog.Printf("Failed to make path relative to git root: %v", err)
				return path
			}
			gitLog.Printf("Normalized path: %s", relPath)
			return relPath
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			// Reached the root of the filesystem without finding .github
			// Fall back to current working directory relative path
			gitLog.Printf("Could not find .github directory, falling back to current working directory")
			wd, err := os.Getwd()
			if err != nil {
				return path
			}
			relPath, err := filepath.Rel(wd, absPath)
			if err != nil {
				return path
			}
			return relPath
		}
		currentDir = parentDir
	}
}

// checkWorkflowFileStatus checks if a workflow file has local modifications, staged changes, or unpushed commits
func checkWorkflowFileStatus(workflowPath string) (*WorkflowFileStatus, error) {
	gitLog.Printf("Checking status for workflow file: %s", workflowPath)

	status := &WorkflowFileStatus{}

	// Check if we're in a git repository
	if !isGitRepo() {
		gitLog.Print("Not in a git repository")
		return status, nil
	}

	// Get the absolute path relative to git root
	gitRoot, err := findGitRoot()
	if err != nil {
		gitLog.Printf("Failed to find git root: %v", err)
		return status, nil // Not in a git repository, return empty status
	}

	// Make path relative to git root if it's absolute
	var relPath string
	if filepath.IsAbs(workflowPath) {
		var err error
		relPath, err = filepath.Rel(gitRoot, workflowPath)
		if err != nil {
			gitLog.Printf("Failed to make path relative: %v", err)
			relPath = workflowPath
		}
	} else {
		relPath = workflowPath
	}

	gitLog.Printf("Checking git status for: %s", relPath)

	// Check for modified or staged changes using git status --porcelain
	cmd := exec.Command("git", "-C", gitRoot, "status", "--porcelain", relPath)
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to check git status: %v", err)
		return status, nil // Ignore error, return empty status
	}

	statusOutput := string(output) // Don't trim - the leading space is significant!
	if len(statusOutput) > 0 {
		gitLog.Printf("Git status output: %q", statusOutput)
		// Parse the status line (format: XY filename)
		// X = index (staged) status, Y = working tree (unstaged) status
		// The format is exactly 2 characters followed by a space and then the filename
		if len(statusOutput) >= 2 {
			stagedStatus := statusOutput[0]
			unstagedStatus := statusOutput[1]

			// Check if file is staged (first character is not space or ?)
			if stagedStatus != ' ' && stagedStatus != '?' {
				status.IsStaged = true
				gitLog.Print("File has staged changes")
			}

			// Check if file is modified in working tree (second character is M or other modification indicators)
			if unstagedStatus == 'M' || unstagedStatus == 'D' || unstagedStatus == 'A' {
				status.IsModified = true
				gitLog.Print("File has unstaged modifications")
			}
		}
	}

	// Check for unpushed commits that affect this file
	// First, check if there's a remote tracking branch
	cmd = exec.Command("git", "-C", gitRoot, "rev-parse", "--abbrev-ref", "@{u}")
	output, err = cmd.Output()
	if err != nil {
		// No upstream branch configured, skip unpushed commits check
		gitLog.Print("No upstream branch configured")
		return status, nil
	}

	upstream := strings.TrimSpace(string(output))
	gitLog.Printf("Upstream branch: %s", upstream)

	// Check if there are commits in the current branch that affect this file and aren't in upstream
	cmd = exec.Command("git", "-C", gitRoot, "log", fmt.Sprintf("%s..HEAD", upstream), "--oneline", "--", relPath)
	output, err = cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to check unpushed commits: %v", err)
		return status, nil // Ignore error, return current status
	}

	if len(strings.TrimSpace(string(output))) > 0 {
		status.HasUnpushedCommits = true
		gitLog.Print("File has unpushed commits")
	}

	return status, nil
}

// hasChangesToCommit checks if there are any changes in the working directory
func hasChangesToCommit() (bool, error) {
	gitLog.Print("Checking for modified files")
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to check git status: %v", err)
		return false, fmt.Errorf("failed to check git status: %w", err)
	}

	hasChanges := len(strings.TrimSpace(string(output))) > 0
	gitLog.Printf("Has changes to commit: %v", hasChanges)
	return hasChanges, nil
}

// hasRemote checks if a remote repository named 'origin' is configured
func hasRemote() bool {
	gitLog.Print("Checking for remote repository")
	cmd := exec.Command("git", "remote", "get-url", "origin")
	err := cmd.Run()
	hasRemoteRepo := err == nil
	gitLog.Printf("Has remote repository: %v", hasRemoteRepo)
	return hasRemoteRepo
}

// pullFromRemote pulls the latest changes from the remote repository using rebase
func pullFromRemote(verbose bool) error {
	gitLog.Print("Pulling latest changes from remote")
	pullCmd := exec.Command("git", "pull", "--rebase")
	if output, err := pullCmd.CombinedOutput(); err != nil {
		gitLog.Printf("Failed to pull changes: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to pull changes: %w", err)
	}
	gitLog.Print("Successfully pulled latest changes")
	return nil
}

// stageAllChanges stages all modified files using git add -A
func stageAllChanges(verbose bool) error {
	gitLog.Print("Staging all changes")
	addCmd := exec.Command("git", "add", "-A")
	if output, err := addCmd.CombinedOutput(); err != nil {
		gitLog.Printf("Failed to stage changes: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to stage changes: %w", err)
	}
	gitLog.Print("Successfully staged all changes")
	return nil
}

// pushToRemote pushes the current branch to the remote repository
func pushToRemote(verbose bool) error {
	gitLog.Print("Pushing changes to remote")
	pushCmd := exec.Command("git", "push")
	if output, err := pushCmd.CombinedOutput(); err != nil {
		gitLog.Printf("Failed to push changes: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to push changes: %w\nOutput: %s", err, string(output))
	}
	gitLog.Print("Successfully pushed changes to remote")
	return nil
}

// commitAndPushChanges is a helper that orchestrates the full commit and push workflow
// It checks for changes, pulls from remote (if exists), stages all changes, commits, and pushes (if remote exists)
func commitAndPushChanges(commitMessage string, verbose bool) error {
	gitLog.Printf("Starting commit and push workflow with message: %s", commitMessage)

	// Check if there are any changes to commit
	hasChanges, err := hasChangesToCommit()
	if err != nil {
		return err
	}

	if !hasChanges {
		gitLog.Print("No changes to commit")
		return nil
	}

	// Pull latest changes from remote before committing (if remote exists)
	if hasRemote() {
		gitLog.Print("Remote repository exists, pulling latest changes")
		if err := pullFromRemote(verbose); err != nil {
			return err
		}
	} else {
		gitLog.Print("No remote repository configured, skipping pull")
	}

	// Stage all modified files
	if err := stageAllChanges(verbose); err != nil {
		return err
	}

	// Commit the changes
	gitLog.Printf("Committing changes with message: %s", commitMessage)
	if err := commitChanges(commitMessage, verbose); err != nil {
		gitLog.Printf("Failed to commit changes: %v", err)
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	// Push the changes (only if remote exists)
	if hasRemote() {
		gitLog.Print("Remote repository exists, pushing changes")
		if err := pushToRemote(verbose); err != nil {
			return err
		}
	} else {
		gitLog.Print("No remote repository configured, skipping push")
	}

	gitLog.Print("Commit and push workflow completed successfully")
	return nil
}

// getDefaultBranch gets the default branch name for the repository
func getDefaultBranch() (string, error) {
	gitLog.Print("Getting default branch name")

	// Get repository slug (owner/repo)
	repoSlug := getRepositorySlugFromRemote()
	if repoSlug == "" {
		gitLog.Print("No remote repository configured, cannot determine default branch")
		return "", fmt.Errorf("no remote repository configured")
	}

	// Parse owner and repo from slug
	parts := strings.Split(repoSlug, "/")
	if len(parts) != 2 {
		gitLog.Printf("Invalid repository slug format: %s", repoSlug)
		return "", fmt.Errorf("invalid repository slug format: %s", repoSlug)
	}

	owner, repo := parts[0], parts[1]

	// Use gh CLI to get default branch from GitHub API
	cmd := exec.Command("gh", "api", fmt.Sprintf("/repos/%s/%s", owner, repo), "--jq", ".default_branch")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to get default branch: %v", err)
		return "", fmt.Errorf("failed to get default branch: %w", err)
	}

	defaultBranch := strings.TrimSpace(string(output))
	if defaultBranch == "" {
		gitLog.Print("Empty default branch returned")
		return "", fmt.Errorf("could not determine default branch")
	}

	gitLog.Printf("Default branch: %s", defaultBranch)
	return defaultBranch, nil
}

// checkOnDefaultBranch checks if the current branch is the default branch
// Returns an error if no remote is configured or if not on the default branch
func checkOnDefaultBranch(verbose bool) error {
	gitLog.Print("Checking if on default branch")

	// Get current branch
	currentBranch, err := getCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Get default branch
	defaultBranch, err := getDefaultBranch()
	if err != nil {
		// If no remote is configured, fail the push operation
		if strings.Contains(err.Error(), "no remote repository configured") {
			gitLog.Print("No remote configured, cannot push")
			return fmt.Errorf("--push requires a remote repository to be configured")
		}
		return fmt.Errorf("failed to get default branch: %w", err)
	}

	// Compare branches
	if currentBranch != defaultBranch {
		gitLog.Printf("Not on default branch: current=%s, default=%s", currentBranch, defaultBranch)
		return fmt.Errorf("not on default branch: current branch is '%s', default branch is '%s'", currentBranch, defaultBranch)
	}

	gitLog.Printf("On default branch: %s", currentBranch)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ On default branch: %s", currentBranch)))
	}
	return nil
}

// confirmPushOperation prompts the user to confirm push operation (skips in CI)
func confirmPushOperation(verbose bool) error {
	gitLog.Print("Checking if user confirmation is needed for push operation")

	// Skip confirmation in CI environments
	if IsRunningInCI() {
		gitLog.Print("Running in CI, skipping user confirmation")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running in CI - skipping confirmation prompt"))
		}
		return nil
	}

	// Prompt user for confirmation
	gitLog.Print("Prompting user for push confirmation")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("This will commit and push changes to the remote repository."))

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you want to proceed with commit and push?").
				Description("This will stage all changes, commit them, and push to the remote repository").
				Value(&confirmed),
		),
	).WithAccessible(console.IsAccessibleMode())

	if err := form.Run(); err != nil {
		gitLog.Printf("Confirmation prompt failed: %v", err)
		return fmt.Errorf("confirmation prompt failed: %w", err)
	}

	if !confirmed {
		gitLog.Print("User declined push operation")
		return fmt.Errorf("push operation cancelled by user")
	}

	gitLog.Print("User confirmed push operation")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Push operation confirmed"))
	}
	return nil
}
