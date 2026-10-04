package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var versionLabelLog = logger.New("cli:update_version_labels")

// repoTagEntry holds a tag name and its resolved commit SHA.
type repoTagEntry struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

var (
	versionLabelMu    sync.Mutex
	versionLabelCache = make(map[string]map[string]string) // repo -> sha -> tag name
	runGHVersionLabel = workflow.RunGHContext
)

// resolveVersionLabel returns a human-readable label for a git ref in the given
// source repo. If the ref is a semver tag or branch name it is returned as-is.
// For commit SHAs it tries to find a matching tag in the source repo (first page
// of tags). If no tag is found it falls back to shortRef.
//
// Results are cached per source repo so that multiple workflows sharing a source
// only trigger one API call.
func resolveVersionLabel(ctx context.Context, sourceRepo, ref string) string {
	if !IsCommitSHA(ref) {
		// Already a tag or branch – just return as-is.
		versionLabelLog.Printf("resolveVersionLabel: ref %q is not a commit SHA, returning as-is", ref)
		return ref
	}

	if tagMap, ok := getVersionLabelCache(sourceRepo); ok {
		if tag, ok := tagMap[ref]; ok {
			versionLabelLog.Printf("resolveVersionLabel: cache hit for %s@%s -> %s", sourceRepo, shortRef(ref), tag)
			return tag
		}
		versionLabelLog.Printf("resolveVersionLabel: cache present for %s but no tag for %s", sourceRepo, shortRef(ref))
		return shortRef(ref)
	}

	versionLabelLog.Printf("resolveVersionLabel: cache miss for %s, loading tags", sourceRepo)
	tagMap, ok := loadRepoTagMap(ctx, sourceRepo)
	if !ok {
		versionLabelLog.Printf("resolveVersionLabel: tag load failed for %s, falling back to short ref", sourceRepo)
		return shortRef(ref)
	}

	setVersionLabelCache(sourceRepo, tagMap)

	if tag, ok := tagMap[ref]; ok {
		return tag
	}
	return shortRef(ref)
}

// clearVersionLabelCache clears per-run source-repo tag caches.
func clearVersionLabelCache() {
	versionLabelMu.Lock()
	defer versionLabelMu.Unlock()
	versionLabelCache = make(map[string]map[string]string) //nolint:packagelevelmutableslicemap // versionLabelMu guards the cache.
}

func getVersionLabelCache(sourceRepo string) (map[string]string, bool) {
	versionLabelMu.Lock()
	defer versionLabelMu.Unlock()
	tagMap, ok := versionLabelCache[sourceRepo]
	return tagMap, ok
}

func setVersionLabelCache(sourceRepo string, tagMap map[string]string) {
	versionLabelMu.Lock()
	defer versionLabelMu.Unlock()
	versionLabelCache[sourceRepo] = tagMap //nolint:packagelevelmutableslicemap // versionLabelMu guards the cache.
}

// loadRepoTagMap fetches tags for sourceRepo and returns a map from full commit
// SHA to tag name. The second return value is false when tag loading fails.
func loadRepoTagMap(ctx context.Context, sourceRepo string) (map[string]string, bool) {
	tagMap := make(map[string]string)
	owner, repoName, err := repoutil.SplitRepoSlug(sourceRepo)
	if err != nil {
		return tagMap, false
	}

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("/repos/%s/%s/tags?per_page=100&page=%d", owner, repoName, page)
		output, err := runGHVersionLabel(ctx, "Fetching version tags...", "api", endpoint)
		if err != nil {
			return tagMap, false
		}

		var tags []repoTagEntry
		if err := json.Unmarshal(output, &tags); err != nil {
			return tagMap, false
		}
		if len(tags) == 0 {
			break
		}
		versionLabelLog.Printf("loadRepoTagMap: fetched %d tag(s) for %s (page %d)", len(tags), sourceRepo, page)

		for _, t := range tags {
			sha := strings.TrimSpace(t.Commit.SHA)
			name := strings.TrimSpace(t.Name)
			if sha == "" || name == "" {
				continue
			}
			// Keep first tag for a SHA so the newest tag (API order) wins.
			if _, exists := tagMap[sha]; !exists {
				tagMap[sha] = name
			}
		}
		if len(tags) < 100 {
			break
		}
	}
	return tagMap, true
}
