package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/repoutil"
	"golang.org/x/sync/singleflight"
)

type outcomeAPICacheKey struct{}

type outcomeAPICache struct {
	mu        sync.RWMutex
	responses map[string][]byte
	inflight  singleflight.Group
}

func withOutcomeAPICache(ctx context.Context) context.Context {
	if _, ok := ctx.Value(outcomeAPICacheKey{}).(*outcomeAPICache); ok {
		return ctx
	}
	return context.WithValue(ctx, outcomeAPICacheKey{}, &outcomeAPICache{responses: make(map[string][]byte)})
}

func outcomeAPIArgs(repo string, args []string) []string {
	_, host := repoutil.NormalizeRepoForAPI(repo)
	if host == "" {
		host = lookupEnv("GH_HOST")
	}
	host = strings.ToLower(normalizedGitHubAPIHost(host))
	return append(slices.Clone(args), "--hostname", host)
}

func requestOutcomeJSON[T any](ctx context.Context, repo string, args []string) (T, error) {
	var result T
	if err := ctx.Err(); err != nil {
		return result, err
	}
	args = outcomeAPIArgs(repo, args)
	fetch := func() ([]byte, error) {
		output, err := requestLifecycleAPI(ctx, repo, args)
		if err != nil {
			return nil, err
		}
		var checked T
		if err := json.Unmarshal(output, &checked); err != nil {
			return nil, fmt.Errorf("parsing outcome response: %w", err)
		}
		return output, nil
	}
	var output []byte
	var err error
	if cache, ok := ctx.Value(outcomeAPICacheKey{}).(*outcomeAPICache); ok {
		output, err = cache.load(ctx, fmt.Sprintf("%q", args), fetch)
	} else {
		output, err = fetch()
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *outcomeAPICache) read(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	output, ok := c.responses[key]
	return output, ok
}

func (c *outcomeAPICache) load(ctx context.Context, key string, fetch func() ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if output, ok := c.read(key); ok {
		outcomeEvalLog.Printf("Reusing outcome API response")
		return output, nil
	}
	pending := c.inflight.DoChan(key, func() (any, error) {
		if output, ok := c.read(key); ok {
			return output, nil
		}
		output, err := fetch()
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cacheableOutcomeResponse(output) {
			c.mu.Lock()
			c.responses[key] = output
			c.mu.Unlock()
		}
		return output, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-pending:
		if result.Err != nil {
			return nil, result.Err
		}
		output, ok := result.Val.([]byte)
		if !ok {
			return nil, fmt.Errorf("unexpected cached outcome response type %T", result.Val)
		}
		return output, nil
	}
}

func cacheableOutcomeResponse(output []byte) bool {
	if strings.TrimSpace(string(output)) == "null" {
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(output, &object); err != nil {
		return true // Paginated REST responses are arrays, validated before caching.
	}
	if raw, ok := object["errors"]; ok {
		var apiErrors []json.RawMessage
		if err := json.Unmarshal(raw, &apiErrors); err != nil || len(apiErrors) > 0 {
			return false
		}
	}
	return true
}

func outcomeCachedLabels(ctx context.Context, endpoint, repo string) ([]map[string]any, bool) {
	cache, ok := ctx.Value(outcomeAPICacheKey{}).(*outcomeAPICache)
	if !ok {
		return nil, false
	}
	number, ok := strings.CutPrefix(endpoint, "issues/")
	if !ok {
		return nil, false
	}
	number, ok = strings.CutSuffix(number, "/labels")
	if !ok || number == "" || strings.Contains(number, "/") {
		return nil, false
	}
	ownerRepo, _ := repoutil.NormalizeRepoForAPI(repo)
	for _, kind := range []string{"issues", "pulls"} {
		args := outcomeAPIArgs(repo, []string{"api", fmt.Sprintf("repos/%s/%s/%s", escapeOwnerRepo(ownerRepo), kind, escapeEndpoint(number))})
		output, found := cache.read(fmt.Sprintf("%q", args))
		if !found {
			continue
		}
		var object struct {
			Labels json.RawMessage `json:"labels"`
		}
		if err := json.Unmarshal(output, &object); err != nil {
			continue
		}
		var labels []map[string]any
		if err := json.Unmarshal(object.Labels, &labels); err == nil && labels != nil && !slices.ContainsFunc(labels, func(label map[string]any) bool {
			name, ok := label["name"].(string)
			return !ok || name == ""
		}) {
			return labels, true
		}
	}
	return nil, false
}
