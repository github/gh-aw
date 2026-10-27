package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
)

type updateGitHubClientKey struct{}

type updateGitHubClient struct {
	client    *api.RESTClient
	shaClient *api.RESTClient
	verbose   bool
	mu        sync.Mutex
	commits   map[string]struct{}
	tags      map[string]string
}

func withUpdateGitHubClient(ctx context.Context, verbose bool) context.Context {
	if updateGitHubClientFromContext(ctx) != nil {
		return ctx
	}
	finish := ctxutil.StartOperation(ctx, "GitHub API client setup")
	opts := api.ClientOptions{
		Host:    workflow.GitHubCLIHost(),
		Timeout: constants.DefaultHTTPClientTimeout,
	}
	if rollout := orgRolloutFromContext(ctx); rollout != nil && rollout.worker && rollout.cacheDir != "" {
		opts.EnableCache = true
		opts.CacheDir = rollout.cacheDir
		opts.CacheTTL = 5 * time.Minute
	}
	client, err := api.NewRESTClient(opts)
	if err != nil {
		finish(err)
		updateLog.Printf("Native GitHub API client unavailable; using GitHub CLI requests: %v", err)
		return ctx
	}
	opts.Headers = map[string]string{"Accept": "application/vnd.github.sha"}
	shaClient, err := api.NewRESTClient(opts)
	finish(err)
	if err != nil {
		updateLog.Printf("Native GitHub SHA client unavailable; using GitHub CLI requests: %v", err)
		return ctx
	}
	return context.WithValue(ctx, updateGitHubClientKey{}, &updateGitHubClient{
		client: client, shaClient: shaClient, verbose: verbose,
		commits: make(map[string]struct{}),
		tags:    make(map[string]string),
	})
}

func updateGitHubClientFromContext(ctx context.Context) *updateGitHubClient {
	if client, ok := ctx.Value(updateGitHubClientKey{}).(*updateGitHubClient); ok {
		return client
	}
	return nil
}

func (c *updateGitHubClient) request(ctx context.Context, client *api.RESTClient, endpoint string) (body []byte, headers http.Header, err error) {
	label, _, _ := strings.Cut(endpoint, "?")
	finish := ctxutil.StartOperation(ctx, "GitHub GET "+label)
	defer func() { finish(err) }()
	if c.verbose {
		var report func()
		ctx, report = traceUpdateGitHubRequest(ctx, label)
		defer report()
	}
	response, err := client.RequestWithContext(ctx, http.MethodGet, strings.TrimPrefix(endpoint, "/"), nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing GitHub response for %s: %w", endpoint, closeErr))
		}
	}()
	body, err = io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, err
	}
	return body, response.Header, nil
}

func (c *updateGitHubClient) get(ctx context.Context, endpoint string, result any) (http.Header, error) {
	body, headers, err := c.request(ctx, c.client, endpoint)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, result); err != nil {
		return nil, fmt.Errorf("decoding GitHub response for %s: %w", endpoint, err)
	}
	return headers, nil
}

func (c *updateGitHubClient) releaseTags(ctx context.Context, repo string) ([]byte, error) {
	var tags []string
	for page := 1; ; page++ {
		var releases []struct {
			TagName string `json:"tag_name"`
		}
		headers, err := c.get(ctx, fmt.Sprintf("/repos/%s/releases?per_page=100&page=%d", repo, page), &releases)
		if err != nil {
			return nil, err
		}
		for _, release := range releases {
			tags = append(tags, release.TagName)
		}
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			return []byte(strings.Join(tags, "\n")), nil
		}
	}
}

func (c *updateGitHubClient) actionTagSHA(ctx context.Context, repo, tag string) (string, error) {
	repo = gitutil.ExtractBaseRepo(repo)
	// Qualify the tag so a branch with the same name cannot shadow it.
	// Request just the SHA, not the commit's file diffs.
	endpoint := fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape("refs/tags/"+tag))
	body, _, err := c.request(ctx, c.shaClient, endpoint)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(body))
	if !IsCommitSHA(sha) {
		return "", fmt.Errorf("invalid commit SHA returned for %s@%s (expected a 40-character commit SHA)", repo, tag)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commits[repo+"|"+strings.ToLower(sha)] = struct{}{}
	c.tags[repo+"|"+tag] = sha
	return sha, nil
}

func (c *updateGitHubClient) validationResolvers(fallback validationResolvers) validationResolvers {
	return validationResolvers{
		verifyActionCommitExists: func(ctx context.Context, repo, sha string) error {
			c.mu.Lock()
			_, verified := c.commits[gitutil.ExtractBaseRepo(repo)+"|"+strings.ToLower(sha)]
			c.mu.Unlock()
			if verified {
				updateLog.Printf("Reusing commit existence verified during this update: %s@%s", repo, sha)
				return nil
			}
			return fallback.verifyActionCommitExists(ctx, repo, sha)
		},
		resolveActionVersionToSHA: func(ctx context.Context, repo, ref string) (string, error) {
			c.mu.Lock()
			sha, verified := c.tags[gitutil.ExtractBaseRepo(repo)+"|"+ref]
			c.mu.Unlock()
			if verified {
				updateLog.Printf("Reusing action tag verified during this update: %s@%s", repo, ref)
				return sha, nil
			}
			return fallback.resolveActionVersionToSHA(ctx, repo, ref)
		},
	}
}
