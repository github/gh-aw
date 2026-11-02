---
mcp-scripts:
  fetch-pr-data:
    description: "Fetches pull request data from GitHub using gh CLI. Returns JSON array of PRs with fields: number, title, author, headRefName, createdAt, state, url, body, labels, updatedAt, closedAt, mergedAt"
    inputs:
      repo:
        type: string
        description: "Repository in owner/repo format (defaults to current repository)"
        required: false
      search:
        type: string
        description: "Search query for filtering PRs (e.g., 'head:copilot/' for Copilot PRs)"
        required: false
      state:
        type: string
        description: "PR state filter: open, closed, merged, or all (default: all)"
        default: "all"
      limit:
        type: number
        description: "Maximum number of PRs to fetch (default: 100)"
        default: 100
      days:
        type: number
        description: "Number of days to look back (default: 30)"
        default: 30
    run: |
      # Fetch PR data using gh CLI
      REPO="${INPUT_REPO:-$GITHUB_REPOSITORY}"
      STATE="${INPUT_STATE:-all}"
      LIMIT="${INPUT_LIMIT:-100}"
      DAYS="${INPUT_DAYS:-30}"
      SEARCH="${INPUT_SEARCH:-}"
      
      # Calculate date N days ago (cross-platform)
      DATE_AGO=$(date -d "${DAYS} days ago" '+%Y-%m-%d' 2>/dev/null || date -v-${DAYS}d '+%Y-%m-%d')
      
      # Build search query
      QUERY="created:>=${DATE_AGO}"
      if [ -n "$SEARCH" ]; then
        QUERY="${SEARCH} ${QUERY}"
      fi
      
      # Fetch PRs
      gh pr list --repo "$REPO" \
        --search "$QUERY" \
        --state "$STATE" \
        --json number,title,author,headRefName,createdAt,state,url,body,labels,updatedAt,closedAt,mergedAt \
        --limit "$LIMIT"
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_REPOSITORY: ${{ github.repository }}
---
<!--
## PR Data Fetch Safe Input Tool

This shared workflow provides a `fetch-pr-data` mcp-script tool that fetches pull request data from GitHub.

### Usage

Import this shared workflow to get access to the `fetch-pr-data` tool:

```yaml
imports:
  - shared/pr-data-mcp-script.md
```

The agent can then use the tool to fetch PR data:
- `fetch-pr-data` with no arguments returns PRs from the last 30 days
- `fetch-pr-data` with `search: "head:copilot/"` returns Copilot coding agent PRs
- `fetch-pr-data` with `state: "merged"` returns only merged PRs

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| repo | string | current repo | Repository in owner/repo format |
| search | string | - | Search query (e.g., "head:copilot/") |
| state | string | all | PR state: open, closed, merged, all |
| limit | number | 100 | Maximum PRs to return |
| days | number | 30 | Days to look back |

### Output

Returns JSON array with PR objects containing:
- number, title, author, headRefName
- createdAt, updatedAt, closedAt, mergedAt
- state, url, body, labels
-->
