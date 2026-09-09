package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var gitLog = logger.New("cli:git")

// isSafeGitRevisionArg reports whether ref cannot be misinterpreted as a git
// CLI flag or terminal input by rejecting empty strings, values starting with
// "-", and control characters. It does not validate that ref is a well-formed
// git revision.
func isSafeGitRevisionArg(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") && !containsControlCharacters(ref)
}

// validateRelPathForGit rejects relative paths that could be misinterpreted as
// a git CLI flag (a leading "-") or that escape the repository root via path
// traversal (a leading ".." path segment after cleaning), before the path is
// passed as an exec.Command argument to git.
func validateRelPathForGit(relPath string) error {
	if relPath == "" {
		return errors.New("path cannot be empty")
	}
	if strings.HasPrefix(relPath, "-") {
		return fmt.Errorf("path %q must not start with '-'", relPath)
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("path %q must not escape the repository root", relPath)
	}
	cleanedSlash := filepath.ToSlash(clean)
	if cleanedSlash == ".." || strings.HasPrefix(cleanedSlash, "../") {
		return fmt.Errorf("path %q must not escape the repository root", relPath)
	}
	return nil
}

func isGitRepo() bool {
	_, err := gitutil.FindGitRoot()
	return err == nil
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

	// Find git root using filesystem traversal from the file's directory
	gitRoot, err := gitutil.FindGitRootFrom(dir)
	if err != nil {
		return "", fmt.Errorf("failed to get repository root for path %s: %w", path, err)
	}
	gitLog.Printf("Found git root for path: %s", gitRoot)
	return gitRoot, nil
}

// parseGitHubRepoSlugFromURL extracts owner/repo from a GitHub URL
// Supports HTTPS (https://github.com/owner/repo), SCP-style SSH ([user@]github.com:owner/repo),
// and SSH URL scheme (ssh://git@github.com/owner/repo) formats.
// Also supports GitHub Enterprise URLs and non-standard SSH usernames (e.g. example@ghe.host:owner/repo).
func parseGitHubRepoSlugFromURL(url string) string {
	gitLog.Printf("Parsing GitHub repo slug from URL: %s", url)

	// Remove .git suffix if present
	url = strings.TrimSuffix(url, ".git")

	githubHost := getGitHubHost()
	githubHostWithoutScheme := strings.TrimPrefix(strings.TrimPrefix(githubHost, "https://"), "http://")

	// Handle HTTPS URLs: https://github.com/owner/repo or https://enterprise.github.com/owner/repo
	if after, ok := strings.CutPrefix(url, githubHost+"/"); ok {
		slug := after
		gitLog.Printf("Extracted slug from HTTPS URL: %s", slug)
		return slug
	}

	// Handle SCP-style SSH URLs with any username: git@github.com:owner/repo,
	// or non-standard usernames like example@example.ghe.com:owner/repo (GHE),
	// or username-less like github.com:owner/repo.
	// Match: [user@]<host>:<owner>/<repo>
	scpHostColon := githubHostWithoutScheme + ":"
	// Try with username (user@host:path)
	if _, afterAt, hasAt := strings.Cut(url, "@"); hasAt {
		if after, ok := strings.CutPrefix(afterAt, scpHostColon); ok {
			gitLog.Printf("Extracted slug from SCP-style SSH URL with username: %s", after)
			return after
		}
	}
	// Try without username (host:path)
	if after, ok := strings.CutPrefix(url, scpHostColon); ok {
		gitLog.Printf("Extracted slug from SCP-style SSH URL without username: %s", after)
		return after
	}

	// Handle SSH URL scheme: ssh://git@github.com/owner/repo or ssh://github.com/owner/repo
	if after, ok := strings.CutPrefix(url, "ssh://"); ok {
		// Strip optional user info (e.g. "git@")
		if _, userStripped, hasAt := strings.Cut(after, "@"); hasAt {
			after = userStripped
		}
		if slug, ok := strings.CutPrefix(after, githubHostWithoutScheme+"/"); ok {
			gitLog.Printf("Extracted slug from SSH URL scheme: %s", slug)
			return slug
		}
	}

	gitLog.Print("Could not extract slug from URL")
	return ""
}

// extractHostFromRemoteURL extracts the host (optionally including port) from a git remote URL.
// Supports HTTPS (https://host[:port]/path), HTTP (http://host[:port]/path), and SSH (git@host[:port]:path or ssh://git@host[:port]/path) formats.
// Returns the host portion as "host[:port]" when parsed, or "github.com" as the default if the URL cannot be parsed.
func extractHostFromRemoteURL(remoteURL string) string {
	// HTTPS / HTTP format: https://[userinfo@]host/path or http://[userinfo@]host/path
	// Use net/url.Parse to correctly handle all userinfo variants (user@, user:pass@,
	// and passwords containing '@') and to extract the bare host without credentials.
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(remoteURL, scheme) {
			if u, err := url.Parse(remoteURL); err == nil && u.Host != "" {
				return u.Host
			}
			// Fallback: strip scheme and any userinfo manually.
			after := remoteURL[len(scheme):]
			if host, _, found := strings.Cut(after, "/"); found {
				// Strip optional userinfo (everything up to and including the last '@').
				if idx := strings.LastIndex(host, "@"); idx >= 0 {
					host = host[idx+1:]
				}
				return host
			}
			if idx := strings.LastIndex(after, "@"); idx >= 0 {
				return after[idx+1:]
			}
			return after
		}
	}

	// SSH URL format: ssh://git@host/path or ssh://host/path
	if after, ok := strings.CutPrefix(remoteURL, "ssh://"); ok {
		// Strip optional user info (e.g. "git@")
		if _, userStripped, hasAt := strings.Cut(after, "@"); hasAt {
			after = userStripped
		}
		if host, _, found := strings.Cut(after, "/"); found {
			return host
		}
		return after
	}

	// SSH scp-like format: [user@]host:path — any username, not just git@
	// Try with username first (user@host:path)
	if _, afterAt, hasAt := strings.Cut(remoteURL, "@"); hasAt {
		if host, _, found := strings.Cut(afterAt, ":"); found {
			return host
		}
	}
	// Try without username (host:path)
	if host, _, found := strings.Cut(remoteURL, ":"); found {
		// Make sure this looks like an scp-style URL (has no slashes before the colon)
		// to avoid matching "ssh://host:port/path" or "https://host:port/path"
		if !strings.Contains(host, "/") {
			return host
		}
	}

	return "github.com"
}

// resolveRemoteURL resolves the best git remote URL to use for a given directory.
// It first tries the 'origin' remote for backward compatibility. If 'origin' is not
// configured but exactly one other remote exists, that remote is used instead.
// Returns the remote URL, the remote name used, and any error.
// dir may be empty to use the current working directory.
func resolveRemoteURL(dir string) (string, string, error) {
	gitArgs := func(args ...string) *exec.Cmd {
		if dir != "" {
			return exec.Command("git", append([]string{"-C", dir}, args...)...)
		}
		return exec.Command("git", args...)
	}

	// First try 'origin' for backward compatibility
	if output, err := gitArgs("config", "--get", "remote.origin.url").Output(); err == nil {
		url := strings.TrimSpace(string(output))
		if url != "" {
			gitLog.Print("Using 'origin' remote")
			return url, "origin", nil
		}
	}

	// Fall back: list all remotes
	output, err := gitArgs("remote").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to list git remotes: %w", err)
	}

	remoteNames := strings.Fields(strings.TrimSpace(string(output)))
	if len(remoteNames) == 0 {
		return "", "", errors.New("no git remotes configured")
	}
	if len(remoteNames) > 1 {
		return "", "", fmt.Errorf("multiple git remotes configured (%s), no 'origin' remote found", strings.Join(remoteNames, ", "))
	}

	// Exactly one remote — use it
	remoteName := remoteNames[0]
	urlOutput, err := gitArgs("config", "--get", "remote."+remoteName+".url").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get URL for remote %q: %w", remoteName, err)
	}

	url := strings.TrimSpace(string(urlOutput))
	gitLog.Printf("No 'origin' remote found; using single configured remote %q", remoteName)
	return url, remoteName, nil
}

// getHostFromOriginRemote returns the hostname of the git remote.
// It prefers the 'origin' remote for backward compatibility. If 'origin' is not
// configured but exactly one other remote exists, that remote is used instead.
// For example, a remote URL of "https://ghes.example.com/org/repo.git" returns "ghes.example.com",
// and "git@github.com:owner/repo.git" returns "github.com".
// Returns "github.com" as the default if the remote URL cannot be determined.
func getHostFromOriginRemote() string {
	remoteURL, remoteName, err := resolveRemoteURL("")
	if err != nil {
		gitLog.Printf("Failed to resolve remote URL: %v", err)
		return "github.com"
	}

	host := extractHostFromRemoteURL(remoteURL)
	gitLog.Printf("Detected GitHub host from remote %q: %s", remoteName, host)
	return host
}

// getRepositorySlugFromRemote extracts the repository slug (owner/repo) from git remote URL.
// It prefers the 'origin' remote for backward compatibility. If 'origin' is not
// configured but exactly one other remote exists, that remote is used instead.
func getRepositorySlugFromRemote() string {
	gitLog.Print("Getting repository slug from git remote")

	remoteURL, _, err := resolveRemoteURL("")
	if err != nil {
		gitLog.Printf("Failed to resolve remote URL: %v", err)
		return ""
	}

	slug := parseGitHubRepoSlugFromURL(remoteURL)
	if slug != "" {
		gitLog.Printf("Repository slug: %s", slug)
	}

	return slug
}

// getRepositorySlugFromRemotePreferringUpstream extracts the repository slug (owner/repo)
// from git remotes, preferring the 'upstream' remote when available.
// This keeps schedule scattering stable for fork checkouts where origin points to the fork.
func getRepositorySlugFromRemotePreferringUpstream() string {
	return getRepositorySlugFromDirPreferringUpstream("")
}

// getRepositorySlugFromDirPreferringUpstream extracts the repository slug (owner/repo)
// for a git working directory, preferring the 'upstream' remote when available.
func getRepositorySlugFromDirPreferringUpstream(dir string) string {
	gitArgs := func(args ...string) *exec.Cmd {
		if dir != "" {
			return exec.Command("git", append([]string{"-C", dir}, args...)...)
		}
		return exec.Command("git", args...)
	}

	if output, err := gitArgs("config", "--get", "remote.upstream.url").Output(); err == nil {
		upstreamURL := strings.TrimSpace(string(output))
		if upstreamURL != "" {
			slug := parseGitHubRepoSlugFromURL(upstreamURL)
			if slug != "" {
				gitLog.Printf("Repository slug from upstream remote: %s", slug)
				return slug
			}
			gitLog.Printf("Unable to parse repository slug from upstream remote URL %q; falling back", upstreamURL)
		}
	}

	remoteURL, _, err := resolveRemoteURL(dir)
	if err != nil {
		if dir == "" {
			gitLog.Printf("Failed to resolve remote URL: %v", err)
		} else {
			gitLog.Printf("Failed to resolve remote URL for path: %v", err)
		}
		return ""
	}

	slug := parseGitHubRepoSlugFromURL(remoteURL)
	if slug != "" {
		if dir == "" {
			gitLog.Printf("Repository slug: %s", slug)
		} else {
			gitLog.Printf("Repository slug for path: %s", slug)
		}
	}

	return slug
}

// getRepositorySlugFromRemoteForPath extracts the repository slug (owner/repo) from the git remote URL
// of the repository containing the specified file path.
// It prefers the 'upstream' remote when available, and otherwise follows standard
// remote resolution (origin first, then single-remote fallback).
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

	return getRepositorySlugFromDirPreferringUpstream(dir)
}

func stageWorkflowChanges() {
	// Find git root and add .github/workflows relative to it
	if gitRoot, err := gitutil.FindGitRoot(); err == nil {
		workflowsPath := filepath.Join(gitRoot, constants.WorkflowsDirSlash)
		_ = exec.Command("git", "-C", gitRoot, "add", workflowsPath).Run()

		// Also stage .gitattributes if it was modified
		_ = stageGitAttributesIfChanged()
	} else {
		// Fallback to relative path if git root can't be found
		_ = exec.Command("git", "add", constants.WorkflowsDirSlash).Run()
		_ = exec.Command("git", "add", ".gitattributes").Run()
	}
}

// ensureGitAttributes ensures that .gitattributes contains the entry to mark .lock.yml files as generated.
// It returns true if the file was modified, false if it was already up to date.
func ensureGitAttributes() (bool, error) {
	gitLog.Print("Ensuring .gitattributes is updated")
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return false, err // Not in a git repository, skip
	}

	gitAttributesPath := filepath.Join(gitRoot, ".gitattributes")
	lines, err := readGitAttributesFile(gitAttributesPath)
	if err != nil {
		return false, err
	}

	lines, modified := ensureRequiredGitAttributesEntry(lines)
	if !modified {
		gitLog.Print(".gitattributes already contains required entries")
		return false, nil
	}

	content := strings.Join(lines, "\n")
	if err := os.WriteFile(gitAttributesPath, []byte(content), constants.FilePermSensitive); err != nil {
		gitLog.Printf("Failed to write .gitattributes: %v", err)
		return false, fmt.Errorf("failed to write .gitattributes: %w", err)
	}

	gitLog.Print("Successfully updated .gitattributes")
	return true, nil
}

func readGitAttributesFile(path string) ([]string, error) {
	if content, err := os.ReadFile(path); err == nil {
		lines := strings.Split(string(content), "\n")
		gitLog.Printf("Read existing .gitattributes with %d lines", len(lines))
		return lines, nil
	}
	gitLog.Print("No existing .gitattributes file found")
	return []string{}, nil
}

func ensureRequiredGitAttributesEntry(lines []string) ([]string, bool) {
	if lines == nil {
		lines = []string{}
	}
	lockYmlEntry := constants.WorkflowsLockYmlGitAttributesEntry
	requiredEntries := []string{lockYmlEntry}
	modified := false
	for _, required := range requiredEntries {
		found := false
		for i, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == required {
				found = true
				break
			}
			if trimmedLine == constants.WorkflowsLockYmlGitAttributesEntryLegacy && required == lockYmlEntry {
				gitLog.Print("Updating legacy .gitattributes entry format")
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
	return lines, modified
}

// stageGitAttributesIfChanged stages .gitattributes if it was modified
func stageGitAttributesIfChanged() error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}
	gitAttributesPath := filepath.Join(gitRoot, ".gitattributes")
	return exec.Command("git", "-C", gitRoot, "add", gitAttributesPath).Run()
}

// ensureLogsGitignore ensures that .github/aw/logs/.gitignore exists to ignore log files
func ensureLogsGitignore() error {
	gitLog.Print("Ensuring .github/aw/logs/.gitignore exists")
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	gitignorePath := filepath.Join(gitRoot, ".github", "aw", "logs", ".gitignore")

	// Check if .gitignore already exists
	if fileutil.FileExists(gitignorePath) {
		gitLog.Print(".github/aw/logs/.gitignore already exists") //nolint:hardcodedfilepath
		return nil
	}

	gitLog.Print("Creating .github/aw/logs directory and .gitignore")
	// Create the logs directory if it doesn't exist
	if err := fileutil.EnsureParentDir(gitignorePath, constants.DirPermPublic); err != nil {
		gitLog.Printf("Failed to ensure .github/aw/logs/.gitignore parent directory: %v", err)
		return fmt.Errorf("failed to create parent directory for .github/aw/logs/.gitignore: %w", err)
	}

	// Write the .gitignore file with owner-only read/write permissions (0600) for security best practices
	gitignoreContent := `# Ignore all downloaded workflow logs
*

# But keep the .gitignore file itself
!.gitignore
`
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), constants.FilePermSensitive); err != nil {
		gitLog.Printf("Failed to write .gitignore: %v", err)
		return fmt.Errorf("failed to write .github/aw/logs/.gitignore: %w", err)
	}

	gitLog.Print("Successfully created .github/aw/logs/.gitignore")
	return nil
}

// getCurrentBranch gets the current git branch name in the current working directory.
func getCurrentBranch() (string, error) {
	return getCurrentBranchIn("")
}

// getCurrentBranchIn gets the current git branch name in the given directory.
// Pass an empty string to use the current working directory.
func getCurrentBranchIn(dir string) (string, error) {
	if dir == "" {
		gitLog.Print("Getting current git branch")
	} else {
		gitLog.Printf("Getting current git branch in %s", dir)
	}
	cmd := exec.Command("git", "branch", "--show-current")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to get current branch: %v", err)
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	branch := strings.TrimSpace(string(output))
	if branch == "" {
		gitLog.Print("Could not determine current branch")
		return "", errors.New("could not determine current branch")
	}

	gitLog.Printf("Current branch: %s", branch)
	return branch, nil
}

// createAndSwitchBranch creates a new branch and switches to it
func createAndSwitchBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, "Creating and switching to branch: "+branchName)

	cmd := exec.Command("git", "checkout", "-b", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create and switch to branch %s: %w", branchName, err)
	}

	return nil
}

// switchBranch switches to the specified branch
func switchBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, "Switching to branch: "+branchName)

	cmd := exec.Command("git", "checkout", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to branch %s: %w", branchName, err)
	}

	return nil
}

// commitChanges commits all staged changes with the given message
func commitChanges(message string, verbose bool) error {
	console.LogVerbose(verbose, "Committing changes with message: "+message)

	cmd := exec.Command("git", "commit", "-m", message)
	if output, err := cmd.CombinedOutput(); err != nil {
		gitLog.Printf("Failed to commit: %v, output: %s", err, string(output))
		outputStr := strings.TrimSpace(string(output))
		return fmt.Errorf("failed to commit changes: %w\n%s", err, outputStr)
	}

	return nil
}

// pushBranch pushes the specified branch to origin
func pushBranch(branchName string, verbose bool) error {
	console.LogVerbose(verbose, "Pushing branch: "+branchName)

	cmd := exec.Command("git", "push", "-u", "origin", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}

// hasPendingChanges reports whether the working directory has any uncommitted
// changes (staged or unstaged). Returns (false, nil) for a clean tree.
func hasPendingChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check git status: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

// checkCleanWorkingDirectory checks if there are uncommitted changes
func checkCleanWorkingDirectory(verbose bool) error {
	return checkCleanWorkingDirectoryIgnoring(verbose, nil)
}

// checkCleanWorkingDirectoryIgnoring checks for uncommitted changes except for
// the provided paths (which may be absolute or repository-relative).
func checkCleanWorkingDirectoryIgnoring(verbose bool, ignoredPaths []string) error {
	console.LogVerbose(verbose, "Checking for uncommitted changes...")

	args := []string{"status", "--porcelain", "--untracked-files=all"}
	if len(ignoredPaths) > 0 {
		gitRoot, err := gitutil.FindGitRoot()
		if err != nil {
			return fmt.Errorf("failed to find git root for path resolution: %w", err)
		}
		args = append(args, "--", ":(top)**")
		for _, ignoredPath := range ignoredPaths {
			cleaned := filepath.Clean(ignoredPath)
			// Convert absolute paths to paths relative to the git root so they
			// work correctly as :(top,...) pathspecs.
			if filepath.IsAbs(cleaned) {
				rel, relErr := filepath.Rel(gitRoot, cleaned)
				if relErr != nil {
					return fmt.Errorf("failed to resolve %s relative to git root: %w", ignoredPath, relErr)
				}
				cleaned = rel
			}
			path := filepath.ToSlash(strings.TrimPrefix(cleaned, "."+string(filepath.Separator)))
			args = append(args, ":(top,literal,exclude)"+path)
		}
	}

	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(string(output)) != "" {
		return errors.New("working directory has uncommitted changes, please commit or stash them first")
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

// checkWorkflowFileStatus checks if a workflow file has local modifications, staged changes, or unpushed commits.
func checkWorkflowFileStatus(workflowPath string) (*WorkflowFileStatus, error) {
	gitLog.Printf("Checking status for workflow file: %s", workflowPath)

	status := &WorkflowFileStatus{}
	if !isGitRepo() {
		gitLog.Print("Not in a git repository")
		return status, nil
	}

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		gitLog.Printf("Failed to find git root: %v", err)
		return status, nil
	}

	relPath := relativeWorkflowPath(gitRoot, workflowPath)
	if err := validateRelPathForGit(relPath); err != nil {
		gitLog.Printf("Rejecting unsafe relative path %q: %v", relPath, err)
		return status, fmt.Errorf("invalid workflow path %q: %w", workflowPath, err)
	}

	gitLog.Printf("Checking git status for: %s", relPath)
	if err := populateLocalWorkflowStatus(gitRoot, relPath, status); err != nil {
		return status, nil
	}

	return populateUnpushedWorkflowStatus(gitRoot, relPath, status)
}

func relativeWorkflowPath(gitRoot, workflowPath string) string {
	if filepath.IsAbs(workflowPath) {
		relPath, err := filepath.Rel(gitRoot, workflowPath)
		if err != nil {
			gitLog.Printf("Failed to make path relative: %v", err)
			return workflowPath
		}
		return relPath
	}
	return workflowPath
}

func populateLocalWorkflowStatus(gitRoot, relPath string, status *WorkflowFileStatus) error {
	cmd := exec.Command("git", "-C", gitRoot, "status", "--porcelain", relPath)
	output, err := cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to check git status: %v", err)
		return err
	}

	statusOutput := string(output)
	if statusOutput == "" {
		return nil
	}

	gitLog.Printf("Git status output: %q", statusOutput)
	if len(statusOutput) >= 2 {
		stagedStatus := statusOutput[0]
		unstagedStatus := statusOutput[1]
		if stagedStatus != ' ' && stagedStatus != '?' {
			status.IsStaged = true
			gitLog.Print("File has staged changes")
		}
		if unstagedStatus == 'M' || unstagedStatus == 'D' || unstagedStatus == 'A' {
			status.IsModified = true
			gitLog.Print("File has unstaged modifications")
		}
	}
	return nil
}

func populateUnpushedWorkflowStatus(gitRoot, relPath string, status *WorkflowFileStatus) (*WorkflowFileStatus, error) {
	cmd := exec.Command("git", "-C", gitRoot, "rev-parse", "--abbrev-ref", "@{u}")
	output, err := cmd.Output()
	if err != nil {
		gitLog.Print("No upstream branch configured")
		return status, nil
	}

	upstream := strings.TrimSpace(string(output))
	gitLog.Printf("Upstream branch: %s", upstream)
	if !isSafeGitRevisionArg(upstream) {
		gitLog.Printf("Rejecting unsafe upstream ref: %q", upstream)
		return status, fmt.Errorf("unexpected upstream ref %q", upstream)
	}

	// #nosec G204 -- upstream is validated above by isSafeGitRevisionArg and relPath was
	// validated by validateRelPathForGit; "--" separates revision args from the path.
	cmd = exec.Command("git", "-C", gitRoot, "log", "--oneline", "HEAD", "--not", upstream, "--", relPath)
	output, err = cmd.Output()
	if err != nil {
		gitLog.Printf("Failed to check unpushed commits: %v", err)
		return status, nil
	}

	if strings.TrimSpace(string(output)) != "" {
		status.HasUnpushedCommits = true
		gitLog.Print("File has unpushed commits")
	}
	return status, nil
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

func isGHCLIAvailable() bool {
	cmd := exec.Command("gh", "--version")
	available := cmd.Run() == nil
	gitLog.Printf("Checked gh CLI availability: available=%v", available)
	return available
}
