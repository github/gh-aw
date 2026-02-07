---
name: detect-rate-limited-prs
description: Detect and analyze PRs that failed due to GitHub API rate limiting
---

# Detect Rate-Limited PRs Skill

This skill helps identify pull requests that have failed or encountered issues due to GitHub API rate limiting.

## Overview

GitHub API rate limiting can cause Copilot PRs and automated workflows to fail. This skill provides utilities to:
- Search for PRs with rate limit indicators
- Analyze PR timeline events for rate limit messages
- Generate reports on rate-limited PRs

## Usage

### Basic Detection

To search for rate-limited PRs in the current repository:

```bash
./detect-rate-limited-prs.sh
```

### With Custom Date Range

Search for rate-limited PRs created after a specific date:

```bash
./detect-rate-limited-prs.sh --since 2026-01-01
```

### Specific Repository

Search in a different repository:

```bash
./detect-rate-limited-prs.sh --repo github/gh-aw
```

### Detailed Analysis

Get detailed timeline analysis for a specific PR:

```bash
./detect-rate-limited-prs.sh --pr 14368
```

## Detection Patterns

The script looks for these indicators of rate limiting:

1. **Comment Patterns**:
   - "rate limit"
   - "API rate limit exceeded"
   - "secondary rate limit"
   - "abuse detection"

2. **HTTP Status Codes**:
   - 403 (Forbidden) with rate limit context
   - 429 (Too Many Requests)

3. **GitHub Actions Failures**:
   - Workflow runs that failed with rate limit errors
   - GitHub Actions bot comments about rate limiting

## Search Query Examples

### Find Copilot PRs from Last 30 Days

```
is:pr author:app/copilot-swe-agent created:>2026-01-07
```

### Find Closed PRs with Comments

```
is:pr is:closed comments:>0 created:>2026-01-01
```

### GraphQL Query for PR Timeline

```graphql
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number
      title
      state
      timelineItems(first: 100, itemTypes: [ISSUE_COMMENT]) {
        nodes {
          __typename
          ... on IssueComment {
            author { login }
            body
            createdAt
          }
        }
      }
    }
  }
}
```

## Output Format

The script outputs JSON with rate-limited PR information:

```json
{
  "rate_limited_prs": [
    {
      "number": 14368,
      "title": "PR Title",
      "state": "closed",
      "created_at": "2026-01-15T10:00:00Z",
      "rate_limit_detected_at": "2026-01-15T10:05:00Z",
      "error_message": "API rate limit exceeded",
      "url": "https://github.com/github/gh-aw/pull/14368"
    }
  ],
  "total_examined": 50,
  "total_rate_limited": 1
}
```

## Requirements

- GitHub CLI (`gh`) authenticated with appropriate permissions
- `jq` for JSON processing
- `pull-requests: read` permission

## Example Workflow Integration

Use this skill in a workflow:

```yaml
---
name: Check Rate Limited PRs
on: workflow_dispatch

jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run detection
        run: ./skills/detect-rate-limited-prs/detect-rate-limited-prs.sh
```
