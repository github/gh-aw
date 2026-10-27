//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubRolloutAPI(t *testing.T, run func(context.Context, string, string, ...string) ([]byte, error)) {
	t.Helper()
	original := orgRolloutAPIFn
	orgRolloutAPIFn = run
	t.Cleanup(func() { orgRolloutAPIFn = original })
}

func TestOrgRolloutPRDiscoveryBatches(t *testing.T) {
	calls := 0
	stubRolloutAPI(t, func(_ context.Context, host, _ string, args ...string) ([]byte, error) {
		require.NotEmpty(t, host)
		calls++
		query := strings.Join(args, " ")
		data := map[string]any{"rateLimit": map[string]int{"remaining": 4000}}
		count := strings.Count(query, "repository(owner:")
		for i := range count {
			data[fmt.Sprintf("r%d", i)] = map[string]any{"pullRequests": map[string]any{
				"nodes": []orgRolloutPR{
					{Number: 1, Body: buildOrgXMLMarker(ghawUpgradeMarkerPrefix, "v1.0.0"), URL: "https://github.com/acme/repo/pull/1"},
					{Number: 2, Body: "unrelated"},
					{Number: 3, Body: buildOrgXMLMarker(ghawUpdateMarkerPrefix, "v2.0.0")},
				},
				"pageInfo": map[string]bool{"hasNextPage": false},
			}}
		}
		return json.Marshal(map[string]any{"data": data})
	})
	var repos []string
	for i := range 41 {
		repos = append(repos, fmt.Sprintf("acme/repo%d", i))
	}
	found, err := discoverRolloutPRs(context.Background(), repos, ghawUpgradeMarkerPrefix)
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
	require.Len(t, found, 41)
	for _, prs := range found {
		require.Len(t, prs, 1)
		assert.Equal(t, 1, prs[0].Number)
	}
}

func TestOrgRolloutPRDiscoveryPagination(t *testing.T) {
	calls := 0
	stubRolloutAPI(t, func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		calls++
		if args[0] == "graphql" {
			return []byte(`{"data":{"rateLimit":{"remaining":4000},"r0":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}}`), nil
		}
		if strings.HasSuffix(args[0], "&page=1") {
			items := make([]map[string]any, 100)
			for index := range items {
				items[index] = map[string]any{"number": index + 1, "body": "unrelated"}
			}
			return json.Marshal(items)
		}
		require.Contains(t, args[0], "page=2")
		return []byte(`[{"number":101,"body":"<!-- gh-aw-upgrade: v1 -->","html_url":"https://github.com/acme/repo/pull/101"}]`), nil
	})
	found, err := discoverRolloutPRs(context.Background(), []string{"acme/repo"}, ghawUpgradeMarkerPrefix)
	require.NoError(t, err)
	require.Len(t, found["acme/repo"], 1)
	assert.Equal(t, 101, found["acme/repo"][0].Number)
	assert.Equal(t, 3, calls)
}

func TestOrgRolloutDiscoveryFailsClosed(t *testing.T) {
	for _, response := range []string{
		`{"errors":[{"message":"Resource not accessible"}],"data":{"r0":null}}`,
		`{"data":{"rateLimit":{"remaining":499},"r0":{"pullRequests":{"nodes":[]}}}}`,
		`{"data":{"rateLimit":{"remaining":4000},"r0":null}}`,
		`{"data":null}`,
		`not json`,
	} {
		t.Run(response, func(t *testing.T) {
			stubRolloutAPI(t, func(context.Context, string, string, ...string) ([]byte, error) {
				return []byte(response), nil
			})
			found, err := discoverRolloutPRs(context.Background(), []string{"acme/repo"}, ghawUpdateMarkerPrefix)
			require.Error(t, err)
			assert.Nil(t, found)
		})
	}
}

func TestOrgRolloutEnterprisePagination(t *testing.T) {
	calls := 0
	stubRolloutAPI(t, func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			assert.NotContains(t, strings.Join(args, " "), "after:")
			return []byte(`{"data":{"enterprise":{"organizations":{"nodes":[{"login":"zoo"}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`), nil
		}
		assert.Contains(t, strings.Join(args, " "), `after:"next"`)
		return []byte(`{"data":{"enterprise":{"organizations":{"nodes":[{"login":"acme"},{"login":"zoo"}],"pageInfo":{"hasNextPage":false}}}}}`), nil
	})
	orgs, err := discoverEnterpriseOrgs(context.Background(), "enterprise")
	require.NoError(t, err)
	assert.Equal(t, []string{"acme", "zoo"}, orgs)
	assert.Equal(t, 2, calls)
}

func TestOrgRolloutExistingPRPolicy(t *testing.T) {
	mutations := 0
	stubRolloutAPI(t, func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		if args[0] == "--method" {
			mutations++
			require.Equal(t, "/repos/acme/repo/pulls/42", args[2], "close only captured old PRs")
			return []byte(`{}`), nil
		}
		return []byte(`[{"number":42,"body":"<!-- gh-aw-upgrade: v1 -->","html_url":"https://github.com/acme/repo/pull/42"}]`), nil
	})
	opts := &orgRolloutOptions{worker: true}
	ctx := context.WithValue(context.Background(), orgRolloutKey{}, opts)
	existing, skip, err := prepareOrgRolloutWorker(ctx, "acme/repo", ghawUpgradeMarkerPrefix)
	require.NoError(t, err)
	assert.True(t, skip)
	require.Len(t, existing, 1)
	assert.Zero(t, mutations, "detection and skipping must not close anything")
	opts.replace = true
	existing, skip, err = prepareOrgRolloutWorker(ctx, "acme/repo", ghawUpgradeMarkerPrefix)
	require.NoError(t, err)
	assert.False(t, skip)
	assert.Zero(t, mutations, "old PR remains open until replacement exists")
	require.NoError(t, finishOrgRolloutWorker(ctx, "acme/repo", existing, "https://github.com/acme/repo/pull/43", nil))
	assert.Equal(t, 1, mutations)
}

func TestOrgRolloutReplacementCloseError(t *testing.T) {
	stubRolloutAPI(t, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("HTTP 403")
	})
	ctx := context.WithValue(context.Background(), orgRolloutKey{}, &orgRolloutOptions{worker: true, replace: true})
	err := finishOrgRolloutWorker(ctx, "acme/repo", []orgRolloutPR{{Number: 42}}, "https://github.com/acme/repo/pull/43", nil)
	require.ErrorContains(t, err, "replacement https://github.com/acme/repo/pull/43 created")
	require.ErrorContains(t, err, "could not close old PR #42")
}

func TestOrgRolloutLegacyPRDetection(t *testing.T) {
	assert.True(t, isRolloutPR(orgRolloutPR{Title: "Upgrade agentic workflows", Branch: "upgrade-agentic-workflows-1234"}, ghawUpgradeMarkerPrefix))
	assert.True(t, isRolloutPR(orgRolloutPR{Title: "Update workflows from source", Branch: "update-workflows-1234"}, ghawUpdateMarkerPrefix))
	assert.False(t, isRolloutPR(orgRolloutPR{Title: "Upgrade agentic workflows", Branch: "my-custom-change"}, ghawUpgradeMarkerPrefix))
	assert.False(t, isRolloutPR(orgRolloutPR{Body: ghawUpgradeMarkerPrefix}, ghawUpdateMarkerPrefix))
}

func TestOrgRolloutSearchCeilingFallback(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return nil })
	original := orgSearchAPIFn
	t.Cleanup(func() { orgSearchAPIFn = original })
	orgSearchAPIFn = func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(`{"total_count":1500,"incomplete_results":false,"items":[]}`), nil
	}
	pages := 0
	stubRolloutAPI(t, func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		pages++
		if strings.HasSuffix(args[0], "&page=1") {
			var items []map[string]any
			for index := range 100 {
				items = append(items, map[string]any{
					"full_name": fmt.Sprintf("acme/repo%03d", index),
					"archived":  index == 0, "disabled": index == 1,
				})
			}
			return json.Marshal(items)
		}
		return []byte(`[{"full_name":"acme/tail"}]`), nil
	})
	repos, err := searchOrgWorkflowRepos(context.Background(), "acme", nil, false)
	require.NoError(t, err)
	assert.Len(t, repos, 99)
	assert.Contains(t, repos, "acme/tail")
	assert.NotContains(t, repos, "acme/repo000")
	assert.NotContains(t, repos, "acme/repo001")
	assert.Equal(t, 2, pages)
}

func TestOrgRolloutSearchBudgetErrorDoesNotSearch(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return errors.New("HTTP 403") })
	original := orgSearchAPIFn
	t.Cleanup(func() { orgSearchAPIFn = original })
	orgSearchAPIFn = func(context.Context, string, string, ...string) ([]byte, error) {
		t.Fatal("must not continue after an API budget or authentication failure")
		return nil, nil
	}
	_, err := searchOrgWorkflowRepos(context.Background(), "acme", nil, false)
	require.ErrorContains(t, err, "HTTP 403")
}
