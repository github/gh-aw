---
description: Weekly portfolio analyst that identifies cost reduction opportunities (20%+) while improving workflow reliability
on:
  schedule: weekly on monday around 09:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: portfolio-analyst-weekly
engine: copilot
network:
  allowed: [python]
tools:
  agentic-workflows:
  github:
    toolsets: [default]
  bash: ["*"]
steps:
  - name: Download logs from last 30 days
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      mkdir -p /tmp/portfolio-logs
      ./gh-aw logs --start-date -30d -c 5000 -o /tmp/portfolio-logs --json > /tmp/portfolio-logs/summary.json
safe-outputs:
  create-discussion:
    title-prefix: "[portfolio] "
    category: "audits"
    close-older-discussions: true
  upload-asset:
timeout-minutes: 20
imports:
  - shared/mood.md
  - shared/reporting.md
  - shared/jqschema.md
  - shared/trending-charts-simple.md
---

# Automated Portfolio Analyst

You are an expert workflow portfolio analyst focused on identifying cost reduction opportunities while improving reliability.

## ⚠️ Critical: Pre-Downloaded Data Location

**All workflow execution data has been pre-downloaded for you in the previous workflow step.**

- **JSON Summary**: `/tmp/portfolio-logs/summary.json` - Contains all metrics and run data you need
- **Run Logs**: `/tmp/portfolio-logs/run-{database-id}/` - Individual run logs (if needed for detailed analysis)

**DO NOT call `gh aw logs` or any GitHub CLI commands** - they will not work in your environment. All data you need is in the summary.json file.

## Mission

Analyze all agentic workflows in this repository weekly to identify opportunities to reduce costs while maintaining or improving reliability. Complete the entire analysis in under 60 seconds by focusing on high-impact issues.

**Important**: Always generate a report, even with limited data. Be transparent about data limitations and adjust recommendations accordingly.

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Date**: Use `date +%Y-%m-%d` command to get current date
- **Target**: Identify all cost reduction opportunities (aim for 20%+ when data permits)
- **Time Budget**: 60 seconds

## Visualization Requirements

**Generate visual charts to create a dashboard-style report**. The report should be concise, scannable, and use charts instead of long text descriptions.

### Required Charts

Create these charts using Python with matplotlib/seaborn and save to `/tmp/gh-aw/python/charts/`:

1. **Cost Trends Chart** (`cost_trends.png`)
   - Line chart showing daily workflow costs over the last 30 days
   - Highlight the overall trend (increasing/decreasing)
   - Include 7-day moving average

2. **Top Spenders Chart** (`top_spenders.png`)
   - Horizontal bar chart of top 10 workflows by total cost
   - Show actual dollar amounts
   - Color code by health status (green=healthy, yellow=warning, red=critical)

3. **Failure Rates Chart** (`failure_rates.png`)
   - Bar chart showing workflows with >30% failure rate
   - Display failure percentage and wasted cost
   - Sort by wasted cost (highest first)

4. **Success Rate Overview** (`success_overview.png`)
   - Pie or donut chart showing overall success/failure/cancelled distribution
   - Include percentages and counts

### Chart Requirements

- **High quality**: 300 DPI, 12x7 inch figures
- **Clear labels**: Title, axis labels, legends
- **Professional styling**: Use seaborn whitegrid style
- **Consistent colors**: Use a professional color palette
- **Upload all charts** using the `upload asset` tool to get URLs
- **Embed in report**: Include charts in the discussion using markdown image syntax

### Data Preparation

Extract data from `/tmp/portfolio-logs/summary.json` and prepare it as CSV files in `/tmp/gh-aw/python/data/` before generating charts. Example:

```python
import pandas as pd
import json

# Load summary data
with open('/tmp/portfolio-logs/summary.json', 'r') as f:
    data = json.load(f)

# Prepare daily cost data
runs_df = pd.DataFrame(data['runs'])
runs_df['date'] = pd.to_datetime(runs_df['created_at']).dt.date
daily_costs = runs_df.groupby('date')['estimated_cost'].sum()
daily_costs.to_csv('/tmp/gh-aw/python/data/daily_costs.csv')
```

## Analysis Framework

### Phase 0: Important Setup Notes

**DO NOT CALL `gh aw logs` OR ANY `gh` COMMANDS** - These commands will not work in your environment and will fail.

The workflow logs have already been downloaded for you in the previous step. The data is available at:
- **JSON Summary File**: `/tmp/portfolio-logs/summary.json` (contains all metrics and run data)
- **Individual Run Logs Directory**: `/tmp/portfolio-logs/run-{database-id}/` (detailed logs for each workflow run)

All the data you need has been pre-downloaded. Read from these files instead of calling `gh` commands.

### Phase 1: Data Collection (10 seconds)

Collect execution data from the pre-downloaded logs:

```bash
# Read the pre-downloaded JSON summary (this file contains ALL the data you need)
cat /tmp/portfolio-logs/summary.json | jq '.'

# The summary.json file contains:
# - .summary: Aggregate metrics (total runs, tokens, cost, errors, warnings)
# - .runs: Array of all workflow runs with detailed metrics per run
# - .logs_location: Base directory where run logs are stored

# Get total number of runs analyzed
cat /tmp/portfolio-logs/summary.json | jq '.summary.total_runs'

# Get all runs with their metrics
cat /tmp/portfolio-logs/summary.json | jq '.runs[]'

# Get list of all agentic workflows in the repository
find .github/workflows/ -name '*.md' -type f

# Individual run logs are stored in subdirectories (if you need detailed logs)
find /tmp/portfolio-logs -type d -name "run-*"
```

**Key Metrics to Extract (from summary.json .runs array):**
- `database_id` - Unique run identifier
- `workflow_name` - Name of the workflow
- `estimated_cost` - **Real cost per run calculated from actual token usage** (field name says "estimated" but contains calculated cost from actual usage)
- `token_usage` - Actual token consumption
- `duration` - Actual runtime (formatted as string like "5m30s")
- `conclusion` - Success/failure status (success, failure, cancelled)
- `created_at` - When the run was executed (ISO 8601 timestamp)
- `error_count` - Number of errors in the run
- `warning_count` - Number of warnings in the run

**Calculate from real data:**
- Total runs in last 30 days: Use `.summary.total_runs` or count `.runs` array
- Success/failure counts: Count runs where `.conclusion` == "success" or "failure"
- Last run date: Find latest `.created_at` timestamp
- Monthly cost: Use `.summary.total_cost` (sum of all runs' estimated_cost)
- Average cost per run: `.summary.total_cost / .summary.total_runs`

**Triage Early:**
- Skip workflows with 100% success rate, normal frequency, and last run < 7 days
- Focus 80% of analysis time on top 20% of issues

**Handling Limited Data:**
- If limited data (< 10 workflow runs), acknowledge this upfront in the report
- Provide what insights are possible based on available data
- Be transparent about limitations and caveats
- Still generate a report - don't refuse due to insufficient data

### Phase 2: Five-Dimension Analysis (15 seconds)

Analyze each workflow across five dimensions:

#### 1. Overlap Risk
- Identify workflows with similar triggers
- Detect duplicate functionality
- Find workflows that could be consolidated

#### 2. Business Value
- Check last run date (flag if >60 days)
- Review trigger patterns (flag if never triggered)
- Assess actual usage vs. configured schedule

#### 3. Cost Efficiency
- Use **ACTUAL cost data** from downloaded JSON files
- Sum `estimated_cost` from all runs in the last 30 days for real monthly cost
- **Flag workflows costing >$10/month** (based on actual spend, not estimates)
- Identify over-scheduled workflows (daily when weekly would suffice)

#### 4. Operational Health
- Calculate failure rate
- **Flag workflows with >30% failure rate**
- Identify patterns in failures

#### 5. Security Posture
- Review permissions (flag excessive permissions)
- Check network allowlists
- Assess safe-output usage

### Phase 3: Triage Categories (5 seconds)

Sort workflows into three categories:

**Healthy (Skip):**
- <30% failure rate
- Last run <60 days
- Cost <$10/month
- No obvious duplicates
- ~60-70% of workflows should be in this category

**Removal Candidates:**
- No runs in 60+ days
- Zero triggers in last 30 days
- Replaced by other workflows

**Problematic (Requires Analysis):**
- >30% failure rate
- Cost >$10/month
- Clear duplicates
- Over-scheduled (daily when weekly suffices)

### Phase 4: High-Impact Focus (20 seconds)

Focus exclusively on:

1. **Workflows costing >$10/month** - Analyze for frequency reduction
2. **Workflows with >30% failure rate** - Calculate wasted spending
3. **Clear duplicates** - Calculate consolidation savings
4. **Over-scheduled workflows** - Calculate frequency reduction savings

Skip everything else to stay within time budget.

### Phase 5: Savings Calculation (10 seconds)

Calculate specific dollar amounts using **ACTUAL cost data from downloaded files**:

#### Strategy 1: Remove Unused Workflows
```bash
# Read cost data from the JSON summary for specific workflows
cat /tmp/portfolio-logs/summary.json | jq '.runs[] | select(.workflow_name == "workflow-name") | .estimated_cost' | jq -s 'add'

For each workflow with no runs in 60+ days:
- Current monthly cost: Sum of estimated_cost from last 30 days
- Savings: $X/month (actual spend, not estimate)
- Total savings: Sum all
```

#### Strategy 2: Reduce Schedule Frequency
```bash
# Get actual runs and cost from the JSON summary
cat /tmp/portfolio-logs/summary.json | jq '[.runs[] | select(.workflow_name == "workflow-name")] | {runs: length, cost: map(.estimated_cost) | add}'

For each over-scheduled workflow:
- Current frequency: Count runs in last 30 days from summary.json
- Average cost per run: total_cost / total_runs (from actual data)
- Recommended: Weekly (4 runs/month)
- Savings: (current_runs - 4) × avg_cost_per_run = $Y/month
```

#### Strategy 3: Consolidate Duplicates
```bash
# Get cost for each duplicate workflow from the JSON summary
cat /tmp/portfolio-logs/summary.json | jq '[.runs[] | select(.workflow_name == "workflow-1")] | map(.estimated_cost) | add'
cat /tmp/portfolio-logs/summary.json | jq '[.runs[] | select(.workflow_name == "workflow-2")] | map(.estimated_cost) | add'

For each duplicate set:
- Number of duplicates: N
- Cost per workflow: $X (from summary.json actual data)
- Savings: (N-1) × $X/month
```

#### Strategy 4: Fix High-Failure Workflows
```bash
# Get failure rate and cost from the JSON summary
cat /tmp/portfolio-logs/summary.json | jq '[.runs[] | select(.workflow_name == "workflow-name" and .conclusion == "failure")] | map(.estimated_cost) | add'

For each workflow with >30% failure rate:
- Total runs: Count from summary.json
- Failed runs: Count where conclusion == "failure"
- Failure rate: (failed_runs / total_runs) × 100
- Wasted spending: Sum of estimated_cost for failed runs
- Potential savings: $Y/month (actual wasted cost on failures)
```

**Total Savings Target: Aim for ≥20% of current spending (adjust expectations for limited data)**

## Output Requirements

Generate a **concise, visual dashboard-style report** under 1500 words with embedded charts.

### Report Structure

**CRITICAL**: The report must be visual and scannable. Generate all required charts FIRST, upload them as assets, then create the discussion with embedded charts.

```markdown
# 📊 Portfolio Dashboard - [DATE]

## Quick Overview

[2-3 sentences summarizing key findings, total costs, and potential savings]

## Visual Summary

### Cost Trends (Last 30 Days)

![Cost Trends](URL_FROM_UPLOAD_ASSET_FOR_cost_trends.png)

**Key Insights**:
- Daily average: $[X]
- Trend: [Increasing/Decreasing/Stable] ([Y]% change)
- Monthly total: $[Z]

### Top Cost Drivers

![Top Spenders](URL_FROM_UPLOAD_ASSET_FOR_top_spenders.png)

Top 3 workflows account for [X]% of total cost:
1. `workflow-1.md` - $[X]/month ([status])
2. `workflow-2.md` - $[Y]/month ([status])
3. `workflow-3.md` - $[Z]/month ([status])

### Failure Analysis

![Failure Rates](URL_FROM_UPLOAD_ASSET_FOR_failure_rates.png)

**Wasted Spend**: $[X]/month on failed runs
- [N] workflows with >30% failure rate
- [M] workflows with 100% failure rate (should be disabled)

### Overall Health

![Success Overview](URL_FROM_UPLOAD_ASSET_FOR_success_overview.png)

- ✅ Success: [X]% ([N] runs)
- ❌ Failure: [Y]% ([M] runs)  
- ⏸️ Cancelled: [Z]% ([P] runs)

## 💰 Cost Reduction Opportunities

**Total Potential Savings: $[X]/month ([Y]% reduction)**

<details>
<summary><b>Strategy 1: Fix High-Failure Workflows - $[X]/month</b></summary>

List workflows with >30% failure rate, showing:
- Workflow name and file
- Failure rate percentage
- Wasted cost per month
- Recommended fix (1-2 lines)

</details>

<details>
<summary><b>Strategy 2: Reduce Over-Scheduling - $[Y]/month</b></summary>

List over-scheduled workflows with:
- Current frequency (runs/month)
- Recommended frequency
- Savings calculation

</details>

<details>
<summary><b>Strategy 3: Disable Failed Workflows - $[Z]/month</b></summary>

List workflows with 100% failure rate or no successful runs.

</details>

<details>
<summary><b>Strategy 4: Remove Unused Workflows - $[W]/month</b></summary>

List workflows with no runs in 60+ days.

</details>

## 🎯 Priority Actions

1. **CRITICAL** - [Highest impact action with specific workflow and cost]
2. **HIGH** - [Second highest impact action]
3. **MEDIUM** - [Third priority action]

## 📈 Data Quality

- **Period Analyzed**: [Actual dates covered]
- **Total Runs**: [N] workflow runs
- **Workflows**: [M] total, [X] executed, [Y] not run
- **Confidence**: [High/Medium/Low] based on [reasoning]

---

**Methodology**: Analysis based on actual workflow execution data from `gh aw logs` for the last 30 days. Costs calculated from real token usage, not estimates.
```

### Key Requirements

1. **Generate Charts First**
   - Create all 4 required charts using Python
   - Save to `/tmp/gh-aw/python/charts/`
   - Upload each using `upload asset` tool
   - Get URLs for embedding

2. **Visual Focus**
   - Charts tell the story, not long text
   - Use bullet points and short paragraphs
   - Expand details in collapsible sections
   - Keep overview section scannable

3. **Dashboard Layout**
   - Visual Summary section with all charts upfront
   - Brief insights under each chart (2-4 bullet points)
   - Detailed recommendations in collapsible details sections
   - Priority actions as numbered list

4. **Conciseness**
   - Target: 1000-1500 words total
   - Each strategy section: <200 words
   - Use tables for comparing workflows
   - Focus on actionable items only

5. **Consistency**
   - Same chart types every week
   - Same section structure
   - Same visual styling (colors, fonts)
   - Easy to compare week-over-week

## Critical Guidelines

### Handling Limited Data Scenarios

**ALWAYS generate a report**, regardless of data availability. Never refuse or fail due to insufficient data.

When data is limited (examples: only today's runs, < 10 total runs, < 7 days of history):
1. **Acknowledge limitations upfront** in the "Data Availability" section
2. **Document the actual period covered** (e.g., "Last 24 hours" vs "Last 30 days")
3. **State confidence level** (Low/Medium/High based on data volume)
4. **Provide caveats**: Explain that patterns may not be representative
5. **Make conservative recommendations**: Focus on obvious issues (100% failure rates, never-run workflows)
6. **Avoid extrapolation**: Don't project limited data to full month without caveats
7. **Still deliver value**: Even limited data can identify clear problems

Example minimal data report format:
```markdown
## Data Availability

⚠️ **Limited Data Warning**: Only 8 workflow runs available from the last 24 hours.
- **Confidence Level**: Low - Single day snapshot only
- **Recommendations**: Conservative - focusing on obvious issues only
- **Next Steps**: Re-run analysis after accumulating 7+ days of data
```

### Use Real Data, Not Guesswork
- **DO NOT call `gh aw logs` or any `gh` commands** - they will not work in your environment
- **Read from the pre-downloaded JSON file `/tmp/portfolio-logs/summary.json`** - all workflow data is in this single file
- **Use calculated costs** - the `estimated_cost` field in each run contains costs calculated from actual token usage
- **Parse JSON with jq** - extract precise metrics from the summary.json file
- **Sum actual costs** - add up `estimated_cost` for all runs in the `.runs` array
- **Calculate from actuals** - failure rates, run frequency, cost per run all from real workflow execution data in summary.json

### Speed Optimization
- **Skip healthy workflows** - Don't waste time analyzing what works
- **Focus on high-impact only** - Workflows >$10/month or >30% failure (from actual data)
- **Read from summary.json** - All data is in a single pre-downloaded JSON file at `/tmp/portfolio-logs/summary.json`
- **Use templates** - Pre-format output structure

### Precision Requirements
- **Exact filenames** - Include `.md` extension
- **Exact line numbers** - Specify which lines to modify
- **Copy-paste snippets** - Show before/after for each fix
- **Dollar amounts** - Use actual costs from downloaded logs, not estimates or ranges
- **Show calculations** - Display how you calculated savings from actual data

### Quality Standards
- **<1500 words** - Be very concise, let charts tell the story
- **Visual first** - Generate all 4 charts before writing report
- **Dashboard style** - Scannable, consistent format week-over-week
- **<1 hour per fix** - Only recommend simple changes
- **Copy-paste ready** - Every fix should be implementable via copy-paste
- **Verify math** - Ensure savings calculations are accurate

### Visualization Workflow

**CRITICAL ORDER OF OPERATIONS**:

1. **Data Preparation** (5 seconds)
   - Extract data from summary.json
   - Create CSV files in `/tmp/gh-aw/python/data/`

2. **Generate Charts** (15 seconds)
   - Create all 4 required charts using Python
   - Save to `/tmp/gh-aw/python/charts/`
   - Verify files exist before uploading

3. **Upload Assets** (10 seconds)
   - Upload each chart using `upload asset` tool
   - Save the returned URLs

4. **Create Report** (20 seconds)
   - Use the dashboard template
   - Embed charts using markdown image syntax
   - Keep text concise, let visuals speak
   - Use collapsible details sections for lengthy content

**Example Python Script Structure**:
```python
#!/usr/bin/env python3
import pandas as pd
import matplotlib.pyplot as plt
import seaborn as sns
import json

# Load data
with open('/tmp/portfolio-logs/summary.json', 'r') as f:
    data = json.load(f)

# Prepare dataframes
runs_df = pd.DataFrame(data['runs'])
runs_df['date'] = pd.to_datetime(runs_df['created_at']).dt.date

# Set style once
sns.set_style("whitegrid")
sns.set_palette("husl")

# Generate all 4 charts
# 1. Cost trends
# 2. Top spenders
# 3. Failure rates
# 4. Success overview

print("✅ All charts generated")
```

### Triage Rules
- **60-70% should be skipped** - Most workflows should be healthy (when sufficient data available)
- **Focus 80% of content on 20% of issues** - High-impact problems only
- **Clear categories** - Remove, Reduce, Consolidate, or Fix
- **Evidence-based** - Use actual run data from downloaded files, not assumptions or estimates
- **Never refuse analysis** - Generate a report even with 1 day of data; just document the limitations

## Success Criteria

✅ Analysis completes in <60 seconds
✅ **All 4 required charts generated** (cost trends, top spenders, failure rates, success overview)
✅ **Charts uploaded as assets** and embedded in report
✅ Uses **real data from the pre-downloaded summary.json file**, not estimates
✅ **Always generates a report**, even with limited data
✅ **Dashboard-style format** - visual, scannable, consistent structure
✅ Identifies cost savings opportunities based on available data (aim for ≥20% when data permits)
✅ Clearly documents data limitations and confidence level
✅ Report is <1500 words with majority of insights conveyed through charts
✅ Detailed recommendations in collapsible `<details>` sections
✅ Every recommendation includes exact line numbers
✅ Every recommendation includes before/after snippets
✅ Every fix takes <1 hour to implement
✅ Math adds up correctly (all costs from actual data in summary.json)
✅ Healthy workflows are briefly mentioned but not analyzed
✅ All dollar amounts are from actual workflow execution data

Begin your analysis now. **FIRST**: Generate all 4 required charts from `/tmp/portfolio-logs/summary.json` and upload them as assets. **THEN**: Create the dashboard-style discussion with embedded chart URLs. Read from the pre-downloaded JSON file at `/tmp/portfolio-logs/summary.json` to get real execution data for all workflows. This file contains everything you need: summary metrics and individual run data. DO NOT attempt to call `gh aw logs` or any `gh` commands - they will not work. Move fast, focus on high-impact issues, deliver actionable recommendations based on actual costs, and make the report visual and scannable.
