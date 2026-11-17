package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func installOutcomeAPIFixture(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("GH_AW_TEST_OUTCOME_CALLS", calls)
	t.Setenv("GH_AW_ADOPTION_DIR", "")
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"),
		[]byte("#!/bin/sh\nprintf '%s\\n' \"$2\" >> \"$GH_AW_TEST_OUTCOME_CALLS\"\n"+script), 0o700))
	return calls
}

func outcomeAPICalls(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Fields(string(content))
}

func TestLogsOutcomeAPIRequestReuse(t *testing.T) {
	response := filepath.Join(t.TempDir(), "pull.json")
	t.Setenv("GH_AW_TEST_OUTCOME_RESPONSE", response)
	calls := installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/pulls/42) cat "$GH_AW_TEST_OUTCOME_RESPONSE" ;;
repos/acme/repo/collaborators/alice/permission) printf '%s' '{"permission":"write"}' ;;
repos/acme/repo/issues/42/comments) printf '%s' '[[{"user":{"login":"alice"},"created_at":"2026-10-02T00:00:00Z"}]]' ;;
repos/acme/repo/pulls/42/reviews|repos/acme/repo/pulls/42/commits) printf '%s' '[[]]' ;;
graphql) printf '%s' '{"data":{"repository":{"pullRequest":{"id":"PR42","closingIssuesReferences":{"nodes":[]}}}}}' ;;
*) printf 'unexpected endpoint: %s\n' "$2" >&2; exit 1 ;;
esac
`)
	require.NoError(t, os.WriteFile(response, []byte(`{"html_url":"https://github.com/acme/repo/pull/42","state":"closed","merged":true,"merged_at":"2026-10-04T00:00:00Z","merged_by":{"login":"alice","type":"User"},"labels":[]}`), 0o600))
	var runs []ProcessedRun
	for i, timestamp := range []string{"2026-10-01T00:00:00Z", "2026-10-03T00:00:00Z"} {
		dir := t.TempDir()
		manifest := `{"type":"create_pull_request","number":42,"timestamp":"` + timestamp + `","url":"https://github.com/acme/repo/pull/42"}` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
		runs = append(runs, ProcessedRun{Run: WorkflowRun{Attempt: 1, DatabaseID: int64(i + 1), Repository: "acme/repo", LogsPath: dir}})
	}
	reports, err := collectLogsLifecycles(context.Background(), runs, "", false, true)
	require.NoError(t, err)
	require.Len(t, reports, 2)
	for _, report := range reports {
		require.Len(t, report.Items, 1)
		assert.Equal(t, OutcomeStatusAccepted, report.Items[0].OutcomeStatus)
	}
	assert.Equal(t, 1, reports[0].Items[0].HumanComments)
	assert.False(t, reports[0].Items[0].ZeroTouch)
	assert.Zero(t, reports[1].Items[0].HumanComments)
	assert.True(t, reports[1].Items[0].ZeroTouch)
	assert.Len(t, outcomeAPICalls(t, calls), 5, "resource, actor permission, comments, reviews, and commits are each read once")

	require.NoError(t, os.WriteFile(response, []byte(`{"html_url":"https://github.com/acme/repo/pull/42","state":"closed","merged":false,"closed_at":"2026-10-05T00:00:00Z","closed_by":{"login":"alice","type":"User"},"labels":[]}`), 0o600))
	reports, err = collectLogsLifecycles(context.Background(), runs, "", false, true)
	require.NoError(t, err)
	assert.Equal(t, OutcomeStatusRejected, reports[0].Items[0].OutcomeStatus)
	assert.Len(t, outcomeAPICalls(t, calls), 10, "later collections must check current state again")
}

func TestOutcomeAPICacheIsolation(t *testing.T) {
	calls := installOutcomeAPIFixture(t, "printf '%s' '{\"state\":\"open\",\"user\":{\"login\":\"alice\"}}'\n")
	ctx := withOutcomeAPICache(context.Background())
	first, err := ghAPIGet(ctx, "issues/1", "acme/repo")
	require.NoError(t, err)
	first["state"] = "closed"
	first["user"].(map[string]any)["login"] = "changed"
	second, err := ghAPIGet(ctx, "issues/1", "github.com/acme/repo")
	require.NoError(t, err)
	assert.Equal(t, "open", second["state"])
	assert.Equal(t, "alice", second["user"].(map[string]any)["login"])
	assert.Len(t, outcomeAPICalls(t, calls), 1)
	for _, request := range []struct{ endpoint, repo string }{
		{"issues/2", "acme/repo"}, {"issues/1", "acme/other"}, {"issues/1", "github.example.com/acme/repo"},
	} {
		_, err := ghAPIGet(ctx, request.endpoint, request.repo)
		require.NoError(t, err)
	}
	assert.Len(t, outcomeAPICalls(t, calls), 4, "different objects, repositories, and hosts cannot share responses")
}

func TestLogsOutcomeCompletedIssueRequestCount(t *testing.T) {
	calls := installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/issues/42) printf '%s' '{"html_url":"https://github.com/acme/repo/issues/42","state":"closed","state_reason":"completed","closed_at":"2026-10-02T00:00:00Z","closed_by":{"login":"alice","type":"User"},"labels":[]}' ;;
repos/acme/repo/collaborators/alice/permission) printf '%s' '{"permission":"write"}' ;;
*) printf 'unexpected endpoint: %s\n' "$2" >&2; exit 1 ;;
esac
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename),
		[]byte("{\"type\":\"create_issue\",\"number\":42,\"timestamp\":\"2026-10-01T00:00:00Z\"}\n"), 0o600))
	reports, err := collectLogsLifecycles(context.Background(), []ProcessedRun{{
		Run: WorkflowRun{Attempt: 1, DatabaseID: 1, Repository: "acme/repo", LogsPath: dir},
	}}, "", false, true)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, 1, reports[0].Summary.Accepted)
	assert.Len(t, outcomeAPICalls(t, calls), 2, "adoption requires the resource and the decision actor's permission")
}

func TestOutcomeAPICacheGraphQLKeys(t *testing.T) {
	calls := installOutcomeAPIFixture(t, "printf '%s' '{\"data\":{\"ok\":true}}'\n")
	ctx := withOutcomeAPICache(context.Background())
	for _, variables := range []map[string]any{
		{"owner": "acme", "number": 1}, {"number": 1, "owner": "acme"}, {"owner": "acme", "number": 2},
	} {
		_, err := ghAPIGraphQL(ctx, "query Test", variables, "acme/repo")
		require.NoError(t, err)
	}
	_, err := ghAPIGraphQL(ctx, "query Other", map[string]any{"number": 1, "owner": "acme"}, "acme/repo")
	require.NoError(t, err)
	_, err = ghAPIGraphQL(ctx, "query Test", map[string]any{"number": 1, "owner": "acme"}, "github.example.com/acme/repo")
	require.NoError(t, err)
	assert.Len(t, outcomeAPICalls(t, calls), 4)
}

func TestOutcomeAPICachedLabels(t *testing.T) {
	for _, tc := range []struct {
		name, labels string
		calls        int
	}{
		{name: "complete", labels: `,"labels":[{"name":"priority"}]`, calls: 1},
		{name: "empty", labels: `,"labels":[]`, calls: 1},
		{name: "omitted", calls: 2},
		{name: "null", labels: `,"labels":null`, calls: 2},
		{name: "malformed", labels: `,"labels":[1]`, calls: 2},
		{name: "null label", labels: `,"labels":[null]`, calls: 2},
		{name: "incomplete label", labels: `,"labels":[{}]`, calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/issues/42) printf '%s' '{"state":"open"`+tc.labels+`}' ;;
repos/acme/repo/issues/42/labels) printf '%s' '[[{"name":"priority"}]]' ;;
*) exit 1 ;;
esac
`)
			ctx := withOutcomeAPICache(context.Background())
			_, err := ghAPIGet(ctx, "issues/42", "acme/repo")
			require.NoError(t, err)
			labels, err := ghAPIGetArray(ctx, "issues/42/labels", "acme/repo")
			require.NoError(t, err)
			if tc.name != "empty" {
				require.Len(t, labels, 1)
				labels[0]["name"] = "changed"
				again, err := ghAPIGetArray(ctx, "issues/42/labels", "acme/repo")
				require.NoError(t, err)
				assert.Equal(t, "priority", again[0]["name"])
			}
			assert.Len(t, outcomeAPICalls(t, calls), tc.calls)
		})
	}
}

func TestOutcomeAPICachedPagination(t *testing.T) {
	calls := installOutcomeAPIFixture(t, `case "$*" in
*"--method GET -f per_page=100 --paginate --slurp"*) printf '%s' '[[{"id":1}],[{"id":2}]]' ;;
*) exit 1 ;;
esac
`)
	ctx := withOutcomeAPICache(context.Background())
	for range 2 {
		items, err := ghAPIGetArray(ctx, "issues/1/comments", "acme/repo")
		require.NoError(t, err)
		require.Len(t, items, 2)
		assert.EqualValues(t, 1, items[0]["id"])
		assert.EqualValues(t, 2, items[1]["id"])
		items[0]["id"] = 99
	}
	assert.Len(t, outcomeAPICalls(t, calls), 1)
	_, err := ghAPIGet(ctx, "issues/1/comments", "acme/repo")
	require.Error(t, err)
	assert.Len(t, outcomeAPICalls(t, calls), 2, "non-paginated requests must not reuse paginated payloads")
}

func TestOutcomeAPICacheRejectsInvalidResponses(t *testing.T) {
	response := filepath.Join(t.TempDir(), "response")
	t.Setenv("GH_AW_TEST_OUTCOME_RESPONSE", response)
	calls := installOutcomeAPIFixture(t, "cat \"$GH_AW_TEST_OUTCOME_RESPONSE\"\n")
	ctx := withOutcomeAPICache(context.Background())
	for _, body := range []string{"{", "[]"} {
		require.NoError(t, os.WriteFile(response, []byte(body), 0o600))
		_, err := ghAPIGet(ctx, "issues/1", "acme/repo")
		require.Error(t, err)
	}
	require.NoError(t, os.WriteFile(response, []byte(`{"state":"open"}`), 0o600))
	_, err := ghAPIGet(ctx, "issues/1", "acme/repo")
	require.NoError(t, err)
	_, err = ghAPIGet(ctx, "issues/1", "acme/repo")
	require.NoError(t, err)
	assert.Len(t, outcomeAPICalls(t, calls), 3)
}

func TestOutcomeAPICacheFailuresAndPartialResponses(t *testing.T) {
	t.Parallel()
	for _, partial := range []string{`{"data":null,"errors":[{"message":"rate limited"}]}`, "null"} {
		cache := &outcomeAPICache{responses: make(map[string][]byte)}
		count := 0
		fetch := func() ([]byte, error) {
			count++
			switch count {
			case 1:
				return nil, errors.New("temporary API failure")
			case 2:
				return []byte(partial), nil
			default:
				return []byte(`{"ok":true}`), nil
			}
		}
		_, err := cache.load(context.Background(), "request", fetch)
		require.Error(t, err)
		for range 3 {
			_, err := cache.load(context.Background(), "request", fetch)
			require.NoError(t, err)
		}
		assert.Equal(t, 3, count, "only the successful complete response is retained")
	}
}

func TestOutcomeAPICacheConcurrentRequests(t *testing.T) {
	t.Parallel()
	cache := &outcomeAPICache{responses: make(map[string][]byte)}
	var calls atomic.Int32
	var group sync.WaitGroup
	type response struct {
		output []byte
		err    error
	}
	responses := make(chan response, 20)
	for range 20 {
		group.Go(func() {
			output, err := cache.load(context.Background(), "request", func() ([]byte, error) {
				calls.Add(1)
				return []byte(`{"ok":true}`), nil
			})
			responses <- response{output: output, err: err}
		})
	}
	group.Wait()
	close(responses)
	for response := range responses {
		require.NoError(t, response.err)
		assert.Equal(t, `{"ok":true}`, string(response.output))
	}
	assert.EqualValues(t, 1, calls.Load())
}

func TestOutcomeAPICacheCancellation(t *testing.T) {
	t.Parallel()
	cache := &outcomeAPICache{responses: make(map[string][]byte)}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := cache.load(context.Background(), "request", func() ([]byte, error) {
			close(started)
			<-release
			return []byte(`{"ok":true}`), nil
		})
		done <- err
	}()
	<-started
	waiting, cancelWaiting := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWaiting()
	_, err := cache.load(waiting, "request", func() ([]byte, error) {
		t.Error("waiting on the same request must not fetch again")
		return nil, errors.New("duplicate request")
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cache.load(ctx, "request", func() ([]byte, error) {
		t.Error("canceled request must not fetch")
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	close(release)
	require.NoError(t, <-done)
	_, err = cache.load(ctx, "request", nil)
	require.ErrorIs(t, err, context.Canceled, "a cache hit cannot bypass cancellation")
}
