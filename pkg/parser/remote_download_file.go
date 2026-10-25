//go:build !js && !wasm

package parser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

// DownloadFileFromGitHub downloads a file from a GitHub repository using the GitHub API.
// This is the exported wrapper for downloadFileFromGitHub.
// Parameters:
// - owner: Repository owner (e.g., "github")
// - repo: Repository name (e.g., "gh-aw")
// - path: Path to the file within the repository (e.g., ".github/workflows/workflow.md")
// - ref: Git reference (branch, tag, or commit SHA)
// Returns the file content as bytes or an error if the file cannot be retrieved.
func DownloadFileFromGitHub(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	return downloadFileFromGitHubWithDepth(ctx, owner, repo, path, ref, 0, "")
}

// DownloadFileFromGitHubForHost downloads a file from a GitHub repository using the GitHub API,
// targeting a specific GitHub host. Use this when the target repository is on a different host
// than the one configured via GH_HOST (e.g., fetching from github.com while GH_HOST is a GHE instance).
// host is the hostname without scheme (e.g., "github.com", "myorg.ghe.com").
// An empty host uses the default configured host (GH_HOST or github.com).
func DownloadFileFromGitHubForHost(ctx context.Context, owner, repo, path, ref, host string) ([]byte, error) {
	return downloadFileFromGitHubWithDepth(ctx, owner, repo, path, ref, 0, host)
}

func downloadFileFromGitHub(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	return downloadFileFromGitHubWithDepth(ctx, owner, repo, path, ref, 0, "")
}

// fetchRemoteFileContentFunc allows tests to inject API fetch behavior.
var fetchRemoteFileContentFunc = fetchRemoteFileContent

// downloadFileViaGitFunc allows tests to inject git fallback behavior.
var downloadFileViaGitFunc = downloadFileViaGit

func downloadFileFromGitHubWithDepth(ctx context.Context, owner, repo, path, ref string, symlinkDepth int, host string) ([]byte, error) {
	client, err := createRESTClientForHostFunc(host)
	if err != nil {
		if errorutil.IsAuthError(err.Error()) || errorutil.IsForbiddenError(err) {
			remoteLog.Printf("REST client creation failed due to auth error, attempting git fallback for %s/%s/%s@%s: %v", owner, repo, path, ref, err)
			content, gitErr := downloadFileViaGitFunc(ctx, owner, repo, path, ref, host)
			if gitErr != nil {
				remoteLog.Printf("Git fallback also failed for %s/%s/%s@%s: %v", owner, repo, path, ref, gitErr)
				return nil, fmt.Errorf("failed to fetch file content: %w", err)
			}
			return content, nil
		}
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	var fileContent struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Name     string `json:"name"`
	}

	err = fetchRemoteFileContentFunc(ctx, client, owner, repo, path, ref, &fileContent)
	if err != nil {
		if errorutil.IsAuthError(err.Error()) || errorutil.IsForbiddenError(err) {
			remoteLog.Printf("GitHub API authentication failed, attempting git fallback for %s/%s/%s@%s", owner, repo, path, ref)
			content, gitErr := downloadFileViaGitFunc(ctx, owner, repo, path, ref, host)
			if gitErr != nil {
				if canUseUnauthenticatedPublicGitHubFallback(owner, repo, host) {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s/%s@%s", owner, repo, path, ref)
					return downloadFileViaPublicAPI(ctx, owner, repo, path, ref)
				}
				return nil, fmt.Errorf("failed to fetch file content via GitHub API (auth error) and git fallback: API error: %w, Git error: %w", err, gitErr)
			}
			return content, nil
		}

		if errorutil.IsNotFoundError(err) && symlinkDepth < constants.MaxSymlinkDepth {
			if content, handled, resolveErr := retryDownloadViaResolvedSymlink(ctx, client, owner, repo, path, ref, symlinkDepth, host); handled {
				return content, resolveErr
			}
		}

		return nil, fmt.Errorf("failed to fetch file content from %s/%s/%s@%s: %w", owner, repo, path, ref, err)
	}

	if fileContent.Content == "" {
		return nil, fmt.Errorf("empty content returned from GitHub API for %s/%s/%s@%s", owner, repo, path, ref)
	}

	content, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 content: %w", err)
	}

	return content, nil
}

// downloadFileViaPublicAPI downloads a file from a public GitHub repository
// using an unauthenticated API call. Used as a last-resort fallback when both
// authenticated API and git clone fail (e.g. enterprise SAML tokens).
func downloadFileViaPublicAPI(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	remoteLog.Printf("Attempting unauthenticated public API download for %s/%s/%s@%s", owner, repo, path, ref)
	body, err := fetchPublicGitHubContentsAPI(ctx, owner, repo, path, ref)
	if err != nil {
		return nil, fmt.Errorf("unauthenticated public API also failed for %s/%s/%s@%s: %w", owner, repo, path, ref, err)
	}

	var fileContent struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &fileContent); err != nil {
		return nil, fmt.Errorf("failed to parse public API file response: %w", err)
	}
	if fileContent.Content == "" {
		return nil, fmt.Errorf("empty content returned from public API for %s/%s/%s@%s", owner, repo, path, ref)
	}

	content, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 content from public API: %w", err)
	}
	return content, nil
}

func retryDownloadViaResolvedSymlink(
	ctx context.Context,
	client *api.RESTClient,
	owner, repo, path, ref string,
	symlinkDepth int,
	host string,
) ([]byte, bool, error) {
	remoteLog.Printf("File not found at %s/%s/%s@%s, checking for symlinks in path (depth: %d)", owner, repo, path, ref, symlinkDepth)
	resolvedPath, resolveErr := resolveRemoteSymlinks(ctx, client, owner, repo, path, ref)
	if resolveErr == nil && resolvedPath != path {
		remoteLog.Printf("Retrying download with symlink-resolved path: %s -> %s", path, resolvedPath)
		content, err := downloadFileFromGitHubWithDepth(ctx, owner, repo, resolvedPath, ref, symlinkDepth+1, host)
		return content, true, err
	}
	return nil, false, nil
}

// checkRemoteSymlink checks if a path in a remote GitHub repository is a symlink.
// Returns the symlink target and true if it is a symlink, or empty string and false otherwise.
// A nil error with false means the path is not a symlink (e.g., it's a directory or file).
func checkRemoteSymlink(ctx context.Context, client *api.RESTClient, owner, repo, dirPath, ref string) (string, bool, error) {
	endpoint := buildContentsAPIPath(owner, repo, dirPath, ref)
	remoteLog.Printf("Checking if path component is symlink: %s/%s/%s@%s", owner, repo, dirPath, ref)

	// The Contents API returns a JSON object for files/symlinks but a JSON array for directories.
	// Decode into json.RawMessage first to distinguish these cases without error-driven control flow.
	var raw json.RawMessage
	err := client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &raw)
	if err != nil {
		remoteLog.Printf("Contents API error for %s: %v", dirPath, err)
		return "", false, err
	}

	// If the response is an array, this is a directory listing — not a symlink
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "" && trimmed[0] == '[' {
		remoteLog.Printf("Path component %s is a directory (not a symlink)", dirPath)
		return "", false, nil
	}

	// Parse the object response to check the type
	var result struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", false, fmt.Errorf("failed to parse contents response for %s: %w", dirPath, err)
	}

	if result.Type == "symlink" && result.Target != "" {
		remoteLog.Printf("Path component %s is a symlink -> %s", dirPath, result.Target)
		return result.Target, true, nil
	}

	remoteLog.Printf("Path component %s is type=%s (not a symlink)", dirPath, result.Type)
	return "", false, nil
}

// resolveRemoteSymlinks resolves symlinks in a remote GitHub repository path.
// The GitHub Contents API doesn't follow symlinks in path components. For example,
// if .github/workflows/shared is a symlink to ../../gh-agent-workflows/shared,
// fetching .github/workflows/shared/elastic-tools.md returns 404.
// This function walks the path components and resolves any symlinks found.
// The caller must provide a REST client (already authenticated for the correct host).
func resolveRemoteSymlinks(ctx context.Context, client *api.RESTClient, owner, repo, filePath, ref string) (string, error) {
	parts := strings.Split(filePath, "/")
	if len(parts) <= 1 {
		return "", fmt.Errorf("no directory components to resolve in path: %s", filePath)
	}

	if client == nil {
		return "", fmt.Errorf("no REST client available for symlink resolution of %s/%s/%s@%s", owner, repo, filePath, ref)
	}

	remoteLog.Printf("Attempting symlink resolution for %s/%s/%s@%s (%d path components)", owner, repo, filePath, ref, len(parts))

	for i := 1; i < len(parts); i++ {
		dirPath := strings.Join(parts[:i], "/")
		resolvedPath, found, err := resolveRemoteSymlinkComponent(ctx, client, remoteSymlinkComponentParams{
			owner:    owner,
			repo:     repo,
			filePath: filePath,
			ref:      ref,
			parts:    parts,
			index:    i,
			dirPath:  dirPath,
		})
		if err != nil {
			return "", err
		}
		if found {
			return resolvedPath, nil
		}
	}

	remoteLog.Printf("No symlinks found after checking all %d directory components of %s", len(parts)-1, filePath)
	return "", fmt.Errorf("no symlinks found in path: %s", filePath)
}

type remoteSymlinkComponentParams struct {
	owner    string
	repo     string
	filePath string
	ref      string
	parts    []string
	index    int
	dirPath  string
}

func resolveRemoteSymlinkComponent(
	ctx context.Context,
	client *api.RESTClient,
	params remoteSymlinkComponentParams,
) (string, bool, error) {
	target, isSymlink, err := checkRemoteSymlink(ctx, client, params.owner, params.repo, params.dirPath, params.ref)
	if err != nil {
		if errorutil.IsNotFoundError(err) {
			remoteLog.Printf("Path component %s returned 404, skipping", params.dirPath)
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to check path component %s for symlinks: %w", params.dirPath, err)
	}
	if !isSymlink {
		return "", false, nil
	}
	parentDir := ""
	if params.index > 1 {
		parentDir = strings.Join(params.parts[:params.index-1], "/")
	}
	resolvedBase, err := resolveAndValidateRemoteSymlinkBase(parentDir, target, params.dirPath)
	if err != nil {
		return "", false, err
	}
	remaining := strings.Join(params.parts[params.index:], "/")
	resolvedPath := pathpkg.Clean(pathpkg.Join(resolvedBase, remaining))
	if resolvedPath == "" || resolvedPath == "." || pathpkg.IsAbs(resolvedPath) || strings.HasPrefix(resolvedPath, "..") {
		return "", false, fmt.Errorf("resolved symlink path escapes repository root: %s", resolvedPath)
	}
	remoteLog.Printf("Resolved symlink in remote path: %s -> %s (full: %s -> %s)", params.dirPath, target, params.filePath, resolvedPath)
	return resolvedPath, true, nil
}

func resolveAndValidateRemoteSymlinkBase(parentDir, target, dirPath string) (string, error) {
	remoteLog.Printf("Resolving symlink: component=%s target=%s parentDir=%s", dirPath, target, parentDir)
	resolvedBase := pathpkg.Clean(target)
	if parentDir != "" {
		resolvedBase = pathpkg.Clean(pathpkg.Join(parentDir, target))
	}
	remoteLog.Printf("Resolved base after path.Clean: %s", resolvedBase)
	if resolvedBase == "" || resolvedBase == "." || pathpkg.IsAbs(resolvedBase) || strings.HasPrefix(resolvedBase, "..") {
		remoteLog.Printf("Rejecting resolved base %q (escapes repository root)", resolvedBase)
		return "", fmt.Errorf("symlink target %q at %s resolves outside repository root: %s", target, dirPath, resolvedBase)
	}
	return resolvedBase, nil
}

// downloadFileViaGit downloads a file from a Git repository using git commands
// This is a fallback for when GitHub API authentication fails
func downloadFileViaGit(ctx context.Context, owner, repo, path, ref, host string) (result []byte, resultErr error) {
	finish := ctxutil.StartOperation(ctx, fmt.Sprintf("git download %s/%s/%s@%s", owner, repo, path, ref))
	defer func() { finish(resultErr) }()
	remoteLog.Printf("Attempting git fallback for %s/%s/%s@%s", owner, repo, path, ref)

	if err := gitutil.ValidateGitRef(ref); err != nil {
		return nil, fmt.Errorf("refusing git fallback: %w", err)
	}
	if err := gitutil.ValidateGitPath(path); err != nil {
		return nil, fmt.Errorf("refusing git fallback: %w", err)
	}

	// First, try via raw.githubusercontent.com — no auth required for public repos and
	// no dependency on git being installed.
	// Only attempt raw URL for github.com repos (not GHE) since raw.githubusercontent.com
	// only serves public GitHub content.
	if canUseUnauthenticatedPublicGitHubFallback(owner, repo, host) {
		content, rawErr := downloadFileViaRawURL(ctx, owner, repo, path, ref)
		if rawErr == nil {
			return content, nil
		}
		remoteLog.Printf("Raw URL download failed for %s/%s/%s@%s, trying git archive: %v", owner, repo, path, ref, rawErr)
	}

	// Use git archive to get the file content without cloning
	// This works for public repositories without authentication
	var githubHost string
	if host != "" {
		githubHost = "https://" + host
	} else {
		githubHost = GetGitHubHostForRepo(owner, repo)
	}
	repoURL := fmt.Sprintf("%s/%s/%s.git", githubHost, owner, repo)

	// git archive command: git archive --remote=<repo> <ref> -- <path>
	// The '--' end-of-options separator ensures ref and path are never parsed as
	// git flags even if they begin with '-' (argument injection, CWE-88).
	// ValidateGitRef/ValidateGitPath above guard against leading '-' and '..' at
	// this layer; '--' is kept as defence-in-depth per the git(1) specification.
	// #nosec G204 -- repoURL, ref, and path are from workflow import configuration authored by the
	// developer; exec.CommandContext with separate args (not shell execution) prevents shell injection.
	cmd := exec.CommandContext(ctx, "git", "archive", "--remote="+repoURL, ref, "--", path)
	archiveOutput, err := cmd.Output()
	if err != nil {
		// If git archive fails, try with git clone + git show as a fallback
		return downloadFileViaGitClone(ctx, owner, repo, path, ref, host)
	}

	// Extract the file from the tar archive using Go's archive/tar (cross-platform)
	content, err := fileutil.ExtractFileFromTar(archiveOutput, path)
	if err != nil {
		return nil, fmt.Errorf("failed to extract file from git archive: %w", err)
	}

	remoteLog.Printf("Successfully downloaded file via git archive: %s/%s/%s@%s", owner, repo, path, ref)
	return content, nil
}

// downloadFileViaRawURL fetches a file using the raw.githubusercontent.com URL.
// This requires no authentication for public repositories and no git installation.
func downloadFileViaRawURL(ctx context.Context, owner, repo, filePath, ref string) ([]byte, error) {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, filePath)
	remoteLog.Printf("Attempting raw URL download: %s", rawURL)

	// Use a client with a timeout to prevent indefinite hangs on slow/unresponsive hosts.
	// #nosec G107 -- rawURL is constructed from workflow import configuration authored by
	// the developer; the owner, repo, filePath, and ref are user-supplied workflow spec fields.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("raw URL request failed for %s: %w", rawURL, err)
	}
	resp, err := publicAPIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("raw URL request failed for %s: %w", rawURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			remoteLog.Printf("Failed to close raw URL response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("raw URL returned HTTP %d for %s", resp.StatusCode, rawURL)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read raw URL response body for %s: %w", rawURL, err)
	}

	remoteLog.Printf("Successfully downloaded file via raw URL: %s", rawURL)
	return content, nil
}

// downloadFileViaGitClone downloads a file by shallow cloning the repository
// This is used as a fallback when git archive doesn't work
func downloadFileViaGitClone(ctx context.Context, owner, repo, path, ref, host string) ([]byte, error) {
	remoteLog.Printf("Attempting git clone fallback for %s/%s/%s@%s", owner, repo, path, ref)

	if err := validateGitCloneFallbackInputs(ref, path); err != nil {
		return nil, err
	}
	tmpDir, err := createGitCloneTempDir()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	repoURL := getRepoGitURL(owner, repo, host)
	if gitutil.IsValidFullSHACaseInsensitive(ref) {
		if err := cloneAndCheckoutSHA(ctx, repoURL, tmpDir, ref); err != nil {
			return nil, err
		}
	} else if err := cloneBranchOrTag(ctx, repoURL, tmpDir, ref); err != nil {
		return nil, err
	}

	content, err := readFileFromClone(tmpDir, path)
	if err != nil {
		return nil, err
	}

	remoteLog.Printf("Successfully downloaded file via git clone: %s/%s/%s@%s", owner, repo, path, ref)
	return content, nil
}

func validateGitCloneFallbackInputs(ref, path string) error {
	if err := gitutil.ValidateGitRef(ref); err != nil {
		return fmt.Errorf("refusing git clone fallback: %w", err)
	}
	if err := gitutil.ValidateGitPath(path); err != nil {
		return fmt.Errorf("refusing git clone fallback: %w", err)
	}
	return nil
}

func createGitCloneTempDir() (string, error) {
	tmpDir, err := os.MkdirTemp("", "gh-aw-git-clone-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	return tmpDir, nil
}

func getRepoGitURL(owner, repo, host string) string {
	githubHost := GetGitHubHostForRepo(owner, repo)
	if host != "" {
		githubHost = "https://" + host
	}
	return fmt.Sprintf("%s/%s/%s.git", githubHost, owner, repo)
}

func cloneAndCheckoutSHA(ctx context.Context, repoURL, tmpDir, ref string) error {
	if output, err := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--no-single-branch", repoURL, tmpDir).CombinedOutput(); err != nil {
		remoteLog.Printf("Clone with --no-single-branch failed, trying full clone: %s", string(output))
		if output, err := exec.CommandContext(ctx, "git", "clone", repoURL, tmpDir).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, string(output))
		}
	}
	// ValidateGitRef above guarantees ref does not start with '-', so it remains a revision argument.
	if output, err := exec.CommandContext(ctx, "git", "-C", tmpDir, "checkout", "--detach", ref).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout commit %s: %w\nOutput: %s", ref, err, string(output))
	}
	return nil
}

func cloneBranchOrTag(ctx context.Context, repoURL, tmpDir, ref string) error {
	// For branch/tag refs, use --branch flag; the value is passed via a separate
	// argument slot and cannot be confused with a flag because it follows --branch.
	if output, err := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", ref, repoURL, tmpDir).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func readFileFromClone(tmpDir, path string) ([]byte, error) {
	filePath := filepath.Join(tmpDir, path)
	if err := fileutil.ValidatePathWithinBase(tmpDir, filePath); err != nil {
		return nil, fmt.Errorf("refusing to read file outside clone directory: %w", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file from cloned repository: %w", err)
	}
	return content, nil
}
