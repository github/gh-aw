//go:build !js && !wasm

package parser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var remoteLog = logger.New("parser:remote_fetch")

// publicAPIClient is a shared HTTP client used for unauthenticated GitHub API
// fallback calls. It carries a timeout to prevent indefinite hangs on slow or
// unresponsive hosts.
var publicAPIClient = &http.Client{Timeout: constants.DefaultHTTPClientTimeout}

// getGitHubHostForRepoFunc allows tests to inject host resolution behavior.
var getGitHubHostForRepoFunc = GetGitHubHostForRepo

func createRESTClientForHost(host string) (*api.RESTClient, error) {
	opts := api.ClientOptions{Timeout: constants.DefaultHTTPClientTimeout}
	if host != "" {
		opts.Host = host
	}
	return api.NewRESTClient(opts)
}

func buildContentsAPIPath(owner, repo, path, ref string) string {
	pathSegments := strings.Split(path, "/")
	for i := range pathSegments {
		pathSegments[i] = url.PathEscape(pathSegments[i])
	}
	return fmt.Sprintf(
		"repos/%s/%s/contents/%s?ref=%s",
		owner,
		repo,
		strings.Join(pathSegments, "/"),
		url.QueryEscape(ref),
	)
}

func fetchRemoteFileContent(ctx context.Context, client *api.RESTClient, owner, repo, path, ref string, fileContent any) (err error) {
	finish := ctxutil.StartOperation(ctx, fmt.Sprintf("GitHub GET %s/%s/contents/%s@%s", owner, repo, path, ref))
	defer func() { finish(err) }()
	remoteLog.Printf("Fetching remote file via REST API: %s/%s path=%s ref=%s", owner, repo, path, ref)
	return client.DoWithContext(ctx, http.MethodGet, buildContentsAPIPath(owner, repo, path, ref), nil, fileContent)
}

// canUseUnauthenticatedPublicGitHubFallback returns true only when the target
// host is explicitly public github.com, or when an empty host is resolved by
// GetGitHubHostForRepo to public github.com. If host resolution ever returns an
// empty value, fallback is denied rather than treating it as public GitHub.
func canUseUnauthenticatedPublicGitHubFallback(owner, repo, host string) bool {
	resolvedHost := host
	if resolvedHost == "" {
		resolvedHost = getGitHubHostForRepoFunc(owner, repo)
	}
	if strings.TrimSpace(resolvedHost) == "" {
		return false
	}
	return strings.EqualFold(
		stringutil.NormalizeGitHubHostURL(resolvedHost),
		string(constants.PublicGitHubHost),
	)
}

// fetchPublicGitHubContentsAPI makes an unauthenticated GET request to the
// GitHub public REST API contents endpoint. This is used as a last-resort
// fallback when the current token (e.g. an enterprise SAML-enforced token)
// cannot access cross-organization public repositories and git clone also
// fails. Unauthenticated requests are subject to a lower rate limit
// (60 req/hour) but are sufficient for the handful of calls during update.
func fetchPublicGitHubContentsAPI(ctx context.Context, owner, repo, path, ref string) (result []byte, resultErr error) {
	// Encode each path segment independently so that '/' separators are
	// preserved — url.PathEscape would turn them into '%2F', breaking nested
	// paths like '.github/workflows/shared/foo.md'.
	segments := strings.Split(path, "/")
	encodedSegments := make([]string, len(segments))
	for i, s := range segments {
		encodedSegments[i] = url.PathEscape(s)
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		owner, repo, strings.Join(encodedSegments, "/"), url.QueryEscape(ref))
	remoteLog.Printf("Unauthenticated public API fallback fetch: %s/%s path=%s ref=%s", owner, repo, path, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	finish := ctxutil.StartOperation(ctx, fmt.Sprintf("GitHub public GET %s/%s/contents/%s@%s", owner, repo, path, ref))
	defer func() { finish(resultErr) }()
	resp, err := publicAPIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			remoteLog.Printf("Failed to close public API response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		remoteLog.Printf("Public API fallback returned non-OK status: %d for %s/%s", resp.StatusCode, owner, repo)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
