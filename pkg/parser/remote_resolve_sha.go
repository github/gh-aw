//go:build !js && !wasm

package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

// createRESTClientForHostFunc allows tests to inject a stub REST client factory.
var createRESTClientForHostFunc = createRESTClientForHost

// resolveRefToSHAViaGitFunc allows tests to inject a stub for the git ls-remote fallback.
var resolveRefToSHAViaGitFunc = resolveRefToSHAViaGit

// resolveRefToSHAViaGit resolves a git ref to SHA using git ls-remote
// This is a fallback for when GitHub API authentication fails
func resolveRefToSHAViaGit(ctx context.Context, owner, repo, ref, host string) (result string, resultErr error) {
	finish := ctxutil.StartOperation(ctx, fmt.Sprintf("git ls-remote %s/%s@%s", owner, repo, ref))
	defer func() { finish(resultErr) }()
	remoteLog.Printf("Attempting git ls-remote fallback for ref resolution: %s/%s@%s", owner, repo, ref)

	if err := gitutil.ValidateGitRef(ref); err != nil {
		return "", fmt.Errorf("refusing git ls-remote fallback: %w", err)
	}

	var githubHost string
	if host != "" {
		githubHost = "https://" + host
	} else {
		githubHost = GetGitHubHostForRepo(owner, repo)
	}
	repoURL := fmt.Sprintf("%s/%s/%s.git", githubHost, owner, repo)

	// Try to resolve the ref using git ls-remote.
	// ValidateGitRef above guarantees ref does not begin with '-' before it is passed
	// as a separate argument, so no extra separator is needed here.
	cmd := exec.CommandContext(ctx, "git", "ls-remote", repoURL, ref)
	output, err := cmd.Output()
	if err != nil {
		// If exact ref doesn't work, try with refs/heads/ and refs/tags/ prefixes
		for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
			cmd = exec.CommandContext(ctx, "git", "ls-remote", repoURL, prefix+ref)
			output, err = cmd.Output()
			if err == nil && len(output) > 0 {
				break
			}
		}

		if err != nil {
			return "", fmt.Errorf("failed to resolve ref via git ls-remote: %w", err)
		}
	}

	// Parse the output: "<sha> <ref>"
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no matching ref found for %s", ref)
	}

	// Extract SHA from the first line
	parts := strings.Fields(lines[0])
	if len(parts) < 1 {
		return "", errors.New("invalid git ls-remote output format")
	}

	sha := parts[0]

	// Validate it's a valid SHA
	if !gitutil.IsValidFullSHACaseInsensitive(sha) {
		return "", fmt.Errorf("invalid SHA format from git ls-remote: %s", sha)
	}

	remoteLog.Printf("Successfully resolved ref via git ls-remote: %s/%s@%s -> %s", owner, repo, ref, sha)
	return sha, nil
}

// resolveRefToSHA resolves a git ref (branch, tag, or SHA) to its commit SHA
func resolveRefToSHA(ctx context.Context, owner, repo, ref, host string) (string, error) {
	// If ref is already a full SHA (40 hex characters), return it as-is
	if gitutil.IsValidFullSHACaseInsensitive(ref) {
		return ref, nil
	}

	client, err := createRESTClientForHostFunc(host)
	if err != nil {
		if errorutil.IsAuthError(err.Error()) {
			remoteLog.Printf("REST client creation failed due to auth error, attempting git ls-remote fallback for %s/%s@%s: %v", owner, repo, ref, err)
			sha, gitErr := resolveRefToSHAViaGitFunc(ctx, owner, repo, ref, host)
			if gitErr != nil {
				if canUseUnauthenticatedPublicGitHubFallback(owner, repo, host) {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s@%s", owner, repo, ref)
					return resolveRefToSHAViaPublicAPI(ctx, owner, repo, ref)
				}
				return "", fmt.Errorf("failed to resolve ref via GitHub API setup (auth error) and git ls-remote: API error: %w, Git error: %w", err, gitErr)
			}
			return sha, nil
		}
		return "", fmt.Errorf("failed to create GitHub REST client: %w", err)
	}

	return resolveRefToSHAWithFallbacks(ctx, client, owner, repo, ref, host, resolveRefToSHAViaGit, resolveRefToSHAViaPublicAPI)
}

type commitLookupResponse struct {
	SHA string `json:"sha"`
}

type restCommitResolver interface {
	DoWithContext(ctx context.Context, method string, path string, body io.Reader, response any) error
}

func resolveRefToSHAWithFallbacks(
	ctx context.Context,
	client restCommitResolver,
	owner, repo, ref, host string,
	gitFallback func(context.Context, string, string, string, string) (string, error),
	publicFallback func(context.Context, string, string, string) (string, error),
) (string, error) {
	var result commitLookupResponse
	finish := ctxutil.StartOperation(ctx, "GitHub GET "+buildCommitLookupAPIPath(owner, repo, ref))
	err := client.DoWithContext(ctx, http.MethodGet, buildCommitLookupAPIPath(owner, repo, ref), nil, &result)
	finish(err)

	if err != nil {
		if isGitHubAPIAuthError(err) {
			remoteLog.Printf("GitHub API authentication failed, attempting git ls-remote fallback for %s/%s@%s", owner, repo, ref)
			// Try fallback using git ls-remote for public repositories
			sha, gitErr := gitFallback(ctx, owner, repo, ref, host)
			if gitErr != nil {
				if canUseUnauthenticatedPublicGitHubFallback(owner, repo, host) {
					remoteLog.Printf("Git fallback also failed, attempting unauthenticated API for %s/%s@%s", owner, repo, ref)
					return publicFallback(ctx, owner, repo, ref)
				}
				return "", fmt.Errorf("failed to resolve ref via GitHub API (auth error) and git ls-remote: API error: %w, Git error: %w", err, gitErr)
			}
			return sha, nil
		}

		return "", fmt.Errorf("failed to resolve ref %s to SHA for %s/%s: %w", ref, owner, repo, err)
	}

	sha := strings.TrimSpace(result.SHA)
	if sha == "" {
		return "", fmt.Errorf("empty SHA returned for ref %s in %s/%s", ref, owner, repo)
	}

	// Validate it's a valid SHA (40 hex characters)
	if !gitutil.IsValidFullSHACaseInsensitive(sha) {
		return "", fmt.Errorf("invalid SHA format returned: %s", sha)
	}

	return sha, nil
}

func isGitHubAPIAuthError(err error) bool {
	var httpErr *api.HTTPError
	return errors.As(err, &httpErr) &&
		(httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)
}

// buildCommitLookupAPIPath returns the GitHub commits API path for a ref,
// URL-escaping the ref segment so branch names containing slashes are valid.
func buildCommitLookupAPIPath(owner, repo, ref string) string {
	return fmt.Sprintf("repos/%s/%s/commits/%s", owner, repo, url.PathEscape(ref))
}

// resolveRefToSHAViaPublicAPI resolves a git ref to its commit SHA using an
// unauthenticated call to the public GitHub API. Used as a last-resort fallback
// when both authenticated API and git ls-remote fail.
func resolveRefToSHAViaPublicAPI(ctx context.Context, owner, repo, ref string) (resolvedSHA string, resultErr error) {
	finish := ctxutil.StartOperation(ctx, fmt.Sprintf("GitHub public GET %s/%s/commits/%s", owner, repo, ref))
	defer func() { finish(resultErr) }()
	remoteLog.Printf("Attempting unauthenticated public API ref resolution for %s/%s@%s", owner, repo, ref)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s",
		owner, repo, url.PathEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := publicAPIClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			remoteLog.Printf("Failed to close public API ref response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unauthenticated public API failed for %s/%s@%s: HTTP %d: %s", owner, repo, ref, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse commit response: %w", err)
	}
	if !gitutil.IsValidFullSHACaseInsensitive(result.SHA) {
		return "", fmt.Errorf("invalid SHA returned from public API: %q", result.SHA)
	}
	return result.SHA, nil
}

// ResolveRefToSHAForHost resolves a git ref to its full commit SHA on a specific GitHub host.
// Use this when the target repository is on a different host than the one configured via GH_HOST.
// host is the hostname without scheme (e.g., "github.com", "myorg.ghe.com").
// An empty host uses the default configured host (GH_HOST or github.com).
func ResolveRefToSHAForHost(ctx context.Context, owner, repo, ref, host string) (string, error) {
	return resolveRefToSHA(ctx, owner, repo, ref, host)
}

// ErrVerificationSkipped is returned by VerifyCommitExists when the check cannot be
// performed due to an auth or network failure. Callers should treat it as non-fatal.
var ErrVerificationSkipped = errors.New("commit verification skipped")

// VerifyCommitExists confirms that a full commit SHA exists in the given repository by
// querying the GitHub commits API. Unlike ResolveRefToSHAForHost, it always performs
// an API call so that a missing commit is detected (not silently passed through).
//
// Returns nil if the commit is found, ErrVerificationSkipped (wrapped) for auth or
// network errors where existence cannot be determined, and an unwrapped error when the
// API definitively reports the commit does not exist (e.g. HTTP 404).
func VerifyCommitExists(ctx context.Context, owner, repo, sha, host string) error {
	client, err := createRESTClientForHostFunc(host)
	if err != nil {
		return fmt.Errorf("%w: failed to create REST client: %w", ErrVerificationSkipped, err)
	}
	var result commitLookupResponse
	finish := ctxutil.StartOperation(ctx, "GitHub GET "+buildCommitLookupAPIPath(owner, repo, sha))
	apiErr := client.DoWithContext(ctx, http.MethodGet, buildCommitLookupAPIPath(owner, repo, sha), nil, &result)
	finish(apiErr)
	if apiErr != nil {
		if isGitHubAPIAuthError(apiErr) {
			return fmt.Errorf("%w: %w", ErrVerificationSkipped, apiErr)
		}
		return apiErr
	}
	return nil
}
