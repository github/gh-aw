---
mcp-scripts:
  github-issue-query:
    description: "Query GitHub issues with jq filtering support. Without --jq, returns schema and data size info. Use --jq '.' to get all data, or specific jq expressions to filter."
    inputs:
      repo:
        type: string
        description: "Repository in owner/repo format (defaults to current repository)"
        required: false
      state:
        type: string
        description: "Issue state: open, closed, all (default: open)"
        required: false
      limit:
        type: number
        description: "Maximum number of issues to fetch (default: 30)"
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_REPOSITORY: ${{ github.repository }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      STATE="${INPUT_STATE:-open}"
      LIMIT="${INPUT_LIMIT:-30}"
      JQ_FILTER="${INPUT_JQ:-}"
      
      # JSON fields to fetch
      JSON_FIELDS="number,title,state,author,createdAt,updatedAt,closedAt,body,labels,assignees,comments,milestone,url"
      
      # Build and execute gh command
      if [[ -n "$REPO" ]]; then
        OUTPUT=$(gh issue list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" --repo "$REPO")
      else
        OUTPUT=$(gh issue list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS")
      fi
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" <<< "$OUTPUT"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' <<< "$OUTPUT")
        DATA_SIZE=${#OUTPUT}
        
        # Validate values are numeric
        if ! [[ "$ITEM_COUNT" =~ ^[0-9]+$ ]]; then
          ITEM_COUNT=0
        fi
        if ! [[ "$DATA_SIZE" =~ ^[0-9]+$ ]]; then
          DATA_SIZE=0
        fi
        
        cat << EOF
      {
        "message": "No --jq filter provided. Use --jq to filter and retrieve data.",
        "item_count": $ITEM_COUNT,
        "data_size_bytes": $DATA_SIZE,
        "schema": {
          "type": "array",
          "description": "Array of issue objects",
          "item_fields": {
            "number": "integer - Issue number",
            "title": "string - Issue title",
            "state": "string - Issue state (OPEN, CLOSED)",
            "author": "object - Author info with login field",
            "createdAt": "string - ISO timestamp of creation",
            "updatedAt": "string - ISO timestamp of last update",
            "closedAt": "string|null - ISO timestamp of close",
            "body": "string - Issue body content",
            "labels": "array - Array of label objects with name field",
            "assignees": "array - Array of assignee objects with login field",
            "comments": "object - Comments info with totalCount field",
            "milestone": "object|null - Milestone info with title field",
            "url": "string - Issue URL"
          }
        },
        "suggested_queries": [
          {"description": "Get all data", "query": "."},
          {"description": "Get issue numbers and titles", "query": ".[] | {number, title}"},
          {"description": "Get open issues only", "query": ".[] | select(.state == \"OPEN\")"},
          {"description": "Get issues by author", "query": ".[] | select(.author.login == \"USERNAME\")"},
          {"description": "Get issues with label", "query": ".[] | select(.labels | map(.name) | index(\"bug\"))"},
          {"description": "Get issues with many comments", "query": ".[] | select(.comments.totalCount > 5) | {number, title, comments: .comments.totalCount}"},
          {"description": "Count by state", "query": "group_by(.state) | map({state: .[0].state, count: length})"}
        ]
      }
      EOF
      fi

  github-pr-query:
    description: "Query GitHub pull requests with jq filtering support. Without --jq, returns schema and data size info. Use --jq '.' to get all data, or specific jq expressions to filter."
    inputs:
      repo:
        type: string
        description: "Repository in owner/repo format (defaults to current repository)"
        required: false
      state:
        type: string
        description: "PR state: open, closed, merged, all (default: open)"
        required: false
      limit:
        type: number
        description: "Maximum number of PRs to fetch (default: 30)"
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_REPOSITORY: ${{ github.repository }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      STATE="${INPUT_STATE:-open}"
      LIMIT="${INPUT_LIMIT:-30}"
      JQ_FILTER="${INPUT_JQ:-}"
      
      # JSON fields to fetch
      JSON_FIELDS="number,title,state,author,createdAt,updatedAt,mergedAt,closedAt,headRefName,baseRefName,isDraft,reviewDecision,additions,deletions,changedFiles,labels,assignees,reviewRequests,url"
      
      # Build and execute gh command
      if [[ -n "$REPO" ]]; then
        OUTPUT=$(gh pr list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" --repo "$REPO")
      else
        OUTPUT=$(gh pr list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS")
      fi
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" <<< "$OUTPUT"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' <<< "$OUTPUT")
        DATA_SIZE=${#OUTPUT}
        
        # Validate values are numeric
        if ! [[ "$ITEM_COUNT" =~ ^[0-9]+$ ]]; then
          ITEM_COUNT=0
        fi
        if ! [[ "$DATA_SIZE" =~ ^[0-9]+$ ]]; then
          DATA_SIZE=0
        fi
        
        cat << EOF
      {
        "message": "No --jq filter provided. Use --jq to filter and retrieve data.",
        "item_count": $ITEM_COUNT,
        "data_size_bytes": $DATA_SIZE,
        "schema": {
          "type": "array",
          "description": "Array of pull request objects",
          "item_fields": {
            "number": "integer - PR number",
            "title": "string - PR title",
            "state": "string - PR state (OPEN, CLOSED, MERGED)",
            "author": "object - Author info with login field",
            "createdAt": "string - ISO timestamp of creation",
            "updatedAt": "string - ISO timestamp of last update",
            "mergedAt": "string|null - ISO timestamp of merge",
            "closedAt": "string|null - ISO timestamp of close",
            "headRefName": "string - Source branch name",
            "baseRefName": "string - Target branch name",
            "isDraft": "boolean - Whether PR is a draft",
            "reviewDecision": "string|null - Review decision (APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED)",
            "additions": "integer - Lines added",
            "deletions": "integer - Lines deleted",
            "changedFiles": "integer - Number of files changed",
            "labels": "array - Array of label objects with name field",
            "assignees": "array - Array of assignee objects with login field",
            "reviewRequests": "array - Array of review request objects",
            "url": "string - PR URL"
          }
        },
        "suggested_queries": [
          {"description": "Get all data", "query": "."},
          {"description": "Get PR numbers and titles", "query": ".[] | {number, title}"},
          {"description": "Get open PRs only", "query": ".[] | select(.state == \"OPEN\")"},
          {"description": "Get merged PRs", "query": ".[] | select(.mergedAt != null)"},
          {"description": "Get PRs by author", "query": ".[] | select(.author.login == \"USERNAME\")"},
          {"description": "Get large PRs", "query": ".[] | select(.changedFiles > 10) | {number, title, changedFiles}"},
          {"description": "Count by state", "query": "group_by(.state) | map({state: .[0].state, count: length})"}
        ]
      }
      EOF
      fi

  github-discussion-query:
    description: "Query GitHub discussions with jq filtering support. Without --jq, returns schema and data size info. Use --jq '.' to get all data, or specific jq expressions to filter."
    inputs:
      repo:
        type: string
        description: "Repository in owner/repo format (defaults to current repository)"
        required: false
      limit:
        type: number
        description: "Maximum number of discussions to fetch (default: 30)"
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_REPOSITORY: ${{ github.repository }}
      GITHUB_REPOSITORY_OWNER: ${{ github.repository_owner }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      LIMIT="${INPUT_LIMIT:-30}"
      JQ_FILTER="${INPUT_JQ:-}"
      
      # Parse repository owner and name
      if [[ -n "$REPO" ]]; then
        OWNER=$(echo "$REPO" | cut -d'/' -f1)
        NAME=$(echo "$REPO" | cut -d'/' -f2)
      else
        # Get current repository from GitHub context
        OWNER="${GITHUB_REPOSITORY_OWNER:-}"
        NAME=$(echo "${GITHUB_REPOSITORY:-}" | cut -d'/' -f2)
      fi
      
      # Validate owner and name
      if [[ -z "$OWNER" || -z "$NAME" ]]; then
        echo "Error: Could not determine repository owner and name" >&2
        exit 1
      fi
      
      # Build GraphQL query for discussions
      GRAPHQL_QUERY=$(cat <<QUERY
      {
        repository(owner: "$OWNER", name: "$NAME") {
          discussions(first: $LIMIT, orderBy: {field: CREATED_AT, direction: DESC}) {
            nodes {
              number
              title
              author {
                login
              }
              createdAt
              updatedAt
              body
              category {
                name
              }
              labels(first: 10) {
                nodes {
                  name
                }
              }
              comments {
                totalCount
              }
              answer {
                id
              }
              url
            }
          }
        }
      }
      QUERY
      )
      
      # Execute GraphQL query via gh api
      GRAPHQL_OUTPUT=$(gh api graphql -f query="$GRAPHQL_QUERY")
      
      # Transform GraphQL output to match gh discussion list format
      OUTPUT=$(echo "$GRAPHQL_OUTPUT" | jq '[.data.repository.discussions.nodes[] | {
        number: .number,
        title: .title,
        author: .author,
        createdAt: .createdAt,
        updatedAt: .updatedAt,
        body: .body,
        category: .category,
        labels: .labels.nodes,
        comments: .comments,
        answer: .answer,
        url: .url
      }]')
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" <<< "$OUTPUT"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' <<< "$OUTPUT")
        DATA_SIZE=${#OUTPUT}
        
        # Validate values are numeric
        if ! [[ "$ITEM_COUNT" =~ ^[0-9]+$ ]]; then
          ITEM_COUNT=0
        fi
        if ! [[ "$DATA_SIZE" =~ ^[0-9]+$ ]]; then
          DATA_SIZE=0
        fi
        
        cat << EOF
      {
        "message": "No --jq filter provided. Use --jq to filter and retrieve data.",
        "item_count": $ITEM_COUNT,
        "data_size_bytes": $DATA_SIZE,
        "schema": {
          "type": "array",
          "description": "Array of discussion objects",
          "item_fields": {
            "number": "integer - Discussion number",
            "title": "string - Discussion title",
            "author": "object - Author info with login field",
            "createdAt": "string - ISO timestamp of creation",
            "updatedAt": "string - ISO timestamp of last update",
            "body": "string - Discussion body content",
            "category": "object - Category info with name field",
            "labels": "array - Array of label objects with name field",
            "comments": "object - Comments info with totalCount field",
            "answer": "object|null - Accepted answer if exists",
            "url": "string - Discussion URL"
          }
        },
        "suggested_queries": [
          {"description": "Get all data", "query": "."},
          {"description": "Get discussion numbers and titles", "query": ".[] | {number, title}"},
          {"description": "Get discussions by author", "query": ".[] | select(.author.login == \"USERNAME\")"},
          {"description": "Get discussions in category", "query": ".[] | select(.category.name == \"Ideas\")"},
          {"description": "Get answered discussions", "query": ".[] | select(.answer != null)"},
          {"description": "Get unanswered discussions", "query": ".[] | select(.answer == null) | {number, title, category: .category.name}"},
          {"description": "Count by category", "query": "group_by(.category.name) | map({category: .[0].category.name, count: length})"}
        ]
      }
      EOF
      fi
---
<!--
## GitHub Queries Safe Input Tools

This shared workflow provides mcp-script tools for querying GitHub issues, pull requests, and discussions with built-in jq filtering support.

### Available Tools

1. **github-issue-query** - Query GitHub issues
2. **github-pr-query** - Query GitHub pull requests  
3. **github-discussion-query** - Query GitHub discussions

### Usage

Import this shared workflow to get access to all query tools:

```yaml
imports:
  - shared/github-queries-mcp-script.md
```

### Tool Parameters

#### github-issue-query

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| repo | string | current repo | Repository in owner/repo format |
| state | string | open | Issue state: open, closed, all |
| limit | number | 30 | Maximum issues to return |
| jq | string | - | jq filter expression (if omitted, returns schema info) |

#### github-pr-query

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| repo | string | current repo | Repository in owner/repo format |
| state | string | open | PR state: open, closed, merged, all |
| limit | number | 30 | Maximum PRs to return |
| jq | string | - | jq filter expression (if omitted, returns schema info) |

#### github-discussion-query

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| repo | string | current repo | Repository in owner/repo format |
| limit | number | 30 | Maximum discussions to return |
| jq | string | - | jq filter expression (if omitted, returns schema info) |

### Smart Schema Response

When called **without** the `jq` parameter, each tool returns:
- Schema information describing the data structure
- Item count and data size
- Suggested jq queries for common operations

This prevents overwhelming the agent with large datasets and helps understand the data structure before querying.

Use `jq: "."` to get all data, or use specific jq expressions for filtered results.

### Example Queries

**Get all open issues:**
```
github-issue-query with jq: "."
```

**Get issue numbers and titles:**
```
github-issue-query with jq: ".[] | {number, title}"
```

**Get merged PRs:**
```
github-pr-query with state: "merged", jq: "."
```

**Get PRs by author:**
```
github-pr-query with jq: ".[] | select(.author.login == \"username\")"
```

**Get unanswered discussions:**
```
github-discussion-query with jq: ".[] | select(.answer == null) | {number, title}"
```

### Output Fields

#### Issues
number, title, state, author, createdAt, updatedAt, closedAt, body, labels, assignees, comments, milestone, url

#### Pull Requests
number, title, state, author, createdAt, updatedAt, mergedAt, closedAt, headRefName, baseRefName, isDraft, reviewDecision, additions, deletions, changedFiles, labels, assignees, reviewRequests, url

#### Discussions
number, title, author, createdAt, updatedAt, body, category, labels, comments, answer, url

### Source

These tools are based on the skills scripts in `.github/skills/`:
- `.github/skills/github-issue-query/query-issues.sh`
- `.github/skills/github-pr-query/query-prs.sh`
- `.github/skills/github-discussion-query/query-discussions.sh`
-->
