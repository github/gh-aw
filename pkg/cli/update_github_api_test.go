//go:build !integration

package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgRolloutDependencyCacheSharedBetweenWorkers(t *testing.T) {
	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("GH_HOST", "github.localhost")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		headers := http.Header{"Date": {time.Now().UTC().Format(http.TimeFormat)}}
		return updateGitHubTestResponse(req, http.StatusOK, `{"default_branch":"main"}`, headers), nil
	})
	cacheDir := t.TempDir()
	for range 2 {
		ctx := context.WithValue(context.Background(), orgRolloutKey{}, &orgRolloutOptions{worker: true, cacheDir: cacheDir})
		ctx = withUpdateGitHubClient(ctx, false)
		client := updateGitHubClientFromContext(ctx)
		require.NotNil(t, client)
		assert.Same(t, client, updateGitHubClientFromContext(withUpdateGitHubClient(ctx, false)))
		var result struct {
			DefaultBranch string `json:"default_branch"`
		}
		_, err := client.get(ctx, "/repos/actions/cache", &result)
		require.NoError(t, err)
		assert.Equal(t, "main", result.DefaultBranch)
	}
	assert.Equal(t, 1, requests, "isolated workers should reuse identical dependency reads")
}

func newUpdateGitHubTestClient(t *testing.T, transport roundTripperFunc) (*updateGitHubClient, context.Context) {
	t.Helper()
	opts := api.ClientOptions{
		Host: "github.localhost", APIHost: "api.github.localhost", AuthToken: "test-token",
		Transport: transport, LogIgnoreEnv: true,
	}
	client, err := api.NewRESTClient(opts)
	require.NoError(t, err)
	opts.Headers = map[string]string{"Accept": "application/vnd.github.sha"}
	shaClient, err := api.NewRESTClient(opts)
	require.NoError(t, err)
	c := &updateGitHubClient{
		client: client, shaClient: shaClient,
		commits: make(map[string]struct{}),
		tags:    make(map[string]string),
	}
	return c, context.WithValue(context.Background(), updateGitHubClientKey{}, c)
}

func updateGitHubTestResponse(req *http.Request, status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body: io.NopCloser(strings.NewReader(body)), Header: headers, Request: req,
	}
}

func TestUpdateGitHubActionTagResolvesOnceAndReusesVerifiedCommit(t *testing.T) {
	t.Parallel()
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requests := 0
	client, ctx := newUpdateGitHubTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, "/repos/actions/cache/commits/refs%2Ftags%2Fv4.2.0", req.URL.EscapedPath())
		assert.Equal(t, "application/vnd.github.sha", req.Header.Get("Accept"))
		return updateGitHubTestResponse(req, http.StatusOK, sha+"\n", nil), nil
	})
	got, err := getActionSHAForTag(ctx, "actions/cache", "v4.2.0")
	require.NoError(t, err)
	assert.Equal(t, sha, got)

	commitChecks, tagChecks := 0, 0
	resolvers := client.validationResolvers(validationResolvers{
		verifyActionCommitExists: func(context.Context, string, string) error {
			commitChecks++
			return nil
		},
		resolveActionVersionToSHA: func(context.Context, string, string) (string, error) {
			tagChecks++
			return sha, nil
		},
	})
	require.NoError(t, resolvers.verifyActionCommitExists(ctx, "actions/cache/restore", strings.ToUpper(sha)))
	resolved, err := resolvers.resolveActionVersionToSHA(ctx, "actions/cache/save", "v4.2.0")
	require.NoError(t, err)
	assert.Equal(t, sha, resolved)
	assert.Equal(t, 0, commitChecks)
	assert.Equal(t, 0, tagChecks)
	assert.Equal(t, 1, requests, "tag peeling and final validation must not issue additional requests")

	require.NoError(t, resolvers.verifyActionCommitExists(ctx, "actions/cache", strings.Repeat("b", 40)))
	_, err = resolvers.resolveActionVersionToSHA(ctx, "actions/cache", "v4.1.0")
	require.NoError(t, err)
	assert.Equal(t, 1, commitChecks, "unverified commits must still be checked")
	assert.Equal(t, 1, tagChecks, "unverified tags must still be checked")
}

func TestUpdateGitHubActionTagDoesNotTrustFailedOrInvalidResponses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"message":"Resource not accessible"}`},
		{"missing", http.StatusNotFound, `{"message":"Not Found"}`},
		{"invalid SHA", http.StatusOK, "not-a-commit"},
		{"empty response", http.StatusOK, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, ctx := newUpdateGitHubTestClient(t, func(req *http.Request) (*http.Response, error) {
				return updateGitHubTestResponse(req, tt.status, tt.body, nil), nil
			})
			_, err := getActionSHAForTag(ctx, "actions/cache", "v4.2.0")
			require.Error(t, err)
			assert.Empty(t, client.commits)
			assert.Empty(t, client.tags)
		})
	}
}

func TestUpdateGitHubReleasePagination(t *testing.T) {
	t.Parallel()
	requests := 0
	client, ctx := newUpdateGitHubTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, "100", req.URL.Query().Get("per_page"))
		switch req.URL.Query().Get("page") {
		case "1":
			return updateGitHubTestResponse(req, http.StatusOK, `[{"tag_name":"v2"}]`,
				http.Header{"Link": {`<https://api.github.localhost/repos/actions/cache/releases?per_page=100&page=2>; rel="next"`}}), nil
		case "2":
			return updateGitHubTestResponse(req, http.StatusOK, `[{"tag_name":"v1"}]`, nil), nil
		default:
			t.Errorf("unexpected page: %s", req.URL.RawQuery)
			return updateGitHubTestResponse(req, http.StatusNotFound, `{"message":"Not Found"}`, nil), nil
		}
	})
	tags, err := client.releaseTags(ctx, "actions/cache")
	require.NoError(t, err)
	assert.Equal(t, "v2\nv1", string(tags))
	assert.Equal(t, 2, requests)
}

func TestUpdateGitHubSourceReadsUseNativeClient(t *testing.T) {
	t.Parallel()
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requests := 0
	_, ctx := newUpdateGitHubTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		switch req.URL.Path {
		case "/repos/owner/repo":
			return updateGitHubTestResponse(req, http.StatusOK, `{"default_branch":"main"}`, nil), nil
		case "/repos/owner/repo/commits/main":
			return updateGitHubTestResponse(req, http.StatusOK, `{"sha":"`+sha+`"}`, nil), nil
		case "/repos/owner/repo/contents/workflows/with space.md":
			assert.Equal(t, "feature/one", req.URL.Query().Get("ref"))
			assert.Contains(t, req.URL.EscapedPath(), "with%20space.md")
			return updateGitHubTestResponse(req, http.StatusOK, `{"content":"aGVsbG8="}`, nil), nil
		default:
			t.Errorf("unexpected endpoint: %s", req.URL)
			return updateGitHubTestResponse(req, http.StatusNotFound, `{"message":"Not Found"}`, nil), nil
		}
	})
	branch, err := getRepoDefaultBranch(ctx, "owner/repo")
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	commit, err := getLatestBranchCommitInfo(ctx, "owner/repo", branch)
	require.NoError(t, err)
	assert.Equal(t, sha, commit.SHA)
	content, err := downloadWorkflowContent(ctx, "owner/repo", "workflows/with space.md", "feature/one", false)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
	assert.Equal(t, 3, requests)
}
