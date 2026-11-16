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

	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateActionsDepsLog = logger.New("cli:update_actions_deps")

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

// isGhAwSetupAction reports whether repo is the gh-aw setup action, either
// github/gh-aw-actions/setup or the legacy github/gh-aw/actions/setup.
func isGhAwSetupAction(repo string) bool {
	return repo == workflow.GitHubActionsOrgRepo+"/setup" || repo == workflow.GitHubOrgRepo+"/actions/setup"
}

// compilerReleaseTag returns the running compiler version as a release tag
// (e.g. "v0.68.3"), or "" for dev, dirty, or otherwise non-release builds. Release
// status comes from workflow.IsRelease so a semver-shaped version on a
// non-release binary still falls back to the regular update policy.
func compilerReleaseTag() string {
	version := GetVersion()
	if !workflow.IsRelease() || parseVersion(version) == nil {
		return ""
	}
	return semverutil.EnsureVPrefix(version)
}

// compilerPinnedActionTag returns the tag repo must be pinned to so that it matches the
// running compiler, or "" when repo follows the regular release-based update policy.
// The setup action is the runtime that compiled workflows depend on, so update and
// upgrade pin it to exactly the compiler version instead of the latest release.
func compilerPinnedActionTag(repo string) string {
	if !isGhAwSetupAction(repo) {
		return ""
	}
	return compilerReleaseTag()
}

// resolveActionUpdateTarget returns the version and SHA an action reference should be
// updated to. Compiler-pinned actions resolve the compiler version tag; all other
// actions resolve their latest release.
func resolveActionUpdateTarget(ctx context.Context, deps actionUpdateDeps, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
	if tag := compilerPinnedActionTag(repo); tag != "" {
		updateActionsDepsLog.Printf("Pinning %s to compiler version %s", repo, tag)
		sha, err := deps.getActionSHAForTag(ctx, gitutil.ExtractBaseRepo(repo), tag)
		if err != nil {
			if errorutil.IsAuthError(err.Error()) && deps.getActionSHAForTagViaGit != nil {
				updateActionsDepsLog.Printf("GitHub API authentication failed, resolving %s@%s via git ls-remote", repo, tag)
				gitSHA, gitErr := deps.getActionSHAForTagViaGit(ctx, gitutil.ExtractBaseRepo(repo), tag)
				if gitErr == nil {
					return tag, gitSHA, nil
				}
				return "", "", fmt.Errorf("cannot resolve SHA for compiler version %s via GitHub API and git: API error: %w, Git Error: %w", tag, err, gitErr)
			}
			return "", "", fmt.Errorf("cannot resolve SHA for compiler version %s: %w", tag, err)
		}
		return tag, sha, nil
	}
	return deps.getLatestRelease(ctx, repo, currentVersion, allowMajor, verbose)
}

type actionUpdateDeps struct {
	getLatestRelease         func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error)
	getLatestReleaseViaGit   func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error)
	runGHReleasesAPI         func(ctx context.Context, baseRepo string) ([]byte, error)
	getActionSHAForTag       func(ctx context.Context, repo, tag string) (string, error)
	getActionSHAForTagViaGit func(ctx context.Context, repo, tag string) (string, error)
	checkCoolDown            func(ctx context.Context, repo, tag string, coolDown time.Duration) coolDownCheckResult
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
	gitShas := make(map[string]*syncutil.OnceLoader[string])
	cooldowns := make(map[string]*syncutil.OnceLoader[coolDownCheckResult])

	cached := base
	cached.getLatestRelease = func(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		repo = gitutil.ExtractBaseRepo(repo)
		key := fmt.Sprintf("%s|%s|%t", repo, currentVersion, allowMajor)
		result, err := loadActionUpdateResult(&mu, latestReleases, key, func() (cachedLatestRelease, error) {
			updateActionsDepsLog.Printf("Cache miss: fetching latest release for %s (current=%s, allowMajor=%t)", repo, currentVersion, allowMajor)
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
			updateActionsDepsLog.Printf("Cache miss: fetching release list for %s", repo)
			return base.runGHReleasesAPI(ctx, repo)
		})
	}
	cached.getActionSHAForTag = func(ctx context.Context, repo, tag string) (string, error) {
		repo = gitutil.ExtractBaseRepo(repo)
		key := repo + "|" + tag
		return loadActionUpdateResult(&mu, shas, key, func() (string, error) {
			updateActionsDepsLog.Printf("Cache miss: resolving SHA for %s@%s", repo, tag)
			return base.getActionSHAForTag(ctx, repo, tag)
		})
	}
	if base.getActionSHAForTagViaGit != nil {
		cached.getActionSHAForTagViaGit = func(ctx context.Context, repo, tag string) (string, error) {
			repo = gitutil.ExtractBaseRepo(repo)
			key := repo + "|" + tag
			return loadActionUpdateResult(&mu, gitShas, key, func() (string, error) {
				updateActionsDepsLog.Printf("Cache miss: resolving SHA for %s@%s via git ls-remote", repo, tag)
				return base.getActionSHAForTagViaGit(ctx, repo, tag)
			})
		}
	}
	cached.checkCoolDown = func(ctx context.Context, repo, tag string, coolDown time.Duration) coolDownCheckResult {
		key := fmt.Sprintf("%s|%s|%s", repo, tag, coolDown)
		result, _ := loadActionUpdateResult(&mu, cooldowns, key, func() (coolDownCheckResult, error) {
			updateActionsDepsLog.Printf("Cache miss: checking cooldown for %s@%s (window=%s)", repo, tag, coolDown)
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
		getActionSHAForTag:       getActionSHAForTag,
		getActionSHAForTagViaGit: getActionSHAForTagViaGit,
	})
}
