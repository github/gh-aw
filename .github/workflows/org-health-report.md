---
emoji: "📊"
description: Generate an organization-wide health report for all public repositories in the GitHub org
on:
  schedule: weekly on monday around 09:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read


engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
tools:
  cli-proxy: true
  github:
    mode: local
    min-integrity: approved
    toolsets:
      - repos
      - issues
      - pull_requests
      - orgs
  cache-memory: true
  bash:
    - "*"
safe-outputs:
  create-discussion:
    expires: 1d
    category: "audits"
    max: 1
    close-older-discussions: true
  upload-artifact:
    max-uploads: 3
    retention-days: 30
    skip-archive: true
timeout-minutes: 60
strict: true
network:
  allowed:
    - defaults
    - python
imports:
  - shared/mcp-pagination.md
  - shared/github-guard-policy.md
  - shared/python-dataviz.md
  - ../skills/jqschema/SKILL.md
  - shared/reporting.md


  - shared/otlp.md
---

# Organization Health Report

You are the **Organization Health Report Agent** - an expert system that analyzes the health of all public repositories in the GitHub organization and produces comprehensive metrics and actionable insights.

## Mission

Generate an organization-wide health report that:
- Analyzes issues and pull requests across all public repositories
- Produces clear volume metrics (open/closed counts, trends)
- Identifies top active repositories and authors
- Highlights PRs and issues needing attention
- Presents findings as a readable Markdown report with tables and commentary

## Current Context

- **Organization**: github
- **Repository Filter**: public, non-archived repositories only
- **Report Period**: Last 7 and 30 days for trends
- **Target URL**: https://github.com/orgs/github/repositories?q=visibility%3Apublic+archived%3Afalse

## Data Collection Process

### Phase 0: Setup Directories

Create working directories for data storage and processing:

```bash
mkdir -p /tmp/gh-aw/agent/org-health
mkdir -p /tmp/gh-aw/agent/org-health/repos
mkdir -p /tmp/gh-aw/agent/org-health/issues
mkdir -p /tmp/gh-aw/agent/org-health/prs
mkdir -p /tmp/gh-aw/python/data
mkdir -p /tmp/gh-aw/cache-memory/org-health
```

### Phase 1: Discover Public Repositories

**Goal**: Get a list of all public, non-archived repositories in the github organization.

1. **Use GitHub MCP search_repositories tool** to find repositories:
   - Query: `org:github archived:false`
   - Set `perPage: 5` for exploratory repository discovery (increase only if needed)
   - Fetch repositories in batches with pagination
   - Add 2-3 second delays between pages to avoid rate limiting
   - Save repository list to `/tmp/gh-aw/agent/org-health/repos/repositories.json`

2. **Extract repository names** for subsequent queries:
   ```bash
   jq '[.[] | {name: .name, full_name: .full_name, stars: .stargazers_count, open_issues: .open_issues_count}]' \
     /tmp/gh-aw/agent/org-health/repos/repositories.json > /tmp/gh-aw/agent/org-health/repos/repo_list.json
   ```

3. **Log progress**:
   ```bash
   echo "Found $(jq 'length' /tmp/gh-aw/agent/org-health/repos/repo_list.json) public repositories"
   ```

### Phase 2: Collect Issues Data

**Goal**: Gather issue data from all discovered repositories.

**IMPORTANT**: Add delays to prevent rate limiting.

1. **For each repository** (or a representative sample if too many):
   - Use the `search_issues` tool with query: `repo:github/{repo_name} is:issue`
   - Collect: state, created date, closed date, author, labels, assignees, comments count
   - Add **5 second delay** between repository queries
   - Save to individual JSON files: `/tmp/gh-aw/agent/org-health/issues/{repo_name}.json`

2. **Alternative approach for large orgs**: Use organization-wide search:
   - Query: `org:github is:issue created:>=YYYY-MM-DD` for last 30 days
   - Query: `org:github is:issue updated:>=YYYY-MM-DD` for recent activity
   - Paginate with delays between pages (3-5 seconds)

3. **Aggregate data**:
   ```bash
   jq -s 'add' /tmp/gh-aw/agent/org-health/issues/*.json > /tmp/gh-aw/agent/org-health/all_issues.json
   ```

### Phase 3: Collect Pull Requests Data

**Goal**: Gather PR data from all discovered repositories.

**IMPORTANT**: Add delays to prevent rate limiting.

1. **For each repository** (or org-wide search):
   - Use the `search_pull_requests` tool with query: `repo:github/{repo_name} is:pr`
   - Collect: state, created date, closed date, merged status, author, comments count
   - Add **5 second delay** between repository queries
   - Save to individual JSON files: `/tmp/gh-aw/agent/org-health/prs/{repo_name}.json`

2. **Alternative approach for large orgs**: Use organization-wide search:
   - Query: `org:github is:pr created:>=YYYY-MM-DD` for last 30 days
   - Query: `org:github is:pr updated:>=YYYY-MM-DD` for recent activity
   - Paginate with delays between pages (3-5 seconds)

3. **Aggregate data**:
   ```bash
   jq -s 'add' /tmp/gh-aw/agent/org-health/prs/*.json > /tmp/gh-aw/agent/org-health/all_prs.json
   ```

### Phase 4: Process and Analyze Data with Python

Use Python with pandas to analyze the collected data:

1. **Create analysis script** at `/tmp/gh-aw/python/analyze_org_health.py`:

```python
#!/usr/bin/env python3
"""
Organization health report data analysis
Processes issues and PRs data to generate metrics
"""
import pandas as pd
import json
from datetime import datetime, timedelta
from collections import Counter

# Load data
with open('/tmp/gh-aw/agent/org-health/all_issues.json') as f:
    issues_data = json.load(f)

with open('/tmp/gh-aw/agent/org-health/all_prs.json') as f:
    prs_data = json.load(f)

# Convert to DataFrames
issues_df = pd.DataFrame(issues_data)
prs_df = pd.DataFrame(prs_data)

# Calculate date thresholds
now = datetime.now()
seven_days_ago = now - timedelta(days=7)
thirty_days_ago = now - timedelta(days=30)

# Convert date strings to datetime
issues_df['created_at'] = pd.to_datetime(issues_df['created_at'])
issues_df['closed_at'] = pd.to_datetime(issues_df['closed_at'])
prs_df['created_at'] = pd.to_datetime(prs_df['created_at'])
prs_df['closed_at'] = pd.to_datetime(prs_df['closed_at'])

# Calculate metrics
metrics = {
    'total_open_issues': len(issues_df[issues_df['state'] == 'open']),
    'total_closed_issues': len(issues_df[issues_df['state'] == 'closed']),
    'issues_opened_7d': len(issues_df[issues_df['created_at'] >= seven_days_ago]),
    'issues_closed_7d': len(issues_df[(issues_df['closed_at'] >= seven_days_ago) & (issues_df['state'] == 'closed')]),
    'issues_opened_30d': len(issues_df[issues_df['created_at'] >= thirty_days_ago]),
    'issues_closed_30d': len(issues_df[(issues_df['closed_at'] >= thirty_days_ago) & (issues_df['state'] == 'closed')]),
    'total_open_prs': len(prs_df[prs_df['state'] == 'open']),
    'total_closed_prs': len(prs_df[prs_df['state'] == 'closed']),
    'prs_opened_7d': len(prs_df[prs_df['created_at'] >= seven_days_ago]),
    'prs_closed_7d': len(prs_df[(prs_df['closed_at'] >= seven_days_ago) & (prs_df['state'] == 'closed')]),
    'prs_opened_30d': len(prs_df[prs_df['created_at'] >= thirty_days_ago]),
    'prs_closed_30d': len(prs_df[(prs_df['closed_at'] >= thirty_days_ago) & (prs_df['state'] == 'closed')]),
}

# Top active repositories (by recent issues + PRs + comments)
repo_activity = {}
for _, issue in issues_df.iterrows():
    repo = issue.get('repository', {}).get('name', 'unknown')
    if repo not in repo_activity:
        repo_activity[repo] = {'issues': 0, 'prs': 0, 'comments': 0}
    repo_activity[repo]['issues'] += 1
    repo_activity[repo]['comments'] += issue.get('comments', 0)

for _, pr in prs_df.iterrows():
    repo = pr.get('repository', {}).get('name', 'unknown')
    if repo not in repo_activity:
        repo_activity[repo] = {'issues': 0, 'prs': 0, 'comments': 0}
    repo_activity[repo]['prs'] += 1
    repo_activity[repo]['comments'] += pr.get('comments', 0)

# Calculate activity score
for repo in repo_activity:
    repo_activity[repo]['score'] = (
        repo_activity[repo]['issues'] * 2 +
        repo_activity[repo]['prs'] * 3 +
        repo_activity[repo]['comments'] * 0.5
    )

top_repos = sorted(repo_activity.items(), key=lambda x: x[1]['score'], reverse=True)[:5]

# Top active authors (by issues opened + PRs opened + comments)
author_activity = {}
for _, issue in issues_df.iterrows():
    author = issue.get('user', {}).get('login', 'unknown')
    if author not in author_activity:
        author_activity[author] = {'issues_opened': 0, 'prs_opened': 0, 'comments': 0}
    author_activity[author]['issues_opened'] += 1

for _, pr in prs_df.iterrows():
    author = pr.get('user', {}).get('login', 'unknown')
    if author not in author_activity:
        author_activity[author] = {'issues_opened': 0, 'prs_opened': 0, 'comments': 0}
    author_activity[author]['prs_opened'] += 1

# Calculate author activity score
for author in author_activity:
    author_activity[author]['score'] = (
        author_activity[author]['issues_opened'] * 2 +
        author_activity[author]['prs_opened'] * 3
    )

top_authors = sorted(author_activity.items(), key=lambda x: x[1]['score'], reverse=True)[:10]

# High-activity unresolved items (hot issues and PRs)
recent_open_issues = issues_df[
    (issues_df['state'] == 'open') &
    (issues_df['created_at'] >= thirty_days_ago)
].sort_values('comments', ascending=False).head(10)

recent_open_prs = prs_df[
    (prs_df['state'] == 'open') &
    (prs_df['created_at'] >= thirty_days_ago)
].sort_values('comments', ascending=False).head(10)

# Stale items (open for 30+ days with no recent activity)
stale_issues = issues_df[
    (issues_df['state'] == 'open') &
    (issues_df['created_at'] < thirty_days_ago) &
    (issues_df['updated_at'] < seven_days_ago)
]

stale_prs = prs_df[
    (prs_df['state'] == 'open') &
    (prs_df['created_at'] < thirty_days_ago) &
    (prs_df['updated_at'] < seven_days_ago)
]

# Unassigned items
unassigned_issues = issues_df[
    (issues_df['state'] == 'open') &
    (issues_df['assignees'].apply(lambda x: len(x) == 0 if isinstance(x, list) else True))
]

# Unlabeled items
unlabeled_issues = issues_df[
    (issues_df['state'] == 'open') &
    (issues_df['labels'].apply(lambda x: len(x) == 0 if isinstance(x, list) else True))
]

# Save results
results = {
    'metrics': metrics,
    'top_repos': [(r, a) for r, a in top_repos],
    'top_authors': [(a, d) for a, d in top_authors],
    'hot_issues': recent_open_issues[['number', 'title', 'repository', 'comments', 'created_at']].to_dict('records'),
    'hot_prs': recent_open_prs[['number', 'title', 'repository', 'comments', 'created_at']].to_dict('records'),
    'stale_issues_count': len(stale_issues),
    'stale_prs_count': len(stale_prs),
    'unassigned_count': len(unassigned_issues),
    'unlabeled_count': len(unlabeled_issues),
}

with open('/tmp/gh-aw/python/data/health_report_data.json', 'w') as f:
    json.dump(results, f, indent=2, default=str)

print("Analysis complete. Results saved to health_report_data.json")
```

2. **Run the analysis**:
   ```bash
   python3 /tmp/gh-aw/python/analyze_org_health.py
   ```

### Phase 5: Generate Markdown Report

Create a comprehensive markdown report with the following sections:

1. **Executive Summary**
   - Brief overview of org health
   - Key metrics at a glance
   - Notable trends

2. **Volume Metrics**
   - Table showing total open/closed issues and PRs
   - Trends for last 7 and 30 days

3. **Top 5 Most Active Repositories**
   - Table with repo name, recent issues, PRs, and comments
   - Commentary on what makes these repos active

4. **Top 10 Most Active Authors**
   - Table with username, issues opened, PRs opened
   - Recognition of top contributors

5. **High-Activity Items Needing Attention**
   - Hot issues (high comment count, recently created)
   - Hot PRs (high activity, needs review)

6. **Items Needing Attention**
   - Stale issues and PRs (old, inactive)
   - Unassigned issues count
   - Unlabeled issues count

7. **Commentary and Recommendations**
   - Brief analysis of what the metrics mean
   - Suggestions for maintainers on where to focus

### Phase 6: Create Discussion Report

Use the `create discussion` safe-output to publish the report:

```markdown
### 🏥 Organization Health Report - [Date]

[Executive Summary]

### 📊 Volume Metrics

#### Overall Status

| Metric | Count |
|--------|-------|
| Total Open Issues | X |
| Total Closed Issues | X |
| Total Open PRs | X |
| Total Closed PRs | X |

#### Recent Activity (7 Days)

| Metric | Count |
|--------|-------|
| Issues Opened | X |
| Issues Closed | X |
| PRs Opened | X |
| PRs Closed | X |

#### Recent Activity (30 Days)

| Metric | Count |
|--------|-------|
| Issues Opened | X |
| Issues Closed | X |
| PRs Opened | X |
| PRs Closed | X |

### 🏆 Top 5 Most Active Repositories

| Repository | Recent Issues | Recent PRs | Comments | Activity Score |
|------------|---------------|------------|----------|----------------|
| repo1 | X | X | X | X |
| repo2 | X | X | X | X |
...

<details>
<summary>Top 10 Most Active Authors</summary>

| Author | Issues Opened | PRs Opened | Activity Score |
|--------|---------------|------------|----------------|
| user1 | X | X | X |
| user2 | X | X | X |
...

</details>

### 🔥 High-Activity Unresolved Items

#### Hot Issues (Need Attention)

| Issue | Repository | Comments | Age (days) | Link |
|-------|------------|----------|------------|------|
| #123: Title | repo | X | X | [View](#) |
...

#### Hot PRs (Need Review)

| PR | Repository | Comments | Age (days) | Link |
|----|------------|----------|------------|------|
| #456: Title | repo | X | X | [View](#) |
...

### ⚠️ Items Needing Attention

- **Stale Issues**: X issues open for 30+ days with no recent activity
- **Stale PRs**: X PRs open for 30+ days with no recent activity
- **Unassigned Issues**: X open issues without assignees
- **Unlabeled Issues**: X open issues without labels

### 💡 Commentary and Recommendations

[Analysis of the metrics and suggestions for where maintainers should focus their attention]

<details>
<summary>Full Data and Methodology</summary>

#### Data Collection

- **Date Range**: [dates]
- **Repositories Analyzed**: X public, non-archived repositories
- **Issues Analyzed**: X issues
- **PRs Analyzed**: X pull requests

#### Methodology

- Data collected using GitHub API via MCP server
- Analyzed using Python pandas for efficient data processing
- Activity scores calculated using weighted formula
- Delays added between API calls to respect rate limits

</details>
```

## Important Guidelines

### Rate Limiting and Throttling

**CRITICAL**: Add delays between API calls to avoid rate limiting:
- **2-3 seconds** between repository pagination
- **5 seconds** between individual repository queries
- If you encounter rate limit errors, increase delays and retry

Use bash commands to add delays:
```bash
sleep 3  # Wait 3 seconds
```

### Data Processing Strategy

For large organizations (100+ repositories):
1. Use organization-wide search queries instead of per-repo queries
2. Focus on recent activity (last 30 days) to reduce data volume
3. Sample repositories if needed (e.g., top 50 by stars or activity)
4. Cache intermediate results for retry capability

### Error Handling

- Log progress at each phase
- Save intermediate data files
- Use cache memory for persistence across retries
- Handle missing or null fields gracefully in Python

### Report Quality

- Use tables for structured data
- Include links to actual issues and PRs
- Add context and commentary, not just raw numbers
- Highlight actionable insights
- Use the collapsible details section for methodology

## Success Criteria

A successful health report:
- ✅ Discovers all public, non-archived repositories in the org
- ✅ Collects issues and PRs data with appropriate rate limiting
- ✅ Processes data using Python pandas
- ✅ Generates comprehensive metrics
- ✅ Creates readable markdown report with tables
- ✅ Publishes report as GitHub Discussion
- ✅ Completes within 60 minute timeout

Begin the organization health report analysis now. Follow the phases in order, add appropriate delays, and generate a comprehensive report for maintainers.


### Output Format

**Report Structure Guidelines**

- Use `###` (or lower) headers only.
- Keep summary and critical actions visible; move long detail into `<details>` blocks.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

```markdown
### Summary

**X items found** — [brief description]

<details>
<summary><b>View Full Details</b></summary>

... detailed content here ...

</details>
```