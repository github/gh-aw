---
private: true
emoji: "📅"
description: Daily report analyzing repository issues with clustering, metrics, and trend charts
on: daily
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read
engine:
  id: copilot
  copilot-sdk: true
  driver: .github/drivers/copilot_sdk_driver_sample_node.cjs
max-tool-denials: 3
runs-on: aw-gpu-runner-T4
strict: true
tracker-id: daily-issues-report
sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    min-integrity: approved
    toolsets: [default, discussions]
timeout-minutes: 30
runtimes:
  node:
    version: "22"
experiments:
  output_format:
    variants: [collapsible, inline, ste]
    description: "Test whether hiding report details behind a <details> block, presenting them inline, or writing in Simplified Technical English (STE) affects discussion engagement"
    hypothesis: "H0: no change in discussion engagement score. H1: inline format produces ≥20% higher reactions+replies by making charts and recommendations immediately visible; ste format improves engagement via clearer, simpler language."
    metric: discussion_engagement_score
    secondary_metrics: [output_length_chars, run_duration_ms, "eval:output_format_adherence"]
    guardrail_metrics:
      - name: empty_output_rate
        threshold: "==0"
    min_samples: 30
    weight: [34, 33, 33]
    start_date: "2026-05-07"
    issue: 30573
    analysis_type: mann_whitney
    tags: [output, readability, engagement]
imports:
  - shared/github-guard-policy.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[daily issues] "
  - ../skills/jqschema/SKILL.md
  - shared/issues-data-fetch.md
  - shared/trends.md
  - shared/python-nlp.md

  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: report-generated
    question: Did the workflow analyze the prefetched issues data and generate a clustered daily issues report with metrics and trends?
  - id: discussion-created
    question: Was a daily issues discussion created successfully with the report findings and recommendations?
  - id: output_format_adherence
    question: Does the report match the writing style expected for the assigned output_format variant (e.g., short active-voice sentences with one fact per sentence when the variant is "ste")?
---

{{#runtime-import? .github/shared-instructions.md}}

{{#runtime-import .github/shared/editorial.md}}

# Daily Issues Report Generator

You are an expert analyst that generates comprehensive daily reports about repository issues, using Python for clustering and visualization.

## Mission

Generate a daily report analyzing up to 1000 issues from the repository (see `issues_analyzed` in scratchpad/metrics-glossary.md):
1. Cluster issues by topic/theme using natural language analysis
2. Calculate various metrics (open/closed rates, response times, label distribution)
3. Generate trend charts showing issue activity over time
4. Create a new discussion with the report
5. Close previous daily issues discussions to avoid clutter

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Date**: Generated daily at 6 AM UTC

## Phase 1: Load and Prepare Data

The issues data has been pre-fetched and is available at `/tmp/gh-aw/agent/issues-data/issues.json`.

1. **Load the issues data**:
   ```bash
   jq 'length' /tmp/gh-aw/agent/issues-data/issues.json
   ```

2. **Prepare data for Python analysis**:
   - Copy issues.json to `/tmp/gh-aw/python/data/issues.json`
   - Validate the data is properly formatted

## Phase 2: Python Analysis with Clustering

Create a Python script to analyze and cluster the issues. Use scikit-learn for clustering if available, or implement simple keyword-based clustering.

### Required Analysis

**Clustering Requirements**:
- Use TF-IDF vectorization on issue titles and bodies
- Apply K-means or hierarchical clustering
- Identify 5-10 major issue clusters/themes
- Label each cluster based on common keywords

**Metrics to Calculate** (see scratchpad/metrics-glossary.md for definitions):
- Total issues (open vs closed) - `total_issues`, `open_issues`, `closed_issues`
- Issues opened in last 7, 30 days - `issues_opened_7d`, `issues_opened_30d`
- Average time to close (for closed issues)
- Most active labels (by issue count)
- Most active authors
- Issues without labels (need triage) - `issues_without_labels`
- Issues without assignees - `issues_without_assignees`
- Stale issues (no activity in 30+ days) - `stale_issues`

### Python Script Structure

```python
#!/usr/bin/env python3
"""
Daily Issues Analysis Script
Clusters issues and generates metrics and visualizations
"""
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
import seaborn as sns
from datetime import datetime, timedelta
import json
from collections import Counter
import re

# Load issues data
with open('/tmp/gh-aw/python/data/issues.json', 'r') as f:
    issues = json.load(f)

df = pd.DataFrame(issues)

# Convert dates
df['createdAt'] = pd.to_datetime(df['createdAt'])
df['updatedAt'] = pd.to_datetime(df['updatedAt'])
df['closedAt'] = pd.to_datetime(df['closedAt'])

# Calculate basic metrics (see scratchpad/metrics-glossary.md for definitions)

# Scope: All issues in repository, no filters
total_issues = len(df)

# Scope: Issues where state = "open" at report time
open_issues = len(df[df['state'] == 'OPEN'])

# Scope: Issues where state = "closed" at report time
closed_issues = len(df[df['state'] == 'CLOSED'])

# Time-based metrics
now = datetime.now(df['createdAt'].iloc[0].tzinfo if len(df) > 0 else None)

# Scope: Issues created in last 7 days
issues_opened_7d = len(df[df['createdAt'] > now - timedelta(days=7)])

# Scope: Issues created in last 30 days
issues_opened_30d = len(df[df['createdAt'] > now - timedelta(days=30)])

# Average time to close
# Scope: Closed issues with valid timestamps
closed_df = df[df['closedAt'].notna()]
if len(closed_df) > 0:
    closed_df['time_to_close'] = closed_df['closedAt'] - closed_df['createdAt']
    avg_close_time = closed_df['time_to_close'].mean()

# Extract labels for clustering
def extract_labels(labels_list):
    if labels_list:
        return [l['name'] for l in labels_list]
    return []

df['label_names'] = df['labels'].apply(extract_labels)

# Simple keyword-based clustering from titles
def cluster_by_keywords(title):
    title_lower = title.lower() if title else ''
    if 'bug' in title_lower or 'fix' in title_lower or 'error' in title_lower:
        return 'Bug Reports'
    elif 'feature' in title_lower or 'enhancement' in title_lower or 'request' in title_lower:
        return 'Feature Requests'
    elif 'doc' in title_lower or 'readme' in title_lower:
        return 'Documentation'
    elif 'test' in title_lower:
        return 'Testing'
    elif 'refactor' in title_lower or 'cleanup' in title_lower:
        return 'Refactoring'
    elif 'security' in title_lower or 'vulnerability' in title_lower:
        return 'Security'
    elif 'performance' in title_lower or 'slow' in title_lower:
        return 'Performance'
    else:
        return 'Other'

df['cluster'] = df['title'].apply(cluster_by_keywords)

# Save metrics to JSON for report
# Note: Using standardized metric names from scratchpad/metrics-glossary.md
metrics = {
    'total_issues': total_issues,
    'open_issues': open_issues,
    'closed_issues': closed_issues,
    'issues_opened_7d': issues_opened_7d,  # Standardized name
    'issues_opened_30d': issues_opened_30d,  # Standardized name
    'cluster_counts': df['cluster'].value_counts().to_dict()
}
with open('/tmp/gh-aw/python/data/metrics.json', 'w') as f:
    json.dump(metrics, f, indent=2, default=str)
```

### Install Additional Libraries

scikit-learn, NLTK, TextBlob, and WordCloud are pre-installed via the shared NLP environment.

## Phase 3: Generate Trend Charts

Generate exactly **2 high-quality charts**:

### Chart 1: Issue Activity Trends
- **Title**: "Issue Activity - Last 30 Days"
- **Content**: 
  - Line showing issues opened per day
  - Line showing issues closed per day
  - 7-day moving average overlay
- **Save to**: `/tmp/gh-aw/python/charts/issue_activity_trends.png`

### Chart 2: Issue Distribution by Cluster
- **Title**: "Issue Clusters by Theme"
- **Chart Type**: Horizontal bar chart
- **Content**:
  - Horizontal bars showing count per cluster
  - Include cluster labels based on keywords
  - Sort by count descending
- **Save to**: `/tmp/gh-aw/python/charts/issue_clusters.png`

### Chart Quality Requirements
- DPI: 300 minimum
- Figure size: 12x7 inches
- Use seaborn styling with professional colors
- Clear labels and legend
- Grid lines for readability

## Phase 4: Upload Charts

Use the `upload asset` tool to upload both charts:
1. Upload `/tmp/gh-aw/python/charts/issue_activity_trends.png`
2. Upload `/tmp/gh-aw/python/charts/issue_clusters.png`
3. Collect the returned URLs for embedding in the discussion

## Phase 5: Create Discussion Report

Create a new discussion with the comprehensive report.

Older daily issues discussions are closed automatically by `close-older-discussions: true` via `shared/daily-audit-discussion.md`.

**Formatting Guideline**: Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy. The discussion title serves as h1, so all content headers should start at h3.

**Report Structure Guidelines**

- Use `###` (or lower) headers only.
- Keep summary and critical actions visible; move long detail into `<details>` blocks.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

### Discussion Format

**Title**: `[daily issues] Daily Issues Report - YYYY-MM-DD`

{{#if experiments.output_format == 'ste'}}
**Simplified Technical English (STE) Variant**: Write every sentence in the body using Simplified Technical English rules:
- Use short sentences. Limit each sentence to 20 words or fewer.
- Write one fact per sentence.
- Use active voice and present tense.
- Use simple, familiar words. Do not use jargon.
- Spell out each acronym on first use.
{{/if}}

**Body**:

```markdown
### Summary

**X items found** — [brief description]

Brief 2-3 paragraph summary of key findings: total issues analyzed, main clusters identified, notable trends, and any concerns that need attention.

{{#if experiments.output_format == 'collapsible'}}<details>
<summary><b>View Full Details</b></summary>{{/if}}

### 📈 Issue Activity Trends

![Issue Activity Trends](URL_FROM_UPLOAD_ASSET_CHART_1)

[2-3 sentence analysis of activity trends - peaks, patterns, recent changes]

### 🏷️ Issue Clusters by Theme

![Issue Clusters](URL_FROM_UPLOAD_ASSET_CHART_2)

[Analysis of the major clusters found and their characteristics]

### Cluster Details

| Cluster | Theme | Issue Count | Sample Issues |
|---------|-------|-------------|---------------|
| 1 | [Theme] | [Count] | #123, #456 |
| 2 | [Theme] | [Count] | #789, #101 |
| ... | ... | ... | ... |

### 📊 Key Metrics

### Volume Metrics
- **Total Issues Analyzed** (`issues_analyzed`): [NUMBER] (Scope: Last 1000 issues)
- **Open Issues** (`open_issues`): [NUMBER] ([PERCENT]%)
- **Closed Issues** (`closed_issues`): [NUMBER] ([PERCENT]%)

### Time-Based Metrics
- **Issues Opened (Last 7 Days)** (`issues_opened_7d`): [NUMBER]
- **Issues Opened (Last 30 Days)** (`issues_opened_30d`): [NUMBER]
- **Average Time to Close**: [DURATION]

### Triage Metrics
- **Issues Without Labels** (`issues_without_labels`): [NUMBER]
- **Issues Without Assignees** (`issues_without_assignees`): [NUMBER]
- **Stale Issues (30+ days)** (`stale_issues`): [NUMBER]

### 🏆 Top Labels

| Label | Issue Count |
|-------|-------------|
| [label] | [count] |
| ... | ... |

### 👥 Most Active Authors

| Author | Issues Created |
|--------|----------------|
| @[author] | [count] |
| ... | ... |

### ⚠️ Issues Needing Attention

### Stale Issues (No Activity 30+ Days)
- #[number]: [title]
- #[number]: [title]

### Unlabeled Issues
- #[number]: [title]
- #[number]: [title]

### 📝 Recommendations

1. [Specific actionable recommendation based on findings]
2. [Another recommendation]
3. [...]

{{#if experiments.output_format == 'collapsible'}}</details>{{/if}}

---
*Report generated automatically by the Daily Issues Report workflow*
*Data source: Last 1000 issues from ${{ github.repository }}*
```

## Important Guidelines

### Data Quality
- Handle missing fields gracefully (null checks)
- Validate date formats before processing
- Skip malformed issues rather than failing

### Clustering Tips
- If scikit-learn is not available, use keyword-based clustering
- Focus on meaningful themes, not just statistical clusters
- Aim for 5-10 clusters maximum for readability

### Chart Quality
- Use consistent color schemes
- Make charts readable when embedded in markdown
- Include proper axis labels and titles

### Report Quality
- Be specific with numbers and percentages
- Highlight actionable insights
- Keep the summary brief but informative

## Success Criteria

A successful run will:
- ✅ Load and analyze all available issues data
- ✅ Cluster issues into meaningful themes
- ✅ Generate 2 high-quality trend charts
- ✅ Upload charts as assets
- ✅ Close previous daily issues discussions
- ✅ Create a new discussion with comprehensive report
- ✅ Include all required metrics and visualizations

Begin your analysis now. Load the data, run the Python analysis, generate charts, and create the discussion report.