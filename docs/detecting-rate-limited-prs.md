# Detecting Rate-Limited Pull Requests

This document provides search queries and GraphQL patterns for detecting pull requests that have failed due to GitHub API rate limiting.

## Problem Statement

Copilot-created PRs and automated workflows sometimes fail because the GitHub API rate limit is exceeded during execution. These failures can be difficult to track down without proper tooling.

## Detection Patterns

### 1. Text-Based Indicators

Rate-limited PRs typically contain these text patterns in comments or logs:
- `rate limit` (case-insensitive)
- `API rate limit exceeded`
- `secondary rate limit`
- `abuse detection`
- `403` (HTTP status) combined with "rate" or "limit"
- `429` (HTTP status - Too Many Requests)

### 2. GitHub Search Queries

#### Basic Search for Copilot PRs

Find recent PRs from Copilot agent:
```
is:pr author:app/copilot-swe-agent
```

#### Date-Filtered Search

Find PRs created in the last 30 days:
```
is:pr author:app/copilot-swe-agent created:>2026-01-07
```

#### Closed PRs with Comments

Find closed PRs that have comments (where rate limit messages might appear):
```
is:pr is:closed comments:>0 created:>2026-01-01
```

#### Repository-Specific Search

Search in a specific repository:
```
is:pr repo:github/gh-aw author:app/copilot-swe-agent
```

### 3. GraphQL Queries

#### Query PR Timeline for Comments

This query fetches a PR's timeline including all comments where rate limit messages typically appear:

```graphql
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number
      title
      state
      createdAt
      closedAt
      author {
        login
      }
      url
      timelineItems(first: 100, itemTypes: [ISSUE_COMMENT, CLOSED_EVENT]) {
        nodes {
          __typename
          ... on IssueComment {
            author {
              login
            }
            body
            createdAt
          }
        }
      }
    }
  }
}
```

**Usage with GitHub CLI:**
```bash
gh api graphql -f query='...' -f owner="github" -f repo="gh-aw" -F number=14368
```

#### Search Multiple PRs

To search multiple PRs efficiently, combine REST API search with GraphQL timeline queries:

1. First, search for PRs using the REST API:
```bash
gh pr list --state all --limit 50 --json number,title,state,author
```

2. Then, for each PR, fetch the timeline using GraphQL to check for rate limit messages.

### 4. Using the Detection Script

The `skills/detect-rate-limited-prs/detect-rate-limited-prs.sh` script automates this process:

#### Check a Specific PR

```bash
./detect-rate-limited-prs.sh --pr 14368
```

#### Check All Recent PRs

```bash
./detect-rate-limited-prs.sh --since 2026-01-01 --limit 50
```

#### Check Specific Repository

```bash
./detect-rate-limited-prs.sh --repo github/gh-aw --state all
```

## Output Format

The detection script outputs JSON:

```json
{
  "rate_limited_prs": [
    {
      "number": 14368,
      "title": "Fix rate limiting issue",
      "state": "closed",
      "created_at": "2026-01-15T10:00:00Z",
      "rate_limit_detected_at": "2026-01-15T10:05:00Z",
      "error_message": "API rate limit exceeded for installation ID...",
      "url": "https://github.com/github/gh-aw/pull/14368"
    }
  ],
  "total_examined": 50,
  "total_rate_limited": 1
}
```

## Example: PR #14368

This PR is a reference example of a rate-limited PR. To examine it:

```bash
# Using the detection script
./detect-rate-limited-prs.sh --pr 14368

# Using GitHub CLI directly
gh pr view 14368 --json comments,timelineItems

# Using GraphQL
gh api graphql -f query='query { repository(owner: "github", name: "gh-aw") { pullRequest(number: 14368) { timelineItems(first: 100) { nodes { __typename } } } } }'
```

## Rate Limit Prevention Strategies

1. **Implement Retry Logic**: Add exponential backoff for GitHub API calls
2. **Use GraphQL**: GraphQL queries can be more efficient than REST API for complex data
3. **Cache Results**: Cache API responses when possible
4. **Monitor Rate Limits**: Check rate limit status before making requests
5. **Use Personal Access Tokens**: Different tokens have different rate limits

## Integration with Workflows

Use the rate-limit-pr-detector.md workflow to automatically scan for rate-limited PRs:

```bash
gh workflow run rate-limit-pr-detector.md
```

The workflow will:
1. Search for recent Copilot PRs
2. Check each PR's timeline for rate limit indicators
3. Generate a comprehensive report
4. Provide recommendations for prevention

## Related Resources

- [GitHub API Rate Limits Documentation](https://docs.github.com/en/rest/overview/resources-in-the-rest-api#rate-limiting)
- [Secondary Rate Limits](https://docs.github.com/en/rest/overview/resources-in-the-rest-api#secondary-rate-limits)
- [Best Practices for Integrators](https://docs.github.com/en/rest/guides/best-practices-for-integrators)

## Troubleshooting

### No Results Found

If the detection script finds no rate-limited PRs:
- Verify the date range is correct
- Check if Copilot PRs use a different author name
- Manually inspect a known rate-limited PR to confirm the detection pattern

### GraphQL Errors

If GraphQL queries fail:
- Ensure you have proper authentication: `gh auth status`
- Check you have the required permissions: `pull-requests: read`
- Verify the repository exists and is accessible

### Rate Limiting While Detecting Rate Limits

If the detection script itself gets rate limited:
- Reduce the `--limit` parameter to check fewer PRs at once
- Add delays between PR checks
- Use a different authentication token with higher limits
