//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const healthSnapshotMetadata = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"settings","body_hash":"prompt"}`

func TestHealthDirectoryAndBlobReuseAcrossWorkflowsAndCommits(t *testing.T) {
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", localGitChecked: true,
		APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
		workflows:    []GitHubWorkflow{{Path: ".github/workflows/a.lock.yml"}, {Path: ".github/workflows/b.lock.yml"}}}
	requests := make(map[string]int)
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		endpoint := args[3]
		requests[endpoint]++
		if strings.Contains(endpoint, "/git/blobs/") {
			return []byte(healthSnapshotMetadata), nil
		}
		require.Contains(t, endpoint, "/contents/.github/workflows?")
		var entries []map[string]string
		for _, name := range []string{"a", "b"} {
			entries = append(entries,
				map[string]string{"path": ".github/workflows/" + name + ".md", "sha": strings.Repeat("c", 40), "type": "file"},
				map[string]string{"path": ".github/workflows/" + name + ".lock.yml", "sha": strings.Repeat("d", 40), "type": "file"})
		}
		out, err := json.Marshal(entries)
		require.NoError(t, err)
		return out, nil
	}
	for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		for _, target := range collection.workflows {
			run := WorkflowRun{WorkflowPath: target.Path, HeadSha: sha, HeadBranch: "main", Event: "schedule", Repository: "org/repo"}
			resolveHealthRevision(run, collection)
			require.NotNil(t, collection.revisions[healthRevisionLookupKey(run)])
			assert.False(t, healthSourceChanged(run, collection))
		}
	}
	assert.Equal(t, 4, collection.APIRequests, "three directory snapshots plus one shared lock blob, not per-file/run reads")
	assert.Equal(t, 4, collection.RevisionLookups)
	assert.Len(t, requests, 4)
	for _, count := range requests {
		assert.Equal(t, 1, count)
	}
	assert.Empty(t, collection.Warning)
}

func TestHealthSnapshotFailuresRemainUnknownAndAreNotRetried(t *testing.T) {
	for _, response := range []string{`null`, `{}`, `[{"path":"../unsafe.md","sha":"x","type":"file"}]`, `[]`} {
		t.Run(response, func(t *testing.T) {
			original := healthRunGH
			t.Cleanup(func() { healthRunGH = original })
			collection := &HealthCollection{Host: "github.com", localGitChecked: true,
				APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000},
				workflows:    []GitHubWorkflow{{Path: "a"}, {Path: "b"}}}
			healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(response), nil
			}
			run := WorkflowRun{WorkflowPath: ".github/workflows/example.lock.yml", HeadSha: strings.Repeat("a", 40),
				HeadBranch: "main", Event: "schedule", Status: "completed", Conclusion: "failure"}
			resolveHealthRevision(run, collection)
			assert.Contains(t, collection.Warning, "unavailable")
			assert.False(t, healthSourceChanged(run, collection))
			assert.Nil(t, collection.revisions[healthRevisionLookupKey(run)])
			requests := collection.APIRequests
			_, err := readHealthLockMetadata(run, run.HeadBranch, collection)
			require.Error(t, err)
			assert.Equal(t, requests, collection.APIRequests, "failed snapshots must not fan out into per-file retries")
		})
	}
}

func createHealthGitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, initTestGitRepo(root))
	require.NoError(t, addOriginRemoteToTestRepo(root, "https://github.com/org/repo.git"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github/workflows"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.md"), []byte("Original prompt"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.lock.yml"), []byte(healthSnapshotMetadata), 0644))
	_, err := runHealthGitContext(context.Background(), root, "add", ".github/workflows")
	require.NoError(t, err)
	_, err = runHealthGitContext(context.Background(), root, "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	require.NoError(t, err)
	out, err := runHealthGitContext(context.Background(), root, "rev-parse", "HEAD")
	require.NoError(t, err)
	return root, strings.TrimSpace(string(out))
}

func TestHealthLocalGitUsesCommittedObjectsAndFreshPushedHead(t *testing.T) {
	root, sha := createHealthGitFixture(t)
	t.Chdir(root)
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.md"), []byte("Unsaved local fix"), 0644))
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.Equal(t, "repos/org/repo/git/ref/heads/main", args[3], "only pushed branch freshness needs the API")
		return []byte(sha), nil
	}
	run := WorkflowRun{WorkflowPath: ".github/workflows/example.lock.yml", HeadSha: sha, HeadBranch: "main", Event: "schedule"}
	resolveHealthRevision(run, collection)
	require.NotNil(t, collection.localGit)
	require.NotNil(t, collection.revisions[healthRevisionLookupKey(run)])
	assert.False(t, healthSourceChanged(run, collection), "dirty worktree edits are not pushed changes")
	assert.Equal(t, 1, collection.APIRequests)
	assert.Zero(t, collection.RevisionLookups)
	assert.Equal(t, 2, collection.LocalRevisionReads)
	assert.Empty(t, collection.Warning)
	// A newer pushed source must still be noticed even with unchanged lock metadata.
	_, err := runHealthGitContext(context.Background(), root, "add", ".github/workflows/example.md")
	require.NoError(t, err)
	_, err = runHealthGitContext(context.Background(), root, "-c", "commit.gpgsign=false", "commit", "-qm", "pushed source")
	require.NoError(t, err)
	newHead, err := runHealthGitContext(context.Background(), root, "rev-parse", "HEAD")
	require.NoError(t, err)
	healthRunGH = func(context.Context, string, ...string) ([]byte, error) { return newHead, nil }
	fresh := &HealthCollection{Host: "github.com", repoOverride: "org/repo", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	resolveHealthRevision(run, fresh)
	assert.True(t, healthSourceChanged(run, fresh))
	health := calculateCurrentWorkflowHealth("Example", []WorkflowRun{run}, HealthConfig{Threshold: 80, collection: fresh})
	assert.Equal(t, "no_current_revision_runs", health.Status)
	assert.Equal(t, 1, fresh.APIRequests)
}

func TestHealthMissingLocalObjectsFallBackWithoutFetching(t *testing.T) {
	root, _ := createHealthGitFixture(t)
	t.Chdir(root)
	original := healthRunGH
	t.Cleanup(func() { healthRunGH = original })
	sha := strings.Repeat("e", 40)
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		endpoint, err := url.Parse(args[3])
		require.NoError(t, err)
		if strings.Contains(endpoint.Path, "/git/ref/heads/") {
			return []byte(sha), nil
		}
		if strings.Contains(endpoint.Path, "/git/blobs/") {
			return []byte(healthSnapshotMetadata), nil
		}
		require.Equal(t, "repos/org/repo/contents/.github/workflows", endpoint.Path)
		return []byte(`[{"path":".github/workflows/example.md","sha":"cccccccccccccccccccccccccccccccccccccccc","type":"file"},{"path":".github/workflows/example.lock.yml","sha":"dddddddddddddddddddddddddddddddddddddddd","type":"file"}]`), nil
	}
	run := WorkflowRun{WorkflowPath: ".github/workflows/example.lock.yml", HeadSha: sha, HeadBranch: "main", Event: "schedule"}
	resolveHealthRevision(run, collection)
	require.NotNil(t, collection.revisions[healthRevisionLookupKey(run)])
	assert.Equal(t, 3, collection.APIRequests, "fresh head, one reused remote snapshot, one shared blob")
	assert.Zero(t, collection.LocalRevisionReads)
	assert.Empty(t, collection.Warning)
}

func TestHealthLocalGitTargetIdentity(t *testing.T) {
	for _, tc := range []struct {
		remote  string
		host    string
		repo    string
		matches bool
	}{
		{"https://github.com/org/repo.git", "github.com", "org/repo", true},
		{"git@github.com:org/repo.git", "github.com", "org/repo", true},
		{"ssh://git@ghe.example.com/org/repo.git", "ghe.example.com", "org/repo", true},
		{"https://github.com/org/repo.git", "ghe.example.com", "org/repo", false},
		{"https://github.com/org/fork.git", "github.com", "org/repo", false},
		{"/local/repo", "github.com", "org/repo", false},
	} {
		assert.Equal(t, tc.matches, healthRemoteMatches(tc.remote, tc.host, tc.repo), tc.remote)
	}
	root, _ := createHealthGitFixture(t)
	t.Chdir(root)
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/other"}
	enableHealthLocalGit(WorkflowRun{Repository: "org/other"}, collection)
	assert.Nil(t, collection.localGit)
	assert.True(t, collection.localGitChecked)
	assert.Zero(t, collection.APIRequests)
}

func TestHealthLocalGitRejectsUntrustedRevisions(t *testing.T) {
	local := &healthLocalGit{dir: t.TempDir()}
	for _, ref := range []string{"--help", "HEAD", "main:path", "abc", "../other"} {
		_, err := local.workflowFiles(ref)
		require.Error(t, err)
		_, err = local.blob(ref)
		require.Error(t, err)
	}
	_, err := local.blob(strings.Repeat("f", 40))
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
}
