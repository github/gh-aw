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
      since:
        type: string
        description: "ISO 8601 date or timestamp. When set, paginate by updatedAt until this boundary instead of applying limit."
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      STATE="${INPUT_STATE:-open}"
      LIMIT="${INPUT_LIMIT:-30}"
      SINCE="${INPUT_SINCE:-}"
      JQ_FILTER="${INPUT_JQ:-}"
      
      # JSON fields to fetch
      JSON_FIELDS="number,title,state,author,createdAt,updatedAt,closedAt,body,labels,assignees,comments,milestone,url"
      
      OUTPUT_FILE=$(mktemp)
      PAGE_OUTPUT_FILE=""
      MERGED_OUTPUT_FILE=""
      FILTERED_OUTPUT_FILE=""
      cleanup() {
        rm -f "$OUTPUT_FILE" "$PAGE_OUTPUT_FILE" "$MERGED_OUTPUT_FILE" "$FILTERED_OUTPUT_FILE"
      }
      trap cleanup EXIT

      # Fetch all items updated in a date window. REST results are ordered by
      # updated time, so stop only after reaching the requested boundary.
      if [[ -n "$SINCE" ]]; then
        if ! date -d "$SINCE" --iso-8601=seconds >/dev/null 2>&1; then
          echo "Error: since must be an ISO 8601 date or timestamp" >&2
          exit 1
        fi
        SINCE=$(date -u -d "$SINCE" '+%Y-%m-%dT%H:%M:%SZ')
        if [[ -n "$REPO" ]]; then
          API_PATH="repos/${REPO}/issues"
        else
          API_PATH="repos/${GITHUB_REPOSITORY}/issues"
        fi
        PAGE=1
        echo '[]' > "$OUTPUT_FILE"
        while :; do
          PAGE_OUTPUT_FILE=$(mktemp)
          gh api "${API_PATH}?state=${STATE}&sort=updated&direction=desc&per_page=100&page=${PAGE}" > "$PAGE_OUTPUT_FILE"
          [[ "$(jq 'length' < "$PAGE_OUTPUT_FILE")" -eq 0 ]] && { rm -f "$PAGE_OUTPUT_FILE"; PAGE_OUTPUT_FILE=""; break; }
          MERGED_OUTPUT_FILE=$(mktemp)
          jq -s '.[0] + .[1]' "$OUTPUT_FILE" "$PAGE_OUTPUT_FILE" > "$MERGED_OUTPUT_FILE"
          mv "$MERGED_OUTPUT_FILE" "$OUTPUT_FILE"
          MERGED_OUTPUT_FILE=""
          [[ "$(jq -r '.[-1].updated_at' < "$PAGE_OUTPUT_FILE")" < "$SINCE" ]] && { rm -f "$PAGE_OUTPUT_FILE"; PAGE_OUTPUT_FILE=""; break; }
          rm -f "$PAGE_OUTPUT_FILE"
          PAGE_OUTPUT_FILE=""
          PAGE=$((PAGE + 1))
        done
        FILTERED_OUTPUT_FILE=$(mktemp)
        jq --arg since "$SINCE" '[.[] | select(.pull_request == null and .updated_at >= $since) | {
          number, title, state: (.state | ascii_upcase), author: .user, createdAt: .created_at,
          updatedAt: .updated_at, closedAt: .closed_at, body, labels, assignees,
          comments: {totalCount: .comments}, milestone, url: .html_url
        }]' "$OUTPUT_FILE" > "$FILTERED_OUTPUT_FILE"
        mv "$FILTERED_OUTPUT_FILE" "$OUTPUT_FILE"
        FILTERED_OUTPUT_FILE=""
      elif [[ -n "$REPO" ]]; then
        gh issue list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" --repo "$REPO" > "$OUTPUT_FILE"
      else
        gh issue list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" > "$OUTPUT_FILE"
      fi
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" "$OUTPUT_FILE"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' < "$OUTPUT_FILE")
        DATA_SIZE=$(wc -c < "$OUTPUT_FILE" | tr -d '[:space:]')
        if [[ "$DATA_SIZE" -gt 0 ]]; then
          DATA_SIZE=$((DATA_SIZE - 1))
        fi
        
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
      since:
        type: string
        description: "ISO 8601 date or timestamp. When set, paginate by updatedAt until this boundary instead of applying limit."
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      STATE="${INPUT_STATE:-open}"
      LIMIT="${INPUT_LIMIT:-30}"
      SINCE="${INPUT_SINCE:-}"
      JQ_FILTER="${INPUT_JQ:-}"
      
      # JSON fields to fetch
      JSON_FIELDS="number,title,state,author,createdAt,updatedAt,mergedAt,closedAt,headRefName,baseRefName,isDraft,reviewDecision,additions,deletions,changedFiles,labels,assignees,reviewRequests,url"
      
      OUTPUT_FILE=$(mktemp)
      PAGE_OUTPUT_FILE=""
      MERGED_OUTPUT_FILE=""
      FILTERED_OUTPUT_FILE=""
      cleanup() {
        rm -f "$OUTPUT_FILE" "$PAGE_OUTPUT_FILE" "$MERGED_OUTPUT_FILE" "$FILTERED_OUTPUT_FILE"
      }
      trap cleanup EXIT

      # Fetch all items updated in a date window. REST results are ordered by
      # updated time, so stop only after reaching the requested boundary.
      if [[ -n "$SINCE" ]]; then
        if ! date -d "$SINCE" --iso-8601=seconds >/dev/null 2>&1; then
          echo "Error: since must be an ISO 8601 date or timestamp" >&2
          exit 1
        fi
        SINCE=$(date -u -d "$SINCE" '+%Y-%m-%dT%H:%M:%SZ')
        if [[ -n "$REPO" ]]; then
          API_PATH="repos/${REPO}/pulls"
        else
          API_PATH="repos/${GITHUB_REPOSITORY}/pulls"
        fi
        REST_STATE="$STATE"
        [[ "$STATE" == "merged" ]] && REST_STATE="closed"
        PAGE=1
        echo '[]' > "$OUTPUT_FILE"
        while :; do
          PAGE_OUTPUT_FILE=$(mktemp)
          gh api "${API_PATH}?state=${REST_STATE}&sort=updated&direction=desc&per_page=100&page=${PAGE}" > "$PAGE_OUTPUT_FILE"
          [[ "$(jq 'length' < "$PAGE_OUTPUT_FILE")" -eq 0 ]] && { rm -f "$PAGE_OUTPUT_FILE"; PAGE_OUTPUT_FILE=""; break; }
          MERGED_OUTPUT_FILE=$(mktemp)
          jq -s '.[0] + .[1]' "$OUTPUT_FILE" "$PAGE_OUTPUT_FILE" > "$MERGED_OUTPUT_FILE"
          mv "$MERGED_OUTPUT_FILE" "$OUTPUT_FILE"
          MERGED_OUTPUT_FILE=""
          [[ "$(jq -r '.[-1].updated_at' < "$PAGE_OUTPUT_FILE")" < "$SINCE" ]] && { rm -f "$PAGE_OUTPUT_FILE"; PAGE_OUTPUT_FILE=""; break; }
          rm -f "$PAGE_OUTPUT_FILE"
          PAGE_OUTPUT_FILE=""
          PAGE=$((PAGE + 1))
        done
        FILTERED_OUTPUT_FILE=$(mktemp)
        jq --arg since "$SINCE" --arg state "$STATE" '[.[] | select(.updated_at >= $since and ($state != "merged" or .merged_at != null)) | {
          number, title, state: (.state | ascii_upcase), author: .user, createdAt: .created_at,
          updatedAt: .updated_at, mergedAt: .merged_at, closedAt: .closed_at,
          headRefName: .head.ref, baseRefName: .base.ref, isDraft: .draft, labels, assignees,
          additions, deletions, changedFiles: .changed_files, url: .html_url
        }]' "$OUTPUT_FILE" > "$FILTERED_OUTPUT_FILE"
        mv "$FILTERED_OUTPUT_FILE" "$OUTPUT_FILE"
        FILTERED_OUTPUT_FILE=""
      elif [[ -n "$REPO" ]]; then
        gh pr list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" --repo "$REPO" > "$OUTPUT_FILE"
      else
        gh pr list --state "$STATE" --limit "$LIMIT" --json "$JSON_FIELDS" > "$OUTPUT_FILE"
      fi
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" "$OUTPUT_FILE"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' < "$OUTPUT_FILE")
        DATA_SIZE=$(wc -c < "$OUTPUT_FILE" | tr -d '[:space:]')
        if [[ "$DATA_SIZE" -gt 0 ]]; then
          DATA_SIZE=$((DATA_SIZE - 1))
        fi
        
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
      since:
        type: string
        description: "ISO 8601 date or timestamp. When set, paginate by updatedAt until this boundary instead of applying limit."
        required: false
      jq:
        type: string
        description: "jq filter expression to apply to output. If not provided, returns schema info instead of full data."
        required: false
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Default values
      REPO="${INPUT_REPO:-}"
      LIMIT="${INPUT_LIMIT:-30}"
      SINCE="${INPUT_SINCE:-}"
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
      
      if [[ -n "$SINCE" ]] && ! date -d "$SINCE" --iso-8601=seconds >/dev/null 2>&1; then
        echo "Error: since must be an ISO 8601 date or timestamp" >&2
        exit 1
      fi
      [[ -n "$SINCE" ]] && SINCE=$(date -u -d "$SINCE" '+%Y-%m-%dT%H:%M:%SZ')

      # Build GraphQL query for discussions
      GRAPHQL_QUERY=$(cat <<QUERY
      query(\$owner: String!, \$name: String!, \$first: Int!, \$after: String) {
        repository(owner: \$owner, name: \$name) {
          discussions(first: \$first, after: \$after, orderBy: {field: UPDATED_AT, direction: DESC}) {
            pageInfo {
              hasNextPage
              endCursor
            }
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
      
      # Paginate to the date boundary when requested; otherwise preserve the
      # existing limit-based behavior for callers that do not specify a window.
      PAGE_SIZE="$LIMIT"
      [[ -n "$SINCE" ]] && PAGE_SIZE=100
      CURSOR=""
      OUTPUT_FILE=$(mktemp)
      PAGE_NODES_FILE=""
      MERGED_OUTPUT_FILE=""
      FILTERED_OUTPUT_FILE=""
      echo '[]' > "$OUTPUT_FILE"
      cleanup() {
        rm -f "$OUTPUT_FILE" "$PAGE_NODES_FILE" "$MERGED_OUTPUT_FILE" "$FILTERED_OUTPUT_FILE"
      }
      trap cleanup EXIT
      while :; do
        GRAPHQL_ARGS=(graphql -f query="$GRAPHQL_QUERY" -f owner="$OWNER" -f name="$NAME" -F first="$PAGE_SIZE")
        [[ -n "$CURSOR" ]] && GRAPHQL_ARGS+=(-f after="$CURSOR")
        GRAPHQL_OUTPUT=$(gh api "${GRAPHQL_ARGS[@]}")
        PAGE_NODES_FILE=$(mktemp)
        echo "$GRAPHQL_OUTPUT" | jq '.data.repository.discussions.nodes' > "$PAGE_NODES_FILE"
        [[ "$(jq 'length' < "$PAGE_NODES_FILE")" -eq 0 ]] && { rm -f "$PAGE_NODES_FILE"; break; }
        MERGED_OUTPUT_FILE=$(mktemp)
        jq -s '.[0] + .[1]' "$OUTPUT_FILE" "$PAGE_NODES_FILE" > "$MERGED_OUTPUT_FILE"
        mv "$MERGED_OUTPUT_FILE" "$OUTPUT_FILE"
        MERGED_OUTPUT_FILE=""
        [[ -n "$SINCE" && "$(jq -r '.[-1].updatedAt // empty' < "$PAGE_NODES_FILE")" < "$SINCE" ]] && { rm -f "$PAGE_NODES_FILE"; break; }
        rm -f "$PAGE_NODES_FILE"
        [[ -z "$SINCE" ]] && break
        [[ "$(jq -r '.data.repository.discussions.pageInfo.hasNextPage' <<< "$GRAPHQL_OUTPUT")" == "true" ]] || break
        CURSOR=$(jq -r '.data.repository.discussions.pageInfo.endCursor' <<< "$GRAPHQL_OUTPUT")
      done

      # Transform GraphQL output to match gh discussion list format
      FILTERED_OUTPUT_FILE=$(mktemp)
      jq --arg since "$SINCE" '[.[] | select($since == "" or .updatedAt >= $since) | {
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
      }]' "$OUTPUT_FILE" > "$FILTERED_OUTPUT_FILE"
      mv "$FILTERED_OUTPUT_FILE" "$OUTPUT_FILE"
      
      # Apply jq filter if specified
      if [[ -n "$JQ_FILTER" ]]; then
        jq "$JQ_FILTER" "$OUTPUT_FILE"
      else
        # Return schema and size instead of full data
        ITEM_COUNT=$(jq 'length' < "$OUTPUT_FILE")
        DATA_SIZE=$(wc -c < "$OUTPUT_FILE" | tr -d '[:space:]')
        if [[ "$DATA_SIZE" -gt 0 ]]; then
          DATA_SIZE=$((DATA_SIZE - 1))
        fi
        
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
