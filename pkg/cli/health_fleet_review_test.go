//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthFleetReviewPinnedSnapshotAndRegistry(t *testing.T) {
	old := healthRunGH
	t.Cleanup(func() { healthRunGH = old })
	sha := strings.Repeat("a", 40)
	blob := strings.Repeat("b", 40)
	pinned := ".github/workflows/pinned.lock.yml"
	source := ".github/workflows/pinned.md"
	added := ".github/workflows/added.lock.yml"
	missing := false
	disabled := false
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", scanBranch: "main",
		pinnedBranchSHA: sha, localGitChecked: true, APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		u, err := url.Parse(args[3])
		require.NoError(t, err)
		switch {
		case strings.HasSuffix(u.Path, "/contents/.github/workflows"):
			require.Equal(t, sha, u.Query().Get("ref"))
			return []byte(fmt.Sprintf(`[{"path":%q,"sha":%q,"type":"file"},{"path":%q,"sha":%q,"type":"file"}]`, pinned, blob, source, blob)), nil
		case strings.Contains(u.Path, "/git/blobs/"):
			return []byte(testHealthLockHeader), nil
		case strings.HasSuffix(u.Path, "/actions/workflows"):
			if missing {
				return []byte(`{"workflows":[]}`), nil
			}
			if disabled {
				return []byte(fmt.Sprintf(`{"workflows":[{"path":%q,"state":"disabled_manually"}]}`, pinned)), nil
			}
			return []byte(fmt.Sprintf(`{"workflows":[{"path":%q,"state":"active"},{"path":%q,"state":"active"}]}`, pinned, added)), nil
		default:
			t.Fatalf("unexpected request %s", args[3])
			return nil, nil
		}
	}
	require.True(t, useHealthFileSnapshots(collection))
	run := WorkflowRun{WorkflowPath: pinned, HeadBranch: "main", HeadSha: sha, Event: "schedule"}
	resolveHealthSourceRevision(run, "main", collection)
	require.Equal(t, blob, collection.sourceRevisions[healthSourceRevisionKey(run, "main")])
	metadata, err := readHealthLockMetadata(run, "main", collection)
	require.NoError(t, err)
	require.NotNil(t, metadata)
	state := &healthFleetState{Scope: healthFleetScope{Branch: "main", HeadSHA: sha}, RegistryPage: 1, Responses: map[string][]byte{}}
	require.NoError(t, collectHealthFleetRegistry(state, collection))
	require.True(t, state.RegistryDrift)
	require.Len(t, state.Workflows, 1)
	require.Equal(t, pinned, state.Workflows[0].Path)
	missing = true
	state = &healthFleetState{Scope: healthFleetScope{Branch: "main", HeadSHA: sha}, RegistryPage: 1, Responses: map[string][]byte{}}
	require.NoError(t, collectHealthFleetRegistry(state, collection))
	require.True(t, state.RegistryDrift)
	require.Empty(t, state.Workflows)
	missing, disabled = false, true
	state = &healthFleetState{Scope: healthFleetScope{Branch: "main", HeadSHA: sha}, RegistryPage: 1, Responses: map[string][]byte{}}
	require.NoError(t, collectHealthFleetRegistry(state, collection))
	require.False(t, state.RegistryDrift)
	require.Empty(t, state.Workflows)
}

func TestHealthFleetReviewRepositoryHosts(t *testing.T) {
	for _, host := range []string{"github.com", "ghe", "ghe.example.com:8443", "host:80"} {
		require.True(t, validHealthFleetRepository(host, "org/repo"), host)
	}
	for _, host := range []string{"-ghe", "ghe-", "a..b", "host:abc", "host:12:34", "host/path"} {
		require.False(t, validHealthFleetRepository(host, "org/repo"), host)
	}
}

func TestHealthFleetReviewCheckpointErrorAndEnterpriseResume(t *testing.T) {
	oldGH, oldRate := healthRunGH, fetchRateLimitForHostFunc
	t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = oldGH, oldRate })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Remaining: 5000}, nil
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	checkpoint := filepath.Join(t.TempDir(), "health.json")
	logs := filepath.Join(t.TempDir(), "logs")
	sha := strings.Repeat("a", 40)
	inventoryCalls := 0
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.Equal(t, "ghe:8443", args[2])
		switch {
		case args[3] == "repos/org/repo":
			return []byte(`{"full_name":"org/repo","default_branch":"main"}`), nil
		case strings.Contains(args[3], "/git/ref/heads/"):
			return []byte(sha), nil
		case strings.Contains(args[3], "/contents/.github/workflows"):
			return []byte(`[]`), nil
		case strings.Contains(args[3], "/actions/workflows"):
			return []byte(`{"workflows":[]}`), nil
		case strings.Contains(args[3], "/actions/runs"):
			inventoryCalls++
			if inventoryCalls == 1 {
				return nil, errors.New("temporary inventory failure")
			}
			if inventoryCalls == 2 {
				return []byte(`{"total_count":1}`), nil
			}
			return []byte(`{"total_count":0,"workflow_runs":[]}`), nil
		default:
			t.Fatalf("unexpected endpoint: %s", args[3])
			return nil, nil
		}
	}
	config := HealthConfig{RepoOverride: "ghe:8443/org/repo", Checkpoint: checkpoint, LogsDir: logs, Threshold: 80, JSONOutput: true}
	require.ErrorContains(t, runHealthFleet(context.Background(), config, start, end, time.Now()), "temporary inventory failure")
	saved, err := loadHealthFleetCheckpoint(config, "ghe:8443", "org/repo", start, end)
	require.NoError(t, err)
	require.Equal(t, sha, saved.Scope.HeadSHA)
	require.Zero(t, saved.RegistryPage)
	require.Equal(t, logs, saved.LogsDir)
	for key := range saved.Responses {
		require.NotContains(t, key, "/actions/runs")
	}
	_, err = loadHealthFleetCheckpoint(config, "other", "org/repo", start, end)
	require.ErrorContains(t, err, "does not match")
	config.RepoOverride = ""
	config.LogsDir = filepath.Join(t.TempDir(), "different")
	config.inputLogsDir = true
	require.ErrorContains(t, runHealthFleet(context.Background(), config, start, end, time.Now()), "logs directory does not match")
	cmd := NewHealthCommand()
	cmd.SetArgs([]string{"--checkpoint", checkpoint, "--logs-dir", config.LogsDir})
	require.ErrorContains(t, cmd.Execute(), "logs directory does not match")
	config.inputLogsDir = false
	require.ErrorContains(t, runHealthFleet(context.Background(), config, start, end, time.Now()), "run response")
	saved, err = loadHealthFleetCheckpoint(config, "github.com", "", start, end)
	require.NoError(t, err)
	for key := range saved.Responses {
		require.NotContains(t, key, "/actions/runs")
	}
	captureAuditGroupStdout(t, func() {
		require.NoError(t, runHealthFleet(context.Background(), config, start, end, time.Now()))
	})
	require.Equal(t, 3, inventoryCalls)
	saved, err = loadHealthFleetCheckpoint(config, "github.com", "", start, end)
	require.NoError(t, err)
	require.Equal(t, logs, saved.LogsDir)
}

func TestHealthFleetReviewInvalidCacheEviction(t *testing.T) {
	responses := map[string][]byte{
		"repos/org/repo/actions/runs/1/jobs?per_page=100&page=1":                             []byte(`{`),
		"repos/org/repo/actions/runs/2/jobs?per_page=1":                                      []byte(`{}`),
		"repos/org/repo/actions/runs/3/jobs?per_page=1":                                      []byte(`{"jobs":[]}`),
		"repos/org/repo/actions/runs/4/jobs?per_page=100":                                    []byte(`{"jobs":[1]}`),
		"repos/org/repo/git/blobs/a":                                                         []byte("not json"),
		"repos/org/repo/git/ref/heads/main\x00--jq\x00.object.sha":                           []byte("invalid"),
		"repos/org/repo/contents/test.md?ref=main\x00--jq\x00.sha":                           []byte("invalid"),
		"repos/org/repo/contents/.github/workflows?ref=main\x00--jq\x00map({path,sha,type})": []byte(`{}`),
		"repos/org/repo/git/blobs/b\x00--jq\x00.content | @base64d":                          []byte("bad metadata"),
		"repos/org/repo/git/blobs/c\x00--jq\x00.content | @base64d":                          []byte(testHealthLockHeader),
	}
	evictInvalidHealthFleetResponses(responses)
	require.Len(t, responses, 3)
}

func TestHealthFleetReviewInvalidSnapshotIsNotPersisted(t *testing.T) {
	old := healthRunGH
	t.Cleanup(func() { healthRunGH = old })
	responses := make(map[string][]byte)
	calls := 0
	healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{}`), nil
		}
		return []byte(`[]`), nil
	}
	newCollection := func() *HealthCollection {
		return &HealthCollection{Host: "github.com", repoOverride: "org/repo", scanBranch: "main",
			pinnedBranchSHA: strings.Repeat("a", 40), fleetResponses: responses,
			APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	}
	require.Error(t, healthWorkflowFiles("main", newCollection()).err)
	require.Empty(t, responses)
	require.NoError(t, healthWorkflowFiles("main", newCollection()).err)
	require.Equal(t, 2, calls)
}

func TestHealthFleetReviewEvictsPayloadWhenBudgetStopsValidation(t *testing.T) {
	old := healthRunGH
	t.Cleanup(func() { healthRunGH = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses := make(map[string][]byte)
	collection := &HealthCollection{ctx: ctx, Host: "github.com", repoOverride: "org/repo",
		pinnedBranchSHA: strings.Repeat("a", 40), fleetResponses: responses,
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
		cancel()
		return []byte("invalid lock"), nil
	}
	_, err := readHealthLockContent("repos/org/repo/git/blobs/"+strings.Repeat("b", 40), strings.Repeat("b", 40), collection)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, responses, 1)
	evictInvalidHealthFleetResponses(responses)
	require.Empty(t, responses)
}

func TestHealthFleetReviewIncompleteJobsRetryRetainsVerdict(t *testing.T) {
	old := healthRunGH
	t.Cleanup(func() { healthRunGH = old })
	target := GitHubWorkflow{Name: "test", Path: ".github/workflows/test.lock.yml"}
	run := WorkflowRun{DatabaseID: 1, WorkflowPath: target.Path, Event: "workflow_run", Status: "completed", Conclusion: "failure"}
	state := &healthFleetState{Workflows: []GitHubWorkflow{target}, Runs: map[int64]WorkflowRun{1: run},
		Verified: map[string]healthFleetWorkflowEvidence{}, Responses: map[string][]byte{}}
	failJobs, failPresence := true, false
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(args[3], "per_page=100") && failJobs {
			return nil, errors.New("temporary failure")
		}
		if strings.Contains(args[3], "per_page=1") && failPresence {
			return nil, errors.New("temporary presence failure")
		}
		return []byte(`{"total_count":1,"jobs":[{"name":"agent"}]}`), nil
	}
	newConfig := func() HealthConfig {
		return HealthConfig{Threshold: 80, LogsDir: t.TempDir(), collection: &HealthCollection{
			Host: "github.com", repoOverride: "org/repo", fleetResponses: state.Responses,
			APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}}
	}
	require.NoError(t, verifyHealthFleet(state, newConfig()))
	previous := state.Verified[target.Path]
	require.True(t, previous.IncompleteJobs)
	failPresence = true
	clear(state.Responses)
	require.Error(t, verifyHealthFleet(state, newConfig()))
	require.Equal(t, previous, state.Verified[target.Path])
	failJobs, failPresence = false, false
	clear(state.Responses)
	require.NoError(t, verifyHealthFleet(state, newConfig()))
	require.False(t, state.Verified[target.Path].IncompleteJobs)
}
