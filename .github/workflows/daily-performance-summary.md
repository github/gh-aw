---
description: Daily project performance summary (90-day window) with trend charts using safe-inputs
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read
engine: codex
strict: true
tracker-id: daily-performance-summary
features:
  dangerous-permissions-write: true
tools:
  github:
    toolsets: [default, discussions]
safe-inputs:
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
safe-outputs:
  upload-asset:
  create-discussion:
    expires: 3d
    category: "general"
    title-prefix: "[daily performance] "
    max: 1
    close-older-discussions: true
  close-discussion:
    max: 10
timeout-minutes: 30
imports:
  - shared/github-queries-safe-input.md
  - shared/trending-charts-simple.md
  - shared/reporting.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Project Performance Summary Generator (Using Safe Inputs)

You are an expert analyst that generates comprehensive daily performance summaries using **safe-input tools** to query GitHub data (PRs, issues, discussions) and creates trend visualizations.

**IMPORTANT**: This workflow uses safe-input tools imported from `shared/github-queries-safe-input.md`. All data gathering MUST be done through these tools.

## Mission

Generate a daily performance summary analyzing the last 90 days of project activity:
1. **Use safe-input tools** to query PRs, issues, and discussions
2. Calculate key performance metrics (velocity, resolution times, activity levels)
3. Generate trend charts showing project activity and performance
4. Create a discussion with the comprehensive performance report
5. Close previous daily performance discussions

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Report Period**: Last 90 days (updated daily)

## Phase 1: Gather Data Using Safe-Input Tools

**CRITICAL**: Use the safe-input tools to query GitHub data. These tools are imported from `shared/github-queries-safe-input.md` and provide the same functionality as the previous Skillz-based approach.

### Available Safe-Input Tools

The following tools are available for querying GitHub data:
- **github-pr-query** - Query pull requests with jq filtering
- **github-issue-query** - Query issues with jq filtering  
- **github-discussion-query** - Query discussions with jq filtering

### 1.1 Query Pull Requests

**Use the `github-pr-query` safe-input tool** to get PR data:

```
github-pr-query with state: "all", limit: 1000, jq: "."
```

The tool provides:
- PR count by state (open, closed, merged)
- Time to merge for merged PRs
- Authors contributing PRs
- Review decision distribution

### 1.2 Query Issues

**Use the `github-issue-query` safe-input tool** to get issue data:

```
github-issue-query with state: "all", limit: 1000, jq: "."
```

The tool provides:
- Issue count by state (open, closed)
- Time to close for closed issues
- Label distribution
- Authors creating issues

### 1.3 Query Discussions

**Use the `github-discussion-query` safe-input tool** to get discussion data:

```
github-discussion-query with limit: 1000, jq: "."
```

The tool provides:
- Discussion count by category
- Answered vs unanswered discussions
- Active discussion authors

## Phase 2: Python Analysis

Create Python scripts to analyze the gathered data and calculate metrics.

### Setup Data Directory

```bash
mkdir -p /tmp/gh-aw/python/data
mkdir -p /tmp/gh-aw/python/charts
```

### Analysis Script

Create a Python analysis script:

```python
#!/usr/bin/env python3
"""
Monthly Performance Analysis
Analyzes PRs, issues, and discussions to generate performance metrics
"""
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
import seaborn as sns
from datetime import datetime, timedelta
import json
import os

# Configuration
CHARTS_DIR = '/tmp/gh-aw/python/charts'
DATA_DIR = '/tmp/gh-aw/python/data'
os.makedirs(CHARTS_DIR, exist_ok=True)
os.makedirs(DATA_DIR, exist_ok=True)

# Set visualization style
sns.set_style("whitegrid")
sns.set_palette("husl")

def load_json_data(filepath):
    """Load JSON data from file"""
    if os.path.exists(filepath):
        with open(filepath, 'r') as f:
            return json.load(f)
    return []

# Load data
prs = load_json_data(f'{DATA_DIR}/prs.json')
issues = load_json_data(f'{DATA_DIR}/issues.json')
discussions = load_json_data(f'{DATA_DIR}/discussions.json')

# Calculate metrics
now = datetime.now()
ninety_days_ago = now - timedelta(days=90)

# PR metrics
pr_df = pd.DataFrame(prs) if prs else pd.DataFrame()
if not pr_df.empty:
    pr_df['createdAt'] = pd.to_datetime(pr_df['createdAt'])
    pr_df['mergedAt'] = pd.to_datetime(pr_df['mergedAt'])
    
    merged_prs = pr_df[pr_df['mergedAt'].notna()]
    merged_prs['time_to_merge'] = merged_prs['mergedAt'] - merged_prs['createdAt']
    avg_merge_time = merged_prs['time_to_merge'].mean() if len(merged_prs) > 0 else timedelta(0)
    
    pr_metrics = {
        'total': len(pr_df),
        'merged': len(merged_prs),
        'open': len(pr_df[pr_df['state'] == 'OPEN']),
        'avg_merge_time_hours': avg_merge_time.total_seconds() / 3600 if avg_merge_time else 0,
        'unique_authors': pr_df['author'].apply(lambda x: x.get('login') if isinstance(x, dict) else x).nunique()
    }
else:
    pr_metrics = {'total': 0, 'merged': 0, 'open': 0, 'avg_merge_time_hours': 0, 'unique_authors': 0}

# Issue metrics
issue_df = pd.DataFrame(issues) if issues else pd.DataFrame()
if not issue_df.empty:
    issue_df['createdAt'] = pd.to_datetime(issue_df['createdAt'])
    issue_df['closedAt'] = pd.to_datetime(issue_df['closedAt'])
    
    closed_issues = issue_df[issue_df['closedAt'].notna()]
    closed_issues['time_to_close'] = closed_issues['closedAt'] - closed_issues['createdAt']
    avg_close_time = closed_issues['time_to_close'].mean() if len(closed_issues) > 0 else timedelta(0)
    
    issue_metrics = {
        'total': len(issue_df),
        'open': len(issue_df[issue_df['state'] == 'OPEN']),
        'closed': len(closed_issues),
        'avg_close_time_hours': avg_close_time.total_seconds() / 3600 if avg_close_time else 0
    }
else:
    issue_metrics = {'total': 0, 'open': 0, 'closed': 0, 'avg_close_time_hours': 0}

# Discussion metrics
discussion_df = pd.DataFrame(discussions) if discussions else pd.DataFrame()
if not discussion_df.empty:
    discussion_metrics = {
        'total': len(discussion_df),
        'answered': len(discussion_df[discussion_df['answer'].notna()]) if 'answer' in discussion_df.columns else 0
    }
else:
    discussion_metrics = {'total': 0, 'answered': 0}

# Save metrics
all_metrics = {
    'prs': pr_metrics,
    'issues': issue_metrics,
    'discussions': discussion_metrics,
    'generated_at': now.isoformat()
}
with open(f'{DATA_DIR}/metrics.json', 'w') as f:
    json.dump(all_metrics, f, indent=2, default=str)

print("Metrics calculated and saved!")
print(json.dumps(all_metrics, indent=2, default=str))
```

## Phase 3: Generate Trend Charts

Generate exactly **3 high-quality charts**:

### Chart 1: Activity Overview

Create a bar chart showing activity across PRs, Issues, and Discussions:

```python
#!/usr/bin/env python3
"""Activity Overview Chart"""
import matplotlib.pyplot as plt
import seaborn as sns
import json
import os

CHARTS_DIR = '/tmp/gh-aw/python/charts'
DATA_DIR = '/tmp/gh-aw/python/data'

# Load metrics
with open(f'{DATA_DIR}/metrics.json', 'r') as f:
    metrics = json.load(f)

# Create activity overview chart
sns.set_style("whitegrid")
fig, ax = plt.subplots(figsize=(12, 7), dpi=300)

categories = ['Pull Requests', 'Issues', 'Discussions']
totals = [
    metrics['prs']['total'],
    metrics['issues']['total'],
    metrics['discussions']['total']
]

colors = ['#4ECDC4', '#FF6B6B', '#45B7D1']
bars = ax.bar(categories, totals, color=colors, edgecolor='white', linewidth=2)

# Add value labels on bars
for bar, value in zip(bars, totals):
    ax.text(bar.get_x() + bar.get_width()/2, bar.get_height() + 0.5,
            str(value), ha='center', va='bottom', fontsize=14, fontweight='bold')

ax.set_title('Monthly Activity Overview', fontsize=18, fontweight='bold', pad=20)
ax.set_ylabel('Count', fontsize=14)
ax.set_xlabel('Category', fontsize=14)
ax.grid(True, alpha=0.3, axis='y')

plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/activity_overview.png', dpi=300, bbox_inches='tight', facecolor='white')
print("Activity overview chart saved!")
```

### Chart 2: PR and Issue Resolution Metrics

Create a chart showing merge times and resolution rates:

```python
#!/usr/bin/env python3
"""Resolution Metrics Chart"""
import matplotlib.pyplot as plt
import seaborn as sns
import json
import os

CHARTS_DIR = '/tmp/gh-aw/python/charts'
DATA_DIR = '/tmp/gh-aw/python/data'

with open(f'{DATA_DIR}/metrics.json', 'r') as f:
    metrics = json.load(f)

sns.set_style("whitegrid")
fig, axes = plt.subplots(1, 2, figsize=(14, 6), dpi=300)

# Chart 2a: PR Status Distribution
pr_data = [metrics['prs']['merged'], metrics['prs']['open']]
pr_labels = ['Merged', 'Open']
colors = ['#2ECC71', '#E74C3C']
axes[0].pie(pr_data, labels=pr_labels, colors=colors, autopct='%1.1f%%',
            startangle=90, explode=(0.05, 0), textprops={'fontsize': 12})
axes[0].set_title('PR Status Distribution', fontsize=14, fontweight='bold')

# Chart 2b: Issue Status Distribution
issue_data = [metrics['issues']['closed'], metrics['issues']['open']]
issue_labels = ['Closed', 'Open']
colors = ['#3498DB', '#F39C12']
axes[1].pie(issue_data, labels=issue_labels, colors=colors, autopct='%1.1f%%',
            startangle=90, explode=(0.05, 0), textprops={'fontsize': 12})
axes[1].set_title('Issue Status Distribution', fontsize=14, fontweight='bold')

fig.suptitle('Resolution Metrics', fontsize=18, fontweight='bold', y=1.02)
plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/resolution_metrics.png', dpi=300, bbox_inches='tight', facecolor='white')
print("Resolution metrics chart saved!")
```

### Chart 3: Performance Trends (Velocity Metrics)

```python
#!/usr/bin/env python3
"""Performance Velocity Chart"""
import matplotlib.pyplot as plt
import seaborn as sns
import json
import os

CHARTS_DIR = '/tmp/gh-aw/python/charts'
DATA_DIR = '/tmp/gh-aw/python/data'

with open(f'{DATA_DIR}/metrics.json', 'r') as f:
    metrics = json.load(f)

sns.set_style("whitegrid")
fig, ax = plt.subplots(figsize=(12, 7), dpi=300)

# Velocity metrics
categories = ['Avg PR Merge Time\n(hours)', 'Avg Issue Close Time\n(hours)', 'PR Authors', 'Discussion Answer Rate\n(%)']
values = [
    round(metrics['prs']['avg_merge_time_hours'], 1),
    round(metrics['issues']['avg_close_time_hours'], 1),
    metrics['prs']['unique_authors'],
    round(metrics['discussions']['answered'] / max(metrics['discussions']['total'], 1) * 100, 1)
]

colors = ['#9B59B6', '#1ABC9C', '#E67E22', '#3498DB']
bars = ax.barh(categories, values, color=colors, edgecolor='white', linewidth=2)

# Add value labels
for bar, value in zip(bars, values):
    ax.text(bar.get_width() + 0.5, bar.get_y() + bar.get_height()/2,
            str(value), ha='left', va='center', fontsize=12, fontweight='bold')

ax.set_title('Performance Velocity Metrics', fontsize=18, fontweight='bold', pad=20)
ax.set_xlabel('Value', fontsize=14)
ax.grid(True, alpha=0.3, axis='x')

plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/velocity_metrics.png', dpi=300, bbox_inches='tight', facecolor='white')
print("Velocity metrics chart saved!")
```

## Phase 4: Upload Charts

Use the `upload asset` tool to upload all three charts:
1. Upload `/tmp/gh-aw/python/charts/activity_overview.png`
2. Upload `/tmp/gh-aw/python/charts/resolution_metrics.png`
3. Upload `/tmp/gh-aw/python/charts/velocity_metrics.png`

Collect the returned URLs for embedding in the discussion.

## Phase 5: Close Previous Discussions

Before creating the new discussion, find and close previous daily performance discussions:

1. Search for discussions with title prefix "[daily performance]"
2. Close each found discussion with reason "OUTDATED"
3. Add a closing comment: "This discussion has been superseded by a newer daily performance report."

## Phase 5.5: Report Formatting Guidelines

**CRITICAL**: Follow these formatting guidelines to create well-structured, readable reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy.**

The discussion title serves as h1, so all content headers should start at h3:
- Use `###` for main sections (e.g., "### Performance Overview", "### Key Metrics")
- Use `####` for subsections (e.g., "#### Pull Requests", "#### Issues")
- Never use `##` (h2) or `#` (h1) in the report body

### 2. Progressive Disclosure
**Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling.**

Use collapsible sections for:
- Detailed benchmark results and performance data
- Full performance metrics tables
- Verbose statistics and historical comparisons
- Technical implementation details

Example:
```markdown
<details>
<summary><b>Detailed Benchmark Results</b></summary>

[Long performance data...]

</details>
```

### 3. Report Structure Pattern

Your report should follow this structure for optimal readability:

1. **Performance Overview** (always visible): Brief executive summary highlighting overall project health, key achievements, and critical issues
2. **Key Highlights** (always visible): Most important metrics and trends that stakeholders need to see immediately
3. **Critical Performance Issues** (always visible): Any problems that require immediate attention with severity indicators
4. **Detailed Benchmark Results** (in `<details>` tags): Comprehensive performance data, metrics tables, and historical comparisons
5. **Optimization Recommendations** (always visible): Actionable insights and suggested improvements

### Design Principles

Create reports that:
- **Build trust through clarity**: Most important info (overview, critical issues, recommendations) immediately visible
- **Exceed expectations**: Add helpful context, trends, comparisons to give stakeholders the full picture
- **Create delight**: Use progressive disclosure to reduce overwhelm while keeping details accessible
- **Maintain consistency**: Follow the same patterns as other reporting workflows in this repository

## Phase 6: Create Discussion Report

Create a new discussion with the comprehensive performance report.

### Discussion Format

**Title**: `[daily performance] Daily Performance Summary - YYYY-MM-DD`

**Body**:

```markdown
### Performance Overview

[Brief 2-3 paragraph executive summary highlighting overall project health and activity levels]

### Key Highlights

- ✅ **[NUMBER]** PRs merged in the last 90 days
- 📊 **[NUMBER]** issues resolved  
- 💬 **[NUMBER]** discussions answered
- ⏱️ Average merge time: **[HOURS]** hours
- 👥 **[NUMBER]** unique contributors

### 🚨 Critical Performance Issues

[List any critical issues that require immediate attention. If none, state "No critical performance issues detected."]

### 📈 Activity Overview

![Activity Overview](URL_FROM_UPLOAD_ASSET_CHART_1)

[Brief 2-3 sentence analysis of activity distribution across PRs, issues, and discussions]

<details>
<summary><b>📊 Detailed Benchmark Results</b></summary>

#### 🎯 Resolution Metrics

![Resolution Metrics](URL_FROM_UPLOAD_ASSET_CHART_2)

[Analysis of PR merge rates and issue resolution rates]

#### ⚡ Velocity Metrics

![Velocity Metrics](URL_FROM_UPLOAD_ASSET_CHART_3)

[Analysis of response times, contributor activity, and discussion engagement]

#### 📊 Complete Performance Data

##### Pull Requests
| Metric | Value |
|--------|-------|
| Total PRs | [NUMBER] |
| Merged | [NUMBER] |
| Open | [NUMBER] |
| Avg Merge Time | [HOURS] hours |
| Unique Contributors | [NUMBER] |

##### Issues
| Metric | Value |
|--------|-------|
| Total Issues | [NUMBER] |
| Closed | [NUMBER] |
| Open | [NUMBER] |
| Avg Resolution Time | [HOURS] hours |

##### Discussions
| Metric | Value |
|--------|-------|
| Total Discussions | [NUMBER] |
| Answered | [NUMBER] |
| Answer Rate | [PERCENT]% |

#### 📈 Historical Comparisons

[Compare with previous periods if historical data is available]

</details>

### 💡 Optimization Recommendations

1. [Actionable recommendation based on the data]
2. [Specific suggestion for improvement]
3. [Priority action item if needed]

---
*Report generated automatically by the Daily Performance Summary workflow*
*Data source: ${{ github.repository }} - Last 90 days*
*Powered by **Safe-Input Tools** - GitHub queries exposed as MCP tools*
```

## Success Criteria

A successful run will:
- ✅ **Query data using safe-input tools** (github-pr-query, github-issue-query, github-discussion-query)
- ✅ Calculate comprehensive performance metrics from tool output
- ✅ Generate 3 high-quality trend charts
- ✅ Upload charts as assets
- ✅ Close previous daily performance discussions
- ✅ Create a new discussion with the complete report

## Safe-Input Tools Usage Reminder

This workflow uses safe-input tools imported from `shared/github-queries-safe-input.md`:
1. Tools are defined in the shared workflow with shell script implementations
2. Each tool supports jq-based filtering for efficient data querying
3. Tools are authenticated with `GITHUB_TOKEN` for GitHub API access
4. Call tools with parameters like: `github-pr-query with state: "all", limit: 1000, jq: "."`

Begin your analysis now. **Use the safe-input tools** to gather data, run Python analysis, generate charts, and create the discussion report.