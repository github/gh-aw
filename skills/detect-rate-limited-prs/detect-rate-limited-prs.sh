#!/bin/bash
# Detect PRs that failed due to GitHub API rate limiting
#
# Usage: ./detect-rate-limited-prs.sh [OPTIONS]
#
# Options:
#   --repo OWNER/REPO    Repository to query (default: current repo)
#   --since DATE         Only check PRs created after this date (ISO 8601)
#   --pr NUMBER          Analyze a specific PR number
#   --state STATE        PR state: open, closed, all (default: all)
#   --limit N            Maximum number of PRs to check (default: 50)

set -e

# Default values
REPO=""
SINCE_DATE=""
SPECIFIC_PR=""
STATE="all"
LIMIT=50

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --repo)
            REPO="$2"
            shift 2
            ;;
        --since)
            SINCE_DATE="$2"
            shift 2
            ;;
        --pr)
            SPECIFIC_PR="$2"
            shift 2
            ;;
        --state)
            STATE="$2"
            shift 2
            ;;
        --limit)
            LIMIT="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

# Function to check if a PR has rate limit indicators in its timeline
check_pr_for_rate_limiting() {
    local pr_number=$1
    local repo_arg=""
    
    if [[ -n "$REPO" ]]; then
        repo_arg="--repo $REPO"
    fi
    
    # Get the repository owner and name
    if [[ -n "$REPO" ]]; then
        OWNER=$(echo "$REPO" | cut -d'/' -f1)
        REPO_NAME=$(echo "$REPO" | cut -d'/' -f2)
    else
        OWNER=$(gh repo view --json owner --jq '.owner.login')
        REPO_NAME=$(gh repo view --json name --jq '.name')
    fi
    
    # Use GraphQL to fetch PR timeline with comments
    TIMELINE_RESULT=$(gh api graphql -f query="
query {
  repository(owner: \"$OWNER\", name: \"$REPO_NAME\") {
    pullRequest(number: $pr_number) {
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
")
    
    # Extract PR info
    PR_TITLE=$(echo "$TIMELINE_RESULT" | jq -r '.data.repository.pullRequest.title')
    PR_STATE=$(echo "$TIMELINE_RESULT" | jq -r '.data.repository.pullRequest.state')
    PR_CREATED=$(echo "$TIMELINE_RESULT" | jq -r '.data.repository.pullRequest.createdAt')
    PR_URL=$(echo "$TIMELINE_RESULT" | jq -r '.data.repository.pullRequest.url')
    
    # Search for rate limit indicators in comments
    RATE_LIMIT_COMMENTS=$(echo "$TIMELINE_RESULT" | jq -r '
        .data.repository.pullRequest.timelineItems.nodes[] |
        select(.body != null) |
        select(.body | test("rate limit|API rate limit|secondary rate limit|abuse detection|429|too many requests"; "i")) |
        {
            author: .author.login,
            body: .body,
            created_at: .createdAt
        }
    ')
    
    if [[ -n "$RATE_LIMIT_COMMENTS" && "$RATE_LIMIT_COMMENTS" != "null" ]]; then
        # Extract the first rate limit message
        FIRST_COMMENT=$(echo "$RATE_LIMIT_COMMENTS" | jq -s '.[0]')
        ERROR_MESSAGE=$(echo "$FIRST_COMMENT" | jq -r '.body' | head -c 200)
        DETECTED_AT=$(echo "$FIRST_COMMENT" | jq -r '.created_at')
        
        # Output the rate-limited PR info
        jq -n \
            --arg number "$pr_number" \
            --arg title "$PR_TITLE" \
            --arg state "$PR_STATE" \
            --arg created_at "$PR_CREATED" \
            --arg detected_at "$DETECTED_AT" \
            --arg error_message "$ERROR_MESSAGE" \
            --arg url "$PR_URL" \
            '{
                number: ($number | tonumber),
                title: $title,
                state: $state,
                created_at: $created_at,
                rate_limit_detected_at: $detected_at,
                error_message: $error_message,
                url: $url
            }'
        return 0
    else
        return 1
    fi
}

# If specific PR is provided, just check that one
if [[ -n "$SPECIFIC_PR" ]]; then
    echo "Checking PR #$SPECIFIC_PR for rate limiting..." >&2
    
    RESULT=$(check_pr_for_rate_limiting "$SPECIFIC_PR")
    
    if [[ $? -eq 0 ]]; then
        echo "$RESULT" | jq -s '{
            rate_limited_prs: .,
            total_examined: 1,
            total_rate_limited: 1
        }'
    else
        echo "No rate limiting detected in PR #$SPECIFIC_PR" >&2
        jq -n '{
            rate_limited_prs: [],
            total_examined: 1,
            total_rate_limited: 0
        }'
    fi
    exit 0
fi

# Build search query for multiple PRs
SEARCH_QUERY="is:pr"

# Add state filter
if [[ "$STATE" != "all" ]]; then
    SEARCH_QUERY="$SEARCH_QUERY is:$STATE"
fi

# Add date filter
if [[ -n "$SINCE_DATE" ]]; then
    SEARCH_QUERY="$SEARCH_QUERY created:>$SINCE_DATE"
fi

# Add Copilot author filter (common source of rate-limited PRs)
SEARCH_QUERY="$SEARCH_QUERY author:app/copilot-swe-agent"

# Add repo filter
if [[ -n "$REPO" ]]; then
    SEARCH_QUERY="$SEARCH_QUERY repo:$REPO"
fi

echo "Searching for PRs: $SEARCH_QUERY" >&2

# Search for PRs
if [[ -n "$REPO" ]]; then
    PRS=$(gh pr list --repo "$REPO" --state "$STATE" --limit "$LIMIT" --json number --jq '.[].number')
else
    PRS=$(gh pr list --state "$STATE" --limit "$LIMIT" --json number --jq '.[].number')
fi

if [[ -z "$PRS" ]]; then
    echo "No PRs found matching criteria" >&2
    jq -n '{
        rate_limited_prs: [],
        total_examined: 0,
        total_rate_limited: 0
    }'
    exit 0
fi

# Check each PR for rate limiting
RATE_LIMITED_PRS=()
TOTAL_EXAMINED=0

for pr_number in $PRS; do
    echo "Checking PR #$pr_number..." >&2
    TOTAL_EXAMINED=$((TOTAL_EXAMINED + 1))
    
    if RESULT=$(check_pr_for_rate_limiting "$pr_number" 2>/dev/null); then
        RATE_LIMITED_PRS+=("$RESULT")
        echo "  ✗ Rate limiting detected in PR #$pr_number" >&2
    else
        echo "  ✓ No rate limiting in PR #$pr_number" >&2
    fi
done

# Output final results
TOTAL_RATE_LIMITED=${#RATE_LIMITED_PRS[@]}

if [[ $TOTAL_RATE_LIMITED -eq 0 ]]; then
    jq -n \
        --arg examined "$TOTAL_EXAMINED" \
        '{
            rate_limited_prs: [],
            total_examined: ($examined | tonumber),
            total_rate_limited: 0
        }'
else
    # Combine all rate-limited PRs into a JSON array
    printf '%s\n' "${RATE_LIMITED_PRS[@]}" | jq -s \
        --arg examined "$TOTAL_EXAMINED" \
        --arg rate_limited "$TOTAL_RATE_LIMITED" \
        '{
            rate_limited_prs: .,
            total_examined: ($examined | tonumber),
            total_rate_limited: ($rate_limited | tonumber)
        }'
fi
