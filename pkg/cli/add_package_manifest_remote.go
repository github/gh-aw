// add_package_manifest_remote.go: thin GitHub remote-fetch wrappers (download
// file/list dir/branch/release lookups) and repo-spec parsing utilities.

package cli

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

func parseRepositoryPackageSpec(spec string) (*RepoSpec, bool, error) {
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") || isLocalWorkflowPath(spec) {
		return nil, false, nil
	}

	parts := strings.SplitN(spec, "@", 2)
	specWithoutVersion := parts[0]
	if strings.HasSuffix(strings.ToLower(specWithoutVersion), ".md") {
		return nil, false, nil
	}

	slashParts := strings.Split(specWithoutVersion, "/")
	if len(slashParts) < 2 || slashParts[0] == "" || slashParts[1] == "" {
		return nil, false, nil
	}
	if !parser.IsValidGitHubIdentifier(slashParts[0]) || !parser.IsValidGitHubRepositoryName(slashParts[1]) {
		return nil, false, nil
	}

	packagePath := strings.Trim(strings.Join(slashParts[2:], "/"), "/")
	if packagePath != "" {
		cleanedPath := path.Clean(packagePath)
		if cleanedPath == "." {
			packagePath = ""
		} else if cleanedPath == ".." || strings.HasPrefix(cleanedPath, "../") {
			return nil, true, fmt.Errorf("invalid repository package path %q: path traversal outside the repository is not allowed. Use a path relative to the repository root. Example: packages/my-package", packagePath)
		} else {
			packagePath = cleanedPath
		}
	}

	repoSpec := &RepoSpec{
		RepoSlug:    path.Join(slashParts[0], slashParts[1]),
		PackagePath: packagePath,
	}
	if len(parts) == 2 {
		repoSpec.Version = parts[1]
	}

	addPackageManifestLog.Printf("Parsed repository package spec %q as repo=%s path=%q version=%q", spec, repoSpec.RepoSlug, repoSpec.PackagePath, repoSpec.Version)
	return repoSpec, true, nil
}

func joinRepositoryPackagePath(packagePath, relativePath string) string {
	if packagePath == "" {
		return filepath.ToSlash(relativePath)
	}
	return filepath.ToSlash(filepath.Join(packagePath, relativePath))
}

func stringValue(value any) (string, bool) {
	s, ok := value.(string)
	return s, ok
}

func isRepositoryFileNotFound(err error) bool {
	return errors.Is(err, errRepositoryPackageFileNotFound)
}

func isRepositoryPackageManifestNotFound(err error) bool {
	return errors.Is(err, errRepositoryPackageManifestNotFound)
}

func downloadRepositoryPackageFileFromGitHubForHost(ctx context.Context, owner, repo, path, ref, host string) ([]byte, error) {
	addPackageManifestLog.Printf("Downloading repository package file %s from %s/%s@%s (host=%q)", path, owner, repo, ref, host)
	content, err := parser.DownloadFileFromGitHubForHost(ctx, owner, repo, path, ref, host)
	return content, normalizeRepositoryPackageRemoteError(err)
}

func listRepositoryPackageWorkflowFilesForHost(ctx context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
	files, err := parser.ListWorkflowFilesForHost(ctx, owner, repo, ref, workflowPath, host)
	return files, normalizeRepositoryPackageRemoteError(err)
}

func listRepositoryPackageDirFilesForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	files, err := parser.ListDirAllFilesForHost(ctx, owner, repo, ref, dirPath, host)
	return files, normalizeRepositoryPackageRemoteError(err)
}

func listRepositoryPackageDirFilesRecursivelyForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	files, err := parser.ListDirAllFilesRecursivelyForHost(ctx, owner, repo, ref, dirPath, host)
	return files, normalizeRepositoryPackageRemoteError(err)
}

func listRepositoryPackageDirSubdirsForHost(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
	dirs, err := parser.ListDirSubdirsForHost(ctx, owner, repo, ref, dirPath, host)
	return dirs, normalizeRepositoryPackageRemoteError(err)
}

func normalizeRepositoryPackageRemoteError(err error) error {
	if err == nil || !errorutil.IsNotFoundError(err) {
		return err
	}
	addPackageManifestLog.Printf("Remote package file/dir not found, normalizing error: %v", err)
	return packageRemoteNotFoundError{cause: err}
}

func resolveRepositoryPackageDefaultBranch(ctx context.Context, repoSlug, host string) (string, error) {
	args := []string{"api", "/repos/" + repoSlug, "--jq", ".default_branch"}
	var output []byte
	var err error
	if host != "" {
		output, err = workflow.RunGHContextWithHost(ctx, "Resolving source repository default branch...", host, args...)
		if err != nil {
			return "", err
		}
	} else {
		output, err = workflow.RunGH("Resolving source repository default branch...", args...)
		if err != nil {
			return "", err
		}
	}

	branch := strings.TrimSpace(string(output))
	if branch == "" {
		targetHost := host
		if targetHost == "" {
			targetHost = "the configured host"
		}
		return "", fmt.Errorf("repository %s on %s returned an empty default branch. Ensure the repository exists and is accessible", repoSlug, targetHost)
	}
	addPackageManifestLog.Printf("Resolved default branch for %s: %s", repoSlug, branch)
	return branch, nil
}

// repositoryPackageEffectiveRef returns the effective ref for repository package
// operations. Explicit user-provided versions always win; otherwise this uses a
// previously resolved package ref when available.
func repositoryPackageEffectiveRef(repoSpec *RepoSpec, pkg *resolvedRepositoryPackage) string {
	if repoSpec != nil && repoSpec.Version != "" {
		return repoSpec.Version
	}
	if pkg != nil && pkg.ResolvedRef != "" {
		return pkg.ResolvedRef
	}
	return ""
}

// isGhAwRepository reports whether repoSlug identifies github/gh-aw.
// Matching is case-insensitive and ignores surrounding whitespace.
func isGhAwRepository(repoSlug string) bool {
	return strings.EqualFold(strings.TrimSpace(repoSlug), ghAwRepositorySlug)
}

// resolveRepositoryPackageLatestRelease resolves the latest stable release tag
// for a repository package source.
//
// repoSlug must be in "owner/repo" format. host is an optional explicit GitHub
// hostname (for example "github.com" or a GHES host); when provided, gh API
// calls are executed against that host.
func resolveRepositoryPackageLatestRelease(ctx context.Context, repoSlug, host string) (string, error) {
	addPackageManifestLog.Printf("Resolving latest release for %s (host=%q)", repoSlug, host)
	deps := workflowUpdateDeps{
		runReleasesAPI: func(innerCtx context.Context, repo string) ([]byte, error) {
			args := []string{"api", fmt.Sprintf("/repos/%s/releases", repo), "--jq", ".[].tag_name"}
			if host != "" {
				return workflow.RunGHContextWithHost(innerCtx, "Fetching releases...", host, args...)
			}
			return workflow.RunGHContext(innerCtx, "Fetching releases...", args...)
		},
	}

	return resolveLatestReleaseWithDeps(ctx, deps, repoSlug, "", true, false, 0)
}
