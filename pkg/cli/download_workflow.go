package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var downloadLog = logger.New("cli:download_workflow")

// downloadWorkflowContentViaGit downloads a workflow file using git archive
func downloadWorkflowContentViaGit(ctx context.Context, repo, path, ref string, verbose bool) ([]byte, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching %s/%s@%s via git", repo, path, ref)))
	}

	downloadLog.Printf("Attempting git fallback for downloading workflow content: %s/%s@%s", repo, path, ref)

	if err := gitutil.ValidateGitRef(ref); err != nil {
		return nil, fmt.Errorf("refusing git fallback: %w", err)
	}
	if err := gitutil.ValidateGitPath(path); err != nil {
		return nil, fmt.Errorf("refusing git fallback: %w", err)
	}

	// Use git archive to get the file content without cloning
	githubHost := getGitHubHostForRepo(repo)
	repoURL := fmt.Sprintf("%s/%s.git", githubHost, repo)

	// git archive command: git archive --remote=<repo> <ref> -- <path>
	// The '--' end-of-options separator ensures path is never parsed as a git flag
	// even if it begins with '-' (argument injection, CWE-88).
	// ValidateGitRef/ValidateGitPath above guard against leading '-' and '..' at
	// this layer; '--' is kept as defence-in-depth per the git(1) specification.
	// #nosec G204 -- repoURL, ref, and path are from workflow import configuration authored by the
	// developer; exec.CommandContext with separate args (not shell execution) prevents shell injection.
	cmd := exec.CommandContext(ctx, "git", "archive", "--remote="+repoURL, ref, "--", path)
	archiveOutput, err := cmd.Output()
	if err != nil {
		downloadLog.Printf("git archive failed, falling back to git clone: repo=%s, ref=%s, err=%v", repo, ref, err)
		// If git archive fails, try with git clone + read file as a fallback
		return downloadWorkflowContentViaGitClone(ctx, repo, path, ref, verbose)
	}

	// Extract the file from the tar archive using Go's archive/tar (cross-platform)
	content, err := fileutil.ExtractFileFromTar(archiveOutput, path)
	if err != nil {
		downloadLog.Printf("Failed to extract %s from git archive: %v", path, err)
		return nil, fmt.Errorf("failed to extract file from git archive: %w", err)
	}
	downloadLog.Printf("Extracted file from git archive: path=%s, size=%d bytes", path, len(content))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Successfully fetched via git archive"))
	}

	return content, nil
}

// downloadWorkflowContentViaGitClone downloads a workflow file by shallow cloning with sparse checkout
func downloadWorkflowContentViaGitClone(ctx context.Context, repo, path, ref string, verbose bool) ([]byte, error) {
	// Validate and clean the path early to prevent path traversal before it is
	// used as a sparse-checkout pattern or joined with a filesystem base directory.
	cleanedPath := filepath.Clean(path)
	if filepath.IsAbs(cleanedPath) || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) || cleanedPath == ".." {
		return nil, fmt.Errorf("unsafe path in workflow reference: %q", path)
	}
	// Use forward slashes so the sparse-checkout pattern is valid for Git on all
	// platforms (Git patterns use "/" as separator; backslashes are escapes).
	// filepath.Join handles forward slashes correctly when building the local path.
	path = filepath.ToSlash(cleanedPath)

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

	githubHost := getGitHubHostForRepo(repo)
	repoURL := fmt.Sprintf("%s/%s.git", githubHost, repo)

	// Initialize git repository
	initCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "init")
	if output, err := initCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to initialize git repository: %w\nOutput: %s", err, string(output))
	}

	// Add remote
	remoteCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "remote", "add", "origin", repoURL)
	if output, err := remoteCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to add remote: %w\nOutput: %s", err, string(output))
	}

	// Enable sparse-checkout
	sparseCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "core.sparseCheckout", "true")
	if output, err := sparseCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to enable sparse checkout: %w\nOutput: %s", err, string(output))
	}

	// Set sparse-checkout pattern to only include the file we need
	sparseInfoDir := filepath.Join(tmpDir, ".git", "info")
	if err := os.MkdirAll(sparseInfoDir, constants.DirPermPublic); err != nil {
		return nil, fmt.Errorf("failed to create sparse-checkout directory: %w", err)
	}

	sparseCheckoutFile := filepath.Join(sparseInfoDir, "sparse-checkout")
	// Use owner-only read/write permissions (0600) for security best practices
	if err := os.WriteFile(sparseCheckoutFile, []byte(path+"\n"), constants.FilePermSensitive); err != nil {
		return nil, fmt.Errorf("failed to write sparse-checkout file: %w", err)
	}

	// Check if ref is a SHA (40 hex characters)
	isSHA := gitutil.IsValidFullSHACaseInsensitive(ref)
	downloadLog.Printf("Fetching ref via sparse checkout: is_sha=%t", isSHA)

	if isSHA {
		// For SHA refs, fetch without specifying a ref (fetch all) then checkout the specific commit
		// Note: sparse-checkout with SHA refs may not reduce bandwidth as much as with branch refs,
		// because the server needs to send enough history to reach the specific commit.
		// However, it still limits the working directory to only the requested file.
		fetchCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "fetch", "--depth", "1", "origin", ref)
		if _, err := fetchCmd.CombinedOutput(); err != nil {
			// If fetching specific SHA fails, try fetching all branches with depth 1
			fetchCmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "fetch", "--depth", "1", "origin")
			if output, err := fetchCmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("failed to fetch repository: %w\nOutput: %s", err, string(output))
			}
		}

		// Checkout the specific commit
		checkoutCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "checkout", ref)
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to checkout commit %s: %w\nOutput: %s", ref, err, string(output))
		}
	} else {
		// For branch/tag refs, fetch the specific ref
		fetchCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "fetch", "--depth", "1", "origin", ref)
		if output, err := fetchCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to fetch ref %s: %w\nOutput: %s", ref, err, string(output))
		}

		// Checkout FETCH_HEAD
		checkoutCmd := exec.CommandContext(ctx, "git", "-C", tmpDir, "checkout", "FETCH_HEAD")
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to checkout FETCH_HEAD: %w\nOutput: %s", err, string(output))
		}
	}

	// Read the file
	filePath := filepath.Join(tmpDir, path)
	if err := fileutil.ValidatePathWithinBase(tmpDir, filePath); err != nil {
		downloadLog.Printf("Refusing to read file outside clone directory: %s", filePath)
		return nil, fmt.Errorf("refusing to read file outside clone directory: %w", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		downloadLog.Printf("Failed to read cloned file: path=%s, err=%v", filePath, err)
		return nil, fmt.Errorf("failed to read file from cloned repository: %w", err)
	}
	downloadLog.Printf("Read cloned file: path=%s, size=%d bytes", path, len(content))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Successfully fetched via git sparse checkout"))
	}

	return content, nil
}

// downloadWorkflowContent downloads the content of a workflow file from GitHub
func downloadWorkflowContent(ctx context.Context, repo, path, ref string, verbose bool) ([]byte, error) {
	downloadLog.Printf("Downloading workflow content: %s/%s@%s", repo, path, ref)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching %s/%s@%s", repo, path, ref)))
	}

	// Use gh CLI to download the file
	output, err := workflow.RunGHCombinedContext(ctx, "Downloading workflow...", "api", fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, path, ref), "--jq", ".content")
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if errorutil.IsAuthError(outputStr) || errorutil.IsAuthError(err.Error()) {
			downloadLog.Printf("GitHub API authentication failed, attempting git fallback for %s/%s@%s", repo, path, ref)
			// Try fallback using git commands
			content, gitErr := downloadWorkflowContentViaGit(ctx, repo, path, ref, verbose)
			if gitErr != nil {
				return nil, fmt.Errorf("failed to fetch file content via GitHub API and git: API error: %w, Git error: %w", err, gitErr)
			}
			return content, nil
		}
		return nil, fmt.Errorf("failed to fetch file content: %w", err)
	}

	// The content is base64 encoded, decode it
	return decodeBase64FileContent(string(output))
}

// decodeBase64FileContent decodes base64-encoded file content returned by the GitHub API.
// The GitHub API wraps lines at 60 characters and may include surrounding whitespace,
// so both are stripped before decoding.
func decodeBase64FileContent(raw string) ([]byte, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), "\n", "")
	content, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		downloadLog.Printf("Base64 decode failed: cleaned_len=%d, err=%v", len(cleaned), err)
		return nil, fmt.Errorf("failed to decode file content: %w", err)
	}
	downloadLog.Printf("Decoded base64 file content: bytes=%d", len(content))
	return content, nil
}
