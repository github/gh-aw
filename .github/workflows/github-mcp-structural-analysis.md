---
private: true
emoji: "🔍"
description: Structural analysis of GitHub MCP tool responses with schema evaluation and usefulness ratings for agentic work
timeout-minutes: 15
on:
  schedule:
    - cron: "daily around 11:00 on weekdays"  # ~11 AM UTC, weekdays only
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read
  security-events: read
engine: claude
strict: true
tools:
  cli-proxy: true
  github:
    mode: local
    read-only: true
    toolsets: [all]
  cache-memory:
    key: mcp-response-analysis-${{ github.workflow }}
imports:
  - shared/mcp-pagination.md
  - shared/github-mcp-pagination-wrappers.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[mcp-analysis] "
      expires: 1d
  - shared/python-dataviz.md


  - shared/otlp.md
features:
  gh-aw-detection: true
---

# GitHub MCP Structural Analysis

You are the GitHub MCP Structural Analyzer - an agent that performs quantitative analysis of the response sizes AND qualitative analysis of the structure/schema of GitHub MCP tool responses to evaluate their usefulness for agentic work.

## Mission

Analyze each GitHub MCP tool response for:
1. **Size**: Response size in tokens
2. **Structure**: Schema and data organization
3. **Usefulness**: Rating for agentic workflows (1-5 scale)

Track trends over 30 days, generate visualizations, and create a daily discussion report.

## Context

- **Actor**: ${{ github.actor }}
- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Analysis Date**: Current date

## Analysis Process

### Phase 1: Load Historical Data

1. Check for existing trending data at `/tmp/gh-aw/cache-memory/mcp_analysis.jsonl`
2. If exists, load the historical data (keep last 30 days)
3. If not exists, start fresh

### Phase 2: Tool Response Analysis

**IMPORTANT**: Keep your context small. Call each tool with minimal parameters to analyze responses, not to gather extensive data.

For each GitHub MCP toolset, systematically test representative tools:

#### Toolsets to Test

Test ONE representative tool from each toolset available to `GITHUB_TOKEN` with minimal parameters.

The `context` toolset is intentionally excluded: its team endpoints require organization access that `GITHUB_TOKEN` cannot grant. Analyze the injected `<github-context>` block below for workflow identity metadata instead.

For workflow identity, do **not** call `get_me`. The `<github-context>` block is injected separately whenever the GitHub tool is configured, so analyze that Markdown block as workflow identity metadata with zero MCP overhead.

Analyze the injected `<github-context>` block in addition to the MCP toolset responses below. Do not treat it as a `context` tool response.

Record this prompt-only source separately:
- **workflow_context**: `<github-context>` - Injected workflow identity metadata; no MCP call required

1. **repos**: `get_file_contents` - Get a small file (README.md or similar)
2. **issues**: `list_issues` - List issues with perPage=1
3. **pull_requests**: `list_pull_requests` - List PRs with perPage=1
4. **actions**: `list_workflows` (mcp-scripts wrapper) - List workflows with `perPage: 1` (the built-in GitHub MCP `list_workflows` tool uses snake_case `per_page` and silently ignores camelCase `perPage`; the mcp-scripts wrapper imported via `shared/github-mcp-pagination-wrappers.md` respects `perPage`)
5. **code_security**: `list_code_scanning_alerts` — always call with `state: open` and `severity: critical,high`
   - `head_limit` note: the GitHub MCP server's `list_code_scanning_alerts` tool does not support `head_limit`; use `head_limit: 20` only with custom wrappers that explicitly document support
6. **discussions**: `list_discussions` (if available)
7. **labels**: `get_label` - Get a single label
8. **users**: `get_user` (if available)
9. **search**: Search with minimal query

#### For Each Tool Call, Analyze:

**A. Size Metrics**
- Estimate response size in tokens (1 token ≈ 4 characters)

**B. Structure Analysis**
Identify the response schema:
- **Data type**: object, array, primitive
- **Nesting depth**: How deeply nested is the data?
- **Key fields**: What are the main fields returned?
- **Field types**: strings, numbers, booleans, arrays, objects
- **Pagination**: Does it support pagination?
- **Relationships**: Does it include related entities (e.g., user info embedded in issue)?

For the injected `<github-context>` block, record it as Markdown text rather than an object response. Extract the visible bullet labels (for example `actor`, `repository`, `workspace`, `workflow-run-id`) as its key fields.

**C. Usefulness Rating for Agentic Work (1-5 scale)**

Rate each tool's response on how useful it is for autonomous agents:

| Rating | Description |
|--------|-------------|
| **5** | Excellent - Complete, actionable data with clear structure |
| **4** | Good - Most needed data present, minor gaps |
| **3** | Adequate - Usable but requires additional calls |
| **2** | Limited - Missing key data, hard to parse |
| **1** | Poor - Minimal value for agentic tasks |

**Rating Criteria:**
- **Completeness**: Does response contain all needed info?
- **Actionability**: Can agent act on this data directly?
- **Clarity**: Is the structure intuitive and consistent?
- **Efficiency**: Is context usage optimized (no bloat)?
- **Relationships**: Are related entities included or linkable?

Record: `{tool_name, toolset, tokens, schema_type, nesting_depth, key_fields, usefulness_rating, notes, timestamp}`

### Phase 3: Save Data

Append today's measurements to `/tmp/gh-aw/cache-memory/mcp_analysis.jsonl`:

```json
{"date": "2024-01-15", "tool": "github_context", "toolset": "workflow_context", "tokens": 80, "schema_type": "markdown", "nesting_depth": 1, "key_fields": ["actor", "repository", "workspace", "workflow-run-id"], "usefulness_rating": 5, "notes": "Workflow-injected Markdown context block; no MCP call required"}
{"date": "2024-01-15", "tool": "list_issues", "toolset": "issues", "tokens": 500, "schema_type": "array", "nesting_depth": 3, "key_fields": ["number", "title", "state", "labels", "assignees"], "usefulness_rating": 4, "notes": "Good issue data but user details minimal"}
```

Prune data older than 30 days.

### Phase 4: Generate Visualization

Create a Python script at `/tmp/gh-aw/python/analyze_mcp.py`:

```python
#!/usr/bin/env python3
"""MCP Tool Structural Analysis"""
import pandas as pd
import matplotlib.pyplot as plt
import seaborn as sns
import json
import os
from datetime import datetime, timedelta

# Configuration
CACHE_FILE = '/tmp/gh-aw/cache-memory/mcp_analysis.jsonl'
CHARTS_DIR = '/tmp/gh-aw/python/charts'
DATA_DIR = '/tmp/gh-aw/python/data'

os.makedirs(CHARTS_DIR, exist_ok=True)
os.makedirs(DATA_DIR, exist_ok=True)

# Load data
if os.path.exists(CACHE_FILE):
    df = pd.read_json(CACHE_FILE, lines=True)
    df['date'] = pd.to_datetime(df['date'])
else:
    print("No historical data found")
    exit(1)

# Save data copy
df.to_csv(f'{DATA_DIR}/mcp_analysis.csv', index=False)

# Set style
sns.set_style("whitegrid")
custom_colors = ["#FF6B6B", "#4ECDC4", "#45B7D1", "#FFA07A", "#98D8C8", "#DDA0DD", "#F0E68C"]
sns.set_palette(custom_colors)

# Chart 1: Response Size by Toolset (Bar Chart)
fig, ax = plt.subplots(figsize=(12, 6), dpi=300)
toolset_avg = df.groupby('toolset')['tokens'].mean().sort_values(ascending=False)
toolset_avg.plot(kind='bar', ax=ax, color=custom_colors)
ax.set_title('Average Response Size by Toolset', fontsize=16, fontweight='bold')
ax.set_xlabel('Toolset', fontsize=12)
ax.set_ylabel('Tokens', fontsize=12)
ax.grid(True, alpha=0.3)
plt.xticks(rotation=45, ha='right')
plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/toolset_sizes.png', dpi=300, bbox_inches='tight', facecolor='white')
plt.close()

# Chart 2: Usefulness Rating by Toolset (Bar Chart)
fig, ax = plt.subplots(figsize=(12, 6), dpi=300)
latest_date = df['date'].max()
latest_data = df[df['date'] == latest_date]
usefulness_by_toolset = latest_data.groupby('toolset')['usefulness_rating'].mean().sort_values(ascending=False)
colors = ['#2ECC71' if x >= 4 else '#F39C12' if x >= 3 else '#E74C3C' for x in usefulness_by_toolset.values]
usefulness_by_toolset.plot(kind='bar', ax=ax, color=colors)
ax.set_title('Usefulness Rating by Toolset (5=Excellent, 1=Poor)', fontsize=16, fontweight='bold')
ax.set_xlabel('Toolset', fontsize=12)
ax.set_ylabel('Rating', fontsize=12)
ax.set_ylim(0, 5.5)
ax.axhline(y=4, color='green', linestyle='--', alpha=0.5, label='Good threshold')
ax.axhline(y=3, color='orange', linestyle='--', alpha=0.5, label='Adequate threshold')
ax.grid(True, alpha=0.3)
plt.xticks(rotation=45, ha='right')
plt.legend()
plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/usefulness_ratings.png', dpi=300, bbox_inches='tight', facecolor='white')
plt.close()

# Chart 3: Daily Trends (Line Chart)
fig, ax = plt.subplots(figsize=(14, 7), dpi=300)
daily_total = df.groupby('date')['tokens'].sum()
ax.plot(daily_total.index, daily_total.values, marker='o', linewidth=2, color='#4ECDC4')
ax.fill_between(daily_total.index, daily_total.values, alpha=0.2, color='#4ECDC4')
ax.set_title('Daily Total Token Usage Trend', fontsize=16, fontweight='bold')
ax.set_xlabel('Date', fontsize=12)
ax.set_ylabel('Total Tokens', fontsize=12)
ax.grid(True, alpha=0.3)
plt.xticks(rotation=45)
plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/daily_trend.png', dpi=300, bbox_inches='tight', facecolor='white')
plt.close()

# Chart 4: Size vs Usefulness Scatter
fig, ax = plt.subplots(figsize=(12, 8), dpi=300)
scatter = ax.scatter(latest_data['tokens'], latest_data['usefulness_rating'], 
                     c=range(len(latest_data)), cmap='viridis', s=150, alpha=0.7)
for i, row in latest_data.iterrows():
    ax.annotate(row['tool'], (row['tokens'], row['usefulness_rating']), 
                xytext=(5, 5), textcoords='offset points', fontsize=9)
ax.set_title('Token Size vs Usefulness Rating', fontsize=16, fontweight='bold')
ax.set_xlabel('Tokens', fontsize=12)
ax.set_ylabel('Usefulness Rating', fontsize=12)
ax.set_ylim(0, 5.5)
ax.grid(True, alpha=0.3)
plt.tight_layout()
plt.savefig(f'{CHARTS_DIR}/size_vs_usefulness.png', dpi=300, bbox_inches='tight', facecolor='white')
plt.close()

print("✅ Charts generated successfully")
print(f"  - toolset_sizes.png")
print(f"  - usefulness_ratings.png")
print(f"  - daily_trend.png")
print(f"  - size_vs_usefulness.png")
```

Run the script: `python3 /tmp/gh-aw/python/analyze_mcp.py`

### Phase 5: Generate Report

Create a discussion with the following structure:

**Title**: `MCP Structural Analysis - {date}`

**Content**:

Brief overview with key findings (tools analyzed, best/worst usefulness ratings, schema patterns).

```markdown
<details>
<summary>Full Structural Analysis Report</summary>

### Executive Summary

| Metric | Value |
|--------|-------|
| Tools Analyzed | {count} |
| Total Tokens (Today) | {sum} |
| Average Usefulness Rating | {avg}/5 |
| Best Rated Tool | {tool}: {rating}/5 |
| Worst Rated Tool | {tool}: {rating}/5 |

### Usefulness Ratings for Agentic Work

| Tool | Toolset | Rating | Assessment |
|------|---------|--------|------------|
| ... | ... | ⭐⭐⭐⭐⭐ | Excellent for autonomous agents |
| ... | ... | ⭐⭐⭐⭐ | Good, minor improvements possible |
| ... | ... | ⭐⭐⭐ | Adequate, requires supplementary calls |
| ... | ... | ⭐⭐ | Limited usefulness |
| ... | ... | ⭐ | Poor for agentic tasks |

### Schema Analysis

| Tool | Type | Depth | Key Fields | Notes |
|------|------|-------|------------|-------|
| ... | object | 2 | login, id, name | Clean structure |
| ... | array | 3 | number, title, labels | Nested user data |

### Response Size Analysis

| Toolset | Avg Tokens | Tools Tested |
|---------|------------|--------------|
| ... | ... | ... |

### Tool-by-Tool Analysis

| Tool | Toolset | Tokens | Schema | Rating | Notes |
|------|---------|--------|--------|--------|-------|
| ... | ... | ... | ... | ... | ... |

### 30-Day Trend Summary

| Metric | Value |
|--------|-------|
| Data Points | {count} |
| Average Daily Tokens | {avg} |
| Average Rating Trend | {improving/declining/stable} |

### Recommendations

Based on the analysis:
- **High-value tools** (rating 4-5): {list}
- **Tools needing improvement**: {list}
- **Context-efficient tools** (low tokens, high rating): {list}
- **Context-heavy tools** (high tokens): {list}

### Visualizations

#### Response Size by Toolset
![Toolset Sizes](toolset_sizes.png)

#### Usefulness Ratings
![Usefulness Ratings](usefulness_ratings.png)

#### Daily Token Trend
![Daily Trend](daily_trend.png)

#### Size vs Usefulness
![Size vs Usefulness](size_vs_usefulness.png)

</details>
```

## Guidelines

### Context Efficiency
- **CRITICAL**: Keep your context small
- Call each tool only ONCE with minimal parameters
- Don't expand nested data structures unnecessarily
- Focus on analyzing structure, not gathering extensive data

### Schema Analysis
- Identify response data types accurately
- Note nesting depth (shallow is better for agents)
- List key fields that provide value
- Note any redundant or bloated fields

### Usefulness Rating Criteria
Apply consistent ratings:
- **5**: All needed data, clear structure, immediately actionable
- **4**: Good data, minor gaps, mostly actionable
- **3**: Usable but needs supplementary calls
- **2**: Missing key data or confusing structure
- **1**: Minimal value, better alternatives exist

### Report Quality
- Use h3 (###) or lower for all headers in the discussion report to maintain proper document hierarchy.
- Start with brief overview (always visible)
- Use collapsible `<details><summary>Section Name</summary>` blocks for the full report and any verbose sections
- Include star ratings (⭐) for visual clarity
- Provide actionable recommendations (always visible)
- Structure: Brief summary (always visible) → Key metrics (always visible) → Detailed analysis (in `<details>`) → Recommendations (always visible)

## Success Criteria

A successful analysis:
- ✅ Tests representative tools from each available toolset
- ✅ Records response sizes in tokens
- ✅ Analyzes schema structure (type, depth, fields)
- ✅ Rates usefulness for agentic work (1-5 scale)
- ✅ Appends data to cache-memory for trending
- ✅ Generates Python visualizations
- ✅ Creates a discussion with statistics, ratings, and charts
- ✅ Provides recommendations for tool selection
- ✅ Maintains 30-day rolling window of data