package cli

// This file resolves the latest release/version/SHA for an action repository,
// via the GitHub API with a git-based fallback, plus cooldown/version-pinning logic.
// See update_actions_deps.go for the shared caching/DI scaffolding.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// getLatestActionRelease gets the latest release for an action repository
// It respects semantic versioning and the allowMajor flag
func getLatestActionRelease(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
	return getLatestActionReleaseWithDeps(ctx, defaultActionUpdateDeps(), repo, currentVersion, allowMajor, verbose)
}

func getLatestActionReleaseWithDeps(ctx context.Context, deps actionUpdateDeps, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
	updateLog.Printf("Getting latest release for %s@%s (allowMajor=%v)", repo, currentVersion, allowMajor)

	// Extract base repository (e.g., "actions/cache/restore" -> "actions/cache")
	baseRepo := gitutil.ExtractBaseRepo(repo)
	updateLog.Printf("Using base repository: %s for action: %s", baseRepo, repo)

	// Use gh CLI to get releases
	output, err := deps.runGHReleasesAPI(ctx, baseRepo)
	if err != nil {
		// Check if this is an authentication error
		outputStr := string(output)
		if errorutil.IsAuthError(outputStr) || errorutil.IsAuthError(err.Error()) {
			updateLog.Printf("GitHub API authentication failed, attempting git ls-remote fallback for %s", repo)
			// Try fallback using git ls-remote
			latestRelease, latestSHA, gitErr := deps.getLatestReleaseViaGit(ctx, repo, currentVersion, allowMajor, verbose)
			if gitErr != nil {
				return "", "", fmt.Errorf("failed to fetch releases via GitHub API and git: API error: %w, Git Error: %w", err, gitErr)
			}
			return latestRelease, latestSHA, nil
		}
		// Include the gh output in the error for better diagnostics
		if trimmed := strings.TrimSpace(outputStr); trimmed != "" {
			return "", "", fmt.Errorf("failed to fetch releases: %w: %s", err, trimmed)
		}
		return "", "", fmt.Errorf("failed to fetch releases: %w", err)
	}

	releases := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(releases) == 0 || releases[0] == "" {
		// No GitHub Releases found; fall back to tag scanning via git ls-remote.
		// Some repositories publish tags without creating GitHub Releases — this is safe
		// to use and the warning below is informational only.
		updateLog.Printf("No releases found via GitHub API for %s, falling back to git ls-remote tag scan", baseRepo)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(baseRepo+": no GitHub Releases found, falling back to tag scanning (safe to ignore)"))
		}
		latestRelease, latestSHA, gitErr := deps.getLatestReleaseViaGit(ctx, repo, currentVersion, allowMajor, verbose)
		if gitErr != nil {
			return "", "", fmt.Errorf("no releases or tags found for %s: %w", baseRepo, gitErr)
		}
		return latestRelease, latestSHA, nil
	}

	// Parse current version
	currentVer := parseVersion(currentVersion)

	// Find all valid stable semantic version releases (skip prereleases such as v1.0.0-beta.1).
	// Per semver rules, v1.1.0-beta.1 > v1.0.0, so without this filter a prerelease of a
	// higher base version could be incorrectly selected as the upgrade target.
	type releaseWithVersion struct {
		tag     string
		version *semverutil.SemanticVersion
	}
	var validReleases []releaseWithVersion
	for _, release := range releases {
		releaseVer := parseVersion(release)
		if releaseVer != nil && releaseVer.Pre == "" {
			validReleases = append(validReleases, releaseWithVersion{
				tag:     release,
				version: releaseVer,
			})
		}
	}

	if len(validReleases) == 0 {
		return "", "", errors.New("no valid semantic version releases found")
	}

	// Sort releases by semver in descending order (highest first)
	slices.SortFunc(validReleases, func(a, b releaseWithVersion) int {
		switch {
		case a.version.IsNewer(b.version):
			return -1
		case b.version.IsNewer(a.version):
			return 1
		default:
			return 0
		}
	})

	// If current version is not valid, return the highest semver release
	if currentVer == nil {
		latestRelease := validReleases[0].tag
		sha, err := deps.getActionSHAForTag(ctx, baseRepo, latestRelease)
		if err != nil {
			return "", "", fmt.Errorf("failed to get SHA for %s: %w", latestRelease, err)
		}
		return latestRelease, sha, nil
	}

	// Find the highest compatible release (respecting major version if !allowMajor)
	var latestCompatible string
	var latestCompatibleVersion *semverutil.SemanticVersion

	for _, rel := range validReleases {
		// Check if compatible based on major version
		if !allowMajor && rel.version.Major != currentVer.Major {
			continue
		}

		// Since releases are sorted by semver descending, first match is highest
		if latestCompatibleVersion == nil || rel.version.IsNewer(latestCompatibleVersion) {
			latestCompatible = rel.tag
			latestCompatibleVersion = rel.version
		} else if !rel.version.IsNewer(latestCompatibleVersion) &&
			rel.version.Major == latestCompatibleVersion.Major &&
			rel.version.Minor == latestCompatibleVersion.Minor &&
			rel.version.Patch == latestCompatibleVersion.Patch {
			// If versions are equal, prefer the less precise one (e.g., "v8" over "v8.0.0")
			// This follows GitHub Actions convention of using major version tags
			if !rel.version.IsPreciseVersion() && latestCompatibleVersion.IsPreciseVersion() {
				latestCompatible = rel.tag
				latestCompatibleVersion = rel.version
			}
		}
	}

	if latestCompatible == "" {
		return "", "", errors.New("no compatible release found")
	}

	// Get the SHA for the latest compatible release
	sha, err := deps.getActionSHAForTag(ctx, baseRepo, latestCompatible)
	if err != nil {
		return "", "", fmt.Errorf("failed to get SHA for %s: %w", latestCompatible, err)
	}

	return latestCompatible, sha, nil
}

// getLatestActionReleaseViaGit gets the latest release using git ls-remote (fallback)
func getLatestActionReleaseViaGit(ctx context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching latest release for %s via git ls-remote (current: %s, allow major: %v)", repo, currentVersion, allowMajor)))
	}

	// Extract base repository (e.g., "actions/cache/restore" -> "actions/cache")
	baseRepo := gitutil.ExtractBaseRepo(repo)
	updateLog.Printf("Using base repository: %s for action: %s (git fallback)", baseRepo, repo)

	githubHost := getGitHubHostForRepo(baseRepo)
	repoURL := fmt.Sprintf("%s/%s.git", githubHost, baseRepo)

	// List all tags
	// #nosec G204 -- repoURL is constructed from workflow configuration authored by the developer
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch releases via git ls-remote: %w", err)
	}

	releases, tagToSHA := parseActionTagRefs(string(output))

	if len(releases) == 0 {
		return "", "", errors.New("no releases found")
	}

	// Parse current version
	currentVer := parseVersion(currentVersion)

	// Find all valid stable semantic version releases (skip prereleases such as v1.0.0-beta.1).
	// Per semver rules, v1.1.0-beta.1 > v1.0.0, so without this filter a prerelease of a
	// higher base version could be incorrectly selected as the upgrade target.
	// git ls-remote --tags returns every tag, so the prerelease check is especially important
	// for this fallback path.
	type releaseWithVersion struct {
		tag     string
		version *semverutil.SemanticVersion
	}
	var validReleases []releaseWithVersion
	for _, release := range releases {
		releaseVer := parseVersion(release)
		if releaseVer != nil && releaseVer.Pre == "" {
			validReleases = append(validReleases, releaseWithVersion{
				tag:     release,
				version: releaseVer,
			})
		}
	}

	if len(validReleases) == 0 {
		return "", "", errors.New("no valid semantic version releases found")
	}

	// Sort releases by semver in descending order (highest first)
	slices.SortFunc(validReleases, func(a, b releaseWithVersion) int {
		switch {
		case a.version.IsNewer(b.version):
			return -1
		case b.version.IsNewer(a.version):
			return 1
		default:
			return 0
		}
	})

	// If current version is not valid, return the highest semver release
	if currentVer == nil {
		latestRelease := validReleases[0].tag
		sha := tagToSHA[latestRelease]
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Current version is not valid, using highest semver release: %s (via git)", latestRelease)))
		}
		return latestRelease, sha, nil
	}

	// Find the highest compatible release (respecting major version if !allowMajor)
	var latestCompatible string
	var latestCompatibleVersion *semverutil.SemanticVersion

	for _, rel := range validReleases {
		// Check if compatible based on major version
		if !allowMajor && rel.version.Major != currentVer.Major {
			continue
		}

		// Since releases are sorted by semver descending, first match is highest
		if latestCompatibleVersion == nil || rel.version.IsNewer(latestCompatibleVersion) {
			latestCompatible = rel.tag
			latestCompatibleVersion = rel.version
		} else if !rel.version.IsNewer(latestCompatibleVersion) &&
			rel.version.Major == latestCompatibleVersion.Major &&
			rel.version.Minor == latestCompatibleVersion.Minor &&
			rel.version.Patch == latestCompatibleVersion.Patch {
			// If versions are equal, prefer the less precise one (e.g., "v8" over "v8.0.0")
			// This follows GitHub Actions convention of using major version tags
			if !rel.version.IsPreciseVersion() && latestCompatibleVersion.IsPreciseVersion() {
				latestCompatible = rel.tag
				latestCompatibleVersion = rel.version
			}
		}
	}

	if latestCompatible == "" {
		return "", "", errors.New("no compatible release found")
	}

	sha := tagToSHA[latestCompatible]
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest compatible release: %s (via git)", latestCompatible)))
	}

	return latestCompatible, sha, nil
}

// parseActionTagRefs parses git ls-remote --tags output, preferring peeled commit
// SHAs over annotated tag-object SHAs while retaining lightweight tag SHAs.
func parseActionTagRefs(output string) ([]string, map[string]string) {
	var releases []string
	tagToSHA := make(map[string]string)
	seenTags := make(map[string]struct{})

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 || !strings.HasPrefix(parts[1], "refs/tags/") {
			continue
		}

		sha := parts[0]
		tagRef := strings.TrimPrefix(parts[1], "refs/tags/")
		peeled := strings.HasSuffix(tagRef, "^{}")
		tag := strings.TrimSuffix(tagRef, "^{}")

		if _, seen := seenTags[tag]; !seen {
			releases = append(releases, tag)
			seenTags[tag] = struct{}{}
		}
		if peeled {
			tagToSHA[tag] = sha
		} else if _, exists := tagToSHA[tag]; !exists {
			tagToSHA[tag] = sha
		}
	}

	return releases, tagToSHA
}

// findCooledDownActionVersion searches for the newest release that is strictly
// newer than currentVersion but has passed the cooldown period.  It is used as
// a fallback when the highest candidate is still in cooldown: rather than
// skipping the update entirely, we walk down the release list toward older
// (but still upgrading) versions until one has cooled down.
//
// Returns ("", "", nil) when no suitable release is found (fail-open).
func findCooledDownActionVersion(
	ctx context.Context,
	deps actionUpdateDeps,
	repo, currentVersion string,
	allowMajor, verbose bool,
	coolDown time.Duration,
	skipTag string,
) (string, string, error) {
	baseRepo := gitutil.ExtractBaseRepo(repo)

	output, err := deps.runGHReleasesAPI(ctx, baseRepo)
	if err != nil {
		updateLog.Printf("findCooledDownActionVersion: failed to fetch releases for %s: %v", repo, err)
		return "", "", nil // fail-open
	}

	releases := strings.Split(strings.TrimSpace(string(output)), "\n")

	currentVer := parseVersion(currentVersion)

	compatibleReleases := sortedCompatibleReleaseCandidates(releases, currentVer, allowMajor)
	candidates := newerReleaseCandidates(compatibleReleases, currentVer)

	for _, c := range candidates {
		if skipTag != "" && c.tag == skipTag {
			continue
		}
		result := deps.checkCoolDown(ctx, repo, c.tag, coolDown)
		if result.InCoolDown {
			cooldownLog.Printf("Action fallback %s@%s: %s", repo, c.tag, result.Message)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping release candidate %s@%s: %s", repo, c.tag, result.Message)))
			}
			continue
		}
		sha, err := deps.getActionSHAForTag(ctx, baseRepo, c.tag)
		if err != nil {
			updateLog.Printf("findCooledDownActionVersion: failed to get SHA for %s@%s: %v", repo, c.tag, err)
			continue // try next candidate
		}
		if sha == "" {
			updateLog.Printf("findCooledDownActionVersion: empty SHA returned for %s@%s; skipping", repo, c.tag)
			continue // skip; never store an entry without a SHA
		}
		return c.tag, sha, nil
	}

	return "", "", nil
}

// getActionSHAForTag gets the commit SHA for a given tag in an action repository.
// For annotated tags (and chained tag objects), it iteratively peels until it
// reaches the underlying non-tag object SHA, matching what tools like Renovate expect.
func getActionSHAForTag(ctx context.Context, repo, tag string) (string, error) {
	updateLog.Printf("Getting SHA for %s@%s", repo, tag)

	// Fetch both SHA and object type to detect annotated tags.
	// Annotated tags have type "tag" and their SHA points to the tag object,
	// not the underlying commit. We must peel to get the commit SHA.
	output, err := workflow.RunGHContext(ctx, "Fetching tag info...", "api", fmt.Sprintf("/repos/%s/git/ref/tags/%s", repo, tag), "--jq", "[.object.sha, .object.type] | @tsv")
	if err != nil {
		return "", fmt.Errorf("failed to resolve tag: %w", err)
	}

	sha, objType, err := workflow.ParseTagRefTSV(string(output))
	if err != nil {
		return "", fmt.Errorf("failed to parse API response for %s@%s: %w", repo, tag, err)
	}

	// Annotated tags (and chained tag objects) point to a tag object rather than
	// directly to a commit. Iteratively peel until we reach a non-tag object so
	// that emitted action pins use the stable underlying commit SHA rather than a
	// mutable tag object SHA (which changes when the tag is re-created).
	const maxTagPeelDepth = 10
	for depth := 0; objType == "tag"; depth++ {
		if depth >= maxTagPeelDepth {
			return "", fmt.Errorf("failed to peel annotated tag: exceeded max depth %d for %s@%s", maxTagPeelDepth, repo, tag)
		}
		updateLog.Printf("Detected annotated tag for %s@%s (depth %d, tag object SHA: %s), peeling to underlying object", repo, tag, depth, sha)
		output2, err := workflow.RunGHContext(ctx, "Peeling annotated tag...", "api", fmt.Sprintf("/repos/%s/git/tags/%s", repo, sha), "--jq", "[.object.sha, .object.type] | @tsv")
		if err != nil {
			return "", fmt.Errorf("failed to peel annotated tag: %w", err)
		}
		sha, objType, err = workflow.ParseTagRefTSV(string(output2))
		if err != nil {
			return "", fmt.Errorf("failed to parse peeled tag API response for %s@%s: %w", repo, tag, err)
		}
	}
	updateLog.Printf("Resolved %s@%s to %s SHA: %s", repo, tag, objType, sha)

	return sha, nil
}

// actionRefPattern matches "uses: org/repo@SHA-or-tag" in workflow files for any org.
// Requires the org to start with an alphanumeric character and contain only alphanumeric,
// hyphens, or underscores (no dots, matching GitHub's org naming rules) to exclude local
// paths (e.g. "./..."). Repository names may additionally contain dots.
// Captures: (1) indentation+uses prefix, (2) repo path, (3) SHA or version tag,
// (4) optional version comment (e.g., "v6.0.2" from "# v6.0.2"), (5) trailing whitespace.
var actionRefPattern = regexp.MustCompile(`(uses:\s+)([a-zA-Z0-9][a-zA-Z0-9_-]*/[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)*)@([a-fA-F0-9]{40}|[^\s#\n]+?)(\s*#\s*\S+)?(\s*)$`)

// latestReleaseResult caches a resolved version/SHA pair.
type latestReleaseResult struct {
	version string
	sha     string
}
