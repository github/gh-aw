#!/bin/bash
set +o histexpand

# delete-old-copilot-branches.sh - Find and delete old copilot/* branches
#
# This script identifies copilot/* branches that:
# - Have a closed or merged PR, OR have no PR at all
# - Last commit is at least 7 days old
#
# The script previews deletion commands by default; --delete executes them directly.
#
# Usage:
#   ./scripts/delete-old-copilot-branches.sh [--delete]
#
# Requirements:
#   - GitHub CLI (gh) installed and authenticated
#   - git installed
#   - jq installed
#   - Run from the repository root directory
#
# Environment Variables:
#   GITHUB_TOKEN - Optional. GitHub token for authentication (useful in CI/CD)
#   MAX_BRANCHES - Optional. Maximum number of branches to delete (default: unlimited)
#
# Exit codes:
#   0 - Success
#   1 - Error (missing dependencies, git errors, etc.)

set -euo pipefail

# Show usage if --help or -h is passed
if [[ "${1:-}" == "--help" ]] || [[ "${1:-}" == "-h" ]]; then
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
fi

delete_branches=false
if [ "$#" -eq 1 ] && [ "$1" = "--delete" ]; then
    delete_branches=true
elif [ "$#" -ne 0 ]; then
    echo "Usage: $0 [--delete]" >&2
    exit 1
fi

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Check for required dependencies
if ! command -v gh &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI (gh) is not installed${NC}" >&2
    echo "Install it from: https://cli.github.com/" >&2
    exit 1
fi

if ! command -v git &> /dev/null; then
    echo -e "${RED}Error: git is not installed${NC}" >&2
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo -e "${RED}Error: jq is not installed${NC}" >&2
    exit 1
fi

# Ensure we're authenticated with GitHub CLI
# In CI environments, GITHUB_TOKEN may be set
if [ -n "${GITHUB_TOKEN:-}" ]; then
    export GH_TOKEN="$GITHUB_TOKEN"
fi

if ! gh auth status &> /dev/null; then
    echo -e "${RED}Error: Not authenticated with GitHub CLI${NC}" >&2
    echo "Run: gh auth login" >&2
    echo "Or set GITHUB_TOKEN environment variable" >&2
    exit 1
fi

echo "Finding copilot/* branches with closed/merged PRs or no PR (last commit 7+ days old)..."
echo ""

# Get current time in seconds since epoch
current_time=$(date +%s)
seven_days_ago=$((current_time - 604800))

# Get max branches limit from environment variable (default: unlimited)
# Convert to integer to handle float values from GitHub Actions
max_branches_raw=${MAX_BRANCHES:-0}
max_branches=${max_branches_raw%.*}

# Track branches to delete
branches_to_delete=()

# Fetch latest remote information
echo "Fetching remote branches..."
git fetch origin --prune &> /dev/null || {
    echo -e "${RED}Error: Failed to fetch from remote${NC}" >&2
    exit 1
}

# Get all remote copilot/* branches
remote_branches=$(git for-each-ref --format='%(refname:strip=3)' refs/remotes/origin/copilot/)

if [ -z "$remote_branches" ]; then
    echo -e "${YELLOW}No copilot/* branches found${NC}"
    exit 0
fi

echo -e "${BLUE}Found $(echo "$remote_branches" | wc -l) copilot/* branch(es)${NC}"
echo ""

# Process each branch
while IFS= read -r branch; do
    # Skip empty lines
    [ -z "$branch" ] && continue
    
    echo -e "${BLUE}Checking: ${NC}$branch"
    
    # Get the last commit date for this branch
    commit_date=$(git log -1 --format=%ct "origin/$branch" 2>/dev/null || echo "0")
    
    if [ "$commit_date" = "0" ]; then
        echo -e "  ${YELLOW}⚠️  Could not determine commit date, skipping${NC}"
        echo ""
        continue
    fi
    
    # Check if commit is at least 7 days old
    if [ "$commit_date" -ge "$seven_days_ago" ]; then
        commit_age_days=$(( (current_time - commit_date) / 86400 ))
        echo -e "  ${YELLOW}⏱️  Last commit is only ${commit_age_days} day(s) old (< 7 days), skipping${NC}"
        echo ""
        continue
    fi
    
    # Calculate age in days for display
    commit_age_days=$(( (current_time - commit_date) / 86400 ))
    echo -e "  ${GREEN}✓ Last commit is ${commit_age_days} day(s) old${NC}"
    
    # Check PR status using GitHub CLI
    # Search for PRs from this branch
    if ! pr_status=$(gh pr list --head "$branch" --state all --json number,state,headRefName \
        | jq -r --arg branch "$branch" '.[0] | select(.headRefName == $branch) | .state // empty'); then
        echo -e "${RED}Error: Failed to check PR status for $branch${NC}" >&2
        exit 1
    fi
    
    should_delete=false
    
    if [ -z "$pr_status" ]; then
        # No PR found - include for deletion
        echo -e "  ${GREEN}✓ No PR found${NC}"
        should_delete=true
    elif [ "$pr_status" = "CLOSED" ] || [ "$pr_status" = "MERGED" ]; then
        # PR is closed or merged - include for deletion
        echo -e "  ${GREEN}✓ PR is ${pr_status}${NC}"
        should_delete=true
    else
        # PR is still open - skip
        echo -e "  ${YELLOW}⚠️  PR is ${pr_status}, skipping${NC}"
    fi
    
    if [ "$should_delete" = true ]; then
        # Check if we've reached the max branches limit
        if [ "$max_branches" -gt 0 ] && [ ${#branches_to_delete[@]} -ge "$max_branches" ]; then
            echo -e "  ${YELLOW}⚠️  Max branches limit ($max_branches) reached, skipping${NC}"
        else
            branches_to_delete+=("$branch")
            echo -e "  ${GREEN}→ Will be deleted${NC}"
        fi
    fi
    
    echo ""
done <<< "$remote_branches"

# Output deletion commands
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ ${#branches_to_delete[@]} -eq 0 ]; then
    echo -e "${GREEN}No branches to delete${NC}"
    echo ""
    echo "All copilot/* branches either:"
    echo "  - Have open PRs"
    echo "  - Have recent commits (< 7 days old)"
    if [ "$max_branches" -gt 0 ]; then
        echo "  - Or the max branches limit ($max_branches) was reached"
    fi
else
    echo -e "${GREEN}Found ${#branches_to_delete[@]} branch(es) to delete"
    if [ "$max_branches" -gt 0 ]; then
        echo -e " (limited to $max_branches)"
    fi
    echo -e ":${NC}"
    echo ""
    
    echo "# Commands to delete branches (preview):"
    echo ""
    
    for branch in "${branches_to_delete[@]}"; do
        printf 'git push origin --delete -- %q\n' "$branch"
    done
    
    echo ""
    if [ "$delete_branches" = true ]; then
        deletion_failed=false
        for branch in "${branches_to_delete[@]}"; do
            if ! git push origin --delete -- "$branch"; then
                echo -e "${RED}Error: Failed to delete branch $branch${NC}" >&2
                deletion_failed=true
            fi
        done
        if [ "$deletion_failed" = true ]; then
            exit 1
        fi
    else
        echo -e "${YELLOW}Note: Review the preview above, then re-run with --delete to delete branches${NC}"
    fi
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

exit 0
