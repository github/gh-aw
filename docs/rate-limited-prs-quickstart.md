# Rate-Limited PR Detection - Quick Start

This guide shows you how to quickly detect and analyze pull requests that have failed due to GitHub API rate limiting.

## Quick Detection Methods

### Method 1: Run the Workflow (Automated)

The easiest way to detect rate-limited PRs is to run the automated workflow:

```bash
gh workflow run rate-limit-pr-detector.md
```

This will:
- Search for recent Copilot PRs
- Check each PR's timeline for rate limit indicators  
- Generate a comprehensive report
- Provide recommendations

### Method 2: Use the Detection Script (Manual)

For manual investigation, use the detection script:

```bash
# Check a specific PR (e.g., PR #14368)
cd skills/detect-rate-limited-prs
./detect-rate-limited-prs.sh --pr 14368

# Check all PRs from the last 30 days
./detect-rate-limited-prs.sh --since 2026-01-01 --limit 50

# Check a specific repository
./detect-rate-limited-prs.sh --repo github/gh-aw --state all
```

### Method 3: GitHub Search (Quick Lookup)

Use GitHub's web interface or CLI to search:

```bash
# Find recent Copilot PRs
gh pr list --search "author:app/copilot-swe-agent"

# View a specific PR with comments
gh pr view 14368 --comments
```

## What Gets Detected

The detection looks for these indicators:

- **Text patterns**: "rate limit", "API rate limit exceeded", "secondary rate limit"
- **HTTP codes**: 403 (Forbidden), 429 (Too Many Requests)
- **Comments**: From GitHub Actions bot or Copilot agent mentioning rate limiting

## Example Output

```json
{
  "rate_limited_prs": [
    {
      "number": 14368,
      "title": "Fix issue with workflow",
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

## Understanding the Results

- **number**: The PR number
- **state**: Current state (open, closed, merged)
- **rate_limit_detected_at**: When the rate limit was first detected
- **error_message**: The actual error message from the PR timeline
- **url**: Direct link to the PR

## Prevention Strategies

To avoid rate limiting in the future:

1. **Add retry logic** with exponential backoff
2. **Use GraphQL** instead of REST API where possible
3. **Cache API responses** to reduce duplicate calls
4. **Monitor rate limits** before making requests
5. **Use different tokens** for different operations

## Need More Details?

See the complete documentation:

- **Full Documentation**: `docs/detecting-rate-limited-prs.md`
- **Skill Documentation**: `skills/detect-rate-limited-prs/SKILL.md`
- **Workflow Details**: `.github/workflows/rate-limit-pr-detector.md`

## GraphQL Query Example

For custom queries, use this GraphQL pattern:

```graphql
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      timelineItems(first: 100, itemTypes: [ISSUE_COMMENT]) {
        nodes {
          ... on IssueComment {
            body
            createdAt
          }
        }
      }
    }
  }
}
```

## Troubleshooting

**Script finds no results?**
- Check the date range with `--since`
- Verify Copilot PRs use `app/copilot-swe-agent` as author
- Try manually checking a known rate-limited PR first

**GraphQL errors?**
- Ensure you're authenticated: `gh auth status`
- Check you have `pull-requests: read` permission

**Script itself gets rate limited?**
- Reduce the `--limit` parameter
- Add delays between PR checks
- Use a token with higher rate limits

## Example Real-World Case

PR #14368 is a reference example of a rate-limited PR. To examine it:

```bash
./detect-rate-limited-prs.sh --pr 14368
```

This will show you the exact error message and when the rate limiting occurred.
