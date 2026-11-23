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
	require.Equal(t, map[string]string{added: "active_registry_only"}, state.RegistryIssues)
	require.Len(t, state.Workflows, 1)
	require.Equal(t, pinned, state.Workflows[0].Path)
	missing = true
	state = &healthFleetState{Scope: healthFleetScope{Branch: "main", HeadSHA: sha}, RegistryPage: 1, Responses: map[string][]byte{}}
	require.NoError(t, collectHealthFleetRegistry(state, collection))
	require.True(t, state.RegistryDrift)
	require.Equal(t, map[string]string{pinned: "missing_registry_entry"}, state.RegistryIssues)
	require.Empty(t, state.Workflows)
	missing, disabled = false, true
	state = &healthFleetState{Scope: healthFleetScope{Branch: "main", HeadSHA: sha}, RegistryPage: 1, Responses: map[string][]byte{}}
	require.NoError(t, collectHealthFleetRegistry(state, collection))
	require.False(t, state.RegistryDrift)
	require.Empty(t, state.Workflows)
}

func TestHealthFleetReviewRegistryOnlyEvidence(t *testing.T) {
	old := healthRunGH
	t.Cleanup(func() { healthRunGH = old })
	const file = ".github/workflows/removed.lock.yml"
	const otherFile = ".github/workflows/other.lock.yml"
	pin, deletion := strings.Repeat("a", 40), strings.Repeat("b", 40)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name        string
		head        string
		date        time.Time
		history     string
		runs        string
		unavailable bool
		resume      bool
		advance     bool
		drift       bool
	}{
		{name: "confirmed old deletion", head: pin, date: start.Add(-time.Second)},
		{name: "deletion within scan", head: pin, date: start, drift: true},
		{name: "branch advanced", head: deletion, date: start.Add(-time.Second), drift: true},
		{name: "branch-only registration", head: pin, history: `[]`, runs: `{"total_count":0,"workflow_runs":[]}`},
		{name: "branch-only proof with changed head", head: deletion, history: `[]`, runs: `{"total_count":0,"workflow_runs":[]}`, drift: true},
		{name: "head advances during page", head: pin, history: `[]`, runs: `{"total_count":0,"workflow_runs":[]}`, advance: true, drift: true},
		{name: "default branch run exists", history: `[]`, runs: `{"total_count":1,"workflow_runs":[{"id":2}]}`, drift: true},
		{name: "incomplete run evidence", history: `[]`, runs: `{"total_count":0}`, drift: true},
		{name: "unavailable run evidence", history: `[]`, drift: true},
		{name: "invalid path history", history: `null`, drift: true},
		{name: "unavailable commit", unavailable: true, drift: true},
		{name: "resume interrupted proof", head: pin, date: start.Add(-time.Second), resume: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := HealthConfig{Checkpoint: filepath.Join(t.TempDir(), "checkpoint.json")}
			state := newHealthFleetState(config, "github.com", "org/repo", start, start.Add(time.Hour))
			state.Scope.Branch, state.Scope.HeadSHA = "main", pin
			collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", scanBranch: "main",
				pinnedBranchSHA: pin, fleetResponses: state.Responses, APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
			head := tt.head
			healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				u, err := url.Parse(args[3])
				require.NoError(t, err)
				switch u.Path {
				case "repos/org/repo/contents/.github/workflows":
					require.Equal(t, pin, u.Query().Get("ref"))
					return []byte(`[]`), nil
				case "repos/org/repo/actions/workflows":
					if tt.advance {
						return []byte(fmt.Sprintf(`{"workflows":[{"id":1,"path":%q,"state":"active"},{"id":2,"path":%q,"state":"active"}]}`, file, otherFile)), nil
					}
					return []byte(fmt.Sprintf(`{"workflows":[{"id":1,"path":%q,"state":"active"}]}`, file)), nil
				case "repos/org/repo/commits":
					require.Equal(t, pin, u.Query().Get("sha"))
					paths := []string{file}
					if tt.advance {
						paths = append(paths, otherFile)
					}
					require.Contains(t, paths, u.Query().Get("path"))
					require.Equal(t, "1", u.Query().Get("per_page"))
					if tt.history != "" {
						return []byte(tt.history), nil
					}
					return []byte(fmt.Sprintf(`[{"sha":%q}]`, deletion)), nil
				case "repos/org/repo/actions/workflows/1/runs", "repos/org/repo/actions/workflows/2/runs":
					if strings.HasSuffix(u.Path, "/2/runs") {
						require.True(t, tt.advance)
						head = deletion
					}
					require.Equal(t, `[]`, tt.history)
					require.Equal(t, "main", u.Query().Get("branch"))
					require.Equal(t, "1", u.Query().Get("per_page"))
					if tt.runs == "" {
						return nil, errors.New("runs unavailable")
					}
					return []byte(tt.runs), nil
				case "repos/org/repo/commits/" + deletion:
					if tt.unavailable {
						return nil, errors.New("commit unavailable")
					}
					return []byte(fmt.Sprintf(`{"sha":%q,"date":%q,"removed":[%q]}`, deletion, tt.date.Format(time.RFC3339), file)), nil
				case "repos/org/repo/git/ref/heads/main":
					return []byte(fmt.Sprintf(`{"sha":%q}`, head)), nil
				default:
					t.Fatalf("unexpected request %s", args[3])
					return nil, nil
				}
			}
			if tt.resume {
				collection.APIRequests = healthMaximumAPIRequests - 2
				collection.checkedRequests = collection.APIRequests
				require.ErrorIs(t, collectHealthFleetRegistry(state, collection), errHealthAPIBudget)
				require.Equal(t, 1, state.RegistryPage)
				require.False(t, state.RegistryDrift, "an interrupted lookup is not resolved drift")
				require.NoError(t, saveHealthFleetCheckpoint(config.Checkpoint, state))
				saved, err := loadHealthFleetCheckpoint(config, "github.com", "org/repo", start, start.Add(time.Hour))
				require.NoError(t, err)
				state = saved
				collection = &HealthCollection{Host: "github.com", repoOverride: "org/repo", scanBranch: "main",
					pinnedBranchSHA: pin, fleetResponses: state.Responses, APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
			}
			require.NoError(t, collectHealthFleetRegistry(state, collection))
			require.Empty(t, state.Workflows, "registry-only entries are never inventoried")
			require.Zero(t, state.RegistryPage)
			for key := range state.Responses {
				require.NotContains(t, key, "/git/ref/heads/")
				require.NotContains(t, key, "/actions/workflows/1/runs")
			}
			require.NoError(t, saveHealthFleetCheckpoint(config.Checkpoint, state))
			saved, err := loadHealthFleetCheckpoint(config, "github.com", "org/repo", start, start.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, pin, saved.Scope.HeadSHA)
			coverage := makeHealthFleetCoverage(saved, collection)
			require.Equal(t, !tt.drift, coverage.RegistryComplete)
			if tt.drift {
				require.Equal(t, "registry_drift", coverage.Reason)
				issues := map[string]string{file: "active_registry_only"}
				if tt.advance {
					issues[otherFile] = "active_registry_only"
				}
				require.Equal(t, issues, coverage.RegistryIssues)
			} else {
				require.Empty(t, coverage.RegistryIssues)
			}
		})
	}
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
