---
private: true
emoji: "⚡"
description: Daily project performance summary (90-day window) with trend charts using mcp-scripts
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read

sandbox:
  agent:
    id: awf
engine:
  id: pi
  model-provider: openai
model: openai/gpt-5.4
strict: true
tracker-id: daily-performance-summary
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, discussions]
safe-outputs:
  upload-asset:
    max: 3
    allowed-exts: [.png, .jpg, .jpeg, .svg]
  close-discussion:
    required-title-prefix: "[daily performance] "
timeout-minutes: 40
imports:
  - uses: shared/daily-audit-charts.md
    with:
      title-prefix: "[daily performance] "
  - shared/github-queries-mcp-script.md
  - shared/reporting.md


  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: performance_data_collected
    question: Did the agent collect performance data for the 90-day window?
  - id: summary_with_charts_created
    question: Was a daily performance summary with trend charts produced?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Project Performance Summary Generator (Using MCP Scripts)

You are an expert analyst that generates comprehensive daily performance summaries using **mcp-script tools** to query GitHub data (PRs, issues, discussions) and creates trend visualizations.

**IMPORTANT**: This workflow uses mcp-script tools imported from `shared/github-queries-mcp-script.md`. All data gathering MUST be done through these tools.

## Mission

Generate a daily performance summary analyzing the last 90 days of project activity:
1. **Use mcp-script tools** to query PRs, issues, and discussions
2. Calculate key performance metrics (velocity, resolution times, activity levels)
3. Generate trend charts showing project activity and performance
4. Create a discussion with the comprehensive performance report
5. Close previous daily performance discussions

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Report Period**: Last 90 days (updated daily)

## Phase 1: Gather Data Using Safe-Input Tools

**CRITICAL**: Use the mcp-script tools to query GitHub data. These tools are imported from `shared/github-queries-mcp-script.md` and provide the same functionality as the previous Skillz-based approach.

### Available Safe-Input Tools

The following tools are available for querying GitHub data:
- **github-pr-query** - Query pull requests with jq filtering
- **github-issue-query** - Query issues with jq filtering  
- **github-discussion-query** - Query discussions with jq filtering

### 1.1 Query Pull Requests

Calculate one UTC `window_start` timestamp exactly 90 days before the run and use the same value in every query and Python filter. Do not count closed or merged items only because their `updatedAt` is inside the window; the query tools use `updatedAt` for pagination, so lifecycle metrics must be filtered again by `mergedAt` or `closedAt`.

**Use the `github-pr-query` mcp-script tool** to get PR data:

```
github-pr-query with state: "all", since: "<window_start>", jq: "."
github-pr-query with state: "open", limit: 1000, jq: "."
```

Save the rolling-window candidate output to `/tmp/gh-aw/python/data/prs.json` and the current open backlog output to `/tmp/gh-aw/python/data/open_prs.json`.

The tool provides:
- PRs updated since `window_start` for the activity candidate set; filter totals by `createdAt >= window_start` and merged throughput by `mergedAt >= window_start` in analysis
- Current open PR backlog from the separate open query
- Time to merge for merged PRs
- Authors contributing PRs
- Review decision distribution

### 1.2 Query Issues

**Use the `github-issue-query` mcp-script tool** to get issue data:

```
github-issue-query with state: "all", since: "<window_start>", jq: "."
github-issue-query with state: "open", limit: 1000, jq: "."
```

Save the rolling-window candidate output to `/tmp/gh-aw/python/data/issues.json` and the current open backlog output to `/tmp/gh-aw/python/data/open_issues.json`.

The tool provides:
- Issues updated since `window_start` for the activity candidate set; filter totals by `createdAt >= window_start` and resolved throughput by `closedAt >= window_start` in analysis
- Current open issue backlog from the separate open query
- Time to close for closed issues
- Label distribution
- Authors creating issues

### 1.3 Query Discussions

**Use the `github-discussion-query` mcp-script tool** to get discussion data:

```
github-discussion-query with since: "<UTC timestamp exactly 90 days before this run>", jq: "."
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
open_prs = load_json_data(f'{DATA_DIR}/open_prs.json')
issues = load_json_data(f'{DATA_DIR}/issues.json')
open_issues = load_json_data(f'{DATA_DIR}/open_issues.json')
discussions = load_json_data(f'{DATA_DIR}/discussions.json')

# Calculate metrics
now = pd.Timestamp.now(tz='UTC')
ninety_days_ago = now - timedelta(days=90)

# PR metrics
pr_df = pd.DataFrame(prs) if prs else pd.DataFrame()
open_pr_count = len(open_prs)
if not pr_df.empty:
    pr_df['createdAt'] = pd.to_datetime(pr_df['createdAt'], utc=True)
    pr_df['mergedAt'] = pd.to_datetime(pr_df['mergedAt'], utc=True)
    
    created_prs = pr_df[pr_df['createdAt'] >= ninety_days_ago].copy()
    merged_prs = pr_df[pr_df['mergedAt'].notna() & (pr_df['mergedAt'] >= ninety_days_ago)].copy()
    merged_prs['time_to_merge'] = merged_prs['mergedAt'] - merged_prs['createdAt']
    avg_merge_time = merged_prs['time_to_merge'].mean() if len(merged_prs) > 0 else timedelta(0)
    
    pr_metrics = {
        'total': len(created_prs),
        'merged': len(merged_prs),
        'open': open_pr_count,
        'avg_merge_time_hours': avg_merge_time.total_seconds() / 3600 if avg_merge_time else 0,
        'unique_authors': created_prs['author'].apply(lambda x: x.get('login') if isinstance(x, dict) else x).nunique()
    }
else:
    pr_metrics = {'total': 0, 'merged': 0, 'open': open_pr_count, 'avg_merge_time_hours': 0, 'unique_authors': 0}

# Issue metrics
issue_df = pd.DataFrame(issues) if issues else pd.DataFrame()
open_issue_count = len(open_issues)
if not issue_df.empty:
    issue_df['createdAt'] = pd.to_datetime(issue_df['createdAt'], utc=True)
    issue_df['closedAt'] = pd.to_datetime(issue_df['closedAt'], utc=True)
    
    created_issues = issue_df[issue_df['createdAt'] >= ninety_days_ago].copy()
    closed_issues = issue_df[issue_df['closedAt'].notna() & (issue_df['closedAt'] >= ninety_days_ago)].copy()
    closed_issues['time_to_close'] = closed_issues['closedAt'] - closed_issues['createdAt']
    avg_close_time = closed_issues['time_to_close'].mean() if len(closed_issues) > 0 else timedelta(0)
    
    issue_metrics = {
        'total': len(created_issues),
        'open': open_issue_count,
        'closed': len(closed_issues),
        'avg_close_time_hours': avg_close_time.total_seconds() / 3600 if avg_close_time else 0
    }
else:
    issue_metrics = {'total': 0, 'open': open_issue_count, 'closed': 0, 'avg_close_time_hours': 0}

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

Upload all three charts as assets:
1. Call the `upload_asset` safe-output tool for each chart using its absolute path:
   - `/tmp/gh-aw/python/charts/activity_overview.png`
   - `/tmp/gh-aw/python/charts/resolution_metrics.png`
   - `/tmp/gh-aw/python/charts/velocity_metrics.png`
2. Record the returned asset URLs for each chart

## Phase 5: Close Previous Discussions

Before creating the new discussion, find and close previous daily performance discussions:

1. Search for discussions with title prefix "[daily performance]"
2. Close each found discussion with reason "OUTDATED"
3. Add a closing comment: "This discussion has been superseded by a newer daily performance report."

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

![Activity Overview](URL_FROM_UPLOAD_ASSET_1)

[Brief 2-3 sentence analysis of activity distribution across PRs, issues, and discussions]

<details>
<summary>📊 Detailed Benchmark Results</summary>

#### 🎯 Resolution Metrics

![Resolution Metrics](URL_FROM_UPLOAD_ASSET_2)

[Analysis of PR merge rates and issue resolution rates]

#### ⚡ Velocity Metrics

![Velocity Metrics](URL_FROM_UPLOAD_ASSET_3)

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
- ✅ **Query data using mcp-script tools** (github-pr-query, github-issue-query, github-discussion-query)
- ✅ Calculate comprehensive performance metrics from tool output
- ✅ Generate 3 high-quality trend charts
- ✅ Upload charts as assets
- ✅ Close previous daily performance discussions
- ✅ Create a new discussion with the complete report

## Safe-Input Tools Usage Reminder

This workflow uses mcp-script tools imported from `shared/github-queries-mcp-script.md`:
1. Tools are defined in the shared workflow with shell script implementations
2. Set `window_start` to the exact UTC timestamp 90 days before the run; the tools paginate by `updatedAt` through that boundary rather than using a record cap
3. Use `createdAt >= window_start` for total PR/issue activity, `mergedAt >= window_start` for merged PR metrics, and `closedAt >= window_start` for resolved issue metrics; do not use `updatedAt` as the final metric date
4. Query open PRs/issues separately without `since` and save them to `open_prs.json` / `open_issues.json` so backlog counts are current rather than update-window counts
5. Each tool supports jq-based filtering for efficient data querying
6. Tools are authenticated with `GITHUB_TOKEN` for GitHub API access
7. Call tools with parameters like: `github-pr-query with state: "all", since: "YYYY-MM-DDT00:00:00Z", jq: "."`

Begin your analysis now. **Use the mcp-script tools** to gather data, run Python analysis, generate charts, and create the discussion report.
