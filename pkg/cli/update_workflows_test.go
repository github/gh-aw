//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockWorkflowUpdateDeps(mockFn func(context.Context, string) ([]byte, error)) workflowUpdateDeps {
	deps := defaultWorkflowUpdateDeps()
	deps.runReleasesAPI = mockFn
	return deps
}

func TestResolveLatestRef_AddedReleaseTracksCompatibleReleases(t *testing.T) {
	t.Parallel()

	spec, err := parseWorkflowSpec("owner/repo/workflows/triage.md@v1.2.0")
	require.NoError(t, err)
	source := buildSourceStringWithCommitSHA(spec, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	sourceSpec, err := parseSourceSpec(source)
	require.NoError(t, err)

	deps := mockWorkflowUpdateDeps(func(_ context.Context, repo string) ([]byte, error) {
		assert.Equal(t, "owner/repo", repo)
		return []byte("v2.0.0\nv1.4.0\nv1.3.0\nv1.2.0"), nil
	})
	deps.getRepoDefaultBranch = func(context.Context, string) (string, error) {
		t.Fatal("a workflow added at a release tag must not track the default branch")
		return "", nil
	}
	deps.getLatestBranchCommit = func(context.Context, string, string) (latestBranchCommitInfo, error) {
		t.Fatal("a workflow added at a release tag must not resolve a branch commit")
		return latestBranchCommitInfo{}, nil
	}

	result, err := resolveLatestRefWithDeps(context.Background(), deps, sourceSpec.Repo, sourceSpec.Ref, false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "v1.4.0", result.Ref)
}

// TestResolveLatestRelease_PrereleaseTagsSkipped verifies that prerelease tags are
// not selected as the upgrade target even when they have a higher base version than
// the latest stable release. Per semver rules, v1.1.0-beta.1 > v1.0.0, so without
// explicit filtering a prerelease could be picked incorrectly.
func TestResolveLatestRelease_PrereleaseTagsSkipped(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v1.1.0-beta.1\nv1.0.0"), nil
	})

	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", true, false, 0)
	require.NoError(t, err, "should not error when stable release exists")
	assert.Equal(t, "v1.0.0", result, "should select latest stable release, not prerelease")
}

// TestResolveLatestRelease_PrereleaseSkippedWhenCurrentVersionInvalid verifies that when
// the current version is not a valid semantic version, the highest stable release by
// semver is returned rather than the first item in the list (which could be a prerelease
// or an older release listed first by the API).
func TestResolveLatestRelease_PrereleaseSkippedWhenCurrentVersionInvalid(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		// Prerelease appears first, and older stable release appears before newer one.
		return []byte("v2.0.0-rc.1\nv1.3.0\nv1.5.0"), nil
	})

	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "not-a-version", true, false, 0)
	require.NoError(t, err, "should not error when stable release exists")
	assert.Equal(t, "v1.5.0", result, "should skip prerelease and return highest stable release by semver")
}

// TestResolveLatestRelease_ErrorWhenOnlyPrereleasesExist verifies that an error is
// returned when the releases list contains only prerelease versions.
func TestResolveLatestRelease_ErrorWhenOnlyPrereleasesExist(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v2.0.0-beta.1\nv1.0.0-rc.1"), nil
	})

	_, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", true, false, 0)
	assert.Error(t, err, "should error when no stable releases exist")
}

// TestResolveLatestRelease_StableReleaseSelected verifies that stable releases are
// correctly selected when there are no prereleases.
func TestResolveLatestRelease_StableReleaseSelected(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v1.2.0\nv1.1.0\nv1.0.0"), nil
	})

	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", false, false, 0)
	require.NoError(t, err, "should not error when stable releases exist")
	assert.Equal(t, "v1.2.0", result, "should select highest compatible stable release")
}

func TestResolveLatestRelease_CRLFOutput(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(context.Context, string) ([]byte, error) {
		return []byte("v1.2.0\r\nv1.1.0\r\nv1.0.0\r\n"), nil
	})
	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", result)
}

// TestResolveLatestRelease_MixedPrereleaseAndStable verifies correct selection when
// releases include both prerelease and stable versions across major versions.
func TestResolveLatestRelease_MixedPrereleaseAndStable(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v2.0.0-alpha.1\nv1.3.0\nv1.2.0-rc.1\nv1.1.0"), nil
	})

	// Without allowMajor, should stay on v1.x and skip prereleases.
	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.1.0", false, false, 0)
	require.NoError(t, err, "should not error when stable v1.x releases exist")
	assert.Equal(t, "v1.3.0", result, "should select latest stable v1.x release, skipping prereleases")
}

// TestResolveLatestRelease_CooldownFallback verifies that when the newest upgrade
// candidate is within the cooldown window the resolver falls back to the next
// older release that has already passed the cooldown period.
func TestResolveLatestRelease_CooldownFallback(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		// v1.3.0 is the newest; v1.2.0 is one step older and has cooled down.
		return []byte("v1.3.0\nv1.2.0\nv1.0.0"), nil
	})
	deps.checkCoolDown = func(_ context.Context, repo, tag string, cd time.Duration) coolDownCheckResult {
		switch tag {
		case "v1.3.0":
			// Too new: published 2 days ago.
			return checkReleaseCoolDownWithDate(repo, tag, time.Now().Add(-2*24*time.Hour), cd)
		case "v1.2.0":
			// Cooled down: published 10 days ago.
			return checkReleaseCoolDownWithDate(repo, tag, time.Now().Add(-10*24*time.Hour), cd)
		default:
			return coolDownCheckResult{}
		}
	}

	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", result, "should fall back to v1.2.0 when v1.3.0 is in cooldown")
}

// TestResolveLatestRelease_CooldownAllInWindow returns currentRef when every
// upgrade candidate is still within the cooldown window.
func TestResolveLatestRelease_CooldownAllInWindow(t *testing.T) {
	t.Parallel()

	deps := mockWorkflowUpdateDeps(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v1.2.0\nv1.1.0\nv1.0.0"), nil
	})
	// All upgrade candidates are too new.
	deps.checkCoolDown = func(_ context.Context, repo, tag string, cd time.Duration) coolDownCheckResult {
		return checkReleaseCoolDownWithDate(repo, tag, time.Now().Add(-1*24*time.Hour), cd)
	}

	result, err := resolveLatestReleaseWithDeps(context.Background(), deps, "owner/repo", "v1.0.0", false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", result, "should return currentRef when all upgrade candidates are in cooldown")
}

func TestResolveLatestRef_MajorTagReleaseCooldown(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		fallback    bool
		wantRef     string
		wantBlocked bool
	}{
		{name: "all candidates blocked", wantRef: "v1", wantBlocked: true},
		{name: "eligible fallback", fallback: true, wantRef: "v1.1.0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deps := mockWorkflowUpdateDeps(func(_ context.Context, repo string) ([]byte, error) {
				assert.Equal(t, "owner/repo", repo)
				return []byte("v1.2.0\nv1.1.0"), nil
			})
			var checkedTags []string
			deps.checkCoolDown = func(_ context.Context, repo, tag string, cd time.Duration) coolDownCheckResult {
				assert.Equal(t, "owner/repo", repo)
				assert.Equal(t, 7*24*time.Hour, cd)
				checkedTags = append(checkedTags, tag)
				return coolDownCheckResult{InCoolDown: !tt.fallback || tag != "v1.1.0"}
			}

			result, err := resolveLatestRefWithDeps(context.Background(), deps, "owner/repo", "v1", false, false, 7*24*time.Hour)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRef, result.Ref)
			assert.Equal(t, tt.wantBlocked, result.CoolDownBlocked)
			assert.Equal(t, []string{"v1.2.0", "v1.1.0"}, checkedTags)
		})
	}
}

func TestResolveLatestRef_BranchCommitCooldownReturnsCurrentBranch(t *testing.T) {
	t.Parallel()

	deps := defaultWorkflowUpdateDeps()
	deps.getLatestBranchCommit = func(_ context.Context, repo, branch string) (latestBranchCommitInfo, error) {
		require.Equal(t, "owner/repo", repo)
		require.Equal(t, "main", branch)
		return latestBranchCommitInfo{
			SHA:         "abc123def456abc123def456abc123def456abc1",
			CommittedAt: time.Now().Add(-2 * 24 * time.Hour),
		}, nil
	}

	result, err := resolveLatestRefWithDeps(context.Background(), deps, "owner/repo", "main", false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "main", result.Ref, "branch update should be skipped while latest commit is in cooldown")
	assert.True(t, result.CoolDownBlocked, "branch update should report cooldown block")
}

func TestResolveLatestRef_CommitCooldownReturnsCurrentSHA(t *testing.T) {
	t.Parallel()

	deps := defaultWorkflowUpdateDeps()
	deps.getRepoDefaultBranch = func(_ context.Context, repo string) (string, error) {
		require.Equal(t, "owner/repo", repo)
		return "main", nil
	}
	deps.getLatestBranchCommit = func(_ context.Context, repo, branch string) (latestBranchCommitInfo, error) {
		require.Equal(t, "owner/repo", repo)
		require.Equal(t, "main", branch)
		return latestBranchCommitInfo{
			SHA:         "abc123def456abc123def456abc123def456abc1",
			CommittedAt: time.Now().Add(-1 * 24 * time.Hour),
		}, nil
	}

	currentSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	result, err := resolveLatestRefWithDeps(context.Background(), deps, "owner/repo", currentSHA, false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, currentSHA, result.Ref, "commit-pinned update should be skipped while latest default-branch commit is in cooldown")
	assert.True(t, result.CoolDownBlocked, "commit-pinned update should report cooldown block")
}

func TestResolveLatestRef_ExemptRepoBypassesBranchCommitCooldown(t *testing.T) {
	t.Parallel()

	deps := defaultWorkflowUpdateDeps()
	deps.getLatestBranchCommit = func(_ context.Context, repo, branch string) (latestBranchCommitInfo, error) {
		require.Equal(t, "actions/checkout", repo)
		require.Equal(t, "main", branch)
		return latestBranchCommitInfo{
			SHA:         "abc123def456abc123def456abc123def456abc1",
			CommittedAt: time.Now().Add(-1 * time.Hour),
		}, nil
	}

	result, err := resolveLatestRefWithDeps(context.Background(), deps, "actions/checkout", "main", false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "abc123def456abc123def456abc123def456abc1", result.Ref)
	assert.False(t, result.CoolDownBlocked)
}

func TestResolveLatestRef_ExemptRepoBypassesDefaultBranchCommitCooldown(t *testing.T) {
	t.Parallel()

	deps := defaultWorkflowUpdateDeps()
	deps.getRepoDefaultBranch = func(_ context.Context, repo string) (string, error) {
		require.Equal(t, "github/codeql-action", repo)
		return "main", nil
	}
	deps.getLatestBranchCommit = func(_ context.Context, repo, branch string) (latestBranchCommitInfo, error) {
		require.Equal(t, "github/codeql-action", repo)
		require.Equal(t, "main", branch)
		return latestBranchCommitInfo{
			SHA:         "abc123def456abc123def456abc123def456abc1",
			CommittedAt: time.Now().Add(-1 * time.Hour),
		}, nil
	}

	currentSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	result, err := resolveLatestRefWithDeps(context.Background(), deps, "github/codeql-action", currentSHA, false, false, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "abc123def456abc123def456abc123def456abc1", result.Ref)
	assert.False(t, result.CoolDownBlocked)
}

func TestParseLatestBranchCommitInfo_MissingCommitterDateIsAllowed(t *testing.T) {
	t.Parallel()

	data := []byte(`{
	  "sha":"abc123def456abc123def456abc123def456abc1",
	  "commit":{
	    "committer":{"date":""},
	    "author":{"date":"2026-07-20T10:00:00Z"}
	  }
	}`)
	info, err := parseLatestBranchCommitInfo("main", data)
	require.NoError(t, err)
	assert.Equal(t, "abc123def456abc123def456abc123def456abc1", info.SHA)
	assert.True(t, info.CommittedAt.IsZero())
}
