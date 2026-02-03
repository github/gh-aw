package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var downloadLog = logger.New("cli:download_workflow")

// resolveLatestReleaseViaGit finds the latest release using git ls-remote
//
//nolint:unused // Fallback implementation for when GitHub API is unavailable
func resolveLatestReleaseViaGit(repo, currentRef string, allowMajor, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching latest release for %s via git ls-remote (current: %s, allow major: %v)", repo, currentRef, allowMajor)))
	}

	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// List all tags
	cmd := exec.Command("git", "ls-remote", "--tags", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch releases via git ls-remote: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var releases []string

	for _, line := range lines {
		// Parse: "<sha> refs/tags/<tag>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			tagRef := parts[1]
			// Skip ^{} annotations (they point to the commit object)
			if strings.HasSuffix(tagRef, "^{}") {
				continue
			}
			tag := strings.TrimPrefix(tagRef, "refs/tags/")
			releases = append(releases, tag)
		}
	}

	if len(releases) == 0 {
		return "", fmt.Errorf("no releases found")
	}

	// Parse current version
	currentVersion := parseVersion(currentRef)
	if currentVersion == nil {
		// If current ref is not a valid version, just return the first release
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Current ref is not a valid version, using first release: %s (via git)", releases[0])))
		}
		return releases[0], nil
	}

	// Find the latest compatible release
	var latestCompatible string
	var latestCompatibleVersion *semanticVersion

	for _, release := range releases {
		releaseVersion := parseVersion(release)
		if releaseVersion == nil {
			continue
		}

		// Check if compatible based on major version
		if !allowMajor && releaseVersion.major != currentVersion.major {
			continue
		}

		// Check if this is newer than what we have
		if latestCompatibleVersion == nil || releaseVersion.isNewer(latestCompatibleVersion) {
			latestCompatible = release
			latestCompatibleVersion = releaseVersion
		}
	}

	if latestCompatible == "" {
		return "", fmt.Errorf("no compatible release found")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest compatible release: %s (via git)", latestCompatible)))
	}

	return latestCompatible, nil
}

// isBranchRefViaGit checks if a ref is a branch using git ls-remote
//
//nolint:unused // Fallback implementation for when GitHub API is unavailable
func isBranchRefViaGit(repo, ref string) (bool, error) {
	downloadLog.Printf("Attempting git ls-remote to check if ref is branch: %s@%s", repo, ref)

	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// List all branches and check if ref matches
	cmd := exec.Command("git", "ls-remote", "--heads", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to list branches via git ls-remote: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		// Format: <sha> refs/heads/<branch>
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			branchRef := parts[1]
			branchName := strings.TrimPrefix(branchRef, "refs/heads/")
			if branchName == ref {
				downloadLog.Printf("Found branch via git ls-remote: %s", ref)
				return true, nil
			}
		}
	}

	return false, nil
}

// isBranchRef checks if a ref is a branch in the repository
//
//nolint:unused // Reserved for future use
func isBranchRef(repo, ref string) (bool, error) {
	// Use gh CLI to list branches
	output, err := workflow.RunGHCombined("Fetching branches...", "api", fmt.Sprintf("/repos/%s/branches", repo), "--jq", ".[].name")
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if gitutil.IsAuthError(outputStr) || gitutil.IsAuthError(err.Error()) {
			downloadLog.Printf("GitHub API authentication failed, attempting git ls-remote fallback")
			// Try fallback using git ls-remote
			isBranch, gitErr := isBranchRefViaGit(repo, ref)
			if gitErr != nil {
				return false, fmt.Errorf("failed to check branch via GitHub API and git: API error: %w, Git error: %v", err, gitErr)
			}
			return isBranch, nil
		}
		return false, err
	}

	branches := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, branch := range branches {
		if branch == ref {
			return true, nil
		}
	}

	return false, nil
}

// resolveBranchHeadViaGit gets the latest commit SHA for a branch using git ls-remote
//
//nolint:unused // Fallback implementation for when GitHub API is unavailable
func resolveBranchHeadViaGit(repo, branch string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching latest commit for branch %s in %s via git ls-remote", branch, repo)))
	}

	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// Get the SHA for the specific branch
	cmd := exec.Command("git", "ls-remote", repoURL, fmt.Sprintf("refs/heads/%s", branch))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch branch info via git ls-remote: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || len(lines[0]) == 0 {
		return "", fmt.Errorf("branch %s not found", branch)
	}

	// Parse the output: "<sha> refs/heads/<branch>"
	parts := strings.Fields(lines[0])
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid git ls-remote output")
	}

	sha := parts[0]
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest commit on %s: %s (via git)", branch, sha)))
	}

	return sha, nil
}

// resolveBranchHead gets the latest commit SHA for a branch
//
//nolint:unused // Reserved for future use
func resolveBranchHead(repo, branch string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching latest commit for branch %s in %s", branch, repo)))
	}

	// Use gh CLI to get branch info
	output, err := workflow.RunGHCombined("Fetching branch info...", "api", fmt.Sprintf("/repos/%s/branches/%s", repo, branch), "--jq", ".commit.sha")
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if gitutil.IsAuthError(outputStr) || gitutil.IsAuthError(err.Error()) {
			downloadLog.Printf("GitHub API authentication failed, attempting git ls-remote fallback")
			// Try fallback using git ls-remote
			sha, gitErr := resolveBranchHeadViaGit(repo, branch, verbose)
			if gitErr != nil {
				return "", fmt.Errorf("failed to fetch branch info via GitHub API and git: API error: %w, Git error: %v", err, gitErr)
			}
			return sha, nil
		}
		return "", fmt.Errorf("failed to fetch branch info: %w", err)
	}

	sha := strings.TrimSpace(string(output))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest commit on %s: %s", branch, sha)))
	}

	return sha, nil
}

// resolveDefaultBranchHeadViaGit gets the latest commit SHA for the default branch using git ls-remote
//
//nolint:unused // Fallback implementation for when GitHub API is unavailable
func resolveDefaultBranchHeadViaGit(repo string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching default branch for %s via git ls-remote", repo)))
	}

	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// Get HEAD to find default branch
	cmd := exec.Command("git", "ls-remote", "--symref", repoURL, "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch repository info via git ls-remote: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("unexpected git ls-remote output format")
	}

	// First line is: "ref: refs/heads/<branch> HEAD"
	// Second line is: "<sha> HEAD"
	var defaultBranch string
	var sha string

	for _, line := range lines {
		if strings.HasPrefix(line, "ref:") {
			// Parse: "ref: refs/heads/<branch> HEAD"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				refPath := parts[1]
				defaultBranch = strings.TrimPrefix(refPath, "refs/heads/")
			}
		} else {
			// Parse: "<sha> HEAD"
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				sha = parts[0]
			}
		}
	}

	if defaultBranch == "" || sha == "" {
		return "", fmt.Errorf("failed to parse default branch or SHA from git ls-remote output")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Default branch: %s (via git)", defaultBranch)))
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest commit on %s: %s (via git)", defaultBranch, sha)))
	}

	return sha, nil
}

// resolveDefaultBranchHead gets the latest commit SHA for the default branch
//
//nolint:unused // Reserved for future use
func resolveDefaultBranchHead(repo string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching default branch for %s", repo)))
	}

	// First get the default branch name
	output, err := workflow.RunGHCombined("Fetching repository info...", "api", fmt.Sprintf("/repos/%s", repo), "--jq", ".default_branch")
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if gitutil.IsAuthError(outputStr) || gitutil.IsAuthError(err.Error()) {
			downloadLog.Printf("GitHub API authentication failed, attempting git ls-remote fallback")
			// Try fallback using git ls-remote to get HEAD
			sha, gitErr := resolveDefaultBranchHeadViaGit(repo, verbose)
			if gitErr != nil {
				return "", fmt.Errorf("failed to fetch repository info via GitHub API and git: API error: %w, Git error: %v", err, gitErr)
			}
			return sha, nil
		}
		return "", fmt.Errorf("failed to fetch repository info: %w", err)
	}

	defaultBranch := strings.TrimSpace(string(output))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Default branch: %s", defaultBranch)))
	}

	return resolveBranchHead(repo, defaultBranch, verbose)
}

// downloadWorkflowContentViaGit downloads a workflow file using git archive
func downloadWorkflowContentViaGit(repo, path, ref string, verbose bool) ([]byte, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching %s/%s@%s via git", repo, path, ref)))
	}

	downloadLog.Printf("Attempting git fallback for downloading workflow content: %s/%s@%s", repo, path, ref)

	// Use git archive to get the file content without cloning
	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// git archive command: git archive --remote=<repo> <ref> <path>
	cmd := exec.Command("git", "archive", "--remote="+repoURL, ref, path)
	archiveOutput, err := cmd.Output()
	if err != nil {
		// If git archive fails, try with git clone + read file as a fallback
		return downloadWorkflowContentViaGitClone(repo, path, ref, verbose)
	}

	// Extract the file from the tar archive
	tarCmd := exec.Command("tar", "-xO", path)
	tarCmd.Stdin = strings.NewReader(string(archiveOutput))
	content, err := tarCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to extract file from git archive: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Successfully fetched via git archive"))
	}

	return content, nil
}

// downloadWorkflowContentViaGitClone downloads a workflow file by shallow cloning with sparse checkout
func downloadWorkflowContentViaGitClone(repo, path, ref string, verbose bool) ([]byte, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching %s/%s@%s via git clone", repo, path, ref)))
	}

	downloadLog.Printf("Attempting git clone fallback for downloading workflow content: %s/%s@%s", repo, path, ref)

	// Create a temporary directory for the sparse checkout
	tmpDir, err := os.MkdirTemp("", "gh-aw-git-clone-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)

	// Initialize git repository
	initCmd := exec.Command("git", "-C", tmpDir, "init")
	if output, err := initCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to initialize git repository: %w\nOutput: %s", err, string(output))
	}

	// Add remote
	remoteCmd := exec.Command("git", "-C", tmpDir, "remote", "add", "origin", repoURL)
	if output, err := remoteCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to add remote: %w\nOutput: %s", err, string(output))
	}

	// Enable sparse-checkout
	sparseCmd := exec.Command("git", "-C", tmpDir, "config", "core.sparseCheckout", "true")
	if output, err := sparseCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to enable sparse checkout: %w\nOutput: %s", err, string(output))
	}

	// Set sparse-checkout pattern to only include the file we need
	sparseInfoDir := filepath.Join(tmpDir, ".git", "info")
	if err := os.MkdirAll(sparseInfoDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sparse-checkout directory: %w", err)
	}

	sparseCheckoutFile := filepath.Join(sparseInfoDir, "sparse-checkout")
	// Use owner-only read/write permissions (0600) for security best practices
	if err := os.WriteFile(sparseCheckoutFile, []byte(path+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("failed to write sparse-checkout file: %w", err)
	}

	// Check if ref is a SHA (40 hex characters)
	isSHA := len(ref) == 40 && gitutil.IsHexString(ref)

	if isSHA {
		// For SHA refs, fetch without specifying a ref (fetch all) then checkout the specific commit
		// Note: sparse-checkout with SHA refs may not reduce bandwidth as much as with branch refs,
		// because the server needs to send enough history to reach the specific commit.
		// However, it still limits the working directory to only the requested file.
		fetchCmd := exec.Command("git", "-C", tmpDir, "fetch", "--depth", "1", "origin", ref)
		if _, err := fetchCmd.CombinedOutput(); err != nil {
			// If fetching specific SHA fails, try fetching all branches with depth 1
			fetchCmd = exec.Command("git", "-C", tmpDir, "fetch", "--depth", "1", "origin")
			if output, err := fetchCmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("failed to fetch repository: %w\nOutput: %s", err, string(output))
			}
		}

		// Checkout the specific commit
		checkoutCmd := exec.Command("git", "-C", tmpDir, "checkout", ref)
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to checkout commit %s: %w\nOutput: %s", ref, err, string(output))
		}
	} else {
		// For branch/tag refs, fetch the specific ref
		fetchCmd := exec.Command("git", "-C", tmpDir, "fetch", "--depth", "1", "origin", ref)
		if output, err := fetchCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to fetch ref %s: %w\nOutput: %s", ref, err, string(output))
		}

		// Checkout FETCH_HEAD
		checkoutCmd := exec.Command("git", "-C", tmpDir, "checkout", "FETCH_HEAD")
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to checkout FETCH_HEAD: %w\nOutput: %s", err, string(output))
		}
	}

	// Read the file
	filePath := filepath.Join(tmpDir, path)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file from cloned repository: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Successfully fetched via git sparse checkout"))
	}

	return content, nil
}

// downloadWorkflowContent downloads the content of a workflow file from GitHub
func downloadWorkflowContent(repo, path, ref string, verbose bool) ([]byte, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching %s/%s@%s", repo, path, ref)))
	}

	// Use gh CLI to download the file
	output, err := workflow.RunGHCombined("Downloading workflow...", "api", fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, path, ref), "--jq", ".content")
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if gitutil.IsAuthError(outputStr) || gitutil.IsAuthError(err.Error()) {
			downloadLog.Printf("GitHub API authentication failed, attempting git fallback for %s/%s@%s", repo, path, ref)
			// Try fallback using git commands
			content, gitErr := downloadWorkflowContentViaGit(repo, path, ref, verbose)
			if gitErr != nil {
				return nil, fmt.Errorf("failed to fetch file content via GitHub API and git: API error: %w, Git error: %v", err, gitErr)
			}
			return content, nil
		}
		return nil, fmt.Errorf("failed to fetch file content: %w", err)
	}

	// The content is base64 encoded, decode it
	contentBase64 := strings.TrimSpace(string(output))
	base64Cmd := exec.Command("base64", "-d")
	base64Cmd.Stdin = strings.NewReader(contentBase64)
	content, err := base64Cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to decode file content: %w", err)
	}

	return content, nil
}
