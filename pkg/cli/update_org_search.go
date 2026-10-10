package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateOrgSearchLog = logger.New("cli:update_org_search")

// orgSearchResponse holds the paginated code-search results returned by the
// GitHub search/code API when discovering repositories in an organization.
type orgSearchResponse struct {
	Items []struct {
		Path       string `json:"path"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"items"`
}

var searchOrgWorkflowReposFn = searchOrgWorkflowRepos

const orgSlugConstraintDescription = "must be 1-39 characters, contain only alphanumeric characters or single hyphens, and cannot start or end with a hyphen"

// orgSlugRe matches valid GitHub organization names: alphanumeric characters
// and single hyphens between segments, not starting or ending with a hyphen,
// length 1–39.
var orgSlugRe = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// isValidOrgSlug reports whether s is a valid GitHub organization slug.
// GitHub org names may only contain alphanumeric characters and hyphens,
// cannot start or end with a hyphen, and are 1–39 characters long.
func isValidOrgSlug(s string) bool {
	return orgSlugRe.MatchString(s) && !strings.Contains(s, "--")
}

func invalidOrgSlugError(org string) error {
	return fmt.Errorf("invalid organization name %q: %s", org, orgSlugConstraintDescription)
}

// searchOrgWorkflowRepos searches an organization's repositories for compiled
// agentic workflow lock files (.lock.yml) in .github/workflows.
//
// It paginates through all code-search results, deduplicates by repository full
// name, and returns a deterministically sorted slice of "owner/repo" strings.
func searchOrgWorkflowRepos(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
	if !isValidOrgSlug(org) {
		return nil, invalidOrgSlugError(org)
	}
	updateOrgSearchLog.Printf("Searching org %q for workflow repos (%d workflow name filters)", org, len(workflowNames))
	query := buildOrgWorkflowSearchQuery(org, workflowNames)
	return searchOrgReposByQuery(ctx, query, verbose)
}

// buildOrgWorkflowSearchQuery constructs the org-mode code-search query for
// lock.yml workflow files. When workflowNames is empty, or every candidate
// normalizes away, it falls back to the base query and relies on the later
// per-repo workflow scan to enforce any requested filters.
func buildOrgWorkflowSearchQuery(org string, workflowNames []string) string {
	base := fmt.Sprintf(`org:%s path:.github/workflows filename:.lock.yml`, org)
	if len(workflowNames) == 0 {
		return base
	}

	filenameFilters := make([]string, 0, len(workflowNames))
	seen := make(map[string]struct{}, len(workflowNames))
	for _, workflowName := range workflowNames {
		normalized := normalizeWorkflowID(workflowName)
		if normalized == "" || normalized == "." {
			continue
		}
		filename := normalized + ".lock.yml"
		if _, ok := seen[filename]; ok {
			continue
		}
		seen[filename] = struct{}{}
		filenameFilters = append(filenameFilters, "filename:"+filename)
	}
	if len(filenameFilters) == 0 {
		// CLI validation already rejects empty workflow names, so this fallback is
		// primarily a safety net for non-CLI callers and tests.
		return base
	}

	slices.Sort(filenameFilters)
	return base + " (" + strings.Join(filenameFilters, " OR ") + ")"
}

// searchOrgReposByQuery paginates through GitHub code-search results for the given
// query, deduplicates by repository full name, and returns a deterministically
// sorted slice of "owner/repo" strings.
func searchOrgReposByQuery(ctx context.Context, query string, verbose bool) ([]string, error) {
	perPage := 100
	page := 1
	seen := make(map[string]struct{})
	var repos []string

	for {
		if err := waitForOrgRateLimitFn(ctx, "search", verbose); err != nil && verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Continuing after search rate limit check failure: %v", err)))
		}
		endpoint := fmt.Sprintf("/search/code?q=%s&per_page=%d&page=%d", url.QueryEscape(query), perPage, page)
		output, err := workflow.RunGHContext(ctx, "Searching repositories...", "api", endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to search organization repositories: %w", err)
		}

		var response orgSearchResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return nil, fmt.Errorf("failed to parse organization search results: %w", err)
		}
		if len(response.Items) == 0 {
			break
		}

		for _, item := range response.Items {
			repo := strings.TrimSpace(item.Repository.FullName)
			if repo == "" {
				continue
			}
			if _, ok := seen[repo]; ok {
				continue
			}
			seen[repo] = struct{}{}
			repos = append(repos, repo)
		}

		updateOrgSearchLog.Printf("Search page %d returned %d items (%d unique repos so far)", page, len(response.Items), len(repos))

		if len(response.Items) < perPage {
			break
		}
		page++
	}

	slices.Sort(repos)
	updateOrgSearchLog.Printf("Org code-search complete: %d unique repos found", len(repos))
	return repos, nil
}

// validateRepoGlobs reports an error for any empty or syntactically invalid
// glob pattern in the --repos flag slice.
func validateRepoGlobs(globs []string) error {
	for _, glob := range globs {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			return errors.New("--repos patterns cannot be empty")
		}
		if _, err := path.Match(glob, "example"); err != nil {
			return fmt.Errorf("invalid --repos pattern %q: %w", glob, err)
		}
	}
	return nil
}

// filterOrgRepos returns the subset of repos that match at least one of the
// provided glob patterns. Each pattern is tested against both the full
// "owner/repo" name and the bare repository name. When globs is empty every
// repository is returned unchanged.
func filterOrgRepos(repos []string, globs []string) []string {
	if len(globs) == 0 {
		return repos
	}
	updateOrgSearchLog.Printf("Filtering %d repos against %d glob pattern(s)", len(repos), len(globs))
	filtered := make([]string, 0, len(repos))
	for _, repo := range repos {
		name := repo
		if _, tail, ok := strings.Cut(repo, "/"); ok {
			name = tail
		}
		for _, glob := range globs {
			if ok, _ := path.Match(glob, repo); ok {
				filtered = append(filtered, repo)
				break
			}
			if ok, _ := path.Match(glob, name); ok {
				filtered = append(filtered, repo)
				break
			}
		}
	}
	updateOrgSearchLog.Printf("Glob filtering reduced %d repos to %d", len(repos), len(filtered))
	return filtered
}
