//go:build !js && !wasm

package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"golang.org/x/sync/singleflight"
)

// gitListCloneCache is a process-lifetime cache of shallow clones used by
// git-based directory listing fallbacks to avoid repeated clone operations for
// the same repository/ref tuple. Entries are not explicitly cleaned up because
// the CLI process is short-lived and temporary directories are OS-managed.
var gitListCloneCache = struct {
	mu   sync.Mutex
	dirs map[string]string
}{
	dirs: make(map[string]string),
}

var gitListCloneGroup singleflight.Group

// listRepoCloneConfig holds the fully-resolved identity of a shallow clone used
// for directory listing. owner and repo are included so callers can pass the
// entire struct without re-expanding individual fields.
type listRepoCloneConfig struct {
	owner    string
	repo     string
	ref      string
	repoURL  string
	cacheKey string
}

func resolveListRepoCloneConfig(owner, repo, ref, host string) (listRepoCloneConfig, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return listRepoCloneConfig{}, errors.New("git fallback requires a non-empty ref")
	}

	githubHost := GetGitHubHostForRepo(owner, repo)
	if host != "" {
		githubHost = stringutil.NormalizeGitHubHostURL(host)
	}
	repoURL := fmt.Sprintf("%s/%s/%s.git", githubHost, owner, repo)
	cacheKey := fmt.Sprintf("%s|%s|%s|%s", githubHost, owner, repo, ref)
	return listRepoCloneConfig{
		owner:    owner,
		repo:     repo,
		ref:      ref,
		repoURL:  repoURL,
		cacheKey: cacheKey,
	}, nil
}

// readCloneFromCache returns the cached clone directory for cacheKey under lock.
func readCloneFromCache(cacheKey string) (string, bool) {
	gitListCloneCache.mu.Lock()
	defer gitListCloneCache.mu.Unlock()
	cloneDir, ok := gitListCloneCache.dirs[cacheKey]
	return cloneDir, ok
}

// evictCloneFromCache removes the cache entry for cacheKey only if it still maps to
// cloneDir, preventing incorrect eviction of a fresh entry written by another goroutine.
func evictCloneFromCache(cacheKey, cloneDir string) {
	gitListCloneCache.mu.Lock()
	defer gitListCloneCache.mu.Unlock()
	if gitListCloneCache.dirs[cacheKey] == cloneDir {
		delete(gitListCloneCache.dirs, cacheKey)
	}
}

// getCachedListRepoClone reads the cached clone path without holding the mutex during
// filesystem I/O to avoid serializing concurrent callers. If the entry is stale it is
// evicted via evictCloneFromCache with a path-equality guard.
// Do not call from code that already holds gitListCloneCache.mu.
func getCachedListRepoClone(cacheKey string) (string, bool) {
	cloneDir, ok := readCloneFromCache(cacheKey)
	if !ok {
		return "", false
	}
	if stat, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil && stat.IsDir() {
		return cloneDir, true
	}
	// Entry appears stale — evict under lock, but only if it still maps to the same
	// path we checked above. A concurrent goroutine may have replaced the entry with a
	// fresh clone in the window between the stat and this lock acquisition.
	evictCloneFromCache(cacheKey, cloneDir)
	return "", false
}

// cloneAndCacheListRepoClone performs the clone and writes the result to the cache.
// It must only be called from within a singleflight.Group.Do callback to prevent
// concurrent duplicate clones for the same cache key. The final cache write is
// performed under gitListCloneCache.mu with a defensive check: if another goroutine
// has already populated the entry (e.g. due to incorrect direct invocation), the
// redundant tmpDir is discarded and the existing entry is returned.
// Do not call from code that already holds gitListCloneCache.mu.
func cloneAndCacheListRepoClone(ctx context.Context, cfg listRepoCloneConfig) (string, error) {
	tmpDir, err := os.MkdirTemp("", "gh-aw-list-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	cloneCmd := gitutil.SourceCommandContext(ctx, cfg.repoURL, "clone", "--depth", "1", "--branch", cfg.ref, "--single-branch", "--filter=blob:none", "--no-checkout", cfg.repoURL, tmpDir)
	cloneOutput, err := cloneCmd.CombinedOutput()
	if err != nil {
		if cleanupErr := os.RemoveAll(tmpDir); cleanupErr != nil {
			remoteLog.Printf("Failed to clean up temp directory %q: %v", tmpDir, cleanupErr)
		}
		remoteLog.Printf("Failed to clone repository: %s", string(cloneOutput))
		return "", fmt.Errorf("failed to clone repository for %s/%s@%s: %w", cfg.owner, cfg.repo, cfg.ref, err)
	}

	gitListCloneCache.mu.Lock()
	defer gitListCloneCache.mu.Unlock()
	if existing, ok := gitListCloneCache.dirs[cfg.cacheKey]; ok {
		// Another goroutine finished first; discard our redundant clone.
		if cleanupErr := os.RemoveAll(tmpDir); cleanupErr != nil {
			remoteLog.Printf("Failed to clean up redundant temp directory %q: %v", tmpDir, cleanupErr)
		}
		return existing, nil
	}
	gitListCloneCache.dirs[cfg.cacheKey] = tmpDir
	return tmpDir, nil
}

func getOrCreateListRepoClone(ctx context.Context, owner, repo, ref, host string) (string, error) {
	config, err := resolveListRepoCloneConfig(owner, repo, ref, host)
	if err != nil {
		return "", err
	}

	if cloneDir, found := getCachedListRepoClone(config.cacheKey); found {
		return cloneDir, nil
	}

	cloneDir, err, _ := gitListCloneGroup.Do(config.cacheKey, func() (any, error) {
		if cloneDir, found := getCachedListRepoClone(config.cacheKey); found {
			return cloneDir, nil
		}
		return cloneAndCacheListRepoClone(ctx, config)
	})
	if err != nil {
		return "", err
	}
	cloneDirPath, ok := cloneDir.(string)
	if !ok {
		return "", errors.New("internal error: clone result was not a string")
	}
	return cloneDirPath, nil
}

// ListWorkflowFiles lists workflow files from a remote GitHub repository
// Returns a list of .md files in the specified directory (excluding subdirectories)
func ListWorkflowFiles(ctx context.Context, owner, repo, ref, workflowPath string) ([]string, error) {
	return listWorkflowFilesForHost(ctx, owner, repo, ref, workflowPath, "")
}

// ListWorkflowFilesForHost lists workflow files from a remote GitHub repository on an explicit host.
// Use this when the target repository is on a different host than the one configured via GH_HOST.
func ListWorkflowFilesForHost(ctx context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
	return listWorkflowFilesForHost(ctx, owner, repo, ref, workflowPath, host)
}

func listWorkflowFilesForHost(ctx context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
	remoteLog.Printf("Listing workflow files for %s/%s@%s (path: %s)", owner, repo, ref, workflowPath)

	client, err := createRESTClientForHost(host)
	if err != nil {
		remoteLog.Printf("Failed to create REST client, attempting git fallback: %v", err)
		return listWorkflowFilesViaGitForHost(ctx, owner, repo, ref, workflowPath, host)
	}

	// Define response struct for GitHub contents API (array of file objects)
	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}

	// Fetch directory contents from GitHub API
	endpoint := buildContentsAPIPath(owner, repo, workflowPath, ref)
	err = client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &contents)
	if err != nil {
		errStr := err.Error()

		// Check if this is an authentication error
		if errorutil.IsAuthError(errStr) {
			remoteLog.Printf("GitHub API authentication failed, attempting git fallback for %s/%s@%s", owner, repo, ref)
			// Try fallback using git commands for public repositories
			files, gitErr := listWorkflowFilesViaGitForHost(ctx, owner, repo, ref, workflowPath, host)
			if gitErr != nil {
				if host == "" || host == "github.com" {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s@%s", owner, repo, ref)
					return listWorkflowFilesViaPublicAPI(ctx, owner, repo, ref, workflowPath)
				}
				return nil, fmt.Errorf("failed to list workflow files via GitHub API (auth error) and git fallback: API error: %w, Git error: %w", err, gitErr)
			}
			return files, nil
		}

		return nil, fmt.Errorf("failed to list workflow files from %s/%s@%s (path: %s): %w", owner, repo, ref, workflowPath, err)
	}

	// Filter to only .md files (not in subdirectories)
	var workflowFiles []string
	for _, item := range contents {
		if item.Type == "file" && strings.HasSuffix(strings.ToLower(item.Name), ".md") {
			workflowFiles = append(workflowFiles, item.Path)
		}
	}

	remoteLog.Printf("Found %d workflow files in %s/%s@%s (path: %s)", len(workflowFiles), owner, repo, ref, workflowPath)
	return workflowFiles, nil
}

func listWorkflowFilesViaGitForHost(ctx context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
	remoteLog.Printf("Attempting git fallback for listing workflow files: %s/%s@%s (path: %s)", owner, repo, ref, workflowPath)

	tmpDir, err := getOrCreateListRepoClone(ctx, owner, repo, ref, host)
	if err != nil {
		return nil, err
	}

	// Use git ls-tree to list files in the specified workflows directory
	lsTreeCmd := gitutil.SourceCommandContext(ctx, "", "-C", tmpDir, "ls-tree", "-r", "--name-only", "HEAD", workflowPath+"/")
	lsTreeOutput, err := lsTreeCmd.CombinedOutput()
	if err != nil {
		remoteLog.Printf("Failed to list files: %s", string(lsTreeOutput))
		return nil, fmt.Errorf("failed to list workflow files: %w", err)
	}

	// Parse output and filter for .md files (not in subdirectories)
	lines := strings.Split(strings.TrimSpace(string(lsTreeOutput)), "\n")
	var workflowFiles []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Only include .md files directly in the workflow path (not in subdirectories)
		if strings.HasSuffix(strings.ToLower(line), ".md") {
			// Check if it's a top-level file (no additional slashes after workflowPath/)
			afterWorkflowPath := strings.TrimPrefix(line, workflowPath+"/")
			if !strings.Contains(afterWorkflowPath, "/") {
				workflowFiles = append(workflowFiles, line)
			}
		}
	}

	remoteLog.Printf("Found %d workflow files via git for %s/%s@%s (path: %s)", len(workflowFiles), owner, repo, ref, workflowPath)
	return workflowFiles, nil
}

// listWorkflowFilesViaPublicAPI lists workflow .md files using an unauthenticated
// call to the public GitHub API. Used as a last-resort fallback when both
// authenticated API and git clone fail.
func listWorkflowFilesViaPublicAPI(ctx context.Context, owner, repo, ref, workflowPath string) ([]string, error) {
	remoteLog.Printf("Attempting unauthenticated public API for listing workflow files: %s/%s@%s (path: %s)", owner, repo, ref, workflowPath)
	body, err := fetchPublicGitHubContentsAPI(ctx, owner, repo, workflowPath, ref)
	if err != nil {
		return nil, fmt.Errorf("unauthenticated public API also failed for %s/%s@%s (path: %s): %w", owner, repo, ref, workflowPath, err)
	}

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &contents); err != nil {
		return nil, fmt.Errorf("failed to parse public API response: %w", err)
	}

	var workflowFiles []string
	for _, item := range contents {
		if item.Type == "file" && strings.HasSuffix(strings.ToLower(item.Name), ".md") {
			workflowFiles = append(workflowFiles, item.Path)
		}
	}
	remoteLog.Printf("Found %d workflow files via public API for %s/%s@%s (path: %s)", len(workflowFiles), owner, repo, ref, workflowPath)
	return workflowFiles, nil
}

// ListDirAllFilesForHost lists all files (any extension) that are direct children of
// the given directory in a remote GitHub repository. Subdirectories and their contents
// are not included. This is used for skill file discovery.
func ListDirAllFilesForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	return listDirAllFilesForHost(ctx, owner, repo, ref, dirPath, host)
}

func listDirAllFilesForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Listing all files in dir for %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	client, err := createRESTClientForHost(host)
	if err != nil {
		remoteLog.Printf("Failed to create REST client, attempting git fallback: %v", err)
		return listDirAllFilesViaGitForHost(ctx, owner, repo, ref, dirPath, host)
	}

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}

	endpoint := buildContentsAPIPath(owner, repo, dirPath, ref)
	err = client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &contents)
	if err != nil {
		errStr := err.Error()
		if errorutil.IsAuthError(errStr) {
			remoteLog.Printf("GitHub API auth failed, attempting git fallback for %s/%s@%s", owner, repo, ref)
			files, gitErr := listDirAllFilesViaGitForHost(ctx, owner, repo, ref, dirPath, host)
			if gitErr != nil {
				if host == "" || host == "github.com" {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s@%s", owner, repo, ref)
					return listDirAllFilesViaPublicAPI(ctx, owner, repo, ref, dirPath)
				}
				return nil, fmt.Errorf("failed to list dir files via API (auth error) and git fallback: API error: %w, Git error: %w", err, gitErr)
			}
			return files, nil
		}
		return nil, fmt.Errorf("failed to list dir files from %s/%s@%s (path: %s): %w", owner, repo, ref, dirPath, err)
	}

	var files []string
	for _, item := range contents {
		if item.Type == "file" {
			files = append(files, item.Path)
		}
	}

	remoteLog.Printf("Found %d files in dir %s/%s@%s (path: %s)", len(files), owner, repo, ref, dirPath)
	return files, nil
}

func listDirAllFilesViaGitForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Git fallback for listing all dir files: %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	tmpDir, err := getOrCreateListRepoClone(ctx, owner, repo, ref, host)
	if err != nil {
		return nil, err
	}

	lsTreeCmd := gitutil.SourceCommandContext(ctx, "", "-C", tmpDir, "ls-tree", "-r", "--name-only", "HEAD", dirPath+"/")
	lsTreeOutput, err := lsTreeCmd.CombinedOutput()
	if err != nil {
		remoteLog.Printf("Failed to list dir files: %s", string(lsTreeOutput))
		return nil, fmt.Errorf("failed to list dir files: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(lsTreeOutput)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Only include direct children (no additional path separator after dirPath/)
		afterDirPath := strings.TrimPrefix(line, dirPath+"/")
		if !strings.Contains(afterDirPath, "/") && afterDirPath != "" {
			files = append(files, line)
		}
	}

	remoteLog.Printf("Found %d files in dir via git for %s/%s@%s (path: %s)", len(files), owner, repo, ref, dirPath)
	return files, nil
}

// listDirAllFilesViaPublicAPI lists files in a directory using an unauthenticated
// call to the public GitHub API. Used as a last-resort fallback when both
// authenticated API and git clone fail.
func listDirAllFilesViaPublicAPI(ctx context.Context, owner, repo, ref, dirPath string) ([]string, error) {
	remoteLog.Printf("Attempting unauthenticated public API for listing dir files: %s/%s@%s (path: %s)", owner, repo, ref, dirPath)
	body, err := fetchPublicGitHubContentsAPI(ctx, owner, repo, dirPath, ref)
	if err != nil {
		return nil, fmt.Errorf("unauthenticated public API also failed for %s/%s@%s (path: %s): %w", owner, repo, ref, dirPath, err)
	}

	var contents []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &contents); err != nil {
		return nil, fmt.Errorf("failed to parse public API response: %w", err)
	}

	var files []string
	for _, item := range contents {
		if item.Type == "file" {
			files = append(files, item.Path)
		}
	}
	remoteLog.Printf("Found %d files via public API for %s/%s@%s (path: %s)", len(files), owner, repo, ref, dirPath)
	return files, nil
}

// ListDirAllFilesRecursivelyForHost lists all files (any extension) that are under the
// given directory in a remote GitHub repository, including files in subdirectories at any
// depth. This is used for copying entire skill folders.
func ListDirAllFilesRecursivelyForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	return listDirAllFilesRecursivelyForHost(ctx, owner, repo, ref, dirPath, host)
}

func listDirAllFilesRecursivelyForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Listing all files recursively in dir for %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	client, err := createRESTClientForHost(host)
	if err != nil {
		remoteLog.Printf("Failed to create REST client, attempting git fallback: %v", err)
		return listDirAllFilesRecursivelyViaGitForHost(ctx, owner, repo, ref, dirPath, host)
	}

	files, err := listContentsRecursively(ctx, client, owner, repo, ref, dirPath)
	if err != nil {
		errStr := err.Error()
		if errorutil.IsAuthError(errStr) {
			remoteLog.Printf("GitHub API auth failed, attempting git fallback for %s/%s@%s", owner, repo, ref)
			gitFiles, gitErr := listDirAllFilesRecursivelyViaGitForHost(ctx, owner, repo, ref, dirPath, host)
			if gitErr != nil {
				// No public API fallback for recursive listing — would require
				// multiple unauthenticated calls and is unlikely to stay within
				// the 60 req/hour rate limit. Surface both errors.
				return nil, fmt.Errorf("failed to list dir files recursively via API (auth error) and git fallback: API error: %w, Git error: %w", err, gitErr)
			}
			return gitFiles, nil
		}
		return nil, err
	}

	remoteLog.Printf("Found %d files recursively in dir %s/%s@%s (path: %s)", len(files), owner, repo, ref, dirPath)
	return files, nil
}

// listContentsRecursively uses the GitHub Contents API to recursively enumerate all
// files under dirPath. Each subdirectory triggers an additional API call.
func listContentsRecursively(ctx context.Context, client *api.RESTClient, owner, repo, ref, dirPath string) ([]string, error) {
	const maxSkillDirRecursionDepth = 10
	return listContentsRecursivelyWithDepth(ctx, client, owner, repo, ref, dirPath, 0, maxSkillDirRecursionDepth)
}

func listContentsRecursivelyWithDepth(ctx context.Context, client *api.RESTClient, owner, repo, ref, dirPath string, depth, maxDepth int) ([]string, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("maximum skill directory recursion depth exceeded at %q (max depth: %d)", dirPath, maxDepth)
	}

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}

	endpoint := buildContentsAPIPath(owner, repo, dirPath, ref)
	if err := client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &contents); err != nil {
		return nil, fmt.Errorf("failed to list dir files from %s/%s (path: %s): %w", owner, repo, dirPath, err)
	}

	var files []string
	for _, item := range contents {
		switch item.Type {
		case "file":
			files = append(files, item.Path)
		case "dir":
			subFiles, err := listContentsRecursivelyWithDepth(ctx, client, owner, repo, ref, item.Path, depth+1, maxDepth)
			if err != nil {
				return nil, err
			}
			files = append(files, subFiles...)
		}
	}
	return files, nil
}

func listDirAllFilesRecursivelyViaGitForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Git fallback for listing all dir files recursively: %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	tmpDir, err := getOrCreateListRepoClone(ctx, owner, repo, ref, host)
	if err != nil {
		return nil, err
	}

	// Normalise dirPath so it never has a trailing slash before we append one.
	cleanDirPath := strings.TrimRight(dirPath, "/")
	lsTreeCmd := gitutil.SourceCommandContext(ctx, "", "-C", tmpDir, "ls-tree", "-r", "--name-only", "HEAD", cleanDirPath+"/")
	lsTreeOutput, err := lsTreeCmd.CombinedOutput()
	if err != nil {
		remoteLog.Printf("Failed to list dir files recursively: %s", string(lsTreeOutput))
		return nil, fmt.Errorf("failed to list dir files recursively: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(lsTreeOutput)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// git ls-tree already scopes results to dirPrefix; include every non-empty line.
		files = append(files, line)
	}

	remoteLog.Printf("Found %d files recursively in dir via git for %s/%s@%s (path: %s)", len(files), owner, repo, ref, dirPath)
	return files, nil
}

// ListDirSubdirsForHost lists subdirectory paths that are direct children of the given
// directory in a remote GitHub repository. This is used for auto-discovering skill dirs.
func ListDirSubdirsForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	return listDirSubdirsForHost(ctx, owner, repo, ref, dirPath, host)
}

func listDirSubdirsForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Listing subdirs in %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	client, err := createRESTClientForHost(host)
	if err != nil {
		remoteLog.Printf("Failed to create REST client, attempting git fallback: %v", err)
		return listDirSubdirsViaGitForHost(ctx, owner, repo, ref, dirPath, host)
	}

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}

	endpoint := buildContentsAPIPath(owner, repo, dirPath, ref)
	err = client.DoWithContext(ctx, http.MethodGet, endpoint, nil, &contents)
	if err != nil {
		errStr := err.Error()
		if errorutil.IsAuthError(errStr) {
			remoteLog.Printf("GitHub API auth failed, attempting git fallback for %s/%s@%s", owner, repo, ref)
			dirs, gitErr := listDirSubdirsViaGitForHost(ctx, owner, repo, ref, dirPath, host)
			if gitErr != nil {
				if host == "" || host == "github.com" {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s@%s", owner, repo, ref)
					return listDirSubdirsViaPublicAPI(ctx, owner, repo, ref, dirPath)
				}
				return nil, fmt.Errorf("failed to list subdirs via API (auth error) and git fallback: API error: %w, Git error: %w", err, gitErr)
			}
			return dirs, nil
		}
		return nil, fmt.Errorf("failed to list subdirs from %s/%s@%s (path: %s): %w", owner, repo, ref, dirPath, err)
	}

	var dirs []string
	for _, item := range contents {
		if item.Type == "dir" {
			dirs = append(dirs, item.Path)
		}
	}

	remoteLog.Printf("Found %d subdirs in %s/%s@%s (path: %s)", len(dirs), owner, repo, ref, dirPath)
	return dirs, nil
}

func listDirSubdirsViaGitForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	remoteLog.Printf("Git fallback for listing subdirs: %s/%s@%s (path: %s)", owner, repo, ref, dirPath)

	tmpDir, err := getOrCreateListRepoClone(ctx, owner, repo, ref, host)
	if err != nil {
		return nil, err
	}

	// Use ls-tree -d to list only direct subdirectory entries.
	lsTreeDirsCmd := gitutil.SourceCommandContext(ctx, "", "-C", tmpDir, "ls-tree", "--name-only", "-d", "HEAD", dirPath+"/")
	lsTreeDirsOutput, err := lsTreeDirsCmd.CombinedOutput()
	if err != nil {
		remoteLog.Printf("Failed to list tree subdirs: %s", string(lsTreeDirsOutput))
		return nil, fmt.Errorf("failed to list subdirs: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(lsTreeDirsOutput)), "\n")
	var dirs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		afterDirPath := strings.TrimPrefix(line, dirPath+"/")
		if !strings.Contains(afterDirPath, "/") && afterDirPath != "" {
			dirs = append(dirs, line)
		}
	}

	remoteLog.Printf("Found %d subdirs via git for %s/%s@%s (path: %s)", len(dirs), owner, repo, ref, dirPath)
	return dirs, nil
}

// listDirSubdirsViaPublicAPI lists subdirectories using an unauthenticated call
// to the public GitHub API. Used as a last-resort fallback when both
// authenticated API and git clone fail (e.g. enterprise SAML tokens).
func listDirSubdirsViaPublicAPI(ctx context.Context, owner, repo, ref, dirPath string) ([]string, error) {
	remoteLog.Printf("Attempting unauthenticated public API for listing subdirs: %s/%s@%s (path: %s)", owner, repo, ref, dirPath)
	body, err := fetchPublicGitHubContentsAPI(ctx, owner, repo, dirPath, ref)
	if err != nil {
		return nil, fmt.Errorf("unauthenticated public API also failed for %s/%s@%s (path: %s): %w", owner, repo, ref, dirPath, err)
	}

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &contents); err != nil {
		return nil, fmt.Errorf("failed to parse public API response: %w", err)
	}

	var dirs []string
	for _, item := range contents {
		if item.Type == "dir" {
			dirs = append(dirs, item.Path)
		}
	}
	remoteLog.Printf("Found %d subdirs via public API for %s/%s@%s (path: %s)", len(dirs), owner, repo, ref, dirPath)
	return dirs, nil
}
