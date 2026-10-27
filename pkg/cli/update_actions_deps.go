package cli

// This file provides the dependency-injection scaffolding and caching layer shared
// by the action update code paths in update_actions_lockfile.go,
// update_actions_release.go, update_actions_workflow_files.go, and
// update_actions_content_refs.go.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// isCoreAction returns true if the repo is a GitHub-maintained core action (actions/* org).
// Core actions are always updated to the latest major version without requiring --major.
func isCoreAction(repo string) bool {
	return strings.HasPrefix(repo, "actions/")
}

// isGhAwNativeAction returns true if the action repo is part of the gh-aw native ecosystem
// (i.e., maintained in the github/gh-aw or github/gh-aw-actions repository). These actions
// are versioned in lock-step with the CLI and must never be updated beyond the running CLI version.
func isGhAwNativeAction(repo string) bool {
	base := gitutil.ExtractBaseRepo(repo)
	return base == "github/gh-aw" || base == "github/gh-aw-actions"
}

type actionUpdateDeps struct {
	getLatestRelease       func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error)
	getLatestReleaseViaGit func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error)
	runGHReleasesAPI       func(ctx context.Context, baseRepo string) ([]byte, error)
	getActionSHAForTag     func(ctx context.Context, repo, tag string) (string, error)
	checkCoolDown          func(ctx context.Context, repo, tag string, coolDown time.Duration) coolDownCheckResult
}

type cachedLatestRelease struct {
	version string
	sha     string
	err     error
}

// newCachedActionUpdateDeps memoizes GitHub reads for the full update command.
// This cache is shared by actions-lock.json updates and Markdown action refs.
func newCachedActionUpdateDeps(base actionUpdateDeps) actionUpdateDeps {
	var mu sync.Mutex
	latestReleases := make(map[string]*syncutil.OnceLoader[cachedLatestRelease])
	releaseLists := make(map[string]*syncutil.OnceLoader[[]byte])
	shas := make(map[string]*syncutil.OnceLoader[string])
	cooldowns := make(map[string]*syncutil.OnceLoader[coolDownCheckResult])

	cached := base
	cached.getLatestRelease = func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		repo = gitutil.ExtractBaseRepo(repo)
		key := fmt.Sprintf("%s|%s|%t", repo, currentVersion, allowMajor)
		result, err := loadActionUpdateResult(&mu, latestReleases, key, func() (cachedLatestRelease, error) {
			var version, sha string
			var err error
			if base.getLatestRelease != nil {
				version, sha, err = base.getLatestRelease(ctx, repo, currentVersion, allowMajor, verbose)
			} else {
				version, sha, err = getLatestActionReleaseWithDeps(ctx, cached, repo, currentVersion, allowMajor, verbose)
			}
			return cachedLatestRelease{version: version, sha: sha}, err
		})
		return result.version, result.sha, err
	}
	cached.runGHReleasesAPI = func(ctx context.Context, repo string) ([]byte, error) {
		return loadActionUpdateResult(&mu, releaseLists, repo, func() ([]byte, error) {
			return base.runGHReleasesAPI(ctx, repo)
		})
	}
	cached.getActionSHAForTag = func(ctx context.Context, repo, tag string) (string, error) {
		repo = gitutil.ExtractBaseRepo(repo)
		key := repo + "|" + tag
		return loadActionUpdateResult(&mu, shas, key, func() (string, error) {
			return base.getActionSHAForTag(ctx, repo, tag)
		})
	}
	cached.checkCoolDown = func(ctx context.Context, repo, tag string, coolDown time.Duration) coolDownCheckResult {
		key := fmt.Sprintf("%s|%s|%s", repo, tag, coolDown)
		result, _ := loadActionUpdateResult(&mu, cooldowns, key, func() (coolDownCheckResult, error) {
			return base.checkCoolDown(ctx, repo, tag, coolDown), nil
		})
		return result
	}
	return cached
}

func loadActionUpdateResult[T any](mu *sync.Mutex, cache map[string]*syncutil.OnceLoader[T], key string, fetch func() (T, error)) (T, error) {
	loader := func() *syncutil.OnceLoader[T] {
		mu.Lock()
		defer mu.Unlock()
		loader := cache[key]
		if loader == nil {
			loader = &syncutil.OnceLoader[T]{}
			cache[key] = loader
		}
		return loader
	}()
	return loader.Get(fetch)
}

func defaultActionUpdateDeps() actionUpdateDeps {
	return newCachedActionUpdateDeps(actionUpdateDeps{
		getLatestReleaseViaGit: getLatestActionReleaseViaGit,
		checkCoolDown:          checkReleaseCoolDown,
		runGHReleasesAPI: func(ctx context.Context, baseRepo string) ([]byte, error) {
			if client := updateGitHubClientFromContext(ctx); client != nil {
				return client.releaseTags(ctx, baseRepo)
			}
			return workflow.RunGHCombinedContext(ctx, "Fetching releases...", "api", "--paginate", fmt.Sprintf("/repos/%s/releases?per_page=100", baseRepo), "--jq", ".[].tag_name")
		},
		getActionSHAForTag: getActionSHAForTag,
	})
}
