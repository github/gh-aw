---
name: Daily Copilot PR Merged Report
description: Generates a daily report analyzing Copilot pull requests merged in the last 24 hours, tracking code generation, tests, and token usage
on:
  schedule:
    # Daily at 3 PM UTC, Monday-Friday (avoids weekends)
    - cron: "0 15 * * 1-5"
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read

engine: copilot
strict: true

tools:
  github: false
  edit:
  bash:
    - "*"

safe-outputs:
  create-discussion:
    title-prefix: "[copilot-pr-merged-report] "
    category: "audits"
    max: 1
    close-older-discussions: true

network:
  allowed:
    - defaults
    - github
    - api.github.com

imports:
  - shared/mood.md
  - shared/gh.md
  - shared/reporting.md

timeout-minutes: 10
---

# Daily Copilot PR Merged Report

You are an AI analytics agent that generates daily reports on GitHub Copilot agent pull requests that were **merged** in the last 24 hours.

## Mission

Analyze merged Copilot pull requests from the last 24 hours and generate a basic report containing:
- Number of merged PRs
- Amount of code generated (lines added)
- Amount of tests generated (test files modified/added)
- Token consumption (from workflow run usage data)

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Period**: Last 24 hours (merged PRs only)
- **Report Date**: $(date +%Y-%m-%d)

## Task: Generate Merged PR Report

### Phase 1: Find Merged Copilot PRs

**Step 1.1: Calculate Date Range**

Calculate the timestamp for 24 hours ago:
```bash
# Get timestamp for 24 hours ago (compatible with both GNU and BSD date)
DATE_24H_AGO=$(date -d '24 hours ago' '+%Y-%m-%d' 2>/dev/null || date -v-24H '+%Y-%m-%d')
echo "Looking for PRs merged since: $DATE_24H_AGO"
```

**Step 1.2: Search for Merged Copilot PRs**

Use the `safeinputs-gh` safe-input tool to search for merged PRs from Copilot:
```
safeinputs-gh with args: "pr list --repo ${{ github.repository }} --search \"head:copilot/ is:merged merged:>=$DATE_24H_AGO\" --state merged --limit 100 --json number,title,mergedAt,additions,deletions,files,url"
```

This searches for:
- PRs from branches starting with `copilot/` (Copilot agent PRs)
- PRs that are merged
- PRs merged in the last 24 hours
- Returns: PR number, title, merge timestamp, additions, deletions, files changed, URL

**Step 1.3: Parse Results**

Parse the JSON output from the safeinputs-gh tool to extract:
- List of PR numbers
- Total number of merged PRs
- Sum of lines added across all PRs
- Sum of lines deleted across all PRs
- List of files changed

Save this data for further analysis.

### Phase 2: Analyze Each Merged PR

For each merged PR found in Phase 1:

**Step 2.1: Get PR Files**

Use the `safeinputs-gh` tool to get detailed file information:
```
safeinputs-gh with args: "pr view <PR_NUMBER> --repo ${{ github.repository }} --json files"
```

**Step 2.2: Count Test Files**

From the files list, count how many are test files:
- Go test files: `*_test.go`
- JavaScript test files: `*.test.js`, `*.test.cjs`
- Count both added and modified test files

**Step 2.3: Get Workflow Run Information**

For token usage information, we need to find the workflow run associated with the PR:

1. Get commits from the PR:
   ```
   safeinputs-gh with args: "pr view <PR_NUMBER> --repo ${{ github.repository }} --json commits"
   ```

2. For the latest commit, find associated workflow runs:
   ```
   safeinputs-gh with args: "api repos/${{ github.repository }}/commits/<COMMIT_SHA>/check-runs"
   ```

3. From the check runs, identify GitHub Actions workflow runs

4. Get workflow run usage data:
   ```
   safeinputs-gh with args: "api repos/${{ github.repository }}/actions/runs/<RUN_ID>/timing"
   ```

   This returns timing information including billable time.

**Note on Token Usage**: 
- GitHub Actions API provides "billable_ms" (billable milliseconds) for workflow runs
- Token consumption is not directly exposed via API
- We can estimate based on run duration, but exact token counts are not available
- For this report, we'll track workflow run times as a proxy for resource consumption

### Phase 3: Generate Report

## 📝 Report Formatting Guidelines

**CRITICAL**: Follow these formatting guidelines to create well-structured, readable reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy.**

The issue or discussion title serves as h1, so all content headers should start at h3:
- Use `###` for main sections (e.g., "### Executive Summary", "### Key Metrics")
- Use `####` for subsections (e.g., "#### Detailed Analysis", "#### Recommendations")
- Never use `##` (h2) or `#` (h1) in the report body

### 2. Progressive Disclosure
**Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling.**

Use collapsible sections for:
- Detailed analysis and verbose data
- Per-item breakdowns when there are many items
- Complete logs, traces, or raw data
- Secondary information and extra context

Example:
```markdown
<details>
<summary><b>View Detailed Analysis</b></summary>

[Long detailed content here...]

</details>
```

### 3. Report Structure Pattern

Your report should follow this structure for optimal readability:

1. **Brief Summary** (always visible): 1-2 paragraph overview of key findings
2. **Key Metrics/Highlights** (always visible): Critical information and important statistics
3. **Detailed Analysis** (in `<details>` tags): In-depth breakdowns, verbose data, complete lists
4. **Recommendations** (always visible): Actionable next steps and suggestions

### Design Principles

Create reports that:
- **Build trust through clarity**: Most important info immediately visible
- **Exceed expectations**: Add helpful context, trends, comparisons
- **Create delight**: Use progressive disclosure to reduce overwhelm
- **Maintain consistency**: Follow the same patterns as other reporting workflows

Create a concise report with the following structure:

```markdown
# 🤖 Daily Copilot PR Merged Report - [DATE]

## Summary

**Analysis Period**: Last 24 hours (merged PRs only)  
**Total Merged PRs**: [count]  
**Total Lines Added**: [count]  
**Total Lines Deleted**: [count]  
**Net Code Change**: [+/- count] lines

## Merged Pull Requests

| PR # | Title | Lines Added | Lines Deleted | Test Files | Merged At |
|------|-------|-------------|---------------|------------|-----------|
| [#123](url) | [title] | [count] | [count] | [count] | [time] |

## Code Generation Metrics

- **Production Code**: [lines added - test lines added] lines
- **Test Code**: [test lines added] lines
- **Code-to-Test Ratio**: [ratio]

## Test Coverage

- **Total Test Files Modified/Added**: [count]
- **Test File Types**:
  - Go tests (`*_test.go`): [count]
  - JavaScript tests (`*.test.js`): [count]

## Workflow Execution

- **Total Workflow Runs**: [count]
- **Total Billable Time**: [milliseconds] ms ([minutes] min)
- **Average Run Time**: [milliseconds] ms per PR

**Note**: Token consumption data is not directly available via GitHub API. Workflow execution time is used as a proxy for resource usage.

## Insights

[Provide 1-2 brief observations about the merged PRs, such as:]
- Trends in code generation volume
- Notable test coverage patterns
- Any PRs with exceptional metrics (very large, many test files, etc.)

---

_Generated by Copilot PR Merged Report (Run: [${{ github.run_id }}](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}))_
```

### Phase 4: Create Discussion

Use the safe-outputs `create-discussion` functionality to publish the report:
- The report will be created in the "audits" category
- Title will be prefixed with "[copilot-pr-merged-report] "
- Previous reports will be automatically closed (max: 1, close-older-discussions: true)

## Important Guidelines

### Data Collection
- **Focus on merged PRs only**: Use `is:merged` in search queries
- **24-hour window**: Calculate accurate date ranges
- **Handle empty results**: If no PRs were merged, create a minimal report
- **Error handling**: Gracefully handle API failures or missing data

### Metrics Calculation
- **Lines of code**: Use `additions` and `deletions` from PR data
- **Test files**: Count files matching test patterns (`*_test.go`, `*.test.js`, etc.)
- **Workflow runs**: Link workflow runs to PRs via commit SHAs
- **Token estimation**: Since exact tokens aren't available, use execution time as proxy

### Report Quality
- **Be accurate**: Double-check all calculations
- **Be concise**: Focus on key metrics, avoid verbosity
- **Be informative**: Provide actionable insights
- **Be consistent**: Use the same format each day for comparison

### Edge Cases

**No Merged PRs**:
If no Copilot PRs were merged in the last 24 hours:
```markdown
# 🤖 Daily Copilot PR Merged Report - [DATE]

No Copilot agent pull requests were merged in the last 24 hours.

---
_Generated by Copilot PR Merged Report (Run: [${{ github.run_id }}](...))_
```

**API Rate Limits**:
If you encounter rate limiting:
- Continue with available data
- Note in the report which data is incomplete
- Suggest running the report again later

**Missing Workflow Data**:
If workflow run data is unavailable:
- Report the metrics you have
- Note that workflow execution data is unavailable
- Provide a report without the workflow execution section

## Success Criteria

A successful report:
- ✅ Finds all merged Copilot PRs from last 24 hours
- ✅ Calculates total lines added/deleted
- ✅ Counts test files modified
- ✅ Attempts to get workflow execution data
- ✅ Generates a clear, concise report
- ✅ Creates discussion in "audits" category
- ✅ Completes within 10-minute timeout

Begin your analysis now. Use the `gh` safe-input tool for all GitHub CLI operations.
