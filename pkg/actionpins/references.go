package actionpins

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/semverutil"
)

// getLatestActionPinReference returns the pinned reference for the latest version of the repo.
// Returns an empty string if no pin is found.
func getLatestActionPinReference(repo string) string {
	pin, ok := GetLatestActionPinByRepo(repo)
	if !ok {
		actionPinsLog.Printf("No action pin found for repo=%s", repo)
		return ""
	}
	return FormatPinnedActionReference(repo, pin.SHA, pin.Version)
}

// FormatPinnedActionReference formats a pinned action reference with repo, SHA, and version comment.
// Example: "actions/checkout@abc123 # v4.1.0"
// Panics if sha is empty, because that would emit invalid workflow YAML and indicates
// a programming error or corrupted action pin data that should already have been rejected.
func FormatPinnedActionReference(repo, sha, version string) string {
	if sha == "" {
		actionPinsLog.Printf("ERROR: empty SHA for repo=%s version=%s, refusing to format pinned reference", repo, version)
		panic(fmt.Sprintf("FormatPinnedActionReference called with empty SHA for repo=%s version=%s — this would produce invalid workflow YAML", repo, version))
	}
	return repo + "@" + sha + " # " + version
}

func formatPinnedActionWithResolution(repo, sha, sourceVersion, resolvedVersion string) string {
	if sourceVersion == resolvedVersion || resolvedVersion == "" {
		return FormatPinnedActionReference(repo, sha, sourceVersion)
	}
	if sourceVersion == "" {
		return FormatPinnedActionReference(repo, sha, resolvedVersion)
	}
	actionPinsLog.Printf("Version resolved: source=%s resolved=%s for repo=%s", sourceVersion, resolvedVersion, repo)
	if semverutil.IsActionVersionTag(sourceVersion) &&
		strings.Count(strings.TrimPrefix(sourceVersion, "v"), ".") == 2 &&
		strings.HasPrefix(sourceVersion, resolvedVersion+".") {
		return FormatPinnedActionReference(repo, sha, sourceVersion)
	}
	return FormatPinnedActionReference(repo, sha, resolvedVersion)
}

// FormatCacheKey generates a cache key for action resolution.
// Example: "actions/checkout@v4"
func FormatCacheKey(repo, version string) string {
	return repo + "@" + version
}

// ExtractRepo extracts the action repository from a uses string.
// Examples: "actions/checkout@v5" -> "actions/checkout"
func ExtractRepo(uses string) string {
	before, _, ok := strings.Cut(uses, "@")
	if !ok {
		return uses
	}
	return before
}

// ExtractVersion extracts the version from a uses string.
// Examples: "actions/checkout@v5" -> "v5", "actions/checkout" -> ""
func ExtractVersion(uses string) string {
	_, after, ok := strings.Cut(uses, "@")
	if !ok {
		return ""
	}
	return after
}
