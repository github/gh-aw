---
name: Rate Limit PR Detector
description: Detect and report PRs that failed due to rate limiting
on:
  workflow_dispatch:

permissions:
  contents: read
  pull-requests: read
  issues: read

engine:
  id: copilot
  model: gpt-5.1-codex-mini

timeout-minutes: 15

tools:
  github:
    toolsets: [default, pull_requests]
---

# Rate Limit PR Detector

Your mission is to detect pull requests that have failed due to rate limiting and provide a comprehensive report.

## Background

Some Copilot-created PRs fail because the GitHub API rate limit is exceeded during execution. These PRs typically have:
- Comments from the Copilot agent or GitHub Actions bot indicating rate limiting
- Specific error messages containing "rate limit" or "API rate limit exceeded"
- Failed workflow runs with rate limit errors

## Your Task

1. **Query Recent PRs**: Search for recent pull requests (last 30 days) from the Copilot agent
   - Focus on PRs authored by `app/copilot-swe-agent` or similar Copilot bots
   - Include both open and closed PRs

2. **Examine Timeline Events**: For each PR, check the timeline for:
   - Comments containing "rate limit", "API rate limit", or "rate limited"
   - Workflow run failures with rate limit messages
   - GitHub Actions bot comments about rate limiting

3. **Identify Rate-Limited PRs**: Create a list of PRs that show evidence of rate limiting

4. **Generate Report**: Create a summary report that includes:
   - PR number and title
   - When the rate limiting occurred
   - The specific error message or comment
   - Current PR state (open/closed/merged)
   - Suggestions for prevention or retry

## Search Query Pattern

Use the GitHub search API or GraphQL to find PRs. Example search query:
```
is:pr author:app/copilot-swe-agent created:>2026-01-01
```

## GraphQL Query for Timeline

Use GraphQL to fetch PR timeline events:
```graphql
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number
      title
      state
      author {
        login
      }
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

## Detection Criteria

A PR is considered rate-limited if it contains any of these patterns:
- "rate limit" (case-insensitive)
- "API rate limit exceeded"
- "403" with "rate" or "limit"
- "secondary rate limit"
- "abuse detection"

## Example PR

Reference PR #14368 as an example of a rate-limited PR to understand the pattern.

## Output Format

Present your findings in a structured format:

```
# Rate-Limited PRs Report

## Summary
- Total PRs examined: X
- Rate-limited PRs found: Y
- Date range: [start] to [end]

## Rate-Limited PRs

### PR #XXXXX: [Title]
- **State**: [open/closed/merged]
- **Created**: [date]
- **Rate Limit Event**: [date]
- **Error Message**: [excerpt from comment/log]
- **URL**: [PR URL]

## Recommendations
[Your suggestions for preventing or handling rate-limited PRs]
```

## Important Notes

- Be thorough but efficient - don't fetch more data than needed
- Use pagination if there are many PRs to examine
- Focus on recent PRs (last 30 days) unless asked otherwise
- Provide actionable insights in your recommendations

Good luck! 🔍
