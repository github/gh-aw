package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var orgRolloutAPIFn = runOrgAPI

type orgRolloutPR struct {
	Number int    `json:"number"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Branch string `json:"headRefName"`
}

func isRolloutPR(pr orgRolloutPR, marker string) bool {
	if strings.Contains(pr.Body, marker) {
		return true
	}
	if marker == ghawUpgradeMarkerPrefix {
		return pr.Title == "Upgrade agentic workflows" && strings.HasPrefix(pr.Branch, "upgrade-agentic-workflows-")
	}
	return pr.Title == "Update workflows from source" && strings.HasPrefix(pr.Branch, "update-workflows-")
}

func rolloutMarker(command string) string {
	if command == "upgrade" {
		return ghawUpgradeMarkerPrefix
	}
	return ghawUpdateMarkerPrefix
}

func rolloutGraphQL(ctx context.Context, query string, target any) error {
	output, err := orgRolloutAPIFn(ctx, getHostFromOriginRemote(), "Discovering rollout targets...",
		"graphql", "-f", "query="+query)
	if err != nil {
		return err
	}
	var response struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("invalid rollout GraphQL response: %w", err)
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("rollout discovery failed: %s", response.Errors[0].Message)
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return errors.New("rollout discovery returned no data")
	}
	return json.Unmarshal(response.Data, target)
}

func graphqlString(value string) string {
	encoded, _ := json.Marshal(value) //nolint:jsonmarshalignoredeerror // Marshaling a string cannot fail.
	return string(encoded)
}

func discoverEnterpriseOrgs(ctx context.Context, enterprise string) ([]string, error) {
	var orgs []string
	cursor := ""
	for {
		var data struct {
			Enterprise *struct {
				Organizations struct {
					Nodes []struct {
						Login string `json:"login"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"organizations"`
			} `json:"enterprise"`
		}
		after := ""
		if cursor != "" {
			after = ",after:" + graphqlString(cursor)
		}
		query := fmt.Sprintf(`query { enterprise(slug:%s) { organizations(first:100%s) { nodes { login } pageInfo { hasNextPage endCursor } } } }`,
			graphqlString(enterprise), after)
		if err := rolloutGraphQL(ctx, query, &data); err != nil {
			return nil, fmt.Errorf("listing enterprise organizations: %w", err)
		}
		if data.Enterprise == nil {
			return nil, fmt.Errorf("enterprise %q is unavailable; verify enterprise organization visibility", enterprise)
		}
		page := data.Enterprise.Organizations
		for _, org := range page.Nodes {
			if !isValidOrgSlug(org.Login) {
				return nil, invalidOrgSlugError(org.Login)
			}
			orgs = append(orgs, org.Login)
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		if page.PageInfo.EndCursor == "" || cursor == page.PageInfo.EndCursor {
			return nil, errors.New("enterprise organization pagination did not advance")
		}
		cursor = page.PageInfo.EndCursor
	}
	slices.Sort(orgs)
	return slices.Compact(orgs), nil
}

// Batch repository connections instead of spending one API request per repository.
// Repositories with more than 100 open PRs use REST pagination, never a partial list.
func discoverRolloutPRs(ctx context.Context, repos []string, marker string) (map[string][]orgRolloutPR, error) {
	found := make(map[string][]orgRolloutPR)
	for start := 0; start < len(repos); start += 20 {
		batch := repos[start:min(start+20, len(repos))]
		var query strings.Builder
		query.WriteString("query { rateLimit { remaining } ")
		for index, repo := range batch {
			owner, name, ok := strings.Cut(repo, "/")
			if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
				return nil, fmt.Errorf("invalid discovered repository %q", repo)
			}
			selection := fmt.Sprintf(`r%d: repository(owner:%s,name:%s) { pullRequests(first:100,states:OPEN) { nodes { number body url title headRefName } pageInfo { hasNextPage } } } `,
				index, graphqlString(owner), graphqlString(name))
			query.WriteString(selection)
		}
		query.WriteString("}")
		var data map[string]json.RawMessage
		if err := rolloutGraphQL(ctx, query.String(), &data); err != nil {
			return nil, err
		}
		var budget struct {
			Remaining int `json:"remaining"`
		}
		if err := json.Unmarshal(data["rateLimit"], &budget); err != nil {
			return nil, fmt.Errorf("reading GraphQL budget: %w", err)
		}
		if budget.Remaining < orgUpdateCoreBuffer {
			return nil, fmt.Errorf("GraphQL API budget below rollout reserve: %w", errOrgRateLimitCritical)
		}
		for index, repo := range batch {
			var connection struct {
				PullRequests struct {
					Nodes    []orgRolloutPR `json:"nodes"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"pullRequests"`
			}
			raw := data[fmt.Sprintf("r%d", index)]
			if len(raw) == 0 || string(raw) == "null" {
				return nil, fmt.Errorf("cannot inspect open PRs in %s", repo)
			}
			if err := json.Unmarshal(raw, &connection); err != nil {
				return nil, fmt.Errorf("reading PRs in %s: %w", repo, err)
			}
			prs := connection.PullRequests.Nodes
			if connection.PullRequests.PageInfo.HasNextPage {
				var err error
				prs, err = listRolloutPRs(ctx, repo)
				if err != nil {
					return nil, err
				}
			}
			for _, pr := range prs {
				if isRolloutPR(pr, marker) {
					found[repo] = append(found[repo], pr)
				}
			}
		}
	}
	return found, nil
}

func listRolloutPRs(ctx context.Context, repo string) ([]orgRolloutPR, error) {
	var prs []orgRolloutPR
	for page := 1; ; page++ {
		output, err := orgRolloutAPIFn(ctx, getHostFromOriginRemote(), "Checking existing rollout PRs...",
			fmt.Sprintf("/repos/%s/pulls?state=open&per_page=100&page=%d", repo, page))
		if err != nil {
			return nil, fmt.Errorf("checking open PRs in %s: %w", repo, err)
		}
		var items []struct {
			Number int    `json:"number"`
			Body   string `json:"body"`
			URL    string `json:"html_url"`
			Title  string `json:"title"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		}
		if err := json.Unmarshal(output, &items); err != nil {
			return nil, fmt.Errorf("reading open PRs in %s: %w", repo, err)
		}
		for _, item := range items {
			prs = append(prs, orgRolloutPR{Number: item.Number, Body: item.Body, URL: item.URL, Title: item.Title, Branch: item.Head.Ref})
		}
		if len(items) < 100 {
			return prs, nil
		}
	}
}

func prepareOrgRolloutWorker(ctx context.Context, repo, marker string) ([]orgRolloutPR, bool, error) {
	opts := orgRolloutFromContext(ctx)
	if opts == nil || !opts.worker {
		return nil, false, nil
	}
	// Recheck immediately before work: discovery may be old after user selection.
	prs, err := listRolloutPRs(ctx, repo)
	if err != nil {
		return nil, false, err
	}
	var existing []orgRolloutPR
	for _, pr := range prs {
		if isRolloutPR(pr, marker) {
			existing = append(existing, pr)
		}
	}
	if len(existing) > 0 && !opts.replace {
		err := emitOrgRolloutResult(orgRolloutResult{Status: "skipped: existing PR", URL: existing[0].URL})
		return existing, true, err
	}
	return existing, false, nil
}

func finishOrgRolloutWorker(ctx context.Context, repo string, existing []orgRolloutPR, prURL string) error {
	if opts := orgRolloutFromContext(ctx); opts == nil || !opts.worker {
		return nil
	}
	if err := emitOrgRolloutResult(orgRolloutResult{Status: "created", URL: prURL}); err != nil {
		return err
	}
	for _, pr := range existing {
		if _, err := orgRolloutAPIFn(ctx, getHostFromOriginRemote(), "Closing superseded rollout PR...",
			"--method", "PATCH", fmt.Sprintf("/repos/%s/pulls/%d", repo, pr.Number), "-f", "state=closed"); err != nil {
			return fmt.Errorf("replacement %s created, but could not close old PR #%d: %w", prURL, pr.Number, err)
		}
	}
	return nil
}

// The existing code search has a 1,000-file ceiling, not a 1,000-repository
// ceiling. Fall back to the full repository inventory when that ceiling is hit.
func listOrgRolloutRepos(ctx context.Context, org string) ([]string, error) {
	var repos []string
	for page := 1; ; page++ {
		output, err := orgRolloutAPIFn(ctx, getHostFromOriginRemote(), "Listing organization repositories...",
			fmt.Sprintf("/orgs/%s/repos?type=all&per_page=100&page=%d", org, page))
		if err != nil {
			return nil, err
		}
		var items []struct {
			FullName string `json:"full_name"`
			Archived bool   `json:"archived"`
			Disabled bool   `json:"disabled"`
		}
		if err := json.Unmarshal(output, &items); err != nil {
			return nil, err
		}
		for _, repo := range items {
			if !repo.Archived && !repo.Disabled {
				repos = append(repos, repo.FullName)
			}
		}
		if len(items) < 100 {
			slices.Sort(repos)
			return repos, nil
		}
	}
}
